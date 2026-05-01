package rfb

import (
	"crypto/des"
	"crypto/rand"
	"errors"
	"fmt"
)

// ErrAuthFailed is returned when the client's VNCAuth response does
// not match the expected value.
var ErrAuthFailed = errors.New("rfb: authentication failed")

// PerformVNCAuth executes the classic 16-byte DES challenge/response
// authentication as specified in RFC 6143 §7.2.2. The password is
// truncated or zero-padded to 8 bytes and bit-reversed within each
// byte (a quirk of the original VNC implementation).
//
// Returns nil on successful authentication, ErrAuthFailed on a
// mismatch, or an underlying I/O error.
func (c *Conn) PerformVNCAuth(password string) error {
	var challenge [16]byte
	if _, err := rand.Read(challenge[:]); err != nil {
		return fmt.Errorf("rfb: generate VNCAuth challenge: %w", err)
	}
	if err := c.writeRaw(challenge[:]); err != nil {
		return err
	}
	if err := c.flushLocked(); err != nil {
		return err
	}

	var response [16]byte
	if err := c.readFull(response[:]); err != nil {
		return fmt.Errorf("rfb: read VNCAuth response: %w", err)
	}

	expected, err := encryptVNCAuthChallenge(challenge[:], password)
	if err != nil {
		return err
	}
	if !constantTimeEqual(expected, response[:]) {
		return ErrAuthFailed
	}
	return nil
}

// encryptVNCAuthChallenge produces the 16-byte response to a VNCAuth
// challenge for the given password. Exposed for tests.
func encryptVNCAuthChallenge(challenge []byte, password string) ([]byte, error) {
	if len(challenge) != 16 {
		return nil, fmt.Errorf("rfb: VNCAuth challenge must be 16 bytes, got %d", len(challenge))
	}

	key := vncAuthKey(password)
	cipher, err := des.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("rfb: build DES cipher: %w", err)
	}

	out := make([]byte, 16)
	cipher.Encrypt(out[:8], challenge[:8])
	cipher.Encrypt(out[8:], challenge[8:])
	return out, nil
}

// vncAuthKey converts a password into the 8-byte DES key used by
// VNCAuth. The password is truncated/zero-padded to 8 bytes, then
// each byte is bit-reversed (RealVNC's original implementation
// quirk, preserved by all modern clients).
func vncAuthKey(password string) []byte {
	key := make([]byte, 8)
	n := len(password)
	if n > 8 {
		n = 8
	}
	for i := 0; i < n; i++ {
		key[i] = bitReverse(password[i])
	}
	return key
}

// bitReverse reverses the 8 bits of b: 0bABCDEFGH -> 0bHGFEDCBA.
func bitReverse(b byte) byte {
	b = (b&0xF0)>>4 | (b&0x0F)<<4
	b = (b&0xCC)>>2 | (b&0x33)<<2
	b = (b&0xAA)>>1 | (b&0x55)<<1
	return b
}

// constantTimeEqual is a small, allocation-free equivalent of
// crypto/subtle.ConstantTimeCompare for fixed-size buffers. Both
// inputs must have the same length.
func constantTimeEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
