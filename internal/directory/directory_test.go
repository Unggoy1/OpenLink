package directory

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"halocommunity/internal/api"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func setup(t *testing.T, cfg Config) (*Server, *Client, *clock) {
	clk := &clock{t: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}
	cfg.Now = clk.Now
	s := New(cfg)
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	return s, NewClient(ts.URL, cfg.RegisterKey), clk
}

func code(err error) int {
	var se *StatusError
	if errors.As(err, &se) {
		return se.Code
	}
	return 0
}

func TestLifecycle(t *testing.T) {
	s, c, clk := setup(t, Config{TTL: 30 * time.Second})
	ctx := context.Background()
	reg, err := c.Register(ctx, api.RegisterRequest{Name: "Test Server", Port: 1343, Build: "b1"})
	if err != nil {
		t.Fatal(err)
	}
	if reg.Host != "127.0.0.1" {
		t.Fatalf("host defaulted to %q", reg.Host)
	}
	list, _ := c.List(ctx, "")
	if len(list) != 1 || list[0].Joinable || list[0].BeaconAgeMS != -1 {
		t.Fatalf("fresh listing should not be joinable: %+v", list)
	}
	if _, err := c.Beacon(ctx, reg.ID); code(err) != 404 {
		t.Fatalf("beacon before heartbeat: %v", err)
	}

	beacon := make([]byte, 79)
	beacon[0] = 7
	if err := c.Heartbeat(ctx, reg.ID, reg.Token, api.Heartbeat{Status: "ready", Players: -1, Beacon: beacon, BeaconAgeMS: 500}); err != nil {
		t.Fatal(err)
	}
	list, _ = c.List(ctx, "b1")
	if len(list) != 1 || !list[0].Joinable {
		t.Fatalf("should be joinable: %+v", list)
	}
	if other, _ := c.List(ctx, "other-build"); len(other) != 0 {
		t.Fatal("build filter failed")
	}
	b, err := c.Beacon(ctx, reg.ID)
	if err != nil || len(b.Beacon) != 79 || b.Beacon[0] != 7 || b.AgeMS != 500 {
		t.Fatalf("beacon %+v %v", b, err)
	}

	clk.Add(20 * time.Second) // beacon now stale, listing still alive
	list, _ = c.List(ctx, "")
	if len(list) != 1 || list[0].Joinable {
		t.Fatalf("stale beacon should not be joinable: %+v", list)
	}
	clk.Add(20 * time.Second) // past TTL
	s.Expire()
	if list, _ = c.List(ctx, ""); len(list) != 0 {
		t.Fatal("listing did not expire")
	}
}

func TestAuthAndValidation(t *testing.T) {
	_, c, _ := setup(t, Config{})
	ctx := context.Background()
	reg, err := c.Register(ctx, api.RegisterRequest{Name: "A", Port: 1343, Build: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Heartbeat(ctx, reg.ID, "wrong", api.Heartbeat{Status: "ready"}); code(err) != 401 {
		t.Fatalf("wrong token: %v", err)
	}
	if err := c.Unregister(ctx, reg.ID, "wrong"); code(err) != 401 {
		t.Fatalf("wrong token delete: %v", err)
	}
	if _, err := c.Register(ctx, api.RegisterRequest{Name: "B", Host: "203.0.113.9", Port: 1343, Build: "b"}); code(err) != 403 {
		t.Fatalf("foreign host without key: %v", err)
	}
	for _, bad := range []api.RegisterRequest{
		{Name: "", Port: 1343, Build: "b"},
		{Name: "x", Port: 0, Build: "b"},
		{Name: "x", Port: 1343, Build: ""},
		{Name: "bad\nname", Port: 1343, Build: "b"},
		{Name: "x", Host: "bad host!", Port: 1343, Build: "b"},
	} {
		if _, err := c.Register(ctx, bad); code(err) != 400 {
			t.Fatalf("%+v accepted: %v", bad, err)
		}
	}
	if err := c.Heartbeat(ctx, reg.ID, reg.Token, api.Heartbeat{Status: "ready", Beacon: make([]byte, api.MaxBeaconBytes+1)}); code(err) != 400 {
		t.Fatalf("oversized beacon: %v", err)
	}
	if err := c.Unregister(ctx, reg.ID, reg.Token); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterKeyAndForeignHost(t *testing.T) {
	_, c, _ := setup(t, Config{RegisterKey: "k"})
	ctx := context.Background()
	reg, err := c.Register(ctx, api.RegisterRequest{Name: "A", Host: "play.example.org", Port: 1343, Build: "b"})
	if err != nil || reg.Host != "play.example.org" {
		t.Fatalf("keyed foreign host: %+v %v", reg, err)
	}
	c.RegisterKey = "nope"
	if _, err := c.Register(ctx, api.RegisterRequest{Name: "A", Port: 1343, Build: "b"}); code(err) != 401 {
		t.Fatalf("bad key: %v", err)
	}
}

func TestPerIPLimit(t *testing.T) {
	_, c, _ := setup(t, Config{MaxPerIP: 2})
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := c.Register(ctx, api.RegisterRequest{Name: "A", Port: 1343, Build: "b"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Register(ctx, api.RegisterRequest{Name: "A", Port: 1343, Build: "b"}); code(err) != 429 {
		t.Fatalf("limit: %v", err)
	}
}
