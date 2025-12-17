//go:build !linux

package usbgadget

import "fmt"

// UVCStreamer stub for non-linux builds
type UVCStreamer struct{}

type Logger interface {
	Info() LogEvent
	Warn() LogEvent
	Error() LogEvent
	Debug() LogEvent
}

type LogEvent interface {
	Str(key, val string) LogEvent
	Int(key string, val int) LogEvent
	Uint32(key string, val uint32) LogEvent
	Bool(key string, val bool) LogEvent
	Err(err error) LogEvent
	Msg(msg string)
}

func NewUVCStreamer(devicePath string, log Logger) *UVCStreamer {
	return &UVCStreamer{}
}

func (s *UVCStreamer) Open() error {
	return fmt.Errorf("UVC not supported on this platform")
}

func (s *UVCStreamer) Close() error {
	return nil
}

func (s *UVCStreamer) SetFormat(width, height uint32) error {
	return fmt.Errorf("UVC not supported on this platform")
}

func (s *UVCStreamer) RequestBuffers(count uint32) error {
	return fmt.Errorf("UVC not supported on this platform")
}

func (s *UVCStreamer) StartStreaming() error {
	return fmt.Errorf("UVC not supported on this platform")
}

func (s *UVCStreamer) StopStreaming() error {
	return nil
}

func (s *UVCStreamer) WriteFrame(data []byte) error {
	return fmt.Errorf("UVC not supported on this platform")
}

func (s *UVCStreamer) SubscribeEvents() error {
	return fmt.Errorf("UVC not supported on this platform")
}

func (s *UVCStreamer) PollEvents() (uint32, error) {
	return 0, fmt.Errorf("UVC not supported on this platform")
}

func (s *UVCStreamer) IsStreaming() bool {
	return false
}

func (s *UVCStreamer) IsOpen() bool {
	return false
}

func (s *UVCStreamer) IsValid() bool {
	return false
}

func (s *UVCStreamer) Reopen() error {
	return fmt.Errorf("UVC not supported on this platform")
}

func (s *UVCStreamer) GetFd() int {
	return -1
}

func (s *UVCStreamer) GetCommittedResolution() (uint32, uint32) {
	return 1280, 720
}

func (s *UVCStreamer) PollEventsWithData() (uint32, []byte, error) {
	return 0, nil, fmt.Errorf("UVC not supported on this platform")
}

func (s *UVCStreamer) HandleSetupEvent(eventData []byte) error {
	return fmt.Errorf("UVC not supported on this platform")
}

func (s *UVCStreamer) HandleDataEvent(eventData []byte) (bool, error) {
	return false, fmt.Errorf("UVC not supported on this platform")
}

func (s *UVCStreamer) WaitForEventWithTimeout(timeoutMs int) (uint32, []byte, error) {
	return 0, nil, fmt.Errorf("UVC not supported on this platform")
}
