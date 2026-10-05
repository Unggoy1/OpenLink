//go:build windows

package hostctl

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestManagedLoaderOwnedTarget(t *testing.T) {
	dll, target := os.Getenv("HOSTCTL_TEST_DLL"), os.Getenv("HOSTCTL_TEST_HARNESS")
	if dll == "" || target == "" {
		t.Skip("enable owned artifacts")
	}
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "transport", true: "reject_native"}[native], func(t *testing.T) {
			cmd := exec.Command(target, "--loader-target")
			input, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { input.Close(); cmd.Process.Kill(); cmd.Wait() }()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := StartManaged(ctx, cmd.Process, target, dll, native)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			st, err := session.Bridge.Status(ctx)
			expected := CodeNativePending
			if native {
				expected = CodeUnsupported
			}
			if err != nil || st.Code != expected || st.Gates != 0 || st.Generation != 0 {
				t.Fatalf("status %+v %v", st, err)
			}
			r, err := session.Bridge.PrepareEngine(ctx, testPair())
			if err != nil || r.Code != expected {
				t.Fatalf("prepare %+v %v", r, err)
			}
			if err = session.Close(); err != nil {
				t.Fatal(err)
			}
			// A stopped/pinned DLL is never silently adopted/restarted.
			if again, e := StartManaged(ctx, cmd.Process, target, dll, native); e == nil {
				again.Close()
				t.Fatal("duplicate loading accepted")
			}
		})
	}
}

func TestManagedLoaderCanceledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if session, err := StartManaged(ctx, nil, "", "", false); err != context.Canceled {
		if session != nil {
			session.Close()
		}
		t.Fatalf("canceled load: %v", err)
	}
}

func TestManagedLoaderBindsOriginalHandle(t *testing.T) {
	dll, target := os.Getenv("HOSTCTL_TEST_DLL"), os.Getenv("HOSTCTL_TEST_HARNESS")
	if dll == "" || target == "" {
		t.Skip("enable owned artifacts")
	}
	start := func() (*exec.Cmd, func()) {
		cmd := exec.Command(target, "--loader-target")
		input, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err = cmd.Start(); err != nil {
			t.Fatal(err)
		}
		return cmd, func() { input.Close(); cmd.Process.Kill(); cmd.Wait() }
	}
	first, cleanupFirst := start()
	defer cleanupFirst()
	second, cleanupSecond := start()
	defer cleanupSecond()
	originalPID := first.Process.Pid
	// Inject a stale/incorrect numeric identity while keeping the original
	// os.Process launch handle. Only owned targets are used.
	first.Process.Pid = second.Process.Pid
	defer func() { first.Process.Pid = originalPID }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if session, err := StartManaged(ctx, first.Process, target, dll, false); err == nil {
		session.Close()
		t.Fatal("loader trusted numeric PID instead of original launch handle")
	}
}

func TestManagedLoaderRejectsWrongExpectedExecutable(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if session, err := StartManaged(ctx, cmd.Process, dll, dll, false); err == nil {
		session.Close()
		t.Fatal("wrong target accepted")
	}
}
