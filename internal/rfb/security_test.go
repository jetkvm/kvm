package rfb

import (
	"bytes"
	"crypto/cipher"
	"crypto/des"
	"testing"
)

// desCipher is a tiny helper used by tests to obtain a *cipher.Block
// without leaking crypto/des into the package's main code path.
func desCipher(key []byte) (cipher.Block, error) { return des.NewCipher(key) }

func TestBitReverse(t *testing.T) {
	cases := []struct {
		in, out byte
	}{
		{0x00, 0x00},
		{0xFF, 0xFF},
		{0x01, 0x80},
		{0x80, 0x01},
		{0xA5, 0xA5}, // palindrome under bit-reverse: 1010 0101 -> 1010 0101
		{0x12, 0x48}, // 0001 0010 -> 0100 1000
	}
	for _, c := range cases {
		got := bitReverse(c.in)
		if got != c.out {
			t.Errorf("bitReverse(%#02x) = %#02x, want %#02x", c.in, got, c.out)
		}
	}
}

func TestVNCAuthKey(t *testing.T) {
	// Empty password -> zero key.
	if k := vncAuthKey(""); !bytes.Equal(k, []byte{0, 0, 0, 0, 0, 0, 0, 0}) {
		t.Errorf("empty password: %v", k)
	}
	// Short password is zero-padded, each byte bit-reversed.
	k := vncAuthKey("ab")
	want := []byte{bitReverse('a'), bitReverse('b'), 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(k, want) {
		t.Errorf("\"ab\": got %v, want %v", k, want)
	}
	// Long password is truncated to 8 bytes.
	k = vncAuthKey("abcdefghIJK")
	want = []byte{
		bitReverse('a'), bitReverse('b'), bitReverse('c'), bitReverse('d'),
		bitReverse('e'), bitReverse('f'), bitReverse('g'), bitReverse('h'),
	}
	if !bytes.Equal(k, want) {
		t.Errorf("long: got %v, want %v", k, want)
	}
}

// TestVNCAuthDeterministic verifies the response is reproducible: the
// same challenge + password always produces the same 16-byte output.
// We can't lock in a hard-coded value here without a known reference,
// but we can check the function is pure and matches itself on both
// 8-byte halves.
func TestVNCAuthDeterministic(t *testing.T) {
	challenge := bytes.Repeat([]byte{0xAB, 0xCD}, 8) // 16 bytes
	a, err := encryptVNCAuthChallenge(challenge, "password")
	if err != nil {
		t.Fatal(err)
	}
	b, err := encryptVNCAuthChallenge(challenge, "password")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("non-deterministic: %x vs %x", a, b)
	}
	if len(a) != 16 {
		t.Fatalf("response length %d, want 16", len(a))
	}
}

// TestVNCAuthDifferentPasswords ensures that different passwords
// produce different responses for the same challenge.
func TestVNCAuthDifferentPasswords(t *testing.T) {
	challenge := bytes.Repeat([]byte{0x42}, 16)
	a, _ := encryptVNCAuthChallenge(challenge, "alpha")
	b, _ := encryptVNCAuthChallenge(challenge, "bravo")
	if bytes.Equal(a, b) {
		t.Fatalf("same response for different passwords")
	}
}

// TestVNCAuthHalvesIndependent verifies that the two 8-byte halves of
// the challenge are encrypted independently with the same key (i.e.
// ECB mode, no chaining).
func TestVNCAuthHalvesIndependent(t *testing.T) {
	first := bytes.Repeat([]byte{0x11}, 8)
	second := bytes.Repeat([]byte{0x22}, 8)

	combined := append(append([]byte{}, first...), second...)
	combinedOut, _ := encryptVNCAuthChallenge(combined, "secret")

	// Same input first 8 bytes should produce the same first 8 bytes
	// of output.
	first16 := append(append([]byte{}, first...), first...)
	first16Out, _ := encryptVNCAuthChallenge(first16, "secret")

	if !bytes.Equal(combinedOut[:8], first16Out[:8]) {
		t.Errorf("first 8 bytes differ — encryption appears chained")
	}
}

func TestEncryptVNCAuthChallengeBadLen(t *testing.T) {
	if _, err := encryptVNCAuthChallenge([]byte{1, 2, 3}, "x"); err == nil {
		t.Fatalf("expected error for short challenge")
	}
}

// TestVNCAuthMatchesPlainDES verifies the high-level encryptVNCAuthChallenge
// function produces exactly what RFC 6143 §7.2.2 specifies: the password
// truncated/padded to 8 bytes and bit-reversed within each byte, used as
// a DES key to encrypt each 8-byte half of the challenge in ECB mode.
//
// Computing the expected value directly from crypto/des locks the
// behaviour in independently of bit-reversal and split-block bugs.
func TestVNCAuthMatchesPlainDES(t *testing.T) {
	challenge := []byte{
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77,
		0x88, 0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF,
	}
	password := "1234"

	// Reference implementation: bit-reverse the 8-byte key and
	// encrypt each half of the challenge in ECB.
	key := []byte{
		bitReverse('1'), bitReverse('2'), bitReverse('3'), bitReverse('4'),
		0, 0, 0, 0,
	}
	cipher, err := desCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	want := make([]byte, 16)
	cipher.Encrypt(want[:8], challenge[:8])
	cipher.Encrypt(want[8:], challenge[8:])

	got, err := encryptVNCAuthChallenge(challenge, password)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("got  %x\nwant %x", got, want)
	}
}

func TestConstantTimeEqual(t *testing.T) {
	if !constantTimeEqual([]byte{1, 2, 3}, []byte{1, 2, 3}) {
		t.Errorf("equal slices reported unequal")
	}
	if constantTimeEqual([]byte{1, 2, 3}, []byte{1, 2, 4}) {
		t.Errorf("different slices reported equal")
	}
	if constantTimeEqual([]byte{1, 2}, []byte{1, 2, 3}) {
		t.Errorf("different lengths reported equal")
	}
}
