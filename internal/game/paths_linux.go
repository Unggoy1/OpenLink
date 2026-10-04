package game

import (
	"os"
	"path/filepath"
)

// On Linux the game runs under Proton from a normal Steam library; the
// install layout (version.txt, game\HaloInfinite.exe) is the same.
func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	rel := filepath.Join("steamapps", "common", "Halo Infinite")
	DefaultInstallDirs = []string{
		filepath.Join(home, ".local", "share", "Steam", rel),
		filepath.Join(home, ".steam", "steam", rel),
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam", rel), // Flatpak
	}
}
