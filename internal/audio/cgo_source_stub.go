//go:build !cgo || !linux || (!arm && !arm64)

package audio

// Stub implementations for non-ARM Linux platforms

type CgoSource struct{}

var _ AudioSource = (*CgoSource)(nil)

func NewCgoOutputSource(alsaDevice string, audioConfig AudioConfig) AudioSource {
	panic("audio CGO source not supported on this platform")
}

func NewCgoInputSource(alsaDevice string, audioConfig AudioConfig) AudioSource {
	panic("audio CGO source not supported on this platform")
}

func (c *CgoSource) Connect() error {
	panic("audio CGO source not supported on this platform")
}

func (c *CgoSource) Disconnect() {
	panic("audio CGO source not supported on this platform")
}

func (c *CgoSource) IsConnected() bool {
	panic("audio CGO source not supported on this platform")
}

func (c *CgoSource) ReadMessage() (uint8, []byte, error) {
	panic("audio CGO source not supported on this platform")
}

func (c *CgoSource) WriteMessage(msgType uint8, payload []byte) error {
	panic("audio CGO source not supported on this platform")
}

// GetLastPCM returns nil on non-ARM platforms.
func GetLastPCM() []byte {
	return nil
}

// ReleasePCMBuffer is a no-op on non-ARM platforms.
func ReleasePCMBuffer(buf []byte) {}

// WritePCM is a stub on non-ARM platforms.
func WritePCM(pcmData []byte) (int, error) {
	return 0, nil
}

// DropPlaybackBuffer is a stub on non-ARM platforms.
func DropPlaybackBuffer() error {
	return nil
}

// SetCLogLevel is a no-op on non-ARM platforms.
func SetCLogLevel(level int) {}
