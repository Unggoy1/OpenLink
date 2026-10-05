package game

import (
	"os"
	"path/filepath"
)

// platformSteamRoots returns the usual Steam folders on Linux. The game runs
// under Proton from a normal Steam library; the install layout (version.txt,
// game\HaloInfinite.exe) is the same as on Windows.
func platformSteamRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".local", "share", "Steam"),
		filepath.Join(home, ".steam", "steam"),
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"), // Flatpak
		filepath.Join(home, "snap", "steam", "common", ".local", "share", "Steam"),                // Snap
	}
}
