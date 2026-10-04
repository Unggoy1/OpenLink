package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"text/tabwriter"
	"time"

	"halocommunity/internal/relay"
)

// adminHeader must be present on every admin request. Browsers cannot add
// custom headers to cross-site requests without a CORS preflight (which this
// API never grants), so a web page cannot drive the API.
const adminHeader = "X-OpenLink-Admin"

type adminStatus struct {
	Version         string              `json:"version"`
	Status          string              `json:"status"`
	PID             int                 `json:"pid"`
	Build           string              `json:"build"`
	Proxy           bool                `json:"proxy"`
	Listen          string              `json:"listen,omitempty"`
	Players         int                 `json:"players"`
	Sessions        []relay.SessionInfo `json:"sessions,omitempty"`
	Dropped         uint64              `json:"dropped"`
	BeaconsCaptured uint64              `json:"beacons_captured"`
	LastBeaconAgeS  float64             `json:"last_beacon_age_s"` // -1 = none yet
	ListingID       string              `json:"listing_id,omitempty"`
	Reachability    string              `json:"reachability,omitempty"`
}

type banRequest struct {
	IP      string `json:"ip"`
	Minutes int    `json:"minutes"` // 0 = permanent (ban) / default 10 (kick)
}

func (a *agent) adminSnapshot() adminStatus {
	a.mu.Lock()
	st := adminStatus{Version: version, Status: a.status, PID: a.pid, Build: a.build,
		ListingID: a.listingID, Reachability: a.reachability, Players: -1}
	a.mu.Unlock()
	_, at, n := a.beacons.Latest()
	st.BeaconsCaptured, st.LastBeaconAgeS = n, -1
	if n > 0 {
		st.LastBeaconAgeS = time.Since(at).Seconds()
	}
	if a.fwd != nil {
		st.Proxy, st.Listen = true, a.cfg.Listen
		st.Players = a.fwd.Active(playerWindow)
		st.Sessions = a.fwd.Sessions()
		st.Dropped = a.fwd.Stats.Snapshot().Dropped
	}
	return st
}

// serveAdmin runs the local admin API until ctx ends.
func (a *agent) serveAdmin(ctx context.Context) {
	host, _, err := net.SplitHostPort(a.cfg.Admin)
	if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
		a.log.Error("admin API must listen on a loopback address (e.g. 127.0.0.1:7180); disabled", "admin", a.cfg.Admin)
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, a.adminSnapshot()) })
	mux.HandleFunc("GET /bans", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, a.bans.List()) })
	mux.HandleFunc("POST /kick", func(w http.ResponseWriter, r *http.Request) { a.handleBan(w, r, true) })
	mux.HandleFunc("POST /ban", func(w http.ResponseWriter, r *http.Request) { a.handleBan(w, r, false) })
	mux.HandleFunc("POST /unban", func(w http.ResponseWriter, r *http.Request) {
		var req banRequest
		ip, ok := decodeIP(w, r, &req)
		if !ok {
			return
		}
		was, err := a.bans.Unban(ip)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"unbanned": was})
	})
	guard := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() || r.Header.Get(adminHeader) != "1" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		mux.ServeHTTP(w, r)
	})
	srv := &http.Server{Addr: a.cfg.Admin, Handler: guard, ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); srv.Close() }()
	a.log.Info("admin API listening", "addr", a.cfg.Admin)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		a.log.Warn("admin API stopped", "err", err)
	}
}

func (a *agent) handleBan(w http.ResponseWriter, r *http.Request, kick bool) {
	var req banRequest
	ip, ok := decodeIP(w, r, &req)
	if !ok {
		return
	}
	d := time.Duration(req.Minutes) * time.Minute
	if kick && d == 0 {
		d = 10 * time.Minute // a kick keeps the player out briefly so the game drops them
	}
	if err := a.bans.Ban(ip, d); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	dropped := 0
	if a.fwd != nil {
		dropped = a.fwd.Drop(ip)
	}
	a.log.Info("player banned", "ip", ip, "for", map[bool]string{true: d.String(), false: "ever"}[d > 0], "sessions_closed", dropped)
	writeJSON(w, 200, map[string]any{"banned": ip.String(), "sessions_closed": dropped})
}

func decodeIP(w http.ResponseWriter, r *http.Request, req *banRequest) (net.IP, bool) {
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad json"})
		return nil, false
	}
	ip := net.ParseIP(req.IP)
	if ip == nil {
		writeJSON(w, 400, map[string]string{"error": "bad ip"})
		return nil, false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip, true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// ---- command-line client for the admin API --------------------------------

func adminCall(addr, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "http://"+addr+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set(adminHeader, "1")
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("no host agent answering on %s (is it running?): %w", addr, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("agent: %s %s", resp.Status, bytes.TrimSpace(msg))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// runAdminCommand handles: status, kick, ban, unban, bans.
func runAdminCommand(addr string, args []string) error {
	minutes := func(i int, def int) int {
		if len(args) > i {
			if m, err := strconv.Atoi(args[i]); err == nil {
				return m
			}
		}
		return def
	}
	switch args[0] {
	case "status":
		var st adminStatus
		if err := adminCall(addr, "GET", "/status", nil, &st); err != nil {
			return err
		}
		fmt.Printf("server   %s (pid %d), build %s, agent %s\n", st.Status, st.PID, st.Build, st.Version)
		beacon := "none yet"
		if st.LastBeaconAgeS >= 0 {
			beacon = fmt.Sprintf("%.0fs ago", st.LastBeaconAgeS)
		}
		fmt.Printf("beacons  %d captured, last %s\n", st.BeaconsCaptured, beacon)
		if st.ListingID != "" {
			fmt.Printf("listing  %s, reachable from the internet: %s\n", st.ListingID, orDash(st.Reachability))
		}
		if !st.Proxy {
			fmt.Println("proxy    off (player list and bans need proxy mode)")
			return nil
		}
		fmt.Printf("proxy    %s, %d player(s) connected, %d datagrams refused\n", st.Listen, st.Players, st.Dropped)
		if len(st.Sessions) > 0 {
			tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "  PLAYER\tCONNECTED\tLAST SEEN\tTO SERVER\tFROM SERVER")
			for _, s := range st.Sessions {
				fmt.Fprintf(tw, "  %s\t%s\t%s ago\t%d\t%d\n", s.Client, time.Since(s.Since).Round(time.Second),
					time.Since(s.LastSeen).Round(time.Second), s.UpPackets, s.DownPackets)
			}
			tw.Flush()
		}
		return nil
	case "kick", "ban":
		if len(args) < 2 {
			return fmt.Errorf("usage: %s <ip> [minutes]", args[0])
		}
		def := 0
		if args[0] == "kick" {
			def = 10
		}
		var out map[string]any
		if err := adminCall(addr, "POST", "/"+args[0], banRequest{IP: args[1], Minutes: minutes(2, def)}, &out); err != nil {
			return err
		}
		fmt.Printf("%s: %v (connections closed: %v)\n", args[0], out["banned"], out["sessions_closed"])
		return nil
	case "unban":
		if len(args) < 2 {
			return errors.New("usage: unban <ip>")
		}
		var out map[string]any
		if err := adminCall(addr, "POST", "/unban", banRequest{IP: args[1]}, &out); err != nil {
			return err
		}
		fmt.Printf("unban %s: was banned = %v\n", args[1], out["unbanned"])
		return nil
	case "bans":
		var bans map[string]time.Time
		if err := adminCall(addr, "GET", "/bans", nil, &bans); err != nil {
			return err
		}
		ips := make([]string, 0, len(bans))
		for ip := range bans {
			ips = append(ips, ip)
		}
		sort.Strings(ips)
		for _, ip := range ips {
			until := "permanent"
			if !bans[ip].IsZero() {
				until = "until " + bans[ip].Local().Format("2006-01-02 15:04")
			}
			fmt.Printf("%-40s %s\n", ip, until)
		}
		if len(ips) == 0 {
			fmt.Println("no bans")
		}
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
