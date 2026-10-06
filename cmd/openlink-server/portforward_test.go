package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"halocommunity/internal/portmap"
)

type fakeMapping struct {
	mu              sync.Mutex
	renews, removes int
	ext             string
}

func (m *fakeMapping) Method() string       { return "UPnP" }
func (m *fakeMapping) ExternalIP() string   { return m.ext }
func (m *fakeMapping) Lease() time.Duration { return 20 * time.Millisecond }
func (m *fakeMapping) Renew(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.renews++
	return nil
}
func (m *fakeMapping) Remove(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removes++
	return nil
}

func TestRunPortForward(t *testing.T) {
	old := portMapper
	defer func() { portMapper = old }()
	for _, tc := range []struct {
		ext, log string
		fail     bool
	}{
		{ext: "203.0.113.5", log: "router forwards the port"},
		{ext: "100.70.1.1", log: "carrier-grade NAT"},
		{fail: true, log: "forward the port in your router by hand"},
	} {
		m := &fakeMapping{ext: tc.ext}
		var got portmap.Request
		portMapper = func(_ context.Context, r portmap.Request) (portmap.Mapping, error) {
			got = r
			if tc.fail {
				return nil, errors.New("no UPnP gateway answered")
			}
			return m, nil
		}
		var out bytes.Buffer
		a := &agent{cfg: config{Listen: "192.168.1.20:1343", PublicPort: 1344}, log: slog.New(slog.NewTextHandler(&out, nil))}
		ctx, cancel := context.WithTimeout(context.Background(), 70*time.Millisecond)
		a.runPortForward(ctx)
		cancel()
		if got.ExternalPort != 1344 || got.InternalPort != 1343 || got.InternalIP.String() != "192.168.1.20" {
			t.Fatalf("request %+v", got)
		}
		if !strings.Contains(out.String(), tc.log) || !strings.Contains(out.String(), "Anyone on the internet") {
			t.Fatalf("log: %s", out.String())
		}
		m.mu.Lock()
		if !tc.fail && (m.renews < 2 || m.removes != 1) {
			t.Fatalf("renews %d removes %d", m.renews, m.removes)
		}
		m.mu.Unlock()
		if tc.fail != strings.HasPrefix(a.portForward, "failed") {
			t.Fatalf("state %q", a.portForward)
		}
	}
}
