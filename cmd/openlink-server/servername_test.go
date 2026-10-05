package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"halocommunity/internal/hostctl"
)

type nameFake struct {
	codes []uint16 // reply code per call; the last repeats
	names []string
}

func (f *nameFake) Closed() bool { return false }
func (f *nameFake) SetName(_ context.Context, name string) (hostctl.Reply, error) {
	f.names = append(f.names, name)
	return hostctl.Reply{Code: f.codes[min(len(f.names), len(f.codes))-1]}, nil
}

func TestSetGameName(t *testing.T) {
	for _, tc := range []struct {
		configured string
		codes      []uint16
		sent       []string
		log        string
	}{
		{"Friday Night Halo", []uint16{hostctl.CodeOK}, []string{"Friday Night Halo"}, "in-game server name set"},
		{"  Café ★ " + strings.Repeat("x", 50), []uint16{hostctl.CodeOK},
			[]string{"Caf  " + strings.Repeat("x", 42)}, "shortened or cleaned"},
		{"★★", []uint16{hostctl.CodeOK}, nil, "no printable ASCII"},
		{"Retry", []uint16{hostctl.CodeNativePending, hostctl.CodeNativePending, hostctl.CodeOK}, []string{"Retry", "Retry", "Retry"}, "in-game server name set"},
		{"Unsupported", []uint16{hostctl.CodeUnsupported}, []string{"Unsupported"}, "code=1"},
		{"Never", []uint16{hostctl.CodeNativePending}, strings.Fields(strings.Repeat("Never ", 10)), "did not answer in time"},
	} {
		var out bytes.Buffer
		a := &agent{cfg: config{Name: tc.configured}, log: slog.New(slog.NewTextHandler(&out, nil))}
		f := &nameFake{codes: tc.codes}
		a.setGameName(context.Background(), f, time.Millisecond)
		if strings.Join(f.names, "|") != strings.Join(tc.sent, "|") || !strings.Contains(out.String(), tc.log) {
			t.Fatalf("%q: sent %q, log %s", tc.configured, f.names, out.String())
		}
	}
}
