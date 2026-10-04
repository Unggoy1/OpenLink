package main

import (
	"flag"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hostagent.json")
	os.WriteFile(path, []byte(`{"directory":"https://file.example","name":"From File","public_port":2000,"max_pps":42,"proxy":false}`), 0o600)

	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	c, err := loadConfig(fs, []string{"-config", path, "-name", "From Flag", "init-config"})
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case c.Name != "From Flag":
		t.Fatalf("explicit flag must win: %q", c.Name)
	case c.Directory != "https://file.example" || c.PublicPort != 2000 || c.MaxPPS != 42 || c.Proxy:
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
