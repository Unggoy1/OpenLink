// Package relay forwards UDP game traffic between local clients and an
// upstream server, one upstream socket per client endpoint. It is used on the
// player side (game → remote server) and on the host side (players → local
// server). Game datagrams are never inspected or changed; the optional
// Intercept hooks only see whole datagrams and can answer or consume ones they
// recognise (reachability probes, playlist votes).
package relay

import (
	"context"
	"errors"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Stats are cumulative counters; read them with Snapshot.
type Stats struct {
	UpPackets, UpBytes, DownPackets, DownBytes atomic.Uint64
	LastUp, LastDown                           atomic.Int64 // unix nanoseconds
	Sessions                                   atomic.Int32
	Dropped                                    atomic.Uint64 // refused: banned, rate-limited or over the session limit
}

// Snapshot is a plain copy of Stats.
type Snapshot struct {
	UpPackets, UpBytes, DownPackets, DownBytes uint64
	LastUp, LastDown                           time.Time
	Sessions                                   int32
	Dropped                                    uint64
}

// Snapshot copies the counters.
func (s *Stats) Snapshot() Snapshot {
	return Snapshot{s.UpPackets.Load(), s.UpBytes.Load(), s.DownPackets.Load(), s.DownBytes.Load(),
		unixTime(s.LastUp.Load()), unixTime(s.LastDown.Load()), s.Sessions.Load(), s.Dropped.Load()}
}

func unixTime(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(0, v)
}

// Forwarder relays between Listen (where clients send) and Upstream (the server).
type Forwarder struct {
	Listen   *net.UDPConn
	Upstream *net.UDPAddr
	Idle     time.Duration // drop a client session after this long without traffic (default 2 min)

	// Optional controls, used on the host side.
	Allow     func(ip net.IP) bool                        // false drops the datagram (bans)
	Intercept func(b []byte) (reply []byte, handled bool) // answer a datagram instead of forwarding it
	// InterceptFrom is like Intercept but also gets the sender, for messages
	// that belong to a client's session (votes). It runs after Intercept.
	InterceptFrom func(b []byte, client *net.UDPAddr) (reply []byte, handled bool)
	// InterceptDown sees each server datagram before it is forwarded to the
	// client; true consumes it (used on the player side for ballots).
	InterceptDown func(b []byte) bool
	MaxSessions   int // 0 = unlimited
	MaxPPS        int // per-client packets per second; 0 = unlimited

	Stats Stats

	mu       sync.Mutex
	sessions map[string]*session
}

type session struct {
	up     *net.UDPConn
	client *net.UDPAddr
	since  time.Time
	last   atomic.Int64
	upPkts atomic.Uint64
	dnPkts atomic.Uint64

	// token bucket for MaxPPS; only touched by the Run goroutine
	tokens float64
	refill time.Time
}

// SessionInfo describes one client endpoint.
type SessionInfo struct {
	Client      string    `json:"client"`
	IP          string    `json:"ip"`
	Since       time.Time `json:"since"`
	LastSeen    time.Time `json:"last_seen"`
	UpPackets   uint64    `json:"up_packets"`
	DownPackets uint64    `json:"down_packets"`
}

// Run forwards until ctx ends.
func (f *Forwarder) Run(ctx context.Context) error {
	if f.Idle == 0 {
		f.Idle = 2 * time.Minute
	}
	f.mu.Lock()
	f.sessions = map[string]*session{}
	f.mu.Unlock()
	go func() { <-ctx.Done(); f.Listen.Close() }()
	go f.reap(ctx)
	defer f.closeAll()

	buf := make([]byte, 2048)
	for {
		n, client, err := f.Listen.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			continue
		}
		if f.Allow != nil && !f.Allow(client.IP) {
			f.Stats.Dropped.Add(1)
			continue
		}
		if f.Intercept != nil {
			if reply, ok := f.Intercept(buf[:n]); ok {
				if reply != nil {
					f.Listen.WriteToUDP(reply, client)
				}
				continue
			}
		}
		if f.InterceptFrom != nil {
			if reply, ok := f.InterceptFrom(buf[:n], client); ok {
				if reply != nil {
					f.Listen.WriteToUDP(reply, client)
				}
				continue
			}
		}
		s, err := f.session(ctx, client)
		if err != nil {
			f.Stats.Dropped.Add(1)
			continue
		}
		if !s.take(f.MaxPPS) {
			f.Stats.Dropped.Add(1)
			continue
		}
		if _, err := s.up.WriteToUDP(buf[:n], f.Upstream); err == nil {
			now := time.Now().UnixNano()
			s.last.Store(now)
			s.upPkts.Add(1)
			f.Stats.UpPackets.Add(1)
			f.Stats.UpBytes.Add(uint64(n))
			f.Stats.LastUp.Store(now)
		}
	}
}

// take spends one token from the client's bucket (capacity = one second of MaxPPS).
func (s *session) take(maxPPS int) bool {
	if maxPPS <= 0 {
		return true
	}
	now := time.Now()
	if s.refill.IsZero() {
		s.tokens, s.refill = float64(maxPPS), now
	}
	s.tokens += now.Sub(s.refill).Seconds() * float64(maxPPS)
	if s.tokens > float64(maxPPS) {
		s.tokens = float64(maxPPS)
	}
	s.refill = now
	if s.tokens < 1 {
		return false
	}
	s.tokens--
	return true
}

var errTooManySessions = errors.New("session limit reached")

func (f *Forwarder) session(ctx context.Context, client *net.UDPAddr) (*session, error) {
	key := client.String()
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.sessions[key]; ok {
		return s, nil
	}
	if f.MaxSessions > 0 && len(f.sessions) >= f.MaxSessions {
		return nil, errTooManySessions
	}
	network := "udp4"
	if f.Upstream.IP.To4() == nil {
		network = "udp6"
	}
	up, err := net.ListenUDP(network, nil)
	if err != nil {
		return nil, err
	}
	s := &session{up: up, client: client, since: time.Now()}
	s.last.Store(time.Now().UnixNano())
	f.sessions[key] = s
	f.Stats.Sessions.Add(1)
	go f.downstream(ctx, s)
	return s, nil
}

// downstream copies server replies back to the client.
func (f *Forwarder) downstream(ctx context.Context, s *session) {
	buf := make([]byte, 2048)
	for {
		n, from, err := s.up.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		if !from.IP.Equal(f.Upstream.IP) || from.Port != f.Upstream.Port {
			continue // only the server may answer through this session
		}
		if f.InterceptDown != nil && f.InterceptDown(buf[:n]) {
			continue
		}
		if _, err := f.Listen.WriteToUDP(buf[:n], s.client); err == nil {
			now := time.Now().UnixNano()
			s.last.Store(now)
			s.dnPkts.Add(1)
			f.Stats.DownPackets.Add(1)
			f.Stats.DownBytes.Add(uint64(n))
			f.Stats.LastDown.Store(now)
		}
	}
}

// Sessions lists client endpoints, most recently active first.
func (f *Forwarder) Sessions() []SessionInfo {
	f.mu.Lock()
	out := make([]SessionInfo, 0, len(f.sessions))
	for _, s := range f.sessions {
		out = append(out, SessionInfo{Client: s.client.String(), IP: s.client.IP.String(), Since: s.since,
			LastSeen: unixTime(s.last.Load()), UpPackets: s.upPkts.Load(), DownPackets: s.dnPkts.Load()})
	}
	f.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out
}

// Active counts client endpoints that sent traffic within window: the number
// of players connected through this forwarder.
func (f *Forwarder) Active(window time.Duration) int {
	cutoff := time.Now().Add(-window).UnixNano()
	n := 0
	f.mu.Lock()
	for _, s := range f.sessions {
		if s.last.Load() >= cutoff {
			n++
		}
	}
	f.mu.Unlock()
	return n
}

// Clients lists the client endpoints that sent traffic within window.
func (f *Forwarder) Clients(window time.Duration) []*net.UDPAddr {
	cutoff := time.Now().Add(-window).UnixNano()
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*net.UDPAddr, 0, len(f.sessions))
	for _, s := range f.sessions {
		if s.last.Load() >= cutoff {
			out = append(out, s.client)
		}
	}
	return out
}

// HasSession reports whether client has a session that sent traffic within window.
func (f *Forwarder) HasSession(client *net.UDPAddr, window time.Duration) bool {
	f.mu.Lock()
	s, ok := f.sessions[client.String()]
	f.mu.Unlock()
	return ok && s.last.Load() >= time.Now().Add(-window).UnixNano()
}

// SendTo sends b to a client from the listening socket, as the server's replies appear.
func (f *Forwarder) SendTo(client *net.UDPAddr, b []byte) error {
	_, err := f.Listen.WriteToUDP(b, client)
	return err
}

// SendUpstream sends b to the server through every client session's socket,
// so the server sees it from the same address as that client's traffic.
// It returns the number of sessions it was sent through.
func (f *Forwarder) SendUpstream(b []byte) int {
	f.mu.Lock()
	ups := make([]*net.UDPConn, 0, len(f.sessions))
	for _, s := range f.sessions {
		ups = append(ups, s.up)
	}
	f.mu.Unlock()
	n := 0
	for _, up := range ups {
		if _, err := up.WriteToUDP(b, f.Upstream); err == nil {
			n++
		}
	}
	return n
}

// Drop closes every session from ip (used with a ban to kick a player).
func (f *Forwarder) Drop(ip net.IP) int {
	n := 0
	f.mu.Lock()
	for k, s := range f.sessions {
		if s.client.IP.Equal(ip) {
			s.up.Close()
			delete(f.sessions, k)
			f.Stats.Sessions.Add(-1)
			n++
		}
	}
	f.mu.Unlock()
	return n
}

func (f *Forwarder) reap(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		cutoff := time.Now().Add(-f.Idle).UnixNano()
		f.mu.Lock()
		for k, s := range f.sessions {
			if s.last.Load() < cutoff {
				s.up.Close()
				delete(f.sessions, k)
				f.Stats.Sessions.Add(-1)
			}
		}
		f.mu.Unlock()
	}
}

func (f *Forwarder) closeAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, s := range f.sessions {
		s.up.Close()
		delete(f.sessions, k)
	}
	f.Stats.Sessions.Store(0)
}
