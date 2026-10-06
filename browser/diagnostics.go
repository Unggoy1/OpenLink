package main

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"time"

	"halocommunity/connect"
	"halocommunity/internal/diag"
	"halocommunity/internal/game"
)

// Diagnostics returns a report for the player to paste when asking for help:
// versions, settings, game build, whether the directory answers, the current
// session and recent events. Public IPs and keys are removed (diag.Redact).
func (a *App) Diagnostics() string {
	var r diag.Report
	s := a.GetSettings()
	r.Section("OpenLink app", fmt.Sprintf("version: %s\nos: %s/%s\ntime: %s\noverlay supported: %v",
		version, runtime.GOOS, runtime.GOARCH, time.Now().Format(time.RFC3339), a.overlay != nil))

	shown := s
	shown.Favorites = nil
	b, _ := json.MarshalIndent(shown, "", "  ")
	r.Section("Settings", fmt.Sprintf("%s\nfavorites: %d", b, len(s.Favorites)))

	build := connect.LocalBuild(s.InstallDir)
	if build == "" {
		build = "not found"
	}
	r.Section("Game", fmt.Sprintf("build: %s\nserver on this PC: %v", build, game.LocalServerRunning()))

	dir := "no directory set"
	if demo != nil {
		dir = "demo build: not checked"
	} else if s.Directory != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		servers, err := connect.List(ctx, s.Directory, "")
		cancel()
		if err != nil {
			dir = "list failed: " + err.Error()
		} else {
			same := 0
			for _, sv := range servers {
				if sv.Build == build {
					same++
				}
			}
			dir = fmt.Sprintf("%d servers listed, %d on this game build", len(servers), same)
		}
	}
	r.Section("Directory", dir)

	if st := a.Status(); st != nil {
		v, _ := json.MarshalIndent(st, "", "  ")
		r.Section("Session", string(v))
	} else {
		r.Section("Session", "not joined")
	}
	r.Section("Recent events", a.events.String())
	return r.String()
}
