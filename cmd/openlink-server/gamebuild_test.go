package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"halocommunity/internal/game"
)

func TestCheckGameBuild(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "game"), 0o755)
	exe := []byte("not really a game")
	os.WriteFile(filepath.Join(root, "game", "HaloInfinite.exe"), exe, 0o644)
	sum := sha256.Sum256(exe)
	in := game.Install{Root: root}
	if err := checkGameBuild(in, "b1", "b1", hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if err := checkGameBuild(in, "b2", "b1", hex.EncodeToString(sum[:])); err == nil || !strings.Contains(err.Error(), "has been updated (installed build b2)") {
		t.Fatalf("new build: %v", err)
	}
	if err := checkGameBuild(in, "b1", "b1", strings.Repeat("0", 64)); err == nil || !strings.Contains(err.Error(), "verify the integrity") {
		t.Fatalf("changed exe: %v", err)
	}
}

// The DLL refuses any other executable, so the agent's pin must be the same.
func TestSupportedBuildMatchesDLL(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "native", "hostctl", "game_b002.cpp"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)wanted\[32\]=\{(.*?)\};`).FindSubmatch(src)
	if m == nil {
		t.Fatal("wanted[32] not found in game_b002.cpp")
	}
	var hexs strings.Builder
	for _, b := range regexp.MustCompile(`0x([0-9a-f]{2})`).FindAllSubmatch(m[1], -1) {
		hexs.Write(b[1])
	}
	if hexs.String() != supportedGameSHA256 {
		t.Fatalf("DLL pins %s, agent pins %s", hexs.String(), supportedGameSHA256)
	}
}
