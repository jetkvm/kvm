//go:build !linux

package usbgadget

import (
	"errors"
	"fmt"
)

// ErrNotStreaming is returned when WriteFrame is called but streaming is not active.
var ErrNotStreaming = errors.New("uvc: device not streaming")

// ErrFrameTooLarge is returned when a frame exceeds the maximum buffer size.
var ErrFrameTooLarge = errors.New("uvc: frame exceeds maximum size")

// ErrFrameEmpty is returned when WriteFrame is called with empty data.
var ErrFrameEmpty = errors.New("uvc: empty frame")

// ErrDeviceNotReady is returned when the device is not ready for operations.
var ErrDeviceNotReady = errors.New("uvc: device not ready")

// UVC event constants (stubs for non-linux builds)
const (
	UVC_EVENT_CONNECT    = 0x08000000
	UVC_EVENT_DISCONNECT = 0x08000001
	UVC_EVENT_STREAMON   = 0x08000002
	UVC_EVENT_STREAMOFF  = 0x08000003
	UVC_EVENT_SETUP      = 0x08000004
	UVC_EVENT_DATA       = 0x08000005

	// UVC format indices correspond to ConfigFS setup order.
	FormatIndexMJPEG = 1
	FormatIndexH264  = 2

	// UVC frame indices (1-based, per format type).
	FrameIndex1080p = 1
	FrameIndex720p  = 2
	FrameIndex480p  = 3
)

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

func (s *UVCStreamer) SetFormatWithCodec(width, height uint32, isMjpeg bool) error {
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

func (s *UVCStreamer) IsStreaming() bool {
	return false
}

func (s *UVCStreamer) IsOpen() bool {
	return false
}

func (s *UVCStreamer) IsValid() bool {
	return false
}

func (s *UVCStreamer) GetCommittedResolution() (uint32, uint32) {
	return 1920, 1080 // Match real implementation default
}

func (s *UVCStreamer) GetCommittedFrameRate() int {
	return 30
}

func (s *UVCStreamer) GetCommittedFormatIndex() uint8 {
	return 2 // H.264 (MJPEG is index 1 when both are configured)
}

func (s *UVCStreamer) IsH264Format() bool {
	return true
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
