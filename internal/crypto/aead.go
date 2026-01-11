// Package crypto provides hardware-accelerated cryptographic operations
// for JetKVM, with automatic fallback to software implementations.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"sync"
)

// AEAD represents an authenticated encryption with associated data cipher.
// On supported platforms (RV1106), this uses hardware acceleration.
// On other platforms, it falls back to Go's stdlib implementation.
type AEAD interface {
	cipher.AEAD

	// Close releases any hardware resources. Always call this when done.
	Close() error

	// IsHardwareAccelerated returns true if using hardware crypto.
	IsHardwareAccelerated() bool
}

// hardwareFallbackReason stores why hardware crypto is unavailable (if at all).
// Set once on first NewAESGCM call; use HardwareCryptoError() to retrieve.
var (
	hardwareFallbackOnce   sync.Once
	hardwareFallbackReason error
)

// HardwareCryptoError returns the error that caused hardware crypto to be
// unavailable, or nil if hardware crypto is being used. This can be called
// after any NewAESGCM call to check hardware status for logging/diagnostics.
func HardwareCryptoError() error {
	return hardwareFallbackReason
}

// NewAESGCM creates a new AES-GCM cipher.
// On RV1106, this uses hardware acceleration via /dev/crypto.
// On other platforms, it uses Go's stdlib AES-GCM.
// Callers can use HardwareCryptoError() to check why hardware is unavailable.
func NewAESGCM(key []byte) (AEAD, error) {
	keyLen := len(key)
	if keyLen != 16 && keyLen != 24 && keyLen != 32 {
		return nil, errors.New("crypto: invalid AES key size")
	}

	// Try hardware first
	hw, err := newHardwareAESGCM(key)
	if err == nil {
		return hw, nil
	}

	// Record hardware unavailability once for diagnostics
	hardwareFallbackOnce.Do(func() {
		hardwareFallbackReason = err
	})

	// Fall back to software
	return newSoftwareAESGCM(key)
}

// softwareAESGCM wraps Go's stdlib AES-GCM to implement our AEAD interface.
type softwareAESGCM struct {
	cipher.AEAD
}

func newSoftwareAESGCM(key []byte) (AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &softwareAESGCM{AEAD: gcm}, nil
}

func (s *softwareAESGCM) Close() error {
	return nil // No resources to release
}

func (s *softwareAESGCM) IsHardwareAccelerated() bool {
	return false
}
