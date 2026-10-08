// Package connect is the player-side join logic shared by the command-line
// connector and the browser app: it lists servers, and for a chosen server it
// replays the server's beacon to the local game and forwards UDP game traffic.
package connect

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"halocommunity/internal/api"
	"halocommunity/internal/beacon"
	"halocommunity/internal/dirclient"
	"halocommunity/internal/game"
	"halocommunity/internal/relay"
	"halocommunity/internal/sim"
	"halocommunity/internal/udpx"
	"halocommunity/vote"
)

// Server is a directory listing.
type Server = api.ServerInfo

// Advertise modes: how the server is shown to the local game.
const (
	ModeLoopback  = "loopback"  // beacon from 127.0.0.1 (tested on Windows and Linux/Proton)
	ModeBroadcast = "broadcast" // beacon broadcast from the LAN address
)

// Limits on the player-side relay: one game client uses one session and
// well under 100 packets a second.
const (
	playerSessions = 4
	playerPPS      = 500
)

// LocalBuild returns the build of the local game install, or "" if none is found.
// installDir may be empty to search the usual Steam libraries.
func LocalBuild(installDir string) string {
	in, err := game.FindInstall(installDir)
	if err != nil {
		return ""
	}
	b, _ := in.Build()
	return b
}

// List returns the directory's servers; build filters by game build when not empty.
func List(ctx context.Context, directoryURL, build string) ([]Server, error) {
	return dirclient.NewClient(directoryURL, "").List(ctx, build)
}

// Options configure a session.
type Options struct {
	Directory string
	Mode      string // ModeLoopback (default) or ModeBroadcast
	LANIP     string // broadcast mode only; empty = default-route address
}

// Status is a snapshot of a running session.
type Status struct {
	Server      Server
	Local       string // address the game connects to
	Mode        string
	BeaconAge   time.Duration // -1 until the first beacon arrives
	Adverts     int64
	UpPackets   uint64
	UpBytes     uint64
	DownPackets uint64
	DownBytes   uint64
	LastDown    time.Time // last packet from the server
	Err         string    // last background error (directory unreachable, ...)
}

// Connected reports whether the server answered recently, i.e. the player is in a game session.
func (s Status) Connected() bool {
	return !s.LastDown.IsZero() && time.Since(s.LastDown) < 5*time.Second
}

// Session advertises one server to the local game and forwards its traffic.
type Session struct {
	server  Server
	local   string
	mode    string
	cancel  context.CancelFunc
	done    chan struct{}
	beacons beacon.Store
	fwd     *relay.Forwarder
	adverts atomic.Int64

	mu       sync.Mutex
	lastErr  string
	ballot   vote.Ballot // latest ballot from the host, if any
	ballotAt time.Time
}

// Start begins a session. Stop it with Stop.
func Start(s Server, o Options) (*Session, error) {
	upstream, err := net.ResolveUDPAddr("udp", net.JoinHostPort(s.Host, strconv.Itoa(s.Port)))
	if err != nil {
		return nil, fmt.Errorf("resolve server address: %w", err)
	}
	gamePort, discPort := api.Ports()
	mode := o.Mode
	if mode == "" {
		mode = ModeLoopback
	}
	var local net.IP
	target := &net.UDPAddr{Port: discPort}
	switch mode {
	case ModeBroadcast:
		if o.LANIP != "" {
			local = net.ParseIP(o.LANIP).To4()
		} else if local, err = udpx.PrimaryIPv4(); err != nil {
			return nil, fmt.Errorf("find LAN address: %w", err)
		}
		if local == nil {
			return nil, fmt.Errorf("bad LAN IP %q", o.LANIP)
		}
		target.IP = net.IPv4bcast
	case ModeLoopback:
		local = net.IPv4(127, 0, 0, 1)
		target.IP = local
	default:
		return nil, fmt.Errorf("unknown advertise mode %q", mode)
	}

	listen, err := net.ListenUDP("udp4", &net.UDPAddr{IP: local, Port: gamePort})
	if err != nil {
		return nil, fmt.Errorf("UDP %s:%d is in use (is a LAN server or another connector running?): %w", local, gamePort, err)
	}
	adv, err := udpx.ListenBroadcast("udp4", net.JoinHostPort(local.String(), "0"))
	if err != nil {
		listen.Close()
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	ss := &Session{server: s, local: fmt.Sprintf("%s:%d", local, gamePort), mode: mode,
		cancel: cancel, done: make(chan struct{})}
	// Only this PC's own game may use the relay. In broadcast mode it listens
	// on the LAN address, where other devices could otherwise send through
	// it (traffic the host would see as this player's).
	own := udpx.LocalIPv4s()
	ss.fwd = &relay.Forwarder{Listen: listen, Upstream: upstream, InterceptDown: ss.takeBallot,
		Allow:       func(ip net.IP) bool { return ip.IsLoopback() || own[ip.String()] },
		MaxSessions: playerSessions, MaxPPS: playerPPS}
	dc := dirclient.NewClient(o.Directory, "")

	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); ss.fwd.Run(ctx) }()
	go func() { defer wg.Done(); ss.pollBeacons(ctx, dc) }()
	go func() {
		defer wg.Done()
		defer adv.Close()
		beacon.Advertise(ctx, adv, target, &ss.beacons, api.BeaconInterval, api.BeaconFreshFor, func() { ss.adverts.Add(1) })
	}()
	go func() { wg.Wait(); close(ss.done) }()
	return ss, nil
}

// takeBallot keeps playlist-vote ballots from the host instead of passing
// them to the game. Any vote-prefixed datagram is consumed.
func (ss *Session) takeBallot(d []byte) bool {
	if !vote.Is(d) {
		return false
	}
	if b, ok := vote.DecodeBallot(d); ok {
		ss.mu.Lock()
		ss.ballot, ss.ballotAt = b, time.Now()
		ss.mu.Unlock()
	}
	return true
}

// Ballot returns the latest ballot and when it arrived; ok is false if the
// host has not sent one. The host repeats ballots about once a second.
func (ss *Session) Ballot() (b vote.Ballot, at time.Time, ok bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.ballot, ss.ballotAt, ss.ballot.Round != 0
}

// Vote sends the player's choice for a round to the host, through the same
// socket as the player's game traffic so the host can tell it is a player.
func (ss *Session) Vote(round uint64, choice int) error {
	d, err := vote.EncodeCast(vote.Cast{Round: round, Choice: choice})
	if err != nil {
		return err
	}
	if ss.fwd.SendUpstream(d) == 0 {
		return errors.New("join the server in Halo Infinite before voting")
	}
	return nil
}

// Stop ends the session and releases its ports.
func (ss *Session) Stop() {
	ss.cancel()
	<-ss.done
}

// Status returns a snapshot.
func (ss *Session) Status() Status {
	st := ss.fwd.Stats.Snapshot()
	age := time.Duration(-1)
	if _, at, n := ss.beacons.Latest(); n > 0 {
		age = time.Since(at)
	}
	ss.mu.Lock()
	errText := ss.lastErr
	ss.mu.Unlock()
	return Status{Server: ss.server, Local: ss.local, Mode: ss.mode, BeaconAge: age, Adverts: ss.adverts.Load(),
		UpPackets: st.UpPackets, UpBytes: st.UpBytes, DownPackets: st.DownPackets, DownBytes: st.DownBytes,
		LastDown: st.LastDown, Err: errText}
}

// pollBeacons fetches the server's latest beacon every 2 s (hosts send a new
// one every 2 s). The stored time is when the host captured it, so a stalled
// host goes stale here too.
func (ss *Session) pollBeacons(ctx context.Context, dc *dirclient.Client) {
	var last []byte
	for ctx.Err() == nil {
		b, err := dc.Beacon(ctx, ss.server.ID)
		switch {
		case err != nil && ctx.Err() == nil:
			msg := err.Error()
			var se *dirclient.StatusError
			if errors.As(err, &se) && se.Code == 404 {
				msg = "server is no longer listed"
			}
			ss.setErr(msg)
		case err == nil:
			ss.setErr("")
			if beacon.Valid(b.Beacon) && string(b.Beacon) != string(last) {
				ss.beacons.Put(b.Beacon, time.Now().Add(-time.Duration(b.AgeMS)*time.Millisecond))
				last = b.Beacon
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

func (ss *Session) setErr(s string) {
	ss.mu.Lock()
	ss.lastErr = s
	ss.mu.Unlock()
}

// Ping measures the round trip to a server's game endpoint with probe
// datagrams. Only proxy-mode hosts (and the simulator) answer probes; for
// others ok is false. rtt is the best of the probes that came back.
func Ping(ctx context.Context, s Server) (rtt time.Duration, ok bool) {
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(s.Host, strconv.Itoa(s.Port)))
	if err != nil {
		return 0, false
	}
	res, err := sim.Probe(ctx, addr, 3, 800*time.Millisecond)
	if err != nil || res.Received == 0 {
		return 0, false
	}
	best := res.RTTs[0]
	for _, r := range res.RTTs[1:] {
		best = min(best, r)
	}
	return best, true
}
