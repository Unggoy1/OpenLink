package main

import (
	"context"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"sync"
	"testing"
	"time"

	"halocommunity/internal/hostctl"
	"halocommunity/internal/playlist"
	"halocommunity/vote"
)

// voteServer is a fake server: the test sets lobby state, players and match count.
type voteServer struct {
	mu        sync.Mutex
	state     int32
	connected int32
	matches   uint32
	selected  []string // entry map asset IDs, in order
	starts    int
}

func (s *voteServer) Closed() bool { return false }
func (s *voteServer) Status(context.Context) (hostctl.Reply, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := hostctl.Reply{Version: 3, State: s.state, Matches: s.matches}
	if s.state == 8 {
		r.Gates = hostctl.GateLobby
	}
	r.Lobby.Connected = s.connected
	return r, nil
}
func (s *voteServer) sel(p hostctl.AssetPair) (hostctl.Reply, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selected = append(s.selected, uuidText(p[:16]))
	return hostctl.Reply{Code: hostctl.CodeSelected, Generation: uint64(len(s.selected))}, nil
}
func (s *voteServer) Prepare(_ context.Context, p hostctl.AssetPair) (hostctl.Reply, error) {
	return s.sel(p)
}
func (s *voteServer) PrepareEngine(_ context.Context, p hostctl.AssetPair) (hostctl.Reply, error) {
	return s.sel(p)
}
func (s *voteServer) Initialize(_ context.Context, p hostctl.AssetPair) (hostctl.Reply, error) {
	return s.sel(p)
}
func (s *voteServer) Start(context.Context) (hostctl.Reply, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starts++
	return hostctl.Reply{Code: hostctl.CodeOK}, nil
}
func (s *voteServer) set(f func(*voteServer)) { s.mu.Lock(); f(s); s.mu.Unlock() }
func (s *voteServer) get() (selected []string, starts int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.selected...), s.starts
}

// voteNet is one or more fake players' apps behind the proxy.
type voteNet struct {
	mu      sync.Mutex
	clients []*net.UDPAddr
	ballots map[string][]vote.Ballot
}

func (n *voteNet) Clients(time.Duration) []*net.UDPAddr {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]*net.UDPAddr(nil), n.clients...)
}
func (n *voteNet) HasSession(c *net.UDPAddr, _ time.Duration) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, x := range n.clients {
		if x.String() == c.String() {
			return true
		}
	}
	return false
}
func (n *voteNet) SendTo(c *net.UDPAddr, d []byte) error {
	b, ok := vote.DecodeBallot(d)
	if !ok {
		panic("agent sent an invalid ballot")
	}
	n.mu.Lock()
	n.ballots[c.String()] = append(n.ballots[c.String()], b)
	n.mu.Unlock()
	return nil
}
func (n *voteNet) latest(c *net.UDPAddr) (vote.Ballot, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	bs := n.ballots[c.String()]
	if len(bs) == 0 {
		return vote.Ballot{}, false
	}
	return bs[len(bs)-1], true
}

func voteEntries() []playlist.Entry {
	ids := []string{"11111111-0000-0000-0000-000000000001", "22222222-0000-0000-0000-000000000002",
		"33333333-0000-0000-0000-000000000003", "44444444-0000-0000-0000-000000000004", "55555555-0000-0000-0000-000000000005"}
	var es []playlist.Entry
	for i, id := range ids {
		es = append(es, playlist.Entry{ID: "e" + string(rune('1'+i)), Name: "Entry " + string(rune('1'+i)),
			Map: playlist.Content{AssetID: id, VersionID: id}, Mode: playlist.Content{AssetID: id, VersionID: id}, ModeKind: "custom"})
	}
	return es
}

func newVoter(srv *voteServer, nw *voteNet) *voter {
	return &voter{log: slog.New(slog.NewTextHandler(io.Discard, nil)), ctl: srv, net: nw, entries: voteEntries(),
		window: 300 * time.Millisecond, delay: 100 * time.Millisecond, options: 4, poll: 10 * time.Millisecond,
		rng: rand.New(rand.NewPCG(1, 2))}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func castFrom(t *testing.T, v *voter, from *net.UDPAddr, round uint64, choice int) (vote.Ballot, bool) {
	t.Helper()
	d, err := vote.EncodeCast(vote.Cast{Round: round, Choice: choice})
	if err != nil {
		t.Fatal(err)
	}
	reply, handled := v.intercept(d, from)
	if !handled {
		t.Fatal("vote datagram not handled")
	}
	if reply == nil {
		return vote.Ballot{}, false
	}
	b, ok := vote.DecodeBallot(reply)
	return b, ok
}

func TestVoteWinnerSelectedThenStartedThenNextRound(t *testing.T) {
	srv := &voteServer{state: 8}
	alice := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 5000}
	nw := &voteNet{clients: []*net.UDPAddr{alice}, ballots: map[string][]vote.Ballot{}}
	v := newVoter(srv, nw)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go v.run(ctx)

	waitFor(t, "initial selection", func() bool { s, _ := srv.get(); return len(s) == 1 })
	time.Sleep(50 * time.Millisecond)
	if _, ok := nw.latest(alice); ok {
		t.Fatal("ballot sent with nobody in the lobby")
	}
	srv.set(func(s *voteServer) { s.connected = 1 })
	waitFor(t, "ballot", func() bool { b, ok := nw.latest(alice); return ok && !b.Closed })
	b, _ := nw.latest(alice)
	if len(b.Options) != 4 || b.Mine != -1 || b.RemainingMS <= 0 {
		t.Fatalf("first ballot %+v", b)
	}
	stranger := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 9), Port: 1}
	if _, ok := castFrom(t, v, stranger, b.Round, 0); ok {
		t.Fatal("vote from a non-player was answered")
	}
	if _, ok := castFrom(t, v, alice, b.Round+1, 0); ok {
		t.Fatal("vote for another round was answered")
	}
	ack, ok := castFrom(t, v, alice, b.Round, 2)
	if !ok || ack.Mine != 2 || ack.Counts[2] != 1 {
		t.Fatalf("ack %+v %v", ack, ok)
	}
	want := b.Options[2].ID
	var wantAsset string
	for _, e := range voteEntries() {
		if e.ID == want {
			wantAsset = e.Map.AssetID[:8]
		}
	}
	waitFor(t, "winner selected", func() bool { s, _ := srv.get(); return len(s) == 2 })
	if s, _ := srv.get(); s[1] != wantAsset {
		t.Fatalf("selected %v, want %s (%s)", s, want, wantAsset)
	}
	waitFor(t, "result ballot", func() bool { b, ok := nw.latest(alice); return ok && b.Closed })
	if r, _ := nw.latest(alice); r.Winner != 2 || r.Mine != 2 {
		t.Fatalf("result %+v", r)
	}
	waitFor(t, "start", func() bool { _, n := srv.get(); return n == 1 })

	// The match runs, ends, and the lobby returns: a new vote without the entry just played.
	srv.set(func(s *voteServer) { s.state = 9; s.matches = 1 })
	time.Sleep(50 * time.Millisecond)
	srv.set(func(s *voteServer) { s.state = 8 })
	waitFor(t, "second ballot", func() bool { b, ok := nw.latest(alice); return ok && !b.Closed && b.Round > ack.Round })
	b2, _ := nw.latest(alice)
	for _, o := range b2.Options {
		if o.ID == want {
			t.Fatalf("entry just played offered again: %+v", b2.Options)
		}
	}
}

func TestNoVotesPicksAnOption(t *testing.T) {
	srv := &voteServer{state: 8, connected: 1}
	alice := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 5000}
	nw := &voteNet{clients: []*net.UDPAddr{alice}, ballots: map[string][]vote.Ballot{}}
	v := newVoter(srv, nw)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go v.run(ctx)
	waitFor(t, "ballot", func() bool { b, ok := nw.latest(alice); return ok && !b.Closed })
	b, _ := nw.latest(alice)
	waitFor(t, "start", func() bool { _, n := srv.get(); return n == 1 })
	r, _ := nw.latest(alice)
	s, _ := srv.get()
	if !r.Closed || r.Winner < 0 || len(s) != 2 {
		t.Fatalf("result %+v selected %v", r, s)
	}
	offered := false
	for _, o := range b.Options {
		for _, e := range voteEntries() {
			if e.ID == o.ID && e.Map.AssetID[:8] == s[1] && o.ID == r.Options[r.Winner].ID {
				offered = true
			}
		}
	}
	if !offered {
		t.Fatalf("selected %s is not the announced winner of %+v", s[1], r)
	}
}

func TestEmptyLobbyCancelsVote(t *testing.T) {
	srv := &voteServer{state: 8, connected: 1}
	alice := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 5000}
	nw := &voteNet{clients: []*net.UDPAddr{alice}, ballots: map[string][]vote.Ballot{}}
	v := newVoter(srv, nw)
	v.window = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go v.run(ctx)
	waitFor(t, "ballot", func() bool { _, ok := nw.latest(alice); return ok })
	srv.set(func(s *voteServer) { s.connected = 0 })
	waitFor(t, "vote cancelled", func() bool { v.mu.Lock(); defer v.mu.Unlock(); return v.phase == phaseWaiting })
	if s, n := srv.get(); len(s) != 1 || n != 0 {
		t.Fatalf("selected %v starts %d after cancel", s, n)
	}
}

func TestPickOptionsAndRanking(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	es := voteEntries()
	for i := 0; i < 50; i++ {
		got := pickOptions(es, "e1", 4, rng)
		seen := map[string]bool{}
		for _, e := range got {
			if e.ID == "e1" || seen[e.ID] {
				t.Fatalf("pick %v", got)
			}
			seen[e.ID] = true
		}
		if len(got) != 4 {
			t.Fatalf("pick size %d", len(got))
		}
	}
	if got := pickOptions(es[:1], "e1", 4, rng); len(got) != 1 || got[0].ID != "e1" {
		t.Fatalf("single-entry playlist %v", got)
	}
	if got := pickOptions(es[:2], "e1", 4, rng); len(got) != 1 || got[0].ID != "e2" {
		t.Fatalf("two-entry playlist %v", got)
	}
	firsts := map[int]int{}
	for i := 0; i < 200; i++ {
		order := ranking([]int{1, 3, 3, 0}, rng)
		if order[2] != 0 || order[3] != 3 {
			t.Fatalf("order %v", order)
		}
		firsts[order[0]]++
	}
	if firsts[1] == 0 || firsts[2] == 0 {
		t.Fatalf("ties not broken randomly: %v", firsts)
	}
}
