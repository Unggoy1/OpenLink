package main

import (
	"slices"
	"testing"
)

func TestParseHotkey(t *testing.T) {
	ok := []struct {
		in, norm string
		mods, vk uint32
	}{
		{"Ctrl+Alt+1", "Ctrl+Alt+1", modCtrl | modAlt, '1'},
		{"alt+ctrl+v", "Ctrl+Alt+V", modCtrl | modAlt, 'V'},
		{" Shift + Ctrl + F10 ", "Ctrl+Shift+F10", modCtrl | modShift, 0x79},
		{"F8", "F8", 0, 0x77},
		{"Pause", "Pause", 0, 0x13},
		{"Win+Num3", "Win+Num3", modWin, 0x63},
		{"Ctrl+`", "Ctrl+`", modCtrl, 0xC0},
	}
	for _, c := range ok {
		h, norm, err := parseHotkey(c.in)
		if err != nil || norm != c.norm || h.mods != c.mods || h.vk != c.vk {
			t.Errorf("parseHotkey(%q) = %+v %q %v; want %q mods %x vk %x", c.in, h, norm, err, c.norm, c.mods, c.vk)
		}
	}
	for _, in := range []string{"", "1", "V", "Shift+1", "Ctrl+", "Ctrl+Alt", "Hyper+1", "Ctrl+Tab", "Ctrl+Alt+1+2"} {
		if _, _, err := parseHotkey(in); err == nil {
			t.Errorf("parseHotkey(%q) accepted", in)
		}
	}
}

func TestNormalizeOverlay(t *testing.T) {
	var s Settings
	if err := normalizeOverlay(&s); err != nil {
		t.Fatal(err)
	}
	if s.OverlayMode != defaultOverlayMode || s.OverlayCorner != defaultOverlayCorner || s.OverlayOpenKey != defaultOverlayOpen ||
		!slices.Equal(s.OverlayVoteKeys, defaultOverlayVoteKeys()) {
		t.Fatalf("defaults: %+v", s)
	}

	s = Settings{OverlayMode: overlayPassive, OverlayVoteKeys: []string{"ctrl+alt+1", "Ctrl+Alt+2", "F7", "Ctrl+1"}}
	if err := normalizeOverlay(&s); err != nil || s.OverlayVoteKeys[0] != "Ctrl+Alt+1" {
		t.Fatalf("passive: %v %+v", err, s)
	}

	for _, bad := range []Settings{
		{OverlayMode: "always"},
		{OverlayVoteKeys: []string{"Ctrl+Alt+1", "Ctrl+Alt+1", "Ctrl+Alt+3", "Ctrl+Alt+4"}},
		{OverlayVoteKeys: []string{"Ctrl+Alt+1"}},
		{OverlayOpenKey: "V"},
	} {
		if err := normalizeOverlay(&bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}
