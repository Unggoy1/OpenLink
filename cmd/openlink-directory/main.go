// openlink-directory is the community server list. Host agents register and send
// heartbeats with their latest beacon; connectors list servers and fetch beacons.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"halocommunity/internal/api"
	"halocommunity/internal/directory"
	"halocommunity/internal/sim"
)

// probeHost sends up to three probes to a listed game endpoint.
func probeHost(ctx context.Context, host string, port int) bool {
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	res, err := sim.Probe(ctx, addr, 3, 1500*time.Millisecond)
	return err == nil && res.Received > 0
}

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(api.Getenv(k)); err == nil {
		return v
	}
	return def
}

// version is set at build time (-ldflags "-X main.version=v0.1.0").
var version = "dev"

func main() {
	defListen := ":8080"
	if p := os.Getenv("PORT"); p != "" { // set by Railway and similar hosts
		defListen = ":" + p
	}
	listen := flag.String("listen", defListen, "HTTP listen address (default :$PORT, else :8080)")
	key := flag.String("register-key", api.Getenv("REGISTER_KEY"), "if set, hosts must send this key to register (env OPENLINK_REGISTER_KEY)")
	ipHeader := flag.String("client-ip-header", api.Getenv("CLIENT_IP_HEADER"), "header carrying the client IP from a trusted reverse proxy, e.g. X-Forwarded-For (rightmost entry is used) or X-Real-IP (env OPENLINK_CLIENT_IP_HEADER)")
	trustProxy := flag.Bool("trust-proxy", false, "shorthand for -client-ip-header X-Forwarded-For")
	hops := flag.Int("client-ip-hops", envInt("CLIENT_IP_HOPS", 1), "with X-Forwarded-For: number of trusted proxies that append to it; the client is that many entries from the right (env OPENLINK_CLIENT_IP_HOPS; likely 2 on Railway: check /v1/whoami?debug=1)")
	ttl := flag.Duration("ttl", 45*time.Second, "drop a listing after this long without a heartbeat")
	probeEvery := flag.Duration("probe-interval", time.Minute, "how often to check that proxy-mode servers answer on their game port (0 = never)")
	flag.Parse()
	if *trustProxy && *ipHeader == "" {
		*ipHeader = "X-Forwarded-For"
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	dir := directory.New(directory.Config{RegisterKey: *key, ClientIPHeader: *ipHeader, ClientIPHops: *hops, TTL: *ttl})
	srv := &http.Server{Addr: *listen, Handler: logRequests(log, dir),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				dir.Expire()
			}
		}
	}()
	if *probeEvery > 0 {
		go func() {
			t := time.NewTicker(15 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					dir.CheckReachability(ctx, *probeEvery, probeHost)
				}
			}
		}()
	}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shut)
	}()

	log.Info("directory listening", "version", version, "addr", *listen, "register_key", *key != "", "client_ip_header", *ipHeader, "client_ip_hops", *hops, "ttl", *ttl)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("listen failed", "err", err)
		os.Exit(1)
	}
}

// logRequests logs writes (register/heartbeat/delete) at debug-free info level,
// skipping the high-volume beacon and list reads.
func logRequests(log *slog.Logger, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodDelete {
			log.Info("request", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr)
		}
		h.ServeHTTP(w, r)
	})
}
