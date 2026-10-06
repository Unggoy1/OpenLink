package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

// The admin API refuses requests a web page could make after rebinding its
// own name to 127.0.0.1: those carry a foreign Host and an Origin.
func TestAdminGuard(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	a := &agent{cfg: config{Admin: addr}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.serveAdmin(ctx)
	get := func(host, origin string, header bool) int {
		req, _ := http.NewRequest("GET", "http://"+addr+"/status", nil)
		req.Host = host
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if header {
			req.Header.Set(adminHeader, "1")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for deadline := time.Now().Add(2 * time.Second); get(addr, "", true) == 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("admin API did not start")
		}
	}
	for _, tc := range []struct {
		host, origin string
		header       bool
		want         int
	}{
		{addr, "", true, 200},
		{"localhost:7180", "", true, 200},
		{addr, "", false, 403},
		{"evil.example:7180", "", true, 403},
		{addr, "http://evil.example", true, 403},
	} {
		if got := get(tc.host, tc.origin, tc.header); got != tc.want {
			t.Errorf("host %q origin %q header %v: %d, want %d", tc.host, tc.origin, tc.header, got, tc.want)
		}
	}
}
