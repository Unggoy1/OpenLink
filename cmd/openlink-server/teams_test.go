package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"halocommunity/internal/hostctl"
	"halocommunity/internal/playlist"
)

func TestTeamPolicyFromConfigAndEntry(t *testing.T) {
	four := &playlist.Entry{ID: "race", Teams: &playlist.Teams{Size: 4}}
	three := &playlist.Entry{ID: "ffa3", Teams: &playlist.Teams{Count: 3}}
	plain := &playlist.Entry{ID: "ctf"}
	both := hostctl.TeamGuardFFA | hostctl.TeamBalance
	for _, c := range []struct {
		balance string
		entry   *playlist.Entry
		want    hostctl.TeamPolicy
	}{
		{"", plain, hostctl.TeamPolicy{Flags: both}},
		{"even", nil, hostctl.TeamPolicy{Flags: both}},
		{"shuffle", three, hostctl.TeamPolicy{Flags: both, Mode: hostctl.TeamModeShuffle, Count: 3}},
		{"", four, hostctl.TeamPolicy{Flags: both, Size: 4}},
		{"off", plain, hostctl.TeamPolicy{Flags: hostctl.TeamGuardFFA}},
		// An entry's teams are applied even with balance off (even), so nobody stays on a missing team.
		{"off", four, hostctl.TeamPolicy{Flags: both, Mode: hostctl.TeamModeEven, Size: 4}},
	} {
		if got := (config{TeamBalance: c.balance}).teamPolicy(c.entry); got != c.want {
			t.Errorf("team_balance %q entry %v: %+v, want %+v", c.balance, c.entry, got, c.want)
		}
	}
}

type teamFake struct{ got []hostctl.TeamPolicy }

func (f *teamFake) TeamPolicy(_ context.Context, p hostctl.TeamPolicy) (hostctl.Reply, error) {
	f.got = append(f.got, p)
	return hostctl.Reply{Code: hostctl.CodeOK}, nil
}

func TestSendTeamsSkipsControllersWithoutTeamRules(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	f := &teamFake{}
	p := hostctl.TeamPolicy{Flags: hostctl.TeamGuardFFA | hostctl.TeamBalance, Count: 4}
	sendTeams(context.Background(), f, p, log, "x")
	sendTeams(context.Background(), struct{}{}, p, log, "y")
	if len(f.got) != 1 || f.got[0] != p {
		t.Fatalf("sent %+v", f.got)
	}
}

func TestConfigTeamBalanceValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openlink-server.json")
	load := func(v string) error {
		os.WriteFile(path, []byte(`{"name": "x", "team_balance": "`+v+`"}`), 0o600)
		_, err := loadConfig(flag.NewFlagSet("t", flag.ContinueOnError), []string{"-config", path})
		return err
	}
	for _, v := range []string{"", "even", "shuffle", "off"} {
		if err := load(v); err != nil {
			t.Errorf("%q rejected: %v", v, err)
		}
	}
	if err := load("random"); err == nil {
		t.Error("unknown team_balance accepted")
	}
}

type warnLog struct{ warnings []string }

func (w *warnLog) Enabled(context.Context, slog.Level) bool { return true }
func (w *warnLog) Handle(_ context.Context, r slog.Record) error {
	if r.Level == slog.LevelWarn {
		w.warnings = append(w.warnings, r.Message)
	}
	return nil
}
func (w *warnLog) WithAttrs([]slog.Attr) slog.Handler { return w }
func (w *warnLog) WithGroup(string) slog.Handler      { return w }

func TestWarnFFATeamsOncePerEntry(t *testing.T) {
	h := &warnLog{}
	a := &agent{log: slog.New(h)}
	plain := &playlist.Entry{ID: "ffa"}
	teams := &playlist.Entry{ID: "ffa-teams", Teams: &playlist.Teams{Count: 2}}
	a.entryTeams(plain)
	a.warnFFATeams(0) // FFA without teams: fine
	a.entryTeams(teams)
	a.warnFFATeams(1)  // a team mode: fine
	a.warnFFATeams(-1) // no mode yet: fine
	a.warnFFATeams(0)
	a.warnFFATeams(0) // once per entry
	if len(h.warnings) != 1 {
		t.Fatalf("warnings %q", h.warnings)
	}
}
