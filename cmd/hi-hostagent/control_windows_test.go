//go:build windows

package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestManagedAgentControlLifecycleOwnedTarget(t *testing.T) {
	dll, target := os.Getenv("HOSTCTL_TEST_DLL"), os.Getenv("HOSTCTL_TEST_HARNESS")
	if dll == "" || target == "" {
		t.Skip("enable owned artifacts")
	}
	cmd := exec.Command(target, "--loader-target")
	input, _ := cmd.StdinPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { input.Close(); cmd.Process.Kill(); cmd.Wait() }()
	a := &agent{cfg: config{HostControlDLL: dll}, log: slog.New(slog.NewTextHandler(io.Discard, nil)), status: "starting"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); a.manageHostControl(ctx, cmd) }()
	if a.getControl() != nil {
		t.Fatal("loaded before setupComplete")
	}
	a.setStatus("ready")
	deadline := time.Now().Add(5 * time.Second)
	for a.getControl() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if a.getControl() == nil {
		a.mu.Lock()
		err := a.controlError
		a.mu.Unlock()
		t.Fatalf("not connected: %s", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("control shutdown stalled")
	}
	if a.getControl() != nil {
		t.Fatal("old bridge retained")
	}
	a.mu.Lock()
	err := a.controlError
	a.mu.Unlock()
	if err != "" {
		t.Fatal(err)
	}
}
