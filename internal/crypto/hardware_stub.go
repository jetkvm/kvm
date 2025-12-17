//go:build !linux || !arm

package crypto

import "errors"

// newHardwareAESGCM returns an error on non-Linux/ARM platforms,
// forcing fallback to software implementation.
func newHardwareAESGCM(key []byte) (AEAD, error) {
	return nil, errors.New("hardware crypto not available on this platform")
}
