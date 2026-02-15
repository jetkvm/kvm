//go:build !cgo || !linux || !arm

package tls

import "crypto"

// SetHardwareRSAMode is a no-op on non-ARM platforms.
func SetHardwareRSAMode(mode string) {}

// GetHardwareRSAMode returns "disabled" on non-ARM platforms.
func GetHardwareRSAMode() string {
	return "disabled"
}

// GetSignerName returns a human-readable name for the signer backend.
// On non-ARM platforms, always returns "Go crypto".
func GetSignerName(key any) string {
	return "Go crypto"
}

// WrapRSAKey is a no-op on non-ARM/non-Linux platforms.
// Returns the key unchanged with no error.
func WrapRSAKey(key crypto.PrivateKey) (crypto.PrivateKey, error) {
	return key, nil
}
