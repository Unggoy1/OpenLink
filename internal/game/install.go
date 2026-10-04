// Package game locates a Halo Infinite install, reads its build and starts the
// retail LAN server process.
package game

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DefaultInstallDirs are tried when no install directory is given.
var DefaultInstallDirs = []string{
	`C:\Program Files (x86)\Steam\steamapps\common\Halo Infinite`,
	`D:\SteamLibrary\steamapps\common\Halo Infinite`,
	`C:\SteamLibrary\steamapps\common\Halo Infinite`,
	`E:\SteamLibrary\steamapps\common\Halo Infinite`,
}

// Install is a game install root (the directory holding version.txt and steam_appid.txt).
type Install struct{ Root string }

// FindInstall returns the given directory if valid, else the first default that is.
func FindInstall(dir string) (Install, error) {
	cands := DefaultInstallDirs
	if dir != "" {
		cands = []string{dir}
	}
	for _, d := range cands {
		if _, err := os.Stat(filepath.Join(d, "game", "HaloInfinite.exe")); err == nil {
			return Install{Root: d}, nil
		}
	}
	if dir != "" {
		return Install{}, fmt.Errorf("no game/HaloInfinite.exe under %s", dir)
	}
	return Install{}, errors.New("game install not found; pass -install <root>")
}

// Build returns the first line of version.txt, e.g. "269225.26.04.08.1618-1.hi_1_13_0".
// Server and clients must match exactly.
func (in Install) Build() (string, error) {
	f, err := os.Open(filepath.Join(in.Root, "version.txt"))
	if err != nil {
		return "", err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	if s.Scan() {
		if b := strings.TrimSpace(s.Text()); b != "" {
			return b, nil
		}
	}
	return "", errors.New("version.txt is empty")
}

// BootstrapLog is the path of the log the server writes its allocation states to.
func (in Install) BootstrapLog() string { return filepath.Join(in.Root, "BootstrapLog.txt") }

// ServerCommand builds the standalone LAN server command: the
// working directory must be the install root, otherwise the game asks Steam to
// relaunch it as a normal client.
func (in Install) ServerCommand(sandbox, bindIP string) *exec.Cmd {
	args := []string{"-server", "-console", "-lan", "-lan_sandbox", sandbox}
	if bindIP != "" {
		args = append(args, "-bindip", bindIP)
	}
	cmd := exec.Command(filepath.Join(in.Root, "game", "HaloInfinite.exe"), args...)
	cmd.Dir = in.Root
	setNewConsole(cmd)
	return cmd
}

// Owner is a local UDP socket and the process that owns it.
type Owner struct {
	PID int
	IP  net.IP // bound address; unspecified (0.0.0.0 or ::) means all addresses
}

// Covers reports whether this socket uses the given local IP ("" = any IP).
func (o Owner) Covers(ip string) bool {
	if ip == "" || o.IP.IsUnspecified() {
		return true
	}
	want := net.ParseIP(ip)
	return want != nil && o.IP.Equal(want)
}
