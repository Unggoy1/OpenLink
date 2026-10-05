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
	owned     bool
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
func (f *lobbyFake) ServerOwned(_ context.Context, enable bool) (hostctl.Reply, error) {
	f.owned = enable
	return hostctl.Reply{Version: 3, Code: hostctl.CodeOK}, nil
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
	if !f.owned || f.starts != 1 || a.lobby == nil || !a.lobby.ServerOwned || a.lobby.Starts != 1 {
		t.Fatalf("owned=%v starts=%d lobby=%+v", f.owned, f.starts, a.lobby)
	}
}
