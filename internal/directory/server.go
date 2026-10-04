// Package directory implements the community server list: registration,
// expiring heartbeats, listing and beacon hand-off. It stores only what host
// agents send; it never sees Xbox credentials or tokens.
package directory

import (
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
	"unicode/utf8"

	"halocommunity/internal/api"
)

// Config controls limits and trust.
type Config struct {
	// RegisterKey, when set, must be sent as X-Register-Key to register. It also
	// allows a host to list an address other than its own source IP.
	RegisterKey string
	// ClientIPHeader names the header a trusted reverse proxy uses to pass the
	// client address. Empty: use the TCP peer address. "X-Forwarded-For": use the
	// rightmost entry, the one the proxy appended; entries to its left come
	// from the client and can be forged. Any other header (e.g. X-Real-IP) is
	// used as-is, so only name one your proxy always overwrites.
	ClientIPHeader string
	TTL            time.Duration // listing expiry without heartbeat
	MaxServers     int
	MaxPerIP       int
	Now            func() time.Time
}

type entry struct {
	info     api.ServerInfo
	token    string
	ownerIP  string
	beacon   []byte
	beaconAt time.Time
}

// Server is the HTTP handler plus its in-memory store.
type Server struct {
	cfg     Config
	mu      sync.Mutex
	servers map[string]*entry
	mux     *http.ServeMux
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
	s := &Server{cfg: cfg, servers: map[string]*entry{}, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) })
	// whoami shows the address the directory attributes to the caller, so an
	// operator can check the proxy setup and a host can learn its public IP.
	s.mux.HandleFunc("GET /v1/whoami", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"ip": s.clientIP(r)})
	})
	s.mux.HandleFunc("POST /v1/servers", s.register)
	s.mux.HandleFunc("PUT /v1/servers/{id}", s.heartbeat)
	s.mux.HandleFunc("DELETE /v1/servers/{id}", s.remove)
	s.mux.HandleFunc("GET /v1/servers", s.list)
	s.mux.HandleFunc("GET /v1/servers/{id}/beacon", s.beacon)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	s.mux.ServeHTTP(w, r)
}

// Expire removes listings whose heartbeat is older than the TTL. Call it periodically.
func (s *Server) Expire() {
	cutoff := s.cfg.Now().Add(-s.cfg.TTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, e := range s.servers {
		if e.info.LastSeen.Before(cutoff) {
			delete(s.servers, id)
		}
	}
}

var hostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	keyOK := s.cfg.RegisterKey != "" &&
		subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Register-Key")), []byte(s.cfg.RegisterKey)) == 1
	if s.cfg.RegisterKey != "" && !keyOK {
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
	if req.Host == "" {
		req.Host = ip
	}
	// Without the operator key a host may only list its own address, so the
	// directory cannot be used to point players' traffic at a third party.
	if !keyOK && req.Host != ip {
		httpError(w, http.StatusForbidden, "host must be the registering address")
		return
	}
	id, tok := randHex(8), randHex(24)
	now := s.cfg.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
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
			Region: req.Region, Status: "starting", Players: -1, LastSeen: now},
		token: tok, ownerIP: ip,
	}
	writeJSON(w, http.StatusCreated, api.RegisterResponse{ID: id, Token: tok, Host: req.Host})
}

func validateRegister(req *api.RegisterRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	switch {
	case req.Name == "" || utf8.RuneCountInString(req.Name) > 48 || !printable(req.Name):
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
		if build == "" || e.info.Build == build {
			out = append(out, s.view(e, now))
		}
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
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
			v = parts[len(parts)-1]
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
