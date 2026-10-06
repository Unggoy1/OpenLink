package directory

import (
	"context"
	"errors"
	"net"
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

// setup lists servers without waiting for the reachability probe, which most
// tests do not exercise; setupGated keeps the production behaviour.
func setup(t *testing.T, cfg Config) (*Server, *Client, *clock) {
	cfg.ShowUnconfirmed = true
	return setupGated(t, cfg)
}

func setupGated(t *testing.T, cfg Config) (*Server, *Client, *clock) {
	clk := &clock{t: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}
	cfg.Now = clk.Now
	cfg.AllowPrivateHosts = true // tests register from 127.0.0.1
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
	_, c, _ := setup(t, Config{RegisterKey: "k", RequireKey: true})
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
	r.Header.Set("Content-Type", "application/json")
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

func TestHeartbeatMatch(t *testing.T) {
	_, c, _ := setup(t, Config{TTL: 30 * time.Second})
	ctx := context.Background()
	reg, err := c.Register(ctx, api.RegisterRequest{Name: "Test Server", Port: 1343, Build: "b1"})
	if err != nil {
		t.Fatal(err)
	}
	thumb := "4eb7a3ac-81f7-4faa-acd8-ce6bbba667af/98a5391c-4a3a-4f04-bdc7-6db58cc27433"
	listed := func(m *api.Match) *api.Match {
		t.Helper()
		if err := c.Heartbeat(ctx, reg.ID, reg.Token, api.Heartbeat{Status: "ready", Players: 2, Match: m}); err != nil {
			t.Fatalf("heartbeat with %+v: %v", m, err)
		}
		list, err := c.List(ctx, "")
		if err != nil || len(list) != 1 {
			t.Fatalf("list: %v %+v", err, list)
		}
		return list[0].Match
	}

	want := api.Match{Phase: api.PhaseInGame, Entry: "kusini-ctf", Name: "CTF: Arena on Kusini Bay", Thumb: thumb}
	if got := listed(&want); got == nil || *got != want {
		t.Fatalf("valid match: got %+v", got)
	}
	// An invalid report is dropped, but the heartbeat still counts.
	for _, bad := range []api.Match{
		{Phase: "dancing"},
		{Phase: api.PhaseLobby, Name: strings.Repeat("n", 81)},
		{Phase: api.PhaseLobby, Name: "two\nlines"},
		{Phase: api.PhaseLobby, Thumb: "https://evil.example/x.jpg"},
	} {
		if got := listed(&bad); got != nil {
			t.Errorf("invalid match %+v listed as %+v", bad, got)
		}
	}
	// A heartbeat without a match (a server still starting up) clears it.
	listed(&want)
	if got := listed(nil); got != nil {
		t.Fatalf("match kept after a heartbeat without one: %+v", got)
	}
}

func TestOpenRegistrationWithTrustedKey(t *testing.T) {
	_, c, _ := setup(t, Config{RegisterKey: "k"})
	ctx := context.Background()
	c.RegisterKey = "" // setup hands the client the key; start without it
	// Open: no key needed to list your own address.
	if _, err := c.Register(ctx, api.RegisterRequest{Name: "Mine", Port: 1343, Build: "b"}); err != nil {
		t.Fatalf("open registration: %v", err)
	}
	// Another address (a tunnel, say) needs the trusted-host key.
	if _, err := c.Register(ctx, api.RegisterRequest{Name: "Tunnel", Host: "203.0.113.9", Port: 1343, Build: "b"}); code(err) != 403 {
		t.Fatalf("foreign address without the key: %v", err)
	}
	c.RegisterKey = "k"
	if reg, err := c.Register(ctx, api.RegisterRequest{Name: "Tunnel", Host: "203.0.113.9", Port: 1343, Build: "b"}); err != nil || reg.Host != "203.0.113.9" {
		t.Fatalf("foreign address with the key: %+v %v", reg, err)
	}
}

func TestDNSNameForOwnAddress(t *testing.T) {
	_, c, _ := setup(t, Config{LookupIP: func(_ context.Context, host string) ([]net.IP, error) {
		switch host {
		case "home.example.net":
			return []net.IP{net.ParseIP("127.0.0.1")}, nil // the test client's address
		case "elsewhere.example.net":
			return []net.IP{net.ParseIP("203.0.113.5")}, nil
		}
		return nil, errors.New("no such host")
	}})
	ctx := context.Background()
	reg, err := c.Register(ctx, api.RegisterRequest{Name: "DDNS", Host: "home.example.net", Port: 1343, Build: "b"})
	if err != nil {
		t.Fatalf("name resolving to the host's own address: %v", err)
	}
	// Listed as the address it resolved to: repointing the name later cannot redirect players.
	if reg.Host != "127.0.0.1" {
		t.Fatalf("listed host %q, want the resolved address", reg.Host)
	}
	for _, h := range []string{"elsewhere.example.net", "missing.example.net"} {
		if _, err := c.Register(ctx, api.RegisterRequest{Name: "X", Host: h, Port: 1343, Build: "b"}); code(err) != 403 {
			t.Fatalf("%s: %v", h, err)
		}
	}
}

func TestUnconfirmedServersHiddenThenDropped(t *testing.T) {
	s, c, clk := setupGated(t, Config{TTL: time.Hour})
	ctx := context.Background()
	up, _ := c.Register(ctx, api.RegisterRequest{Name: "Up", Port: 1343, Build: "b"})
	down, _ := c.Register(ctx, api.RegisterRequest{Name: "Down", Port: 1344, Build: "b"})
	for _, r := range []api.RegisterResponse{up, down} {
		c.Heartbeat(ctx, r.ID, r.Token, api.Heartbeat{Status: "ready", Proxy: true})
	}
	if list, _ := c.List(ctx, ""); len(list) != 0 {
		t.Fatalf("unconfirmed servers listed: %+v", list)
	}
	if me, err := c.Self(ctx, up.ID, up.Token); err != nil || me.Listed || me.Name != "Up" {
		t.Fatalf("self before the probe: %+v %v", me, err)
	}
	if _, err := c.Self(ctx, up.ID, "wrong"); code(err) != 401 {
		t.Fatalf("self with a wrong token: %v", err)
	}
	s.CheckReachability(ctx, time.Minute, func(_ context.Context, _ string, port int) bool { return port == 1343 })
	list, _ := c.List(ctx, "")
	if len(list) != 1 || list[0].Name != "Up" || list[0].Listed {
		t.Fatalf("after the probe: %+v", list)
	}
	if me, _ := c.Self(ctx, up.ID, up.Token); !me.Listed {
		t.Fatal("self after the probe: not listed")
	}
	// The never-confirmed listing is dropped after ConfirmWithin; the confirmed one stays.
	clk.Add(6 * time.Minute)
	for _, r := range []api.RegisterResponse{up, down} {
		c.Heartbeat(ctx, r.ID, r.Token, api.Heartbeat{Status: "ready", Proxy: true})
	}
	s.Expire()
	if err := c.Heartbeat(ctx, down.ID, down.Token, api.Heartbeat{Status: "ready", Proxy: true}); code(err) != 404 {
		t.Fatalf("unconfirmed listing kept: %v", err)
	}
	if list, _ := c.List(ctx, ""); len(list) != 1 {
		t.Fatalf("confirmed listing lost: %+v", list)
	}
}

func TestRegisterRateLimit(t *testing.T) {
	s, c, clk := setup(t, Config{RegisterBurst: 3, MaxPerIP: 100})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := c.Register(ctx, api.RegisterRequest{Name: "A", Port: 1343, Build: "b"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Register(ctx, api.RegisterRequest{Name: "A", Port: 1343, Build: "b"}); code(err) != 429 {
		t.Fatalf("burst: %v", err)
	}
	clk.Add(11 * time.Minute)
	s.Expire()
	if _, err := c.Register(ctx, api.RegisterRequest{Name: "A", Port: 1343, Build: "b"}); err != nil {
		t.Fatalf("after the window: %v", err)
	}
}

func TestBansAndAdmin(t *testing.T) {
	s, c, _ := setup(t, Config{AdminKey: "adm", BannedNames: []string{"BadWord"}, BannedIPs: []string{"10.9.0.0/16"}})
	ctx := context.Background()
	if _, err := c.Register(ctx, api.RegisterRequest{Name: "my badword server", Port: 1343, Build: "b"}); code(err) != 403 {
		t.Fatalf("banned name word: %v", err)
	}
	reg, err := c.Register(ctx, api.RegisterRequest{Name: "Fine", Port: 1343, Build: "b"})
	if err != nil {
		t.Fatal(err)
	}
	admin := func(method, path, key, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if key != "" {
			r.Header.Set("X-Admin-Key", key)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := admin("GET", "/v1/admin/servers", "", ""); w.Code != 401 {
		t.Fatalf("admin without key: %d", w.Code)
	}
	if w := admin("GET", "/v1/admin/servers", "adm", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"owner_ip":"127.0.0.1"`) {
		t.Fatalf("admin list: %d %s", w.Code, w.Body)
	}
	// Banning the owner's address removes its listing and blocks it.
	if w := admin("POST", "/v1/admin/bans", "adm", `{"ip":"127.0.0.1"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"removed":1`) {
		t.Fatalf("ban: %d %s", w.Code, w.Body)
	}
	if err := c.Heartbeat(ctx, reg.ID, reg.Token, api.Heartbeat{Status: "ready"}); code(err) != 404 {
		t.Fatalf("banned listing kept: %v", err)
	}
	if _, err := c.Register(ctx, api.RegisterRequest{Name: "Again", Port: 1343, Build: "b"}); code(err) != 403 {
		t.Fatalf("banned address registered: %v", err)
	}
	if w := admin("GET", "/v1/admin/bans", "adm", ""); !strings.Contains(w.Body.String(), "10.9.0.0/16") || !strings.Contains(w.Body.String(), "badword") {
		t.Fatalf("ban list: %s", w.Body)
	}
	if w := admin("DELETE", "/v1/admin/bans", "adm", `{"ip":"127.0.0.1"}`); w.Code != 200 {
		t.Fatalf("unban: %d %s", w.Code, w.Body)
	}
	again, err := c.Register(ctx, api.RegisterRequest{Name: "Again", Port: 1343, Build: "b"})
	if err != nil {
		t.Fatalf("after unban: %v", err)
	}
	if w := admin("DELETE", "/v1/admin/servers/"+again.ID, "adm", ""); w.Code != 204 {
		t.Fatalf("admin remove: %d", w.Code)
	}
	if list, _ := c.List(ctx, ""); len(list) != 0 {
		t.Fatalf("removed listing still shown: %+v", list)
	}
	if w := admin("POST", "/v1/admin/bans", "adm", `{"ip":"not an ip"}`); w.Code != 400 {
		t.Fatalf("bad ban accepted: %d", w.Code)
	}
}

func TestAdminOffWithoutKey(t *testing.T) {
	s := New(Config{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/admin/servers", nil)
	r.Header.Set("X-Admin-Key", "")
	s.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("admin API without an admin key: %d", w.Code)
	}
}

func TestDescription(t *testing.T) {
	_, c, _ := setup(t, Config{BannedNames: []string{"badword"}})
	ctx := context.Background()
	if _, err := c.Register(ctx, api.RegisterRequest{Name: "A", Port: 1343, Build: "b", Description: "line\nbreak"}); code(err) != 400 {
		t.Fatalf("control character: %v", err)
	}
	if _, err := c.Register(ctx, api.RegisterRequest{Name: "A", Port: 1343, Build: "b", Description: strings.Repeat("x", 121)}); code(err) != 400 {
		t.Fatalf("too long: %v", err)
	}
	if _, err := c.Register(ctx, api.RegisterRequest{Name: "A", Port: 1343, Build: "b", Description: "a BadWord here"}); code(err) != 403 {
		t.Fatalf("banned word in description: %v", err)
	}
	if _, err := c.Register(ctx, api.RegisterRequest{Name: "A", Port: 1343, Build: "b", Description: "  Casual BTB, be nice  "}); err != nil {
		t.Fatal(err)
	}
	if list, _ := c.List(ctx, ""); len(list) != 1 || list[0].Description != "Casual BTB, be nice" {
		t.Fatalf("listing: %+v", list)
	}
}

func TestRegistrationHardening(t *testing.T) {
	s, c, clk := setupGated(t, Config{BannedNames: []string{"badword"}})
	ctx := context.Background()
	post := func(contentType string) int {
		r := httptest.NewRequest("POST", "/v1/servers", strings.NewReader(`{"name":"x","port":1343,"build":"b"}`))
		r.RemoteAddr = "127.0.0.1:1"
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	// A cross-site form or no-cors fetch cannot register: JSON is required.
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded"} {
		if got := post(ct); got != http.StatusUnsupportedMediaType {
			t.Fatalf("content type %q: %d", ct, got)
		}
	}
	// A flood past the limit is not recorded, so the record stays small.
	for i := 0; i < 1000; i++ {
		post("application/json")
	}
	s.mu.Lock()
	n := len(s.regTimes["127.0.0.1"])
	s.mu.Unlock()
	if n > s.cfg.RegisterBurst {
		t.Fatalf("rate-limit record grew to %d entries", n)
	}
	// A match report with a banned word is dropped; the heartbeat still counts.
	clk.Add(time.Hour)
	s.Expire()
	reg, err := c.Register(ctx, api.RegisterRequest{Name: "Fine", Port: 1343, Build: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Heartbeat(ctx, reg.ID, reg.Token, api.Heartbeat{Status: "ready", Players: 0,
		Match: &api.Match{Phase: api.PhaseInGame, Name: "BadWord Slayer"}}); err != nil {
		t.Fatal(err)
	}
	if self, _ := c.Self(ctx, reg.ID, reg.Token); self.Match != nil {
		t.Fatalf("banned word in the match name was published: %+v", self.Match)
	}
}

func TestPrivateHostsAndUnconfirmedCap(t *testing.T) {
	strict := New(Config{MaxUnconfirmed: 1})
	if strict.cfg.AllowPrivateHosts {
		t.Fatal("private hosts allowed by default")
	}
	register := func(s *Server, host, from string) int {
		r := httptest.NewRequest("POST", "/v1/servers", strings.NewReader(`{"name":"x","port":1343,"build":"b"}`))
		r.RemoteAddr = from + ":1"
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	if got := register(strict, "", "192.168.1.5"); got != http.StatusForbidden {
		t.Fatalf("private registering address: %d", got)
	}
	if got := register(strict, "", "198.51.100.7"); got != http.StatusCreated {
		t.Fatalf("public address: %d", got)
	}
	// The first listing is still unconfirmed, so a second one has to wait.
	if got := register(strict, "", "198.51.100.8"); got != http.StatusServiceUnavailable {
		t.Fatalf("over the unconfirmed cap: %d", got)
	}
}

func TestLimitKey(t *testing.T) {
	for in, want := range map[string]string{
		"198.51.100.7":         "198.51.100.7",
		"2001:db8:1:2:3:4:5:6": "2001:db8:1:2::/64",
		"2001:db8:1:2:ffff::1": "2001:db8:1:2::/64",
		"2001:db8:1:3::1":      "2001:db8:1:3::/64",
	} {
		if got := limitKey(in); got != want {
			t.Errorf("limitKey(%s) = %s, want %s", in, got, want)
		}
	}
}

// The client does not follow a redirect to another host or scheme.
func TestClientRefusesCrossOriginRedirect(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("redirect followed to %s", r.URL)
		w.Write([]byte("[]"))
	}))
	defer other.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusFound)
	}))
	defer redirector.Close()
	if _, err := NewClient(redirector.URL, "").List(context.Background(), ""); err == nil {
		t.Fatal("cross-origin redirect accepted")
	}
}
