//go:build !cgo || !linux || !arm

package tls

import "crypto"

// WrapRSAKey is a no-op on non-ARM/non-Linux platforms.
// Returns the key unchanged with no error.
func WrapRSAKey(key crypto.PrivateKey) (crypto.PrivateKey, error) {
	return key, nil
}
