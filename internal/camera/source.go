package camera

import (
	"fmt"
	"sync/atomic"
)

// Source defines the video source for UVC output.
type Source string

const (
	SourceHDMI   Source = "hdmi"   // HDMI loopback (default)
	SourceCamera Source = "camera" // Browser camera passthrough
)

// Internal int32 values for lock-free atomic operations (no type assertion overhead)
const (
	sourceValueHDMI   int32 = 0
	sourceValueCamera int32 = 1
)

// IsValid returns true if the source is a valid value.
func (s Source) IsValid() bool {
	switch s {
	case SourceHDMI, SourceCamera:
		return true
	default:
		return false
	}
}

// String returns the string representation of the source.
func (s Source) String() string {
	return string(s)
}

// ParseSource parses a string into a Source, returning an error if invalid.
func ParseSource(s string) (Source, error) {
	source := Source(s)
	if !source.IsValid() {
		return "", fmt.Errorf("invalid camera source: %s (valid: %s, %s)", s, SourceHDMI, SourceCamera)
	}
	return source, nil
}

// sourceStore provides thread-safe storage for the current UVC source.
// Uses atomic.Int32 internally to avoid type assertion overhead in hot path.
type sourceStore struct {
	value atomic.Int32
}

func newSourceStore() *sourceStore {
	s := &sourceStore{}
	s.value.Store(sourceValueHDMI)
	return s
}

func (s *sourceStore) Get() Source {
	if s.value.Load() == sourceValueCamera {
		return SourceCamera
	}
	return SourceHDMI
}

func (s *sourceStore) Set(source Source) {
	if source == SourceCamera {
		s.value.Store(sourceValueCamera)
	} else {
		s.value.Store(sourceValueHDMI)
	}
}

// IsCamera returns true if source is camera. HOTPATH: Single atomic load, no type assertion.
func (s *sourceStore) IsCamera() bool {
	return s.value.Load() == sourceValueCamera
}

// IsHDMI returns true if source is HDMI. HOTPATH: Single atomic load, no type assertion.
func (s *sourceStore) IsHDMI() bool {
	return s.value.Load() == sourceValueHDMI
}
