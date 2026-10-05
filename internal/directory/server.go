// Package directory implements the community server list: registration,
// expiring heartbeats, listing and beacon hand-off. It stores only what host
// agents send; it never sees Xbox credentials or tokens.
package directory

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"halocommunity/internal/api"
)

// Config controls limits and trust.
type Config struct {
	// RegisterKey is the trusted-host key (X-Register-Key). Registration is
	// open; the key only lets a host list an address other than its own
	// source IP, such as a tunnel. With RequireKey every host needs it.
	RegisterKey string
	RequireKey  bool
	// AdminKey, when set, enables the /v1/admin endpoints (X-Admin-Key).
	AdminKey string
	// ShowUnconfirmed lists servers before the reachability probe has
	// answered once. Off in production: a listing is shown to players only
	// once confirmed, and dropped if not confirmed within ConfirmWithin.
	ShowUnconfirmed bool
	ConfirmWithin   time.Duration // default 5 minutes
	// RegisterBurst registrations are allowed per IP per RegisterWindow
	// (defaults 12 per 10 minutes).
	RegisterBurst  int
	RegisterWindow time.Duration
	// BannedIPs (addresses or CIDR ranges) may not register; a name
	// containing one of BannedNames (any case) is refused. The admin API adds
	// more at run time; those last until the directory restarts.
	BannedIPs   []string
	BannedNames []string
	// LookupIP resolves a host name a host lists (default: the system resolver).
	LookupIP func(ctx context.Context, host string) ([]net.IP, error)
	// ClientIPHeader names the header a trusted reverse proxy uses to pass the
	// client address. Empty: use the TCP peer address. "X-Forwarded-For": use the
	// rightmost entry, the one the proxy appended; entries to its left come
	// from the client and can be forged. Any other header (e.g. X-Real-IP) is
	// used as-is, so only name one your proxy always overwrites.
	ClientIPHeader string
	// ClientIPHops is how many trusted proxies append to X-Forwarded-For
	// (default 1). The client address is the hops-th entry from the right.
	// Some platforms (apparently Railway) have an outer edge layer in front of
	// their proxy and need 2; /v1/whoami?debug=1 shows the chain. Set it to the
	// number of proxies that always append; a larger value would let clients
	// choose their own address.
	ClientIPHops int
	TTL          time.Duration // listing expiry without heartbeat
	MaxServers   int
	MaxPerIP     int
	Now          func() time.Time
}

type entry struct {
	info         api.ServerInfo
	token        string
	ownerIP      string
	beacon       []byte
	beaconAt     time.Time
	registeredAt time.Time
	confirmed    bool // the reachability probe has answered at least once
}

// Server is the HTTP handler plus its in-memory store.
type Server struct {
	cfg       Config
	mu        sync.Mutex
	servers   map[string]*entry
	regTimes  map[string][]time.Time // recent registrations per IP
	bannedIP  map[string]bool
	bannedNet []*net.IPNet
	bannedNm  []string // lower case
	mux       *http.ServeMux
}

// New builds a directory with defaults filled in.
func New(cfg Config) *Server {
	if cfg.TTL == 0 {
		cfg.TTL = 45 * time.Second
	}
	if cfg.MaxServers == 0 {
		cfg.MaxServers = 500
	}
	if cfg.MaxPerIP == 0 {
		cfg.MaxPerIP = 8
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.ConfirmWithin == 0 {
		cfg.ConfirmWithin = 5 * time.Minute
	}
	if cfg.RegisterBurst == 0 {
		cfg.RegisterBurst = 12
	}
	if cfg.RegisterWindow == 0 {
		cfg.RegisterWindow = 10 * time.Minute
	}
	if cfg.LookupIP == nil {
		cfg.LookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		}
	}
	s := &Server{cfg: cfg, servers: map[string]*entry{}, regTimes: map[string][]time.Time{},
		bannedIP: map[string]bool{}, mux: http.NewServeMux()}
	for _, b := range cfg.BannedIPs {
		s.ban(b)
	}
	for _, n := range cfg.BannedNames {
		s.banName(n)
	}
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) })
	// whoami shows the address the directory attributes to the caller, so an
	// operator can check the proxy setup and a host can learn its public IP.
	// With ?debug=1 it also echoes the caller's own forwarding headers, to work
	// out which header and hop count a hosting platform needs.
	s.mux.HandleFunc("GET /v1/whoami", func(w http.ResponseWriter, r *http.Request) {
		out := map[string]string{"ip": s.clientIP(r)}
		if r.URL.Query().Get("debug") == "1" {
			out["peer"] = r.RemoteAddr
			for _, h := range []string{"X-Forwarded-For", "X-Real-Ip", "Forwarded", "True-Client-Ip", "Cf-Connecting-Ip", "X-Envoy-External-Address"} {
				if v := strings.Join(r.Header.Values(h), ", "); v != "" {
					out[h] = v
				}
			}
		}
		writeJSON(w, http.StatusOK, out)
	})
	s.mux.HandleFunc("POST /v1/servers", s.register)
	s.mux.HandleFunc("PUT /v1/servers/{id}", s.heartbeat)
	s.mux.HandleFunc("DELETE /v1/servers/{id}", s.remove)
	s.mux.HandleFunc("GET /v1/servers", s.list)
	s.mux.HandleFunc("GET /v1/servers/{id}", s.self)
	s.mux.HandleFunc("GET /v1/servers/{id}/beacon", s.beacon)
	if cfg.AdminKey != "" {
		s.mux.HandleFunc("GET /v1/admin/servers", s.admin(s.adminServers))
		s.mux.HandleFunc("DELETE /v1/admin/servers/{id}", s.admin(s.adminRemove))
		s.mux.HandleFunc("GET /v1/admin/bans", s.admin(s.adminBans))
		s.mux.HandleFunc("POST /v1/admin/bans", s.admin(s.adminBan))
		s.mux.HandleFunc("DELETE /v1/admin/bans", s.admin(s.adminUnban))
	}
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	s.mux.ServeHTTP(w, r)
}

// Expire removes listings whose heartbeat is older than the TTL, and listings
// the reachability probe never confirmed within ConfirmWithin. Call it
// periodically.
func (s *Server) Expire() {
	now := s.cfg.Now()
	cutoff := now.Add(-s.cfg.TTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, e := range s.servers {
		unconfirmed := !s.cfg.ShowUnconfirmed && !e.confirmed && now.Sub(e.registeredAt) > s.cfg.ConfirmWithin
		if e.info.LastSeen.Before(cutoff) || unconfirmed {
			delete(s.servers, id)
		}
	}
	for ip, ts := range s.regTimes {
		if len(ts) == 0 || now.Sub(ts[len(ts)-1]) > s.cfg.RegisterWindow {
			delete(s.regTimes, ip)
		}
	}
}

var hostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	keyOK := s.cfg.RegisterKey != "" &&
		subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Register-Key")), []byte(s.cfg.RegisterKey)) == 1
	if s.cfg.RequireKey && !keyOK {
		httpError(w, http.StatusUnauthorized, "registration key required")
		return
	}
	var req api.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	ip := s.clientIP(r)
	if err := validateRegister(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	banned, badName := s.bannedLocked(ip), s.badNameLocked(req.Name)
	s.mu.Unlock()
	switch {
	case banned:
		httpError(w, http.StatusForbidden, "this address may not list servers")
		return
	case badName:
		httpError(w, http.StatusForbidden, "server name not allowed")
		return
	}
	if req.Host == "" {
		req.Host = ip
	}
	// Without the trusted-host key a host may only list its own address (or
	// a DNS name that resolves to it), so the directory cannot be used to
	// point players' traffic at a third party.
	if !keyOK && req.Host != ip && !s.resolvesTo(r.Context(), req.Host, ip) {
		httpError(w, http.StatusForbidden, "host must be the registering address")
		return
	}
	id, tok := randHex(8), randHex(24)
	now := s.cfg.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	recent := s.regTimes[ip][:0]
	for _, t := range s.regTimes[ip] {
		if now.Sub(t) < s.cfg.RegisterWindow {
			recent = append(recent, t)
		}
	}
	s.regTimes[ip] = append(recent, now)
	if len(recent) >= s.cfg.RegisterBurst {
		httpError(w, http.StatusTooManyRequests, "too many registrations from this address; try again later")
		return
	}
	if len(s.servers) >= s.cfg.MaxServers {
		httpError(w, http.StatusServiceUnavailable, "directory full")
		return
	}
	perIP := 0
	for _, e := range s.servers {
		if e.ownerIP == ip {
			perIP++
		}
	}
	if perIP >= s.cfg.MaxPerIP {
		httpError(w, http.StatusTooManyRequests, "too many listings from this address")
		return
	}
	s.servers[id] = &entry{
		info: api.ServerInfo{ID: id, Name: req.Name, Host: req.Host, Port: req.Port, Build: req.Build,
			Region: req.Region, Status: "starting", Players: -1, LastSeen: now, Reachability: api.ReachUnknown},
		token: tok, ownerIP: ip, registeredAt: now,
	}
	writeJSON(w, http.StatusCreated, api.RegisterResponse{ID: id, Token: tok, Host: req.Host})
}

// resolvesTo reports whether host is a DNS name with an address equal to ip
// (for example a dynamic-DNS name for the host's own connection).
func (s *Server) resolvesTo(ctx context.Context, host, ip string) bool {
	if net.ParseIP(host) != nil {
		return false // a different literal address
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addrs, err := s.cfg.LookupIP(ctx, host)
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if normIP(a.String()) == ip {
			return true
		}
	}
	return false
}

func validateRegister(req *api.RegisterRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	switch {
	case !api.ValidServerName(req.Name):
		return errors.New("name must be 1-48 printable characters")
	case req.Port < 1 || req.Port > 65535:
		return errors.New("bad port")
	case req.Build == "" || len(req.Build) > 64 || !printable(req.Build):
		return errors.New("bad build")
	case len(req.Region) > 32 || !printable(req.Region):
		return errors.New("bad region")
	case req.Host != "" && net.ParseIP(req.Host) == nil && !hostRe.MatchString(req.Host):
		return errors.New("bad host")
	}
	return nil
}

func printable(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func (s *Server) authorized(r *http.Request) (*entry, int) {
	e, ok := s.servers[r.PathValue("id")]
	if !ok {
		return nil, http.StatusNotFound
	}
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(tok), []byte(e.token)) != 1 {
		return nil, http.StatusUnauthorized
	}
	return e, 0
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	var hb api.Heartbeat
	if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if len(hb.Beacon) > api.MaxBeaconBytes || len(hb.Status) > 16 || !printable(hb.Status) {
		httpError(w, http.StatusBadRequest, "bad heartbeat")
		return
	}
	now := s.cfg.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, code := s.authorized(r)
	if code != 0 {
		httpError(w, code, http.StatusText(code))
		return
	}
	e.info.LastSeen = now
	e.info.Status = hb.Status
	e.info.Players = hb.Players
	// What the server is playing is optional and display-only: an invalid
	// report is dropped rather than failing the heartbeat (and the listing).
	e.info.Match = nil
	if hb.Match != nil && hb.Match.Valid() {
		m := *hb.Match
		e.info.Match = &m
	}
	if e.info.Proxy != hb.Proxy {
		e.info.Proxy = hb.Proxy
		e.info.Reachability, e.info.CheckedAt = api.ReachUnknown, time.Time{}
	}
	if len(hb.Beacon) > 0 {
		e.beacon = append(e.beacon[:0], hb.Beacon...)
		e.beaconAt = now.Add(-time.Duration(max(hb.BeaconAgeMS, 0)) * time.Millisecond)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, code := s.authorized(r); code != 0 {
		httpError(w, code, http.StatusText(code))
		return
	}
	delete(s.servers, r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) view(e *entry, now time.Time) api.ServerInfo {
	info := e.info
	info.BeaconAgeMS = -1
	if e.beacon != nil {
		age := now.Sub(e.beaconAt)
		info.BeaconAgeMS = age.Milliseconds()
		info.Joinable = age < api.BeaconFreshFor && (info.Status == "ready" || info.Status == "simulated")
	}
	return info
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	now := s.cfg.Now()
	build := r.URL.Query().Get("build")
	s.mu.Lock()
	out := make([]api.ServerInfo, 0, len(s.servers))
	for _, e := range s.servers {
		if (build == "" || e.info.Build == build) && s.listed(e) {
			out = append(out, s.view(e, now))
		}
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

// listed reports whether players see e: once the probe has confirmed it.
func (s *Server) listed(e *entry) bool { return s.cfg.ShowUnconfirmed || e.confirmed }

// self returns a host's own listing (token required), shown or not, so its
// agent can tell the host whether the directory has reached the server yet.
func (s *Server) self(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	e, code := s.authorized(r)
	var info api.ServerInfo
	if code == 0 {
		info = s.view(e, s.cfg.Now())
		info.Listed = s.listed(e)
	}
	s.mu.Unlock()
	if code != 0 {
		httpError(w, code, http.StatusText(code))
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) beacon(w http.ResponseWriter, r *http.Request) {
	now := s.cfg.Now()
	s.mu.Lock()
	e, ok := s.servers[r.PathValue("id")]
	var resp api.BeaconResponse
	if ok && e.beacon != nil {
		resp = api.BeaconResponse{Beacon: append([]byte(nil), e.beacon...), AgeMS: now.Sub(e.beaconAt).Milliseconds()}
	}
	s.mu.Unlock()
	if !ok {
		httpError(w, http.StatusNotFound, "no such server")
		return
	}
	if resp.Beacon == nil {
		httpError(w, http.StatusNotFound, "no beacon yet")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) clientIP(r *http.Request) string {
	if h := s.cfg.ClientIPHeader; h != "" {
		// Join repeated header lines; a proxy may add its own line.
		v := strings.Join(r.Header.Values(h), ",")
		if strings.EqualFold(h, "X-Forwarded-For") {
			parts := strings.Split(v, ",")
			hops := max(s.cfg.ClientIPHops, 1)
			if len(parts) < hops {
				v = "" // fewer entries than trusted proxies: not via the proxy chain
			} else {
				v = parts[len(parts)-hops]
			}
		}
		if ip := normIP(strings.TrimSpace(v)); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := normIP(host); ip != "" {
		return ip
	}
	return host
}

// normIP returns the canonical form of an IP (IPv4-mapped IPv6 becomes
// IPv4), or "" if s is not an IP.
func normIP(s string) string {
	ip := net.ParseIP(s)
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// ProbeFunc reports whether host:port answers a probe.
type ProbeFunc func(ctx context.Context, host string, port int) bool

// CheckReachability probes every proxy-mode listing not checked within
// interval and records the result. Only listed endpoints are probed, never an
// address a caller supplies directly, so the directory cannot be used to send
// traffic at arbitrary targets. Call it periodically.
func (s *Server) CheckReachability(ctx context.Context, interval time.Duration, probe ProbeFunc) {
	type target struct {
		id, host string
		port     int
	}
	now := s.cfg.Now()
	var due []target
	s.mu.Lock()
	for id, e := range s.servers {
		if e.info.Proxy && now.Sub(e.info.CheckedAt) >= interval {
			due = append(due, target{id, e.info.Host, e.info.Port})
		}
	}
	s.mu.Unlock()

	sem := make(chan struct{}, 8) // at most 8 probes in flight
	var wg sync.WaitGroup
	for _, t := range due {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			ok := probe(ctx, t.host, t.port)
			s.mu.Lock()
			if e, found := s.servers[t.id]; found && e.info.Host == t.host && e.info.Port == t.port {
				e.info.Reachability = api.ReachUnreachable
				if ok {
					e.info.Reachability = api.ReachOK
					e.confirmed = true
				}
				e.info.CheckedAt = s.cfg.Now()
			}
			s.mu.Unlock()
		}()
	}
	wg.Wait()
}
