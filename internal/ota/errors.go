package ota

import "errors"

var (
	// ErrVersionNotFound is returned when the specified version is not found
	ErrVersionNotFound = errors.New("specified version not found")
)
