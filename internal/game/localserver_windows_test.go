package game

import (
	"bufio"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestMain lets the test binary act as a stand-in server: copied as
// openlink-server.exe and run with HOLD_UDP_1343 set, it holds the port
// until its stdin closes.
func TestMain(m *testing.M) {
	if os.Getenv("HOLD_UDP_1343") != "" {
		c, err := net.ListenPacket("udp4", "127.0.0.9:1343")
		if err != nil {
			os.Exit(3)
		}
		defer c.Close()
		os.Stdout.WriteString("ready\n")
		io.Copy(io.Discard, os.Stdin)
		return
	}
	os.Exit(m.Run())
}

func TestLocalServerRunning(t *testing.T) {
	if LocalServerRunning() {
		t.Skip("a server already runs on this PC")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "openlink-server.exe")
	b, err := os.ReadFile(self)
	if err != nil || os.WriteFile(exe, b, 0o755) != nil {
		t.Fatal("copy test binary")
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "HOLD_UDP_1343=1")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	defer stdin.Close()
	if line, _ := bufio.NewReader(stdout).ReadString('\n'); line != "ready\n" {
		t.Fatalf("stand-in did not bind: %q", line)
	}
	if !LocalServerRunning() {
		t.Fatal("openlink-server.exe holding UDP 1343 not detected")
	}
	// Our own socket on 1343 does not count.
	stdin.Close()
	cmd.Wait()
	own, err := net.ListenPacket("udp4", "127.0.0.9:1343")
	if err != nil {
		t.Fatal(err)
	}
	defer own.Close()
	if LocalServerRunning() {
		t.Fatal("own socket counted as a local server")
	}
}
