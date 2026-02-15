package meshvpn

import "errors"

var (
	ErrNotSupported       = errors.New("feature not supported by this provider")
	ErrNotInstalled       = errors.New("provider not installed")
	ErrAlreadyInstalled   = errors.New("provider already installed")
	ErrVerificationFailed = errors.New("verification failed")
	ErrNoActiveProvider   = errors.New("no active provider")
	ErrProviderNotFound   = errors.New("provider not found")
)
