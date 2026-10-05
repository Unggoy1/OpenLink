package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"halocommunity/connect"
)

// DefaultDirectory is the community directory used until the player changes it.
// HICOMM_DIRECTORY overrides it.
const DefaultDirectory = "https://openlink.unggoy.xyz"

// Settings are stored per user.
type Settings struct {
	Directory  string   `json:"directory"`
	Mode       string   `json:"mode"`       // loopback or broadcast
	InstallDir string   `json:"installDir"` // empty = search the usual Steam libraries
	Favorites  []string `json:"favorites"`  // server keys (host:port), stable across re-registration
	// MuteVoteSound turns off the chime played when a playlist vote opens.
	MuteVoteSound bool `json:"muteVoteSound"`
}

func settingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "OpenLink", "settings.json"), nil
}

func loadSettings() Settings {
	s := Settings{Directory: DefaultDirectory, Mode: connect.ModeLoopback}
	if v := os.Getenv("HICOMM_DIRECTORY"); v != "" {
		s.Directory = v
	}
	if p, err := settingsPath(); err == nil {
		if b, err := os.ReadFile(p); err == nil {
			json.Unmarshal(b, &s)
		}
	}
	if s.Mode == "" {
		s.Mode = connect.ModeLoopback
	}
	return s
}

func saveSettings(s Settings) error {
	p, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}
