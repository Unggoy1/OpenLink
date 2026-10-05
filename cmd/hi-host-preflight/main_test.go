package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingExe(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code != 2 || out.Len() != 0 || errOut.Len() == 0 {
		t.Fatalf("code=%d out=%s stderr=%s", code, &out, &errOut)
	}
}

func TestUnreadableFile(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"-exe", filepath.Join(t.TempDir(), "absent.exe")}, &out, &errOut); code != 2 || out.Len() != 0 || errOut.Len() == 0 {
		t.Fatalf("code=%d out=%s stderr=%s", code, &out, &errOut)
	}
}

func TestMalformedFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.exe")
	if err := os.WriteFile(p, []byte("MZ"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"-exe", p}, &out, &errOut); code != 2 || out.Len() != 0 || errOut.Len() == 0 {
		t.Fatalf("code=%d out=%s stderr=%s", code, &out, &errOut)
	}
}

// Valid minimal PE32+ image, unrelated to the game or its allowed fingerprints.
func unknownPE() []byte {
	b := make([]byte, 0x500)
	copy(b, "MZ")
	binary.LittleEndian.PutUint32(b[0x3c:], 0x80)
	copy(b[0x80:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(b[0x84:], 0x8664)
	binary.LittleEndian.PutUint16(b[0x86:], 1)
	binary.LittleEndian.PutUint16(b[0x94:], 240)
	binary.LittleEndian.PutUint16(b[0x98:], 0x20b)
	binary.LittleEndian.PutUint64(b[0xb0:], 0x140000000)
	binary.LittleEndian.PutUint32(b[0xd4:], 0x400)
	binary.LittleEndian.PutUint32(b[0x104:], 16)
	copy(b[0x188:], ".text")
	binary.LittleEndian.PutUint32(b[0x190:], 0x80)
	binary.LittleEndian.PutUint32(b[0x194:], 0x2000)
	binary.LittleEndian.PutUint32(b[0x198:], 0x80)
	binary.LittleEndian.PutUint32(b[0x19c:], 0x400)
	binary.LittleEndian.PutUint32(b[0x1ac:], 0x60000020)
	return b
}

func TestUnknownBuildReport(t *testing.T) {
	p := filepath.Join(t.TempDir(), "unknown.exe")
	b := unknownPE()
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"-exe", p}, &out, &errOut)
	var report struct {
		SchemaVersion int               `json:"schema_version"`
		Supported     bool              `json:"supported_build"`
		ControlReady  bool              `json:"control_ready"`
		Reason        string            `json:"reason"`
		Targets       []json.RawMessage `json:"targets"`
		Pending       []string          `json:"pending_gates"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("invalid report: %v / %s", err, &out)
	}
	if code != 3 || errOut.Len() != 0 || report.SchemaVersion != 1 || report.Supported || report.ControlReady ||
		report.Reason != "unknown_build" || len(report.Targets) != 0 || len(report.Pending) != 3 {
		t.Fatalf("code=%d report=%+v stderr=%s", code, report, &errOut)
	}
	after, err := os.ReadFile(p)
	if err != nil || !bytes.Equal(b, after) {
		t.Fatal("diagnostic modified input")
	}
}

func TestPreflightNeverClaimsLiveControl(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"-h"}, &out, &errOut); code != 0 || out.Len() != 0 {
		t.Fatalf("help code=%d stdout=%s", code, &out)
	}
	for _, phrase := range []string{"read-only", "No DLL is loaded"} {
		if !strings.Contains(errOut.String(), phrase) {
			t.Fatalf("missing help boundary %q: %s", phrase, &errOut)
		}
	}
	for _, args := range [][]string{{"-pid", "123"}, {"-exe", "unused", "attach"}} {
		out.Reset()
		errOut.Reset()
		if code := run(args, &out, &errOut); code != 2 || out.Len() != 0 {
			t.Fatalf("unsupported operation accepted: %v code=%d out=%s", args, code, &out)
		}
	}
}
