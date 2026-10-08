package beacon

import (
	"crypto/aes"
	"crypto/cipher"
	"debug/pe"
	"errors"
	"fmt"
	"sync"
	"unicode/utf16"
)

// Every LAN server on a PC broadcasts from that PC's LAN address and port 7117,
// whatever its -bindip (A081), so the source address cannot tell several local
// servers apart. The game seals beacons with AES-128-GCM under a key built into
// the executable (key slot 0xff, B002 global 0x143b8c790; 140b67110 installs it,
// 140bf85ac opens packets): bytes 0-11 are the nonce, byte 12 the key slot,
// 16-31 the tag and the rest the ciphertext, with no additional data. Opening a
// beacon shows the server name the DLL set, which identifies our own server.
const (
	keyVA   = 0x143b8c790
	keySlot = 0xff
	keySize = 16
)

// LoadKey reads the beacon key from the game executable. Use it only with the
// build the agent verified (gamebuild.go): the address is specific to it.
func LoadKey(exe string) ([]byte, error) {
	f, err := pe.Open(exe)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	oh, ok := f.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || keyVA < oh.ImageBase {
		return nil, errors.New("not a 64-bit game executable")
	}
	rva := uint32(keyVA - oh.ImageBase)
	for _, s := range f.Sections {
		if rva >= s.VirtualAddress && rva+keySize <= s.VirtualAddress+s.Size {
			key := make([]byte, keySize)
			if _, err := s.ReadAt(key, int64(rva-s.VirtualAddress)); err != nil {
				return nil, err
			}
			return key, nil
		}
	}
	return nil, fmt.Errorf("beacon key address %#x is not in the executable", keyVA)
}

// Open returns the plaintext of a beacon sealed with key, or false if b is not one.
func Open(key, b []byte) ([]byte, bool) {
	if len(b) < 32 || b[12] != keySlot {
		return nil, false
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, false
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, false
	}
	sealed := append(append([]byte(nil), b[32:]...), b[16:32]...) // Go expects the tag after the ciphertext
	plain, err := gcm.Open(nil, b[:12], sealed, nil)
	return plain, err == nil
}

// ContainsName reports whether plain carries name as UTF-16 or ASCII
// characters at any bit offset (the beacon body is a bit stream).
func ContainsName(plain []byte, name string) bool {
	if name == "" {
		return false
	}
	var le, be []byte
	for _, u := range utf16.Encode([]rune(name)) {
		le = append(le, byte(u), byte(u>>8))
		be = append(be, byte(u>>8), byte(u))
	}
	patterns := [][]byte{le, be, []byte(name)}
	for shift := uint(0); shift < 8; shift++ {
		for _, view := range [][]byte{shifted(plain, shift, false), shifted(plain, shift, true)} {
			for _, p := range patterns {
				if indexBytes(view, p) {
					return true
				}
			}
		}
	}
	return false
}

// shifted returns plain read shift bits further on, with bits taken from the
// low end of each byte first (lsb) or the high end first.
func shifted(plain []byte, shift uint, lsb bool) []byte {
	if shift == 0 {
		return plain
	}
	out := make([]byte, 0, len(plain))
	for i := 0; i+1 < len(plain); i++ {
		if lsb {
			out = append(out, plain[i]>>shift|plain[i+1]<<(8-shift))
		} else {
			out = append(out, plain[i]<<shift|plain[i+1]>>(8-shift))
		}
	}
	return out
}

func indexBytes(s, p []byte) bool {
	for i := 0; i+len(p) <= len(s); i++ {
		if string(s[i:i+len(p)]) == string(p) {
			return true
		}
	}
	return false
}

// Owner keeps only this server's beacons when several servers run on one PC.
// A beacon that opens and carries Name is ours. Others are dropped only after
// one of ours has been seen, so a server whose name was not set yet, or a
// beacon layout this code does not read, keeps the old behaviour (every local
// beacon). Reset starts over, e.g. when the server restarts.
type Owner struct {
	Key  []byte
	Name string

	mu      sync.Mutex
	matched bool
	foreign uint64
}

// Keep reports whether b should be stored as this server's beacon.
func (o *Owner) Keep(b []byte) bool {
	if o == nil || len(o.Key) == 0 || o.Name == "" {
		return true
	}
	plain, ok := Open(o.Key, b)
	if !ok {
		return true
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if ContainsName(plain, o.Name) {
		o.matched = true
		return true
	}
	if o.matched {
		o.foreign++
		return false
	}
	return true
}

// Reset forgets that our beacon was seen.
func (o *Owner) Reset() {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.matched = false
	o.mu.Unlock()
}

// State reports whether our beacon has been identified and how many beacons
// of other local servers were dropped.
func (o *Owner) State() (matched bool, foreign uint64) {
	if o == nil {
		return false, 0
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.matched, o.foreign
}
