// hi-directory is the community server list. Host agents register and send
// heartbeats with their latest beacon; connectors list servers and fetch beacons.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"halocommunity/internal/directory"
)

func main() {
	defListen := ":8080"
	if p := os.Getenv("PORT"); p != "" { // set by Railway and similar hosts
		defListen = ":" + p
	}
	listen := flag.String("listen", defListen, "HTTP listen address (default :$PORT, else :8080)")
	key := flag.String("register-key", os.Getenv("HICOMM_REGISTER_KEY"), "if set, hosts must send this key to register (env HICOMM_REGISTER_KEY)")
	ipHeader := flag.String("client-ip-header", os.Getenv("HICOMM_CLIENT_IP_HEADER"), "header carrying the client IP from a trusted reverse proxy, e.g. X-Forwarded-For (rightmost entry is used) or X-Real-IP (env HICOMM_CLIENT_IP_HEADER)")
	trustProxy := flag.Bool("trust-proxy", false, "shorthand for -client-ip-header X-Forwarded-For")
	ttl := flag.Duration("ttl", 45*time.Second, "drop a listing after this long without a heartbeat")
	flag.Parse()
	if *trustProxy && *ipHeader == "" {
		*ipHeader = "X-Forwarded-For"
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	dir := directory.New(directory.Config{RegisterKey: *key, ClientIPHeader: *ipHeader, TTL: *ttl})
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
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shut)
	}()

	log.Info("directory listening", "addr", *listen, "register_key", *key != "", "client_ip_header", *ipHeader, "ttl", *ttl)
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
