package kvm

import "errors"

var (
	ErrPermissionDeniedKeyboard = errors.New("permission denied: keyboard input")
	ErrPermissionDeniedMouse    = errors.New("permission denied: mouse input")
	ErrNotPrimarySession        = errors.New("operation requires primary session")
	ErrSessionNotFound          = errors.New("session not found")
)
