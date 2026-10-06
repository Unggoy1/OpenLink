package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"halocommunity/internal/game"
)

// The one Halo Infinite build openlink-control.dll supports: version.txt and
// the SHA-256 of game/HaloInfinite.exe, the same pin as wanted[] in
// native/hostctl/game_b002.cpp (TestSupportedBuildMatchesDLL keeps them equal).
const (
	supportedBuild      = "269225.26.04.08.1618-1.hi_1_13_0"
	supportedGameSHA256 = "5da518ec21f5ab7baeb021aeed2b9892eeb65320dc17d2682f8f55271c203da8"
)

// checkGameBuild refuses an install the control DLL would reject, with a
// message a host can act on, before any server is started.
func checkGameBuild(in game.Install, build, wantBuild, wantSHA256 string) error {
	if build != wantBuild {
		return fmt.Errorf("Halo Infinite has been updated (installed build %s). This OpenLink Server supports build %s only, "+
			"so it will not start a server. Wait for an OpenLink Server update for the new Halo build", build, wantBuild)
	}
	f, err := os.Open(filepath.Join(in.Root, "game", "HaloInfinite.exe"))
	if err != nil {
		return fmt.Errorf("check game build: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("check game build: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != wantSHA256 {
		return fmt.Errorf("game/HaloInfinite.exe does not match Halo Infinite build %s (SHA-256 %s…): "+
			"in Steam, verify the integrity of the game files, then start OpenLink Server again", build, got[:16])
	}
	return nil
}
