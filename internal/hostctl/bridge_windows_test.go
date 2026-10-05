//go:build windows

package hostctl

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestNativeBackendOptInRejectsOwnedExecutable(t *testing.T) {
	dll, harness := os.Getenv("HOSTCTL_TEST_DLL"), os.Getenv("HOSTCTL_TEST_HARNESS")
	if dll == "" || harness == "" {
		t.Skip("enable owned DLL/harness paths")
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var token [32]byte
	token[0] = 13
	command := exec.Command(harness, dll, strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
	command.Env = append(os.Environ(), "HICT_TEST_TOKEN="+hex.EncodeToString(token[:]), "HICT_TEST_CONTROLLER_PID="+strconv.Itoa(os.Getpid()), "HICT_TEST_NATIVE=2")
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer command.Process.Kill()
	listener.SetDeadline(time.Now().Add(5 * time.Second))
	connection, err := listener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bridge, err := AcceptBridge(ctx, connection, token, uint32(command.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	for _, operation := range []func(context.Context) (Reply, error){bridge.Status, func(ctx context.Context) (Reply, error) { return bridge.PrepareEngine(ctx, testPair()) }, func(ctx context.Context) (Reply, error) { return bridge.Initialize(ctx, testPair()) }} {
		reply, err := operation(ctx)
		if err != nil || reply.Code != CodeUnsupported || reply.Gates != 0 || reply.Generation != 0 || reply.Pair != (AssetPair{}) {
			t.Fatalf("owned opt-in rejection %+v %v", reply, err)
		}
	}
	bridge.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owned harness did not exit")
	}
}

func TestNativeDLLTransport(t *testing.T) {
	dll, harness := os.Getenv("HOSTCTL_TEST_DLL"), os.Getenv("HOSTCTL_TEST_HARNESS")
	if dll == "" || harness == "" {
		t.Skip("enable with built DLL and test-owned harness paths")
	}
	for _, p := range []string{dll, harness} {
		if _, err := os.Stat(p); err != nil {
			t.Fatal(err)
		}
	}
	l, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	var token [32]byte
	for i := range token {
		token[i] = byte(i + 1)
	}
	cmd := exec.Command(harness, dll, strconv.Itoa(l.Addr().(*net.TCPAddr).Port))
	cmd.Env = append(os.Environ(), "HICT_TEST_TOKEN="+hex.EncodeToString(token[:]), "HICT_TEST_CONTROLLER_PID="+strconv.Itoa(os.Getpid()))
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() { cmd.Process.Kill() }()
	l.SetDeadline(time.Now().Add(10 * time.Second))
	conn, err := l.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b, err := AcceptBridge(ctx, conn, token, uint32(cmd.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	// An operator can leave a connected controller idle between commands.
	time.Sleep(6 * time.Second)
	st, err := b.Status(ctx)
	if err != nil || st.Code != CodeNativePending || st.Gates != 0 || st.Generation != 0 {
		t.Fatalf("status: %+v %v", st, err)
	}
	r, err := b.Prepare(ctx, testPair())
	if err != nil || r.Code != CodeNativePending || r.Pair != (AssetPair{}) {
		t.Fatalf("prepare: %+v %v", r, err)
	}
	r, err = b.Initialize(ctx, testPair())
	if err != nil || r.Code != CodeNativePending || r.Pair != (AssetPair{}) {
		t.Fatalf("initialize transport: %+v %v", r, err)
	}
	b.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("DLL worker/harness did not exit")
	}
}

func TestNativeRejectsMalformedCommandsAndStops(t *testing.T) {
	for _, kind := range []string{"oversize", "wrong_secret", "repeated_id", "stop_idle", "partial_header", "partial_payload"} {
		t.Run(kind, func(t *testing.T) {
			dll, harness := os.Getenv("HOSTCTL_TEST_DLL"), os.Getenv("HOSTCTL_TEST_HARNESS")
			if dll == "" || harness == "" {
				t.Skip("enable with built DLL and test-owned harness paths")
			}
			l, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			var token [32]byte
			token[0] = 1
			cmd := exec.Command(harness, dll, strconv.Itoa(l.Addr().(*net.TCPAddr).Port))
			var diagnostic bytes.Buffer
			cmd.Stderr = &diagnostic
			cmd.Env = append(os.Environ(), "HICT_TEST_TOKEN="+hex.EncodeToString(token[:]), "HICT_TEST_CONTROLLER_PID="+strconv.Itoa(os.Getpid()))
			if kind == "stop_idle" {
				cmd.Env = append(cmd.Env, "HICT_TEST_STOP=1")
			}
			cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
			input, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Kill()
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			l.SetDeadline(time.Now().Add(5 * time.Second))
			conn, err := l.AcceptTCP()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			b, err := AcceptBridge(ctx, conn, token, uint32(cmd.Process.Pid))
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			conn.SetDeadline(time.Now().Add(7 * time.Second))
			switch kind {
			case "oversize":
				var length [4]byte
				binary.LittleEndian.PutUint32(length[:], 113)
				_, err = conn.Write(length[:])
			case "wrong_secret":
				token[0]++
				err = EncodeRequest(conn, Request{Op: OpStatus, ID: 2, Token: token})
			case "repeated_id":
				if _, err = b.Status(ctx); err != nil {
					t.Fatal(err)
				}
				err = EncodeRequest(conn, Request{Op: OpStatus, ID: 2, Token: token})
			case "stop_idle":
				_, err = input.Write([]byte{'\n'})
			case "partial_header":
				_, err = conn.Write([]byte{48})
			case "partial_payload":
				_, err = conn.Write([]byte{48, 0, 0, 0, 'H'})
			}
			if err != nil {
				t.Fatal(err)
			}
			var first [1]byte
			n, err := conn.Read(first[:])
			if n != 0 || err == nil {
				t.Fatal("rejected command or stop produced a reply")
			}
			select {
			case err := <-done:
				if kind == "stop_idle" && err != nil {
					t.Fatalf("%v: %s", err, diagnostic.String())
				}
				if kind != "stop_idle" && err == nil {
					t.Fatal("malformed command accepted")
				}
			case <-ctx.Done():
				t.Fatal("worker did not stop")
			}
		})
	}
}

func TestNativeStopDuringStartup(t *testing.T) {
	dll, harness := os.Getenv("HOSTCTL_TEST_DLL"), os.Getenv("HOSTCTL_TEST_HARNESS")
	if dll == "" || harness == "" {
		t.Skip("enable with built DLL and test-owned harness paths")
	}
	l, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, harness, dll, strconv.Itoa(l.Addr().(*net.TCPAddr).Port))
	var token [32]byte
	token[0] = 1
	cmd.Env = append(os.Environ(), "HICT_TEST_TOKEN="+hex.EncodeToString(token[:]), "HICT_TEST_CONTROLLER_PID="+strconv.Itoa(os.Getpid()), "HICT_TEST_STOP=2")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
}

func TestNativeRejectsWrongControllerPID(t *testing.T) {
	dll, harness := os.Getenv("HOSTCTL_TEST_DLL"), os.Getenv("HOSTCTL_TEST_HARNESS")
	if dll == "" || harness == "" {
		t.Skip("enable with built DLL and test-owned harness paths")
	}
	l, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	var token [32]byte
	for i := range token {
		token[i] = byte(i + 1)
	}
	cmd := exec.Command(harness, dll, strconv.Itoa(l.Addr().(*net.TCPAddr).Port))
	cmd.Env = append(os.Environ(), "HICT_TEST_TOKEN="+hex.EncodeToString(token[:]), "HICT_TEST_CONTROLLER_PID=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	l.SetDeadline(time.Now().Add(5 * time.Second))
	conn, err := l.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var first [1]byte
	n, err := conn.Read(first[:])
	if n != 0 || err == nil {
		t.Fatal("DLL disclosed handshake bytes to wrong controller PID")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("wrong controller PID accepted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("rejected worker did not exit")
	}
}
