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
type sourceStore struct {
	value atomic.Value
}

func newSourceStore() *sourceStore {
	s := &sourceStore{}
	s.value.Store(SourceHDMI)
	return s
}

func (s *sourceStore) Get() Source {
	if v := s.value.Load(); v != nil {
		return v.(Source)
	}
	return SourceHDMI
}

func (s *sourceStore) Set(source Source) {
	s.value.Store(source)
}

func (s *sourceStore) IsCamera() bool {
	return s.Get() == SourceCamera
}

func (s *sourceStore) IsHDMI() bool {
	return s.Get() == SourceHDMI
}
