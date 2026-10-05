package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"halocommunity/internal/hostctl"
)

type hostController interface {
	Closed() bool
	Status(context.Context) (hostctl.Reply, error)
	Prepare(context.Context, hostctl.AssetPair) (hostctl.Reply, error)
	PrepareEngine(context.Context, hostctl.AssetPair) (hostctl.Reply, error)
	Initialize(context.Context, hostctl.AssetPair) (hostctl.Reply, error)
}

type controlInfo struct {
	Native    bool          `json:"native_backend"`
	Connected bool          `json:"connected"`
	Error     string        `json:"error,omitempty"`
	Rotation  *rotationInfo `json:"rotation,omitempty"`
	Lobby     *lobbyInfo    `json:"lobby,omitempty"`
	Vote      *voteInfo     `json:"vote,omitempty"`
}
type contentID struct {
	AssetID   string `json:"asset_id"`
	VersionID string `json:"version_id"`
}
type controlSelection struct {
	Map        contentID `json:"map"`
	Mode       contentID `json:"mode"`
	ModeKind   string    `json:"mode_kind"`
	Initialize bool      `json:"initialize,omitempty"`
}

func parseSelection(s controlSelection) (hostctl.AssetPair, error) {
	var pair hostctl.AssetPair
	if s.Initialize && s.ModeKind != "custom" {
		return pair, errors.New("initial selection requires mode_kind custom")
	}
	if s.ModeKind != "custom" && s.ModeKind != "engine" {
		return pair, errors.New("mode_kind must be custom or engine")
	}
	for i, id := range []string{s.Map.AssetID, s.Map.VersionID, s.Mode.AssetID, s.Mode.VersionID} {
		if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
			return pair, errors.New("asset/version IDs must be canonical nonzero UUIDs")
		}
		b, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
		if err != nil || len(b) != 16 {
			return pair, errors.New("invalid asset/version UUID")
		}
		copy(pair[i*16:], b)
	}
	if !pair.Valid() {
		return pair, errors.New("asset/version UUIDs must be nonzero")
	}
	return pair, nil
}

func (a *agent) getControl() hostController { a.mu.Lock(); defer a.mu.Unlock(); return a.control }
func (a *agent) failControl(controller hostController, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.control == controller && controller.Closed() {
		a.control = nil
		a.controlError = err.Error()
	}
}
func controlReply(r hostctl.Reply) map[string]any {
	name := map[uint16]string{hostctl.CodeOK: "ok", hostctl.CodeUnsupported: "unsupported", hostctl.CodeNativePending: "pending", hostctl.CodeBusy: "busy", hostctl.CodeInvalid: "invalid", hostctl.CodeFailed: "failed", hostctl.CodeSelected: "selected", hostctl.CodeApplied: "applied"}[r.Code]
	m := map[string]any{"status": name, "code": r.Code, "gates": r.Gates, "generation": r.Generation}
	if r.Version >= 2 {
		m["state"], m["matches"] = r.State, r.Matches
	}
	return m
}

// dispatchSelection sends one parsed selection with the operation its kind needs.
func dispatchSelection(ctx context.Context, controller hostController, s controlSelection, pair hostctl.AssetPair) (hostctl.Reply, error) {
	switch {
	case s.Initialize:
		return controller.Initialize(ctx, pair)
	case s.ModeKind == "engine":
		return controller.PrepareEngine(ctx, pair)
	default:
		return controller.Prepare(ctx, pair)
	}
}
func (a *agent) handleControlStatus(w http.ResponseWriter, r *http.Request) {
	controller := a.getControl()
	if controller == nil {
		writeJSON(w, 503, map[string]string{"error": "host control is not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	reply, err := controller.Status(ctx)
	if err != nil {
		a.failControl(controller, err)
		writeJSON(w, 503, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, controlReply(reply))
}
func (a *agent) handleControlSelect(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var selection controlSelection
	err := decoder.Decode(&selection)
	if err == nil {
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			err = errors.New("exactly one JSON request required")
		}
	}
	pair, validation := parseSelection(selection)
	if err != nil || validation != nil {
		writeJSON(w, 400, map[string]string{"error": "request requires canonical nonzero map/mode asset/version IDs and mode_kind custom or engine"})
		return
	}
	controller := a.getControl()
	if controller == nil {
		writeJSON(w, 503, map[string]string{"error": "host control is not connected"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	reply, err := dispatchSelection(ctx, controller, selection, pair)
	if err != nil {
		a.failControl(controller, err)
		writeJSON(w, 503, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, controlReply(reply))
}

// This runs only for this supervisor's managed launch, after its PID-specific
// setupComplete observation. No retry or adoption of an existing process.
func (a *agent) manageHostControl(ctx context.Context, cmd *exec.Cmd) {
	if a.cfg.HostControlDLL == "" {
		return
	}
	for a.getStatus() != "ready" {
		sleep(ctx, 250*time.Millisecond)
		if ctx.Err() != nil {
			return
		}
	}
	if ctx.Err() != nil {
		return
	}
	a.log.Info("starting explicit host control", "pid", cmd.Process.Pid, "native_backend", a.cfg.HostControlNative)
	session, err := hostctl.StartManaged(ctx, cmd.Process, cmd.Path, a.cfg.resolve(a.cfg.HostControlDLL), a.cfg.HostControlNative)
	if err != nil {
		a.mu.Lock()
		a.controlError = err.Error()
		a.mu.Unlock()
		a.log.Warn("host control unavailable", "err", err)
		return
	}
	a.mu.Lock()
	a.control = session.Bridge
	a.controlError = ""
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.control = nil
		a.mu.Unlock()
		if err := session.Close(); err != nil {
			a.mu.Lock()
			a.controlError = err.Error()
			a.mu.Unlock()
			a.log.Warn("host control cleanup unconfirmed", "err", err)
		}
	}()
	a.log.Info("host control transport connected", "pid", cmd.Process.Pid)
	if a.cfg.HostControlNative {
		go a.runLobby(ctx, session.Bridge, lobbyPoll)
	}
	switch {
	case a.cfg.Vote != nil && a.fwd != nil:
		go a.runVoting(ctx, session.Bridge)
	case a.cfg.Playlist != "":
		go a.runRotation(ctx, session.Bridge)
	}
	<-ctx.Done()
}

func (a *agent) markUnmanagedControl() {
	if a.cfg.HostControlDLL != "" {
		a.mu.Lock()
		a.controlError = "existing server was not launched by this agent; DLL loading skipped"
		a.mu.Unlock()
	}
}

func selectCommand(addr, path string) error {
	// Selection commands send only a bounded JSON file to the local guarded API.
	request, err := readSelectionFile(path)
	if err != nil {
		return err
	}
	var reply map[string]any
	if err = adminCall(addr, "POST", "/host-control/select", request, &reply); err != nil {
		return err
	}
	fmt.Printf("host control: %v (generation %v, gates %v)\n", reply["status"], reply["generation"], reply["gates"])
	return nil
}
