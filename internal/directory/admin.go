package directory

import (
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"halocommunity/internal/api"
)

// The admin API (enabled by Config.AdminKey, sent as X-Admin-Key) lets the
// operator see who listed what, remove listings and ban addresses or name
// words. Bans added here last until the directory restarts; put lasting ones
// in Config.BannedIPs/BannedNames (OPENLINK_BANNED_IPS/_NAMES).

// AdminServer is a listing as the operator sees it.
type AdminServer struct {
	api.ServerInfo
	OwnerIP      string    `json:"owner_ip"`
	Shown        bool      `json:"shown"` // players see it (reachability confirmed)
	RegisteredAt time.Time `json:"registered_at"`
}

// Bans is the current ban list.
type Bans struct {
	IPs   []string `json:"ips"`
	Names []string `json:"names"`
}

// banRequest adds or removes one ban: an IP or CIDR range, or a name word.
type banRequest struct {
	IP   string `json:"ip,omitempty"`
	Name string `json:"name,omitempty"`
}

func (s *Server) admin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Admin-Key")), []byte(s.cfg.AdminKey)) != 1 {
			httpError(w, http.StatusUnauthorized, "admin key required")
			return
		}
		h(w, r)
	}
}

func (s *Server) adminServers(w http.ResponseWriter, _ *http.Request) {
	now := s.cfg.Now()
	s.mu.Lock()
	out := make([]AdminServer, 0, len(s.servers))
	for _, e := range s.servers {
		out = append(out, AdminServer{ServerInfo: s.view(e, now), OwnerIP: e.ownerIP, Shown: s.listed(e), RegisteredAt: e.registeredAt})
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) adminRemove(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	_, ok := s.servers[r.PathValue("id")]
	delete(s.servers, r.PathValue("id"))
	s.mu.Unlock()
	if !ok {
		httpError(w, http.StatusNotFound, "no such server")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminBans(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	b := s.bansLocked()
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, b)
}

// adminBan adds a ban and removes the listings it covers.
func (s *Server) adminBan(w http.ResponseWriter, r *http.Request) {
	req, ok := readBan(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.IP != "" && !s.ban(req.IP) {
		httpError(w, http.StatusBadRequest, "ip must be an address or CIDR range")
		return
	}
	if req.Name != "" && !s.banName(req.Name) {
		httpError(w, http.StatusBadRequest, "bad name word")
		return
	}
	removed := 0
	for id, e := range s.servers {
		if s.bannedLocked(e.ownerIP) || s.badNameLocked(e.info.Name) {
			delete(s.servers, id)
			removed++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"bans": s.bansLocked(), "removed": removed})
}

func (s *Server) adminUnban(w http.ResponseWriter, r *http.Request) {
	req, ok := readBan(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.IP != "" {
		if _, n, err := net.ParseCIDR(req.IP); err == nil {
			s.bannedNet = deleteNet(s.bannedNet, n)
		} else {
			delete(s.bannedIP, normIP(req.IP))
		}
	}
	if word := strings.ToLower(strings.TrimSpace(req.Name)); word != "" {
		for i, n := range s.bannedNm {
			if n == word {
				s.bannedNm = append(s.bannedNm[:i], s.bannedNm[i+1:]...)
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"bans": s.bansLocked()})
}

func readBan(w http.ResponseWriter, r *http.Request) (banRequest, bool) {
	var req banRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.IP == "" && req.Name == "") {
		httpError(w, http.StatusBadRequest, `send {"ip": "..."} or {"name": "..."}`)
		return req, false
	}
	return req, true
}

// ban adds an address or CIDR range; false if it is neither. s.mu held (or
// during New).
func (s *Server) ban(v string) bool {
	v = strings.TrimSpace(v)
	if _, n, err := net.ParseCIDR(v); err == nil {
		s.bannedNet = append(deleteNet(s.bannedNet, n), n)
		return true
	}
	if ip := normIP(v); ip != "" {
		s.bannedIP[ip] = true
		return true
	}
	return false
}

// banName adds a word that may not appear in server names. s.mu held (or during New).
func (s *Server) banName(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" || len(v) > 48 {
		return false
	}
	for _, n := range s.bannedNm {
		if n == v {
			return true
		}
	}
	s.bannedNm = append(s.bannedNm, v)
	return true
}

func (s *Server) bannedLocked(ip string) bool {
	if s.bannedIP[ip] {
		return true
	}
	parsed := net.ParseIP(ip)
	for _, n := range s.bannedNet {
		if parsed != nil && n.Contains(parsed) {
			return true
		}
	}
	return false
}

func (s *Server) badNameLocked(name string) bool {
	name = strings.ToLower(name)
	for _, n := range s.bannedNm {
		if strings.Contains(name, n) {
			return true
		}
	}
	return false
}

func (s *Server) bansLocked() Bans {
	b := Bans{IPs: []string{}, Names: append([]string{}, s.bannedNm...)}
	for ip := range s.bannedIP {
		b.IPs = append(b.IPs, ip)
	}
	for _, n := range s.bannedNet {
		b.IPs = append(b.IPs, n.String())
	}
	sort.Strings(b.IPs)
	sort.Strings(b.Names)
	return b
}

func deleteNet(nets []*net.IPNet, n *net.IPNet) []*net.IPNet {
	out := nets[:0]
	for _, x := range nets {
		if x.String() != n.String() {
			out = append(out, x)
		}
	}
	return out
}
