package main

import (
	"context"
	"io"
	"log/slog"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"halocommunity/internal/hostctl"
	"halocommunity/internal/playlist"
)

// scriptedControl plays back lifecycle states and records selections.
type scriptedControl struct {
	mu       sync.Mutex
	states   []hostctl.Reply // one per Status call; the last repeats
	calls    int
	selected []string // "init:<map asset>", "prepare:...", "engine:..."
	fail     int      // selections to fail before succeeding
	done     chan struct{}
	want     int
}

func (c *scriptedControl) Closed() bool { return false }
func (c *scriptedControl) Status(context.Context) (hostctl.Reply, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.states[min(c.calls, len(c.states)-1)]
	c.calls++
	return r, nil
}
func (c *scriptedControl) record(kind string, pair hostctl.AssetPair) (hostctl.Reply, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail > 0 {
		c.fail--
		return hostctl.Reply{Code: hostctl.CodeFailed}, nil
	}
	c.selected = append(c.selected, kind+":"+uuidText(pair[:16]))
	if len(c.selected) == c.want {
		close(c.done)
	}
	return hostctl.Reply{Code: hostctl.CodeSelected, Generation: uint64(len(c.selected))}, nil
}
func (c *scriptedControl) Prepare(_ context.Context, p hostctl.AssetPair) (hostctl.Reply, error) {
	return c.record("prepare", p)
}
func (c *scriptedControl) PrepareEngine(_ context.Context, p hostctl.AssetPair) (hostctl.Reply, error) {
	return c.record("engine", p)
}
func (c *scriptedControl) Initialize(_ context.Context, p hostctl.AssetPair) (hostctl.Reply, error) {
	return c.record("init", p)
}

func uuidText(b []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, 0, 8)
	for _, v := range b[:4] {
		out = append(out, hex[v>>4], hex[v&15])
	}
	return string(out)
}

const rotationDoc = `{"schema_version": 1, "selection": "sequential", "entries": [
 {"id": "a", "map": {"asset_id": "aaaaaaaa-0000-0000-0000-000000000001", "version_id": "aaaaaaaa-0000-0000-0000-000000000002"},
  "mode": {"asset_id": "aaaaaaaa-0000-0000-0000-000000000003", "version_id": "aaaaaaaa-0000-0000-0000-000000000004"}},
 {"id": "b", "mode_kind": "engine", "map": {"asset_id": "bbbbbbbb-0000-0000-0000-000000000001", "version_id": "bbbbbbbb-0000-0000-0000-000000000002"},
  "mode": {"asset_id": "bbbbbbbb-0000-0000-0000-000000000003", "version_id": "bbbbbbbb-0000-0000-0000-000000000004"}}]}`

func lifecycle(state int32, matches uint32) hostctl.Reply {
	r := hostctl.Reply{Code: hostctl.CodeNativePending, Version: 2, State: state, Matches: matches,
		Gates: hostctl.GateBuild | hostctl.GateRole | hostctl.GateDispatch}
	if state == 8 || state == 6 {
		r.Gates |= hostctl.GateLobby
	}
	return r
}

func runScripted(t *testing.T, c *scriptedControl) *agent {
	t.Helper()
	f, err := playlist.Parse([]byte(rotationDoc))
	if err != nil {
		t.Fatal(err)
	}
	a := &agent{cfg: config{Playlist: "test.json"}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		a.rotate(ctx, c, playlist.NewBag(f, rand.New(rand.NewPCG(1, 1))), time.Millisecond)
	}()
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		t.Error("rotation did not make the expected selections")
	}
	cancel()
	<-stopped
	return a
}

func TestRotationInitializesThenAdvancesAfterEachMatch(t *testing.T) {
	c := &scriptedControl{want: 3, done: make(chan struct{}), states: []hostctl.Reply{
		lifecycle(7, 0), // not yet a lobby: nothing selected
		lifecycle(8, 0), // lobby: initialize entry a
		lifecycle(8, 0), lifecycle(11, 0), lifecycle(9, 1), lifecycle(9, 1), lifecycle(10, 1),
		lifecycle(8, 1), // back in lobby after match 1: entry b (engine)
		lifecycle(8, 1), lifecycle(11, 1), lifecycle(9, 2), lifecycle(10, 2),
		lifecycle(8, 2), // after match 2: entry a again, as an ordinary prepare
	}}
	a := runScripted(t, c)
	want := []string{"init:aaaaaaaa", "engine:bbbbbbbb", "prepare:aaaaaaaa"}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.selected) != len(want) {
		t.Fatalf("selected %v want %v", c.selected, want)
	}
	for i := range want {
		if c.selected[i] != want[i] {
			t.Fatalf("selected %v want %v", c.selected, want)
		}
	}
	if a.rotation.Current != "a" || a.rotation.Selections != 3 || a.rotation.Matches != 2 {
		t.Fatalf("rotation info %+v", *a.rotation)
	}
}

func TestRotationRetriesThenSelects(t *testing.T) {
	c := &scriptedControl{want: 1, fail: 2, done: make(chan struct{}), states: []hostctl.Reply{lifecycle(8, 0)}}
	runScripted(t, c)
	if c.selected[0] != "init:aaaaaaaa" {
		t.Fatalf("selected %v", c.selected)
	}
}

func TestRotationNeedsLifecycleReplies(t *testing.T) {
	c := &scriptedControl{done: make(chan struct{}), states: []hostctl.Reply{{Code: hostctl.CodeNativePending, Version: 1}}}
	f, _ := playlist.Parse([]byte(rotationDoc))
	a := &agent{cfg: config{Playlist: "test.json"}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	a.rotate(context.Background(), c, playlist.NewBag(f, nil), time.Millisecond) // returns
	if a.rotation.Error == "" || len(c.selected) != 0 {
		t.Fatalf("v1 replies should stop rotation: %+v %v", *a.rotation, c.selected)
	}
}
