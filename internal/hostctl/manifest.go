// Package hostctl verifies on-disk inputs for future server control. It never
// opens a process, loads a DLL, or calls a game function.
package hostctl

import "encoding/hex"

type Manifest struct {
	SHA256    [32]byte
	Machine   uint16
	ImageBase uint64
	Targets   []Target
}

type Target struct {
	Name   string
	RVA    uint64
	Prefix [16]byte
}

// Return a fresh manifest so callers cannot change the production allowlist.
// The prefixes are file fingerprints, not signatures for scanning other builds.
func b002Manifest() Manifest {
	m := Manifest{Machine: 0x8664, ImageBase: 0x140000000}
	decodeHex(m.SHA256[:], "5DA518EC21F5AB7BAEB021AEED2B9892EEB65320DC17D2682F8F55271C203DA8")
	for _, t := range []struct {
		name   string
		rva    uint64
		prefix string
	}{
		{"HostAllowedToStart", 0x2e937f8, "0fb61509fcf201803d3aa50102000fb6"},
		{"HostInitGameDifficulty", 0x2e93814, "4883ec28803d21a5010200740f488bd1"},
		{"HostInitGameType", 0x2e93838, "4883ec28803dfda4010200740f488bd1"},
		{"HostInitGameVariant", 0x2e9385c, "4883ec28803dd9a40102007415488bd1"},
		{"HostInitMapName", 0x2e93884, "4883ec28803db1a40102007405e8327f"},
		{"HostInitUsersRequiredToStart", 0x2e9389c, "8b05d2f9f201803d97a40102000f45c1"},
		{"HostInitConfigured (tentative)", 0x9a61cc, "48895c24084889742410574883ec2048"},
	} {
		target := Target{Name: t.name, RVA: t.rva}
		decodeHex(target.Prefix[:], t.prefix)
		m.Targets = append(m.Targets, target)
	}
	return m
}

func decodeHex(dst []byte, text string) {
	n, err := hex.Decode(dst, []byte(text))
	if err != nil || n != len(dst) {
		panic("invalid built-in host-control fingerprint")
	}
}
