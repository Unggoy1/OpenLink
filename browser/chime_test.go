package main

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestChimeWAV(t *testing.T) {
	w := chimeWAV()
	if !bytes.HasPrefix(w, []byte("RIFF")) || string(w[8:16]) != "WAVEfmt " || string(w[36:40]) != "data" {
		t.Fatal("bad WAV header")
	}
	if riff := binary.LittleEndian.Uint32(w[4:]); int(riff) != len(w)-8 {
		t.Fatalf("RIFF size %d for %d bytes", riff, len(w))
	}
	if data := binary.LittleEndian.Uint32(w[40:]); int(data) != len(w)-44 || data == 0 {
		t.Fatalf("data size %d for %d bytes", data, len(w))
	}
}
