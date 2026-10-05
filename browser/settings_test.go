package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSettingsDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)         // os.UserConfigDir on Windows
	t.Setenv("XDG_CONFIG_HOME", dir) // and on Linux
	t.Setenv("HICOMM_DIRECTORY", "")
	p := filepath.Join(dir, "OpenLink", "settings.json")
	os.MkdirAll(filepath.Dir(p), 0o755)

	for saved, want := range map[string]string{
		"": DefaultDirectory, // nothing saved
		`{"directory": "` + oldDefaultDirectory + `"}`: DefaultDirectory, // the old default moves
		`{"directory": "https://dir.example"}`:         "https://dir.example",
	} {
		os.Remove(p)
		if saved != "" {
			os.WriteFile(p, []byte(saved), 0o644)
		}
		if got := loadSettings().Directory; got != want {
			t.Errorf("saved %s: directory %q, want %q", saved, got, want)
		}
	}
}
