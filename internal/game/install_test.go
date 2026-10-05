package game

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A game in a second Steam library (any drive or folder) is found through
// Steam's own libraryfolders.vdf, with no built-in guesses.
func TestFindInstallReadsSteamLibraries(t *testing.T) {
	base := t.TempDir()
	steam := filepath.Join(base, "Steam")
	library := filepath.Join(base, "Games 2", "My Library") // spaces and backslashes survive the VDF escaping
	game := filepath.Join(library, "steamapps", "common", "Halo Infinite")
	os.MkdirAll(filepath.Join(steam, "steamapps"), 0o755)
	os.MkdirAll(filepath.Join(game, "game"), 0o755)
	os.WriteFile(filepath.Join(game, "game", "HaloInfinite.exe"), nil, 0o644)

	esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	vdf := "\"libraryfolders\"\n{\n\t\"0\"\n\t{\n\t\t\"path\"\t\t\"" + esc.Replace(steam) +
		"\"\n\t}\n\t\"1\"\n\t{\n\t\t\"path\"\t\t\"" + esc.Replace(library) + "\"\n\t\t\"apps\"\n\t\t{\n\t\t\t\"1240440\"\t\t\"1\"\n\t\t}\n\t}\n}\n"
	os.WriteFile(filepath.Join(steam, "steamapps", "libraryfolders.vdf"), []byte(vdf), 0o644)

	old := steamRoots
	steamRoots = func() []string { return []string{steam, steam} }
	defer func() { steamRoots = old }()

	in, err := FindInstall("")
	if err != nil || in.Root != game {
		t.Fatalf("FindInstall: %q %v (candidates %q)", in.Root, err, installCandidates())
	}
	if n := len(installCandidates()); n != 2 {
		t.Fatalf("expected 2 candidates (Steam folder and the library), got %d", n)
	}

	steamRoots = func() []string { return []string{filepath.Join(base, "nowhere")} }
	if _, err := FindInstall(""); err == nil {
		t.Fatal("found a game without any Steam library")
	}
}
