package main

import (
	"context"
	"errors"
	"flag"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"halocommunity/internal/hostctl"
)

const selectionJSON = `{"map":{"asset_id":"00112233-4455-6677-8899-aabbccddeeff","version_id":"10112233-4455-6677-8899-aabbccddeeff"},"mode":{"asset_id":"20112233-4455-6677-8899-aabbccddeeff","version_id":"30112233-4455-6677-8899-aabbccddeeff"},"mode_kind":"engine"}`

type fakeControl struct {
	engine  bool
	initial bool
	calls   int
}

func (*fakeControl) Closed() bool { return false }

type failedControl struct{ fakeControl }

func (*failedControl) Closed() bool { return true }

func (*failedControl) Status(context.Context) (hostctl.Reply, error) {
	return hostctl.Reply{}, errors.New("bridge permanently closed")
}
func TestFailedControlMarkedUnavailable(t *testing.T) {
	a := &agent{control: &failedControl{}}
	w := httptest.NewRecorder()
	a.handleControlStatus(w, httptest.NewRequest("GET", "/host-control", nil))
	if w.Code != 503 || a.getControl() != nil {
		t.Fatalf("failed bridge retained: %d", w.Code)
	}
	a.mu.Lock()
	message := a.controlError
	a.mu.Unlock()
	if message == "" {
		t.Fatal("failure reason lost")
	}
}

type canceledControl struct{ fakeControl }

func (*canceledControl) Status(context.Context) (hostctl.Reply, error) {
	return hostctl.Reply{}, context.Canceled
}
func (*canceledControl) Closed() bool { return false }
func TestCanceledBeforeIOKeepsHealthyControl(t *testing.T) {
	controller := &canceledControl{}
	a := &agent{control: controller}
	w := httptest.NewRecorder()
	a.handleControlStatus(w, httptest.NewRequest("GET", "/host-control", nil))
	if w.Code != 503 || a.getControl() != controller {
		t.Fatal("pre-I/O cancellation discarded healthy bridge")
	}
}

func (*fakeControl) Status(context.Context) (hostctl.Reply, error) {
	return hostctl.Reply{Code: hostctl.CodeNativePending}, nil
}
func (f *fakeControl) Prepare(context.Context, hostctl.AssetPair) (hostctl.Reply, error) {
	f.calls++
	return hostctl.Reply{Code: hostctl.CodeSelected}, nil
}
func (f *fakeControl) PrepareEngine(context.Context, hostctl.AssetPair) (hostctl.Reply, error) {
	f.engine = true
	f.calls++
	return hostctl.Reply{Code: hostctl.CodeSelected}, nil
}
func (f *fakeControl) Initialize(context.Context, hostctl.AssetPair) (hostctl.Reply, error) {
	f.initial = true
	f.calls++
	return hostctl.Reply{Code: hostctl.CodeSelected}, nil
}

func TestHostControlConfiguration(t *testing.T) {
	load := func(args ...string) (config, error) {
		return loadConfig(flag.NewFlagSet("t", flag.ContinueOnError), args)
	}
	// A real server always uses the control DLL, by default the one next to the program.
	c, err := load()
	if err != nil || !c.HostControlNative || c.HostControlDLL != filepath.Join(exeDir(), "openlink-control.dll") {
		t.Fatalf("default: %+v %v", c, err)
	}
	if c, err := load("-control-dll", "owned.dll"); err != nil || c.HostControlDLL != "owned.dll" || !c.HostControlNative {
		t.Fatalf("override: %+v %v", c, err)
	}
	// A simulation has no game, so the DLL and the playlist do not apply.
	c, err = load("-simulate", "-control-dll", "owned.dll", "-playlist", "playlist.json")
	if err != nil || c.HostControlDLL != "" || c.HostControlNative || c.Playlist != "" {
		t.Fatalf("simulate: %+v %v", c, err)
	}
	if _, err := load("-manage=false"); err == nil {
		t.Fatal("a real server the agent does not start was accepted")
	}
}

func TestCheckControlFiles(t *testing.T) {
	dir := t.TempDir()
	dll := filepath.Join(dir, "openlink-control.dll")
	os.WriteFile(dll, nil, 0o600)
	if err := checkControlFiles(dll); err == nil {
		t.Fatal("missing loader accepted")
	}
	os.WriteFile(filepath.Join(dir, "openlink-loader.exe"), nil, 0o600)
	if err := checkControlFiles(dll); err != nil {
		t.Fatal(err)
	}
}

func TestHostControlSelectValidationAndMeaning(t *testing.T) {
	f := &fakeControl{}
	a := &agent{control: f}
	r := httptest.NewRequest("POST", "/host-control/select", strings.NewReader(selectionJSON))
	w := httptest.NewRecorder()
	a.handleControlSelect(w, r)
	if w.Code != 200 || !f.engine || f.calls != 1 || !strings.Contains(w.Body.String(), `"status":"selected"`) || strings.Contains(w.Body.String(), `"status":"applied"`) {
		t.Fatalf("%d %s %+v", w.Code, w.Body.String(), f)
	}
	for _, body := range []string{selectionJSON + `{}`, strings.Replace(selectionJSON, `"engine"`, `"unknown"`, 1), strings.Replace(selectionJSON, "00112233-4455-6677-8899-aabbccddeeff", "00000000-0000-0000-0000-000000000000", 1), strings.Replace(selectionJSON, `"mode_kind"`, `"typo"`, 1)} {
		w = httptest.NewRecorder()
		a.handleControlSelect(w, httptest.NewRequest("POST", "/host-control/select", strings.NewReader(body)))
		if w.Code != 400 || f.calls != 1 {
			t.Fatalf("invalid request executed %d %s", w.Code, w.Body.String())
		}
	}
	a.control = nil
	w = httptest.NewRecorder()
	a.handleControlSelect(w, httptest.NewRequest("POST", "/host-control/select", strings.NewReader(selectionJSON)))
	if w.Code != 503 {
		t.Fatalf("unavailable %d", w.Code)
	}
}

func TestExplicitInitialSelection(t *testing.T) {
	f := &fakeControl{}
	a := &agent{control: f}
	body := strings.Replace(selectionJSON, `"mode_kind":"engine"`, `"mode_kind":"custom","initialize":true`, 1)
	w := httptest.NewRecorder()
	a.handleControlSelect(w, httptest.NewRequest("POST", "/host-control/select", strings.NewReader(body)))
	if w.Code != 200 || f.calls != 1 || !f.initial || f.engine {
		t.Fatalf("explicit initialization rejected: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	a.handleControlSelect(w, httptest.NewRequest("POST", "/host-control/select", strings.NewReader(strings.Replace(body, `"custom"`, `"engine"`, 1))))
	if w.Code != 400 || f.calls != 1 {
		t.Fatal("engine mode initialization executed")
	}
}
