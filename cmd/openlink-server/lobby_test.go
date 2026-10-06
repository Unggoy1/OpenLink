package main

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"halocommunity/internal/hostctl"
)

// lobbyFake plays a lobby: connected peers per poll, start mode 1 after Start.
type lobbyFake struct {
	mu        sync.Mutex
	connected []int32
	polls     int
	starts    int
	mode      uint32
	leaders   []uint64
	policies  []uint32
	startMode int32
	done      chan struct{}
}

func (f *lobbyFake) Closed() bool { return false }
func (f *lobbyFake) Status(context.Context) (hostctl.Reply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.connected[min(f.polls, len(f.connected)-1)]
	f.polls++
	if f.polls == len(f.connected)+3 {
		close(f.done)
	}
	return hostctl.Reply{Version: 3, State: 8, Lobby: hostctl.Lobby{Flags: hostctl.LobbyValid | hostctl.LobbyStartMode, Connected: n, StartMode: f.startMode}}, nil
}
func (f *lobbyFake) Start(context.Context) (hostctl.Reply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	f.startMode = 1
	return hostctl.Reply{Version: 3, Code: hostctl.CodeOK}, nil
}
func (f *lobbyFake) ServerOwned(_ context.Context, mode uint32) (hostctl.Reply, error) {
	f.mode = mode
	return hostctl.Reply{Version: 3, Code: hostctl.CodeOK}, nil
}

func (f *lobbyFake) TeamPolicy(_ context.Context, flags uint32) (hostctl.Reply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.policies = append(f.policies, flags)
	return hostctl.Reply{Version: 5, Code: hostctl.CodeOK}, nil
}

func TestTeamPolicySentFromConfig(t *testing.T) {
	off := false
	for _, c := range []struct {
		balance *bool
		want    uint32
	}{{nil, hostctl.TeamGuardFFA | hostctl.TeamBalance}, {&off, hostctl.TeamGuardFFA}} {
		f := &lobbyFake{connected: []int32{0}, done: make(chan struct{})}
		a := &agent{cfg: config{TeamBalance: c.balance}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		ctx, cancel := context.WithCancel(context.Background())
		go func() { <-f.done; cancel() }()
		a.runLobby(ctx, f, time.Millisecond)
		f.mu.Lock()
		if len(f.policies) != 1 || f.policies[0] != c.want {
			t.Errorf("team_balance %v: policies %v, want [%d]", c.balance, f.policies, c.want)
		}
		f.mu.Unlock()
	}
}

// SetLeader answers Pending once (no tick yet), then OK.
func (f *lobbyFake) SetLeader(_ context.Context, xuid uint64) (hostctl.Reply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leaders = append(f.leaders, xuid)
	if len(f.leaders) == 1 {
		return hostctl.Reply{Version: 4, Code: hostctl.CodeNativePending}, nil
	}
	return hostctl.Reply{Version: 4, Code: hostctl.CodeOK}, nil
}

func TestServerOwnedHoldsLeaderOnlyWithoutPlayerOwner(t *testing.T) {
	for owner, want := range map[string][]uint64{"": {defaultLobbyLeader, defaultLobbyLeader}, "first_player": nil} {
		f := &lobbyFake{connected: []int32{0}, done: make(chan struct{})}
		a := &agent{cfg: config{ServerOwned: true, LobbyOwner: owner}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		ctx, cancel := context.WithCancel(context.Background())
		go func() { <-f.done; cancel() }()
		a.runLobby(ctx, f, time.Millisecond)
		f.mu.Lock()
		if len(f.leaders) != len(want) || (len(want) > 0 && (f.leaders[0] != want[0] || f.leaders[1] != want[1])) {
			t.Errorf("lobby_owner %q: SetLeader calls %v, want %v", owner, f.leaders, want)
		}
		f.mu.Unlock()
	}
}

func TestAutoStartWaitsForPlayersThenStartsOnce(t *testing.T) {
	f := &lobbyFake{connected: []int32{0, 0, 1, 1, 1, 1}, done: make(chan struct{})}
	a := &agent{cfg: config{ServerOwned: true, AutoStart: &autoStart{MinPlayers: 1, DelaySeconds: -1}},
		log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-f.done; cancel() }()
	a.runLobby(ctx, f, time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mode != hostctl.ServerOwnedNoOwner || f.starts != 1 || a.lobby == nil || !a.lobby.ServerOwned || a.lobby.Starts != 1 {
		t.Fatalf("mode=%v starts=%d lobby=%+v", f.mode, f.starts, a.lobby)
	}
}
