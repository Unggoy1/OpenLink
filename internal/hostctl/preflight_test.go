package hostctl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Synthetic PE with a deliberately different RVA and file offset. No game bytes.
func fixture() ([]byte, Manifest) {
	b := make([]byte, 0x500)
	copy(b, "MZ")
	binary.LittleEndian.PutUint32(b[0x3c:], 0x80)
	copy(b[0x80:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(b[0x84:], 0x8664)
	binary.LittleEndian.PutUint16(b[0x86:], 1)
	binary.LittleEndian.PutUint16(b[0x94:], 240)
	binary.LittleEndian.PutUint16(b[0x98:], 0x20b)
	binary.LittleEndian.PutUint64(b[0xb0:], 0x140000000)
	binary.LittleEndian.PutUint32(b[0xb8:], 0x1000)
	binary.LittleEndian.PutUint32(b[0xbc:], 0x200)
	binary.LittleEndian.PutUint32(b[0xd0:], 0x3000)
	binary.LittleEndian.PutUint32(b[0xd4:], 0x400)
	binary.LittleEndian.PutUint32(b[0x104:], 16)
	copy(b[0x188:], ".text")
	binary.LittleEndian.PutUint32(b[0x190:], 0x80)
	binary.LittleEndian.PutUint32(b[0x194:], 0x2000)
	binary.LittleEndian.PutUint32(b[0x198:], 0x80)
	binary.LittleEndian.PutUint32(b[0x19c:], 0x400)
	binary.LittleEndian.PutUint32(b[0x1ac:], 0x60000020)
	prefix := [16]byte{0x48, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	copy(b[0x410:], prefix[:])
	m := Manifest{SHA256: sha256.Sum256(b), Machine: 0x8664, ImageBase: 0x140000000,
		Targets: []Target{{Name: "fixture_handler", RVA: 0x2010, Prefix: prefix}}}
	return b, m
}

func inspectFixture(t *testing.T, b []byte, m Manifest) Report {
	t.Helper()
	r, err := inspect(bytes.NewReader(b), int64(len(b)), m)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestInspectFixture(t *testing.T) {
	b, m := fixture()
	r := inspectFixture(t, b, m)
	if !r.SupportedBuild || r.Reason != "verified_file_only" || len(r.Targets) != 1 ||
		!r.Targets[0].Verified || r.Targets[0].RVA != 0x2010 {
		t.Fatalf("unexpected report: %+v", r)
	}
}

func TestRejectChangedHash(t *testing.T) {
	b, m := fixture()
	b[0x4ff] ^= 1 // Outside the candidate bytes and all PE headers.
	r := inspectFixture(t, b, m)
	if r.SupportedBuild || r.Reason != "unknown_build" {
		t.Fatalf("changed file accepted: %+v", r)
	}
	for _, target := range r.Targets {
		if target.Verified {
			t.Fatalf("verified target on unknown build: %+v", target)
		}
	}
}

func TestRejectMalformedPE(t *testing.T) {
	for _, size := range []int{0x20, 0x90, 0x190, 0x420} {
		t.Run(fmt.Sprintf("length_%#x", size), func(t *testing.T) {
			b, m := fixture()
			b = b[:size]
			m.SHA256 = sha256.Sum256(b)
			if _, err := inspect(bytes.NewReader(b), int64(len(b)), m); err == nil {
				t.Fatal("truncated PE accepted")
			}
		})
	}
}

func TestRejectAmbiguousSection(t *testing.T) {
	b, m := fixture()
	binary.LittleEndian.PutUint16(b[0x86:], 2)
	copy(b[0x1b0:0x1d8], b[0x188:0x1b0])
	binary.LittleEndian.PutUint32(b[0x1bc:], 0x2020)
	// Distinct valid raw ranges isolate the ambiguous virtual mapping.
	binary.LittleEndian.PutUint32(b[0x1c4:], 0x480)
	m.SHA256 = sha256.Sum256(b)
	if _, err := inspect(bytes.NewReader(b), int64(len(b)), m); err == nil {
		t.Fatal("overlapping RVA ranges accepted")
	}
}

func TestRejectVirtualOnlyTarget(t *testing.T) {
	b, m := fixture()
	binary.LittleEndian.PutUint32(b[0x190:], 0x200)
	m.SHA256 = sha256.Sum256(b)
	m.Targets[0].RVA = 0x2090
	r := inspectFixture(t, b, m)
	if r.SupportedBuild || len(r.Targets) != 1 || r.Targets[0].Verified || r.Targets[0].Reason != "not_file_backed" {
		t.Fatalf("virtual bytes accepted: %+v", r)
	}
}

func TestRejectNonExecutableTarget(t *testing.T) {
	b, m := fixture()
	binary.LittleEndian.PutUint32(b[0x1ac:], 0x40000040)
	m.SHA256 = sha256.Sum256(b)
	r := inspectFixture(t, b, m)
	if r.SupportedBuild || r.Targets[0].Verified || r.Targets[0].Reason != "not_executable" {
		t.Fatalf("data-section target accepted: %+v", r)
	}
}

func TestRejectWrongPrefix(t *testing.T) {
	b, m := fixture()
	b[0x410] ^= 1
	m.SHA256 = sha256.Sum256(b) // Known fixture, but a wrong candidate.
	r := inspectFixture(t, b, m)
	if r.SupportedBuild || r.Reason != "target_validation_failed" || r.Targets[0].Verified || r.Targets[0].Reason != "prefix_mismatch" {
		t.Fatalf("bad prefix accepted: %+v", r)
	}
}

func TestSupportedFileIsNotControlReady(t *testing.T) {
	b, m := fixture()
	r := inspectFixture(t, b, m)
	if r.ControlReady || r.SchemaVersion != 1 || !reflect.DeepEqual(r.PendingGates,
		[]string{"variant_consumer", "engine_dispatch", "server_lifecycle"}) {
		t.Fatalf("file check overclaimed readiness: %+v", r)
	}
}

func TestInspectFileUsesProductionManifest(t *testing.T) {
	b, _ := fixture()
	p := filepath.Join(t.TempDir(), "fixture.exe")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := InspectFile(p)
	if err != nil || r.SupportedBuild || r.ControlReady || r.Reason != "unknown_build" {
		t.Fatalf("fixture accepted by production manifest: %+v, %v", r, err)
	}
	after, err := os.ReadFile(p)
	if err != nil || !bytes.Equal(b, after) {
		t.Fatal("input changed")
	}
	if _, err := InspectFile(filepath.Join(t.TempDir(), "absent.exe")); err == nil {
		t.Fatal("missing input accepted")
	}
}
