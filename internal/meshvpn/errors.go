package meshvpn

import "errors"

var (
	// ErrNotSupported is returned when a feature is not supported by a provider
	ErrNotSupported = errors.New("feature not supported by this provider")

	// ErrNotInstalled is returned when the provider binaries are not installed
	ErrNotInstalled = errors.New("provider not installed")

	// ErrAlreadyInstalled is returned when attempting to install an already installed provider
	ErrAlreadyInstalled = errors.New("provider already installed")

	// ErrVerificationFailed is returned when checksum verification fails
	ErrVerificationFailed = errors.New("verification failed")

	// ErrNoActiveProvider is returned when no provider is currently active
	ErrNoActiveProvider = errors.New("no active provider")

	// ErrProviderNotFound is returned when a requested provider is not registered
	ErrProviderNotFound = errors.New("provider not found")
)
