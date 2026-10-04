package directory

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestClientIPHeader(t *testing.T) {
	req := func(remote string, hdr map[string][]string) *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = remote
		for k, vs := range hdr {
			for _, v := range vs {
				r.Header.Add(k, v)
			}
		}
		return r
	}
	cases := []struct {
		name, header, remote string
		hdr                  map[string][]string
		want                 string
	}{
		{"no proxy ignores headers", "", "198.51.100.7:5000",
			map[string][]string{"X-Forwarded-For": {"203.0.113.1"}}, "198.51.100.7"},
		{"xff uses proxy-appended entry, not forged first entry", "X-Forwarded-For", "10.0.0.2:1",
			map[string][]string{"X-Forwarded-For": {"203.0.113.1, 198.51.100.7"}}, "198.51.100.7"},
		{"xff across repeated header lines", "X-Forwarded-For", "10.0.0.2:1",
			map[string][]string{"X-Forwarded-For": {"203.0.113.1", "198.51.100.7"}}, "198.51.100.7"},
		{"single-value header", "X-Real-IP", "10.0.0.2:1",
			map[string][]string{"X-Real-Ip": {"198.51.100.7"}}, "198.51.100.7"},
		{"garbage header falls back to peer", "X-Real-IP", "10.0.0.2:1",
			map[string][]string{"X-Real-Ip": {"not-an-ip"}}, "10.0.0.2"},
		{"ipv4-mapped peer normalised", "", "[::ffff:198.51.100.7]:1", nil, "198.51.100.7"},
	}
	for _, c := range cases {
		s := New(Config{ClientIPHeader: c.header})
		if got := s.clientIP(req(c.remote, c.hdr)); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestForgedForwardedForCannotListThirdParty(t *testing.T) {
	s := New(Config{ClientIPHeader: "X-Forwarded-For"})
	body := `{"name":"x","port":1343,"build":"b","host":"203.0.113.1"}`
	r := httptest.NewRequest("POST", "/v1/servers", strings.NewReader(body))
	r.RemoteAddr = "10.0.0.2:1"
	// The client forges the first entry; the proxy appends the real address.
	r.Header.Set("X-Forwarded-For", "203.0.113.1, 198.51.100.7")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("forged XFF registration: got %d %s", w.Code, w.Body.String())
	}
}

func TestClientIPHops(t *testing.T) {
	s := New(Config{ClientIPHeader: "X-Forwarded-For", ClientIPHops: 2})
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "100.64.0.14:1"
	// client forged "203.0.113.1"; edge appended the client, then the outer layer appended itself
	r.Header.Set("X-Forwarded-For", "203.0.113.1, 198.51.100.7, 79.127.217.66")
	if got := s.clientIP(r); got != "198.51.100.7" {
		t.Fatalf("hops=2 got %s", got)
	}
	r.Header.Set("X-Forwarded-For", "79.127.217.66") // fewer entries than hops: ignore header
	if got := s.clientIP(r); got != "100.64.0.14" {
		t.Fatalf("short chain got %s", got)
	}
}

func TestReachability(t *testing.T) {
	s, c, clk := setup(t, Config{RegisterKey: "k"})
	ctx := context.Background()
	up, _ := c.Register(ctx, api.RegisterRequest{Name: "Up", Host: "192.0.2.1", Port: 1343, Build: "b"})
	down, _ := c.Register(ctx, api.RegisterRequest{Name: "Down", Host: "192.0.2.2", Port: 1343, Build: "b"})
	plain, _ := c.Register(ctx, api.RegisterRequest{Name: "Plain", Host: "192.0.2.3", Port: 1343, Build: "b"})
	for _, r := range []api.RegisterResponse{up, down} {
		c.Heartbeat(ctx, r.ID, r.Token, api.Heartbeat{Status: "ready", Proxy: true})
	}
	c.Heartbeat(ctx, plain.ID, plain.Token, api.Heartbeat{Status: "ready"})

	var pmu sync.Mutex
	var probed []string
	probe := func(_ context.Context, host string, _ int) bool {
		pmu.Lock()
		probed = append(probed, host)
		pmu.Unlock()
		return host == "192.0.2.1"
	}
	s.CheckReachability(ctx, time.Minute, func(ctx context.Context, h string, p int) bool { return probe(ctx, h, p) })
	got := map[string]string{}
	list, _ := c.List(ctx, "")
	for _, sv := range list {
		got[sv.Name] = sv.Reachability
	}
	if got["Up"] != api.ReachOK || got["Down"] != api.ReachUnreachable || got["Plain"] != api.ReachUnknown {
		t.Fatalf("reachability %v", got)
	}
	if len(probed) != 2 {
		t.Fatalf("probed %v (non-proxy hosts must not be probed)", probed)
	}
	probed = nil
	s.CheckReachability(ctx, time.Minute, probe) // within interval: nothing due
	clk.Add(2 * time.Minute)
	s.CheckReachability(ctx, time.Minute, probe)
	if len(probed) != 2 {
		t.Fatalf("recheck probed %v", probed)
	}
	// Leaving proxy mode resets the result.
	c.Heartbeat(ctx, up.ID, up.Token, api.Heartbeat{Status: "ready", Proxy: false})
	list, _ = c.List(ctx, "")
	for _, sv := range list {
		if sv.Name == "Up" && sv.Reachability != api.ReachUnknown {
			t.Fatalf("after leaving proxy mode: %s", sv.Reachability)
		}
	}
}
