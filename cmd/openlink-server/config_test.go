package main

import (
	"flag"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"halocommunity/internal/api"
)

func TestConfigDirectoryDefault(t *testing.T) {
	t.Setenv("OPENLINK_DIRECTORY", "")
	t.Setenv("HICOMM_DIRECTORY", "")
	dir := t.TempDir()
	load := func(doc string) config {
		t.Helper()
		path := filepath.Join(dir, "openlink-server.json")
		os.WriteFile(path, []byte(doc), 0o600)
		c, err := loadConfig(flag.NewFlagSet("t", flag.ContinueOnError), []string{"-config", path})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	if c := load(`{"name": "x"}`); c.Directory != api.DefaultDirectory {
		t.Errorf("no directory in the file: got %q", c.Directory)
	}
	if c := load(`{"directory": ""}`); c.Directory != "" {
		t.Errorf(`"directory": "" should turn listing off, got %q`, c.Directory)
	}
	t.Setenv("HICOMM_DIRECTORY", "http://127.0.0.1:8080") // the older name still works
	if c := load(`{"name": "x"}`); c.Directory != "http://127.0.0.1:8080" {
		t.Errorf("old env name: got %q", c.Directory)
	}
	t.Setenv("OPENLINK_DIRECTORY", "http://127.0.0.1:9090") // and the new one wins
	if c := load(`{"name": "x"}`); c.Directory != "http://127.0.0.1:9090" {
		t.Errorf("env: got %q", c.Directory)
	}
}

func TestConfigPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openlink-server.json")
	os.WriteFile(path, []byte(`{"directory":"https://file.example","name":"From File","public_port":2000,"max_pps":42,"max_players":7}`), 0o600)

	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	c, err := loadConfig(fs, []string{"-config", path, "-name", "From Flag", "init-config"})
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case c.Name != "From Flag":
		t.Fatalf("explicit flag must win: %q", c.Name)
	case c.Directory != "https://file.example" || c.PublicPort != 2000 || c.MaxPPS != 42 || c.MaxPlayers != 7:
		t.Fatalf("file values lost: %+v", c)
	case c.Listen != "0.0.0.0:1343" || c.ServerIP != "127.0.0.1":
		t.Fatalf("defaults for keys absent from the file lost: %+v", c)
	case fs.Arg(0) != "init-config":
		t.Fatalf("subcommand lost: %v", fs.Args())
	}
	if got := c.resolve("bans.json"); got != filepath.Join(dir, "bans.json") {
		t.Fatalf("relative paths must resolve next to the config: %s", got)
	}

	// Saved config round-trips, and never includes run-only switches.
	out := filepath.Join(dir, "saved.json")
	if err := c.save(out); err != nil {
		t.Fatal(err)
	}
	c2, err := loadConfig(flag.NewFlagSet("t2", flag.ContinueOnError), []string{"-config", out})
	if err != nil || c2.Name != "From Flag" || c2.PublicPort != 2000 || c2.Simulate {
		t.Fatalf("round trip: %+v %v", c2, err)
	}
	// The default DLL path is not written out, so the folder can move.
	if b, _ := os.ReadFile(out); strings.Contains(string(b), "control_dll") {
		t.Fatalf("saved config pins the default DLL path:\n%s", b)
	}
}

func TestBans(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bans.json")
	b, err := loadBans(path)
	if err != nil {
		t.Fatal(err)
	}
	ip, other := net.ParseIP("198.51.100.7"), net.ParseIP("198.51.100.8")
	if !b.Allowed(ip) {
		t.Fatal("empty list blocks")
	}
	b.Ban(ip, 0)
	b.Ban(other, -time.Second) // already expired
	b.bans[other.String()] = time.Now().Add(-time.Second)
	if b.Allowed(ip) || !b.Allowed(other) {
		t.Fatal("permanent ban not applied, or expired ban still applied")
	}
	reloaded, err := loadBans(path)
	if err != nil || reloaded.Allowed(ip) {
		t.Fatalf("ban not persisted: %v", err)
	}
	if was, _ := reloaded.Unban(ip); !was || !reloaded.Allowed(ip) {
		t.Fatal("unban failed")
	}
}

// The config shipped in the host release zip must stay loadable.
func TestPackagedExampleConfig(t *testing.T) {
	c, err := loadConfig(flag.NewFlagSet("t", flag.ContinueOnError), []string{"-config", "../../packaging/host/openlink-server.example.json"})
	if err != nil || !c.HostControlNative || c.Playlist != "playlist.json" || c.ServerIP != "127.0.0.2" {
		t.Fatalf("packaged config example: %+v %v", c, err)
	}
}

func TestConfigServerName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openlink-server.json")
	load := func(doc string) error {
		os.WriteFile(path, []byte(doc), 0o600)
		_, err := loadConfig(flag.NewFlagSet("t", flag.ContinueOnError), []string{"-config", path})
		return err
	}
	for _, name := range []string{"Friday Night Halo", strings.Repeat("é", 48)} {
		if err := load(`{"name": "` + name + `"}`); err != nil {
			t.Errorf("%q rejected: %v", name, err)
		}
	}
	for _, name := range []string{"", "   ", strings.Repeat("x", 49), `tab\there`} {
		if err := load(`{"name": "` + name + `"}`); err == nil || !strings.Contains(err.Error(), "1-48") {
			t.Errorf("%q accepted: %v", name, err)
		}
	}
	if err := load(`{"name": "", "directory": ""}`); err != nil {
		t.Errorf("unlisted server with no name rejected: %v", err)
	}
}

func TestConfigDescription(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openlink-server.json")
	load := func(doc string) error {
		os.WriteFile(path, []byte(doc), 0o600)
		_, err := loadConfig(flag.NewFlagSet("t", flag.ContinueOnError), []string{"-config", path})
		return err
	}
	if err := load(`{"name": "x", "description": "Casual BTB, be nice"}`); err != nil {
		t.Fatal(err)
	}
	if err := load(`{"name": "x", "description": "` + strings.Repeat("d", 121) + `"}`); err == nil {
		t.Fatal("121-character description accepted")
	}
}

func TestConfigLobbyLeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openlink-server.json")
	load := func(doc string) (config, error) {
		os.WriteFile(path, []byte(doc), 0o600)
		return loadConfig(flag.NewFlagSet("t", flag.ContinueOnError), []string{"-config", path})
	}
	for doc, want := range map[string]uint64{
		`{"name": "x"}`:                       0,
		`{"name": "x", "server_owned": true}`: defaultLobbyLeader,
		`{"name": "x", "server_owned": true, "lobby_owner": "first_player"}`:         0,
		`{"name": "x", "server_owned": true, "lobby_leader_xuid": 2533274962600518}`: 2533274962600518,
	} {
		c, err := load(doc)
		if err != nil || c.lobbyLeader() != want {
			t.Errorf("%s: leader %d, want %d (%v)", doc, c.lobbyLeader(), want, err)
		}
	}
	for _, doc := range []string{
		`{"name": "x", "lobby_leader_xuid": 5}`,
		`{"name": "x", "server_owned": true, "lobby_owner": "first_player", "lobby_leader_xuid": 5}`,
	} {
		if _, err := load(doc); err == nil {
			t.Errorf("%s accepted", doc)
		}
	}
}
