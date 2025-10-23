//go:build !linux || (!arm && !arm64)

package audio

import "fmt"

// StubSource is a no-op implementation for platforms without CGO audio support
type StubSource struct {
	direction  string
	alsaDevice string
}

// NewCgoOutputSource creates a stub audio source for output (unsupported platforms)
func NewCgoOutputSource(alsaDevice string) AudioSource {
	return &StubSource{
		direction:  "output",
		alsaDevice: alsaDevice,
	}
}

// NewCgoInputSource creates a stub audio source for input (unsupported platforms)
func NewCgoInputSource(alsaDevice string) AudioSource {
	return &StubSource{
		direction:  "input",
		alsaDevice: alsaDevice,
	}
}

// Connect returns an error indicating CGO audio is not supported
func (s *StubSource) Connect() error {
	return fmt.Errorf("CGO audio not supported on this platform")
}

// Disconnect is a no-op
func (s *StubSource) Disconnect() {}

// IsConnected returns false
func (s *StubSource) IsConnected() bool {
	return false
}

// ReadMessage returns an error
func (s *StubSource) ReadMessage() (uint8, []byte, error) {
	return 0, nil, fmt.Errorf("CGO audio not supported on this platform")
}

// WriteMessage returns an error
func (s *StubSource) WriteMessage(msgType uint8, payload []byte) error {
	return fmt.Errorf("CGO audio not supported on this platform")
}
