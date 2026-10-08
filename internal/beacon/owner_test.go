package beacon

import (
	"crypto/aes"
	"crypto/cipher"
	"testing"
	"unicode/utf16"
)

var testKey = []byte("0123456789abcdef") // a test key, not the game's

// seal builds a datagram the way the game does: nonce, key slot, tag, ciphertext.
func seal(t *testing.T, key, plain []byte, slot byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := []byte("nonce-012345")
	out := gcm.Seal(nil, nonce, plain, nil)
	ct, tag := out[:len(plain)], out[len(plain):]
	b := make([]byte, 32, 32+len(ct))
	copy(b, nonce)
	b[12] = slot
	copy(b[16:], tag)
	return append(b, ct...)
}

// body puts name as UTF-16LE after a 3-byte header, then moves it bits further
// on (low bits first), as a bit stream might.
func body(name string, bits uint) []byte {
	b := []byte{0x0d, 0x00, 0x01}
	for _, u := range utf16.Encode([]rune(name)) {
		b = append(b, byte(u), byte(u>>8))
	}
	b = append(b, 0, 0, 0, 0)
	if bits == 0 {
		return b
	}
	out := make([]byte, len(b)+1)
	for i, v := range b {
		out[i] |= v << bits
		out[i+1] |= v >> (8 - bits)
	}
	return out
}

func TestOpenAndName(t *testing.T) {
	for _, bits := range []uint{0, 3, 7} {
		b := seal(t, testKey, body("Alpha Server", bits), keySlot)
		plain, ok := Open(testKey, b)
		if !ok {
			t.Fatalf("bits %d: did not open", bits)
		}
		if !ContainsName(plain, "Alpha Server") || ContainsName(plain, "Bravo Server") {
			t.Fatalf("bits %d: name match wrong", bits)
		}
	}
	b := seal(t, testKey, body("x", 0), keySlot)
	b[40] ^= 1
	if _, ok := Open(testKey, b); ok {
		t.Fatal("opened a tampered beacon")
	}
	if _, ok := Open(testKey, seal(t, testKey, body("x", 0), 0)); ok {
		t.Fatal("opened a packet with another key slot")
	}
}

func TestOwnerKeepsOnlyOurs(t *testing.T) {
	ours := seal(t, testKey, body("Alpha", 2), keySlot)
	other := seal(t, testKey, body("Bravo", 2), keySlot)
	opaque := []byte("not a sealed beacon, but long enough")
	o := &Owner{Key: testKey, Name: "Alpha"}
	// Until ours is seen everything is kept (name not set yet, or a layout we cannot read).
	if !o.Keep(other) || !o.Keep(opaque) {
		t.Fatal("dropped a beacon before ours was identified")
	}
	if !o.Keep(ours) || o.Keep(other) || !o.Keep(ours) {
		t.Fatal("did not keep only our beacons once ours was seen")
	}
	if !o.Keep(opaque) {
		t.Fatal("dropped a datagram that does not open")
	}
	if matched, foreign := o.State(); !matched || foreign != 1 {
		t.Fatalf("state %v %d", matched, foreign)
	}
	o.Reset()
	if !o.Keep(other) {
		t.Fatal("dropped another beacon after Reset")
	}
	var none *Owner
	if !none.Keep(other) || !(&Owner{Name: "Alpha"}).Keep(other) {
		t.Fatal("an owner without a key must keep everything")
	}
}
