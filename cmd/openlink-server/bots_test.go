package main

import (
	"bytes"
	"context"
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"halocommunity/internal/hostctl"
	"halocommunity/internal/playlist"
)

func TestBotPolicyFromConfigAndEntry(t *testing.T) {
	on := func(fill, max, difficulty uint32) hostctl.BotPolicy {
		return hostctl.BotPolicy{Flags: hostctl.BotBackfill, FillTo: fill, MaxBots: max, Difficulty: difficulty}
	}
	def := hostctl.BotPolicy{}
	if entryBotsDefault {
		def = on(8, 8, hostctl.BotMarine)
	}
	server := &botBackfill{}
	tuned := &botBackfill{FillTo: 12, MaxBots: 4, Difficulty: "odst"}
	for _, c := range []struct {
		name   string
		server *botBackfill
		entry  *playlist.Entry
		want   hostctl.BotPolicy
	}{
		{"server off", nil, &playlist.Entry{Bots: &playlist.Bots{Enabled: true}}, hostctl.BotPolicy{}},
		{"entry without bots", server, &playlist.Entry{}, def},
		{"manual selection", server, nil, def},
		{"entry on", server, &playlist.Entry{Bots: &playlist.Bots{Enabled: true}}, on(8, 8, hostctl.BotMarine)},
		{"entry off", server, &playlist.Entry{Bots: &playlist.Bots{}}, hostctl.BotPolicy{}},
		{"server settings", tuned, &playlist.Entry{Bots: &playlist.Bots{Enabled: true}}, on(12, 4, hostctl.BotODST)},
		{"entry overrides", tuned, &playlist.Entry{Bots: &playlist.Bots{Enabled: true, FillTo: 16, Difficulty: "spartan"}}, on(16, 4, hostctl.BotSpartan)},
	} {
		if got := (config{BotBackfill: c.server}).botPolicy(c.entry); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestConfigBotBackfillValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openlink-server.json")
	load := func(v string) (config, error) {
		os.WriteFile(path, []byte(`{"name": "x", "bot_backfill": `+v+`}`), 0o600)
		return loadConfig(flag.NewFlagSet("t", flag.ContinueOnError), []string{"-config", path})
	}
	for _, v := range []string{`{}`, `{"fill_to": 2}`, `{"fill_to": 24, "max_bots": 1, "difficulty": "recruit"}`} {
		if c, err := load(v); err != nil || c.BotBackfill == nil {
			t.Errorf("%s rejected: %v", v, err)
		}
	}
	for _, v := range []string{`{"fill_to": 1}`, `{"fill_to": 25}`, `{"max_bots": 9}`, `{"max_bots": -1}`, `{"difficulty": "legendary"}`} {
		if _, err := load(v); err == nil {
			t.Errorf("%s accepted", v)
		}
	}
}

type botFake struct{ got []hostctl.BotPolicy }

func (f *botFake) BotPolicy(_ context.Context, p hostctl.BotPolicy) (hostctl.Reply, error) {
	f.got = append(f.got, p)
	return hostctl.Reply{Code: hostctl.CodeOK}, nil
}

func TestSendBotsSkipsControllersWithoutBots(t *testing.T) {
	var out bytes.Buffer
	log := slog.New(slog.NewTextHandler(&out, nil))
	f := &botFake{}
	p := hostctl.BotPolicy{Flags: hostctl.BotBackfill, FillTo: 8, MaxBots: 8, Difficulty: hostctl.BotSpartan}
	sendBots(context.Background(), f, p, log, "x")
	sendBots(context.Background(), struct{}{}, p, log, "y")
	if len(f.got) != 1 || f.got[0] != p || !strings.Contains(out.String(), "difficulty=spartan") {
		t.Fatalf("sent %+v, log %s", f.got, out.String())
	}
}

func TestBotsLogLineOnlyOnChange(t *testing.T) {
	var out bytes.Buffer
	a := &agent{log: slog.New(slog.NewTextHandler(&out, nil))}
	var last botView
	l := hostctl.Lobby{BotSupported: true, BotsEnabled: 1, BotState: hostctl.BotStateFilled, BotCount: 3, BotHumans: 1, BotTicks: 10}
	a.logBots(l, &last)
	l.BotTicks = 20
	a.logBots(l, &last) // only the tick count changed
	l.BotCount = 2
	a.logBots(l, &last)
	if n := strings.Count(out.String(), "msg=bots"); n != 2 || !strings.Contains(out.String(), "state=filled") {
		t.Fatalf("%d bots lines:\n%s", n, out.String())
	}
}

func TestRegisteredDifficulties(t *testing.T) {
	for mask, want := range map[uint16]string{0: "none", 1 << 6: "marine", 1<<9 | 1<<8: "recruit,spartan", 1<<6 | 1<<7 | 1<<8 | 1<<9: "recruit,marine,odst,spartan"} {
		if got := registeredDifficulties(mask); got != want {
			t.Errorf("%#x: %q, want %q", mask, got, want)
		}
	}
}
