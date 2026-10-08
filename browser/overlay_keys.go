package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"halocommunity/vote"
)

// The vote overlay (Windows only, overlay_windows.go). New installs start in
// defaultOverlayMode: passive on Windows (user decision 2026-10-05), off elsewhere.
const (
	overlayOff         = "off"
	overlayPassive     = "passive"     // shows itself; vote with one hotkey per choice
	overlayInteractive = "interactive" // a hotkey opens it; vote with the keyboard or mouse
)

var overlayCorners = []string{"top-left", "top-right", "bottom-left", "bottom-right"}

const (
	defaultOverlayCorner = "top-left" // covers the lobby's map preview, which can show another map than the vote's
	defaultOverlayOpen   = "Ctrl+Alt+V"
)

func defaultOverlayVoteKeys() []string {
	keys := make([]string, vote.MaxOptions)
	for i := range keys {
		keys[i] = "Ctrl+Alt+" + strconv.Itoa(i+1)
	}
	return keys
}

// Hotkeys are written like "Ctrl+Alt+1": modifiers in the order Ctrl, Alt,
// Shift, Win, then one key. The key names match what the settings panel
// records from a key press (frontend/src/HotkeyInput.svelte).
const (
	modAlt   = 0x1
	modCtrl  = 0x2
	modShift = 0x4
	modWin   = 0x8
)

type hotkey struct {
	mods uint32 // MOD_* flags for RegisterHotKey
	vk   uint32 // Windows virtual-key code
}

var hotkeyVK = func() map[string]uint32 {
	m := map[string]uint32{
		"`": 0xC0, "Insert": 0x2D, "Delete": 0x2E, "Home": 0x24, "End": 0x23,
		"PageUp": 0x21, "PageDown": 0x22, "Pause": 0x13,
	}
	for c := 'A'; c <= 'Z'; c++ {
		m[string(c)] = uint32(c)
	}
	for c := '0'; c <= '9'; c++ {
		m[string(c)] = uint32(c)
		m["Num"+string(c)] = 0x60 + uint32(c-'0')
	}
	for i := 1; i <= 24; i++ {
		m["F"+strconv.Itoa(i)] = 0x6F + uint32(i)
	}
	return m
}()

// parseHotkey reads a hotkey and returns it with its normalized spelling.
func parseHotkey(s string) (hotkey, string, error) {
	parts := strings.Split(strings.TrimSpace(s), "+")
	var h hotkey
	key := ""
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if i < len(parts)-1 {
			switch strings.ToLower(p) {
			case "ctrl", "control":
				h.mods |= modCtrl
			case "alt":
				h.mods |= modAlt
			case "shift":
				h.mods |= modShift
			case "win", "meta":
				h.mods |= modWin
			default:
				return hotkey{}, "", fmt.Errorf("%q: unknown modifier %q", s, p)
			}
			continue
		}
		for name := range hotkeyVK {
			if strings.EqualFold(name, p) {
				key = name
			}
		}
	}
	if key == "" {
		return hotkey{}, "", fmt.Errorf("%q: unsupported key", s)
	}
	h.vk = hotkeyVK[key]
	// A bare key would be taken from every app (and from the game's own
	// controls) while a vote is open, so only F-keys and Pause may go alone.
	bareOK := key == "Pause" || (key[0] == 'F' && len(key) > 1)
	if h.mods&(modCtrl|modAlt|modWin) == 0 && !bareOK {
		return hotkey{}, "", fmt.Errorf("%q: add Ctrl, Alt or Win (only F-keys and Pause can be used alone)", s)
	}
	var b strings.Builder
	for _, m := range []struct {
		flag uint32
		name string
	}{{modCtrl, "Ctrl"}, {modAlt, "Alt"}, {modShift, "Shift"}, {modWin, "Win"}} {
		if h.mods&m.flag != 0 {
			b.WriteString(m.name + "+")
		}
	}
	b.WriteString(key)
	return h, b.String(), nil
}

// normalizeOverlay fills in defaults and checks the overlay settings.
func normalizeOverlay(s *Settings) error {
	if s.OverlayMode == "" {
		s.OverlayMode = defaultOverlayMode
	}
	if s.OverlayMode != overlayOff && s.OverlayMode != overlayPassive && s.OverlayMode != overlayInteractive {
		return fmt.Errorf("unknown overlay mode %q", s.OverlayMode)
	}
	if !slices.Contains(overlayCorners, s.OverlayCorner) {
		s.OverlayCorner = defaultOverlayCorner
	}
	if strings.TrimSpace(s.OverlayOpenKey) == "" {
		s.OverlayOpenKey = defaultOverlayOpen
	}
	_, norm, err := parseHotkey(s.OverlayOpenKey)
	if err != nil {
		return fmt.Errorf("overlay key: %w", err)
	}
	s.OverlayOpenKey = norm
	if len(s.OverlayVoteKeys) == 0 {
		s.OverlayVoteKeys = defaultOverlayVoteKeys()
	}
	if len(s.OverlayVoteKeys) != vote.MaxOptions {
		return fmt.Errorf("expected %d vote keys", vote.MaxOptions)
	}
	keys := make([]string, len(s.OverlayVoteKeys))
	for i, k := range s.OverlayVoteKeys {
		_, norm, err := parseHotkey(k)
		if err != nil {
			return fmt.Errorf("vote key %d: %w", i+1, err)
		}
		if slices.Contains(keys[:i], norm) {
			return errors.New("each vote key must be different: " + norm)
		}
		keys[i] = norm
	}
	s.OverlayVoteKeys = keys
	return nil
}
