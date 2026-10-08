package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"halocommunity/internal/diag"
	"halocommunity/internal/game"
)

// diagnosticsCommand writes openlink-diagnostics-<time>.txt next to the log:
// versions, settings, game build check, control files, playlist check, the
// running agent's status and the end of the log. Public IPs and keys are
// removed (diag.Redact) so hosts can post it where they ask for help.
func diagnosticsCommand(c config) error {
	var r diag.Report
	r.Section("OpenLink Server", fmt.Sprintf("version: %s\nos: %s/%s %s\ntime: %s\nconfig: %s",
		version, runtime.GOOS, runtime.GOARCH, osVersion(), time.Now().Format(time.RFC3339), orDash(c.path)))

	cfg, _ := json.MarshalIndent(c, "", "  ")
	r.Section("Settings", string(cfg))

	var g strings.Builder
	if in, err := game.FindInstall(c.Install); err != nil {
		fmt.Fprintf(&g, "install: not found: %v\n", err)
	} else {
		fmt.Fprintf(&g, "install: %s\n", in.Root)
		build, err := in.Build()
		fmt.Fprintf(&g, "build: %s %v\nsupported build: %s\n", build, errOrEmpty(err), supportedBuild)
		if err := checkGameBuild(in, build, supportedBuild, supportedGameSHA256); err != nil {
			fmt.Fprintf(&g, "build check: %v\n", err)
		} else {
			g.WriteString("build check: ok\n")
		}
	}
	r.Section("Game", g.String())

	var files strings.Builder
	dll := c.resolve(c.HostControlDLL)
	if dll == "" {
		dll = filepath.Join(exeDir(), "openlink-control.dll")
	}
	for _, p := range []string{dll, filepath.Join(filepath.Dir(dll), "openlink-loader.exe")} {
		fmt.Fprintf(&files, "%s: %s\n", p, fileSummary(p))
	}
	r.Section("Control files", files.String())

	if _, rep, err := checkPlaylist(c, c.resolve(c.Playlist), c.Vote != nil); err != nil {
		r.Section("Playlist", fmt.Sprintf("%s: %v", orDash(c.Playlist), err))
	} else {
		r.Section("Playlist", fmt.Sprintf("%s: ok (largest ballot %d bytes)", c.Playlist, rep.ballot))
	}

	if c.Admin == "" {
		r.Section("Running agent", "admin API off (\"admin\": \"\"), status not available")
	} else {
		var st any
		if err := adminCall(c.Admin, "GET", "/status", nil, &st); err != nil {
			r.Section("Running agent", "not running or not answering: "+err.Error())
		} else {
			b, _ := json.MarshalIndent(st, "", "  ")
			r.Section("Running agent", string(b))
		}
	}
	r.Section("Autostart", autostartSummary(c))

	logPath := filepath.Join(c.logDir(), logName)
	if b, err := os.ReadFile(logPath); err != nil {
		r.Section("Log", "no log: "+err.Error())
	} else {
		r.Section("Log (last 400 lines of "+logName+")", diag.Tail(string(b), 400))
	}

	out := filepath.Join(c.logDir(), "openlink-diagnostics-"+time.Now().Format("20060102-150405")+".txt")
	if err := os.WriteFile(out, []byte(r.String()), 0o600); err != nil {
		return err
	}
	fmt.Printf("Wrote %s\nPublic IP addresses and keys are removed. Look it over, then attach it when you ask for help.\n", out)
	return nil
}

func fileSummary(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "missing"
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("%d bytes, sha256 %s", n, hex.EncodeToString(h.Sum(nil))[:16])
}

func osVersion() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	out, err := exec.Command("cmd", "/c", "ver").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func errOrEmpty(err error) string {
	if err == nil {
		return ""
	}
	return "(" + err.Error() + ")"
}
