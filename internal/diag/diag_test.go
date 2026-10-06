package diag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	for in, want := range map[string]string{
		"host 203.0.113.7:1343 joined":                     "host <public IP>:1343 joined",
		"listen 192.168.11.72:1344 via 10.0.0.2":           "listen 192.168.11.72:1344 via 10.0.0.2",
		"server 127.0.0.2 and 0.0.0.0 and 255.255.255.255": "server 127.0.0.2 and 0.0.0.0 and 255.255.255.255",
		`"register_key": "abc123",`:                        `"register_key": "<redacted>",`,
		"token=deadbeef err=x":                             "token=<redacted> err=x",
		`{"token":"t0k"}`:                                  `{"token":"<redacted>"}`,
		"build 269225.26.04.08.1618-1.hi_1_13_0":           "build 269225.26.04.08.1618-1.hi_1_13_0",
		`"overlayOpenKey": "Ctrl+Alt+V"`:                   `"overlayOpenKey": "Ctrl+Alt+V"`,
		"key=abc":                                          "key=<redacted>",
	} {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEventsAndTail(t *testing.T) {
	e := NewEvents(3)
	for _, s := range []string{"a", "b", "c", "d"} {
		e.Add("%s", s)
	}
	lines := strings.Split(e.String(), "\n")
	if len(lines) != 3 || !strings.HasSuffix(lines[0], " b") || !strings.HasSuffix(lines[2], " d") {
		t.Fatalf("events: %q", lines)
	}
	if got := Tail("1\n2\n3\n", 2); got != "2\n3" {
		t.Fatalf("tail: %q", got)
	}
	var r Report
	r.Section("Config", `"public_host": "198.51.100.4"`)
	if !strings.Contains(r.String(), "== Config ==") || strings.Contains(r.String(), "198.51.100.4") {
		t.Fatalf("report: %s", r.String())
	}
}

func TestRedactHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	in := "config: " + home + `\x.json and ` + filepath.ToSlash(home) + "/y and " + strings.ToUpper(home)
	if got := Redact(in); strings.Contains(strings.ToLower(got), strings.ToLower(filepath.Base(home))) {
		t.Fatalf("home not redacted: %q", got)
	}
}
