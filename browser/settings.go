package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"halocommunity/connect"
	"halocommunity/internal/api"
)

// DefaultDirectory is the community directory used until the player changes it.
// OPENLINK_DIRECTORY (or HICOMM_DIRECTORY) overrides it.
const DefaultDirectory = "https://openlink-dir.unggoy.xyz"

// Settings are stored per user.
type Settings struct {
	Directory  string   `json:"directory"`
	Mode       string   `json:"mode"`       // loopback or broadcast
	InstallDir string   `json:"installDir"` // empty = search the usual Steam libraries
	Favorites  []string `json:"favorites"`  // server keys (host:port), stable across re-registration
	// MuteVoteSound turns off the chime played when a playlist vote opens.
	MuteVoteSound bool `json:"muteVoteSound"`
	// The in-game vote overlay (Windows only): off, passive or interactive.
	OverlayMode     string   `json:"overlayMode"`
	OverlayCorner   string   `json:"overlayCorner"`
	OverlayOpenKey  string   `json:"overlayOpenKey"`  // interactive: opens the overlay
	OverlayVoteKeys []string `json:"overlayVoteKeys"` // passive: one per choice
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
	if v := api.Getenv("DIRECTORY"); v != "" {
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
	if normalizeOverlay(&s) != nil {
		// Hand-edited and invalid: start with the overlay off and default keys.
		s.OverlayMode, s.OverlayOpenKey, s.OverlayVoteKeys = overlayOff, "", nil
		normalizeOverlay(&s)
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
