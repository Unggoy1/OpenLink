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

func (f *lobbyFake) EndMatch(context.Context) (hostctl.Reply, error) {
	return hostctl.Reply{Version: 7, Code: hostctl.CodeOK}, nil
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

func TestServerOwnsLobbyAndHoldsLeader(t *testing.T) {
	for xuid, want := range map[uint64]uint64{0: defaultLobbyLeader, 2533274962600518: 2533274962600518} {
		f := &lobbyFake{connected: []int32{0}, done: make(chan struct{})}
		a := &agent{cfg: config{LobbyLeaderXUID: xuid}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		ctx, cancel := context.WithCancel(context.Background())
		go func() { <-f.done; cancel() }()
		a.runLobby(ctx, f, time.Millisecond)
		f.mu.Lock()
		if f.mode != hostctl.ServerOwnedNoOwner || len(f.leaders) != 2 || f.leaders[0] != want || f.leaders[1] != want {
			t.Errorf("lobby_leader_xuid %d: mode %d, SetLeader calls %v, want %d twice", xuid, f.mode, f.leaders, want)
		}
		f.mu.Unlock()
	}
}

func TestAutoStartWaitsForPlayersThenStartsOnce(t *testing.T) {
	f := &lobbyFake{connected: []int32{0, 0, 1, 1, 1, 1}, done: make(chan struct{})}
	a := &agent{cfg: config{AutoStart: &autoStart{MinPlayers: 1, DelaySeconds: -1}},
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
