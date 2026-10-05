package main

import (
	"bytes"
	"encoding/binary"
	"math"
)

// chimeWAV returns a short two-note chime as a 16-bit mono WAV, generated
// rather than shipped as an asset.
func chimeWAV() []byte {
	const rate = 44100
	notes := []struct {
		freq float64
		secs float64
	}{{880, 0.12}, {1318.5, 0.22}}
	var pcm []int16
	for _, n := range notes {
		count := int(n.secs * rate)
		for i := 0; i < count; i++ {
			t := float64(i) / rate
			attack := math.Min(1, t/0.01)        // 10 ms fade in
			release := math.Exp(-t * 9 / n.secs) // decay over the note
			pcm = append(pcm, int16(0.3*32767*attack*release*math.Sin(2*math.Pi*n.freq*t)))
		}
	}
	var b bytes.Buffer
	data := len(pcm) * 2
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+data))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(rate), uint32(rate * 2), uint16(2), uint16(16)} {
		binary.Write(&b, binary.LittleEndian, v) // fmt chunk: size, PCM, mono, rate, byte rate, align, bits
	}
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(data))
	binary.Write(&b, binary.LittleEndian, pcm)
	return b.Bytes()
}
