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
	"regexp"
	"strings"
)

// Install is a game install root (the directory holding version.txt and steam_appid.txt).
type Install struct{ Root string }

// FindInstall returns the given directory if valid. Without one it looks in
// every Steam library that Steam itself lists (libraryfolders.vdf), so any
// drive or folder the player chose for their library is found.
func FindInstall(dir string) (Install, error) {
	cands := []string{dir}
	if dir == "" {
		cands = installCandidates()
	}
	for _, d := range cands {
		if _, err := os.Stat(filepath.Join(d, "game", "HaloInfinite.exe")); err == nil {
			return Install{Root: d}, nil
		}
	}
	if dir != "" {
		return Install{}, fmt.Errorf("no game/HaloInfinite.exe under %s", dir)
	}
	return Install{}, errors.New("game install not found in any Steam library; pass -install <root>")
}

// steamRoots returns the Steam installation folders to read library lists
// from (paths_*.go). A variable so tests can point it at a fake Steam.
var steamRoots = platformSteamRoots

// installCandidates lists where the game would be in each known Steam library.
func installCandidates() []string {
	var out []string
	seen := map[string]bool{}
	add := func(library string) {
		p := filepath.Join(library, "steamapps", "common", "Halo Infinite")
		if k := strings.ToLower(filepath.Clean(p)); !seen[k] {
			seen[k] = true
			out = append(out, p)
		}
	}
	readRoots := map[string]bool{}
	for _, root := range steamRoots() {
		if k := strings.ToLower(filepath.Clean(root)); readRoots[k] {
			continue // the registry and the default often name the same folder
		} else {
			readRoots[k] = true
		}
		add(root) // the Steam folder is itself a library
		for _, lib := range libraryFolders(filepath.Join(root, "steamapps", "libraryfolders.vdf")) {
			add(lib)
		}
	}
	return out
}

// vdfPath matches a library entry in libraryfolders.vdf: "path" "D:\\SteamLibrary".
var vdfPath = regexp.MustCompile(`"path"\s+"((?:[^"\\]|\\.)*)"`)

// libraryFolders returns the library paths listed in a Steam libraryfolders.vdf.
func libraryFolders(vdf string) []string {
	b, err := os.ReadFile(vdf)
	if err != nil {
		return nil
	}
	unescape := strings.NewReplacer(`\\`, `\`, `\"`, `"`)
	var out []string
	for _, m := range vdfPath.FindAllStringSubmatch(string(b), -1) {
		out = append(out, unescape.Replace(m[1]))
	}
	return out
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
