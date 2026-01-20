// Package rdp implements an RDP server for JetKVM.
package rdp

import "github.com/rs/zerolog"

// Dependencies defines external dependencies for the RDP server.
// This allows the server to be decoupled from the main kvm package.
type Dependencies struct {
	// Logger provides structured logging.
	Logger zerolog.Logger

	// Config provides access to RDP configuration.
	Config ConfigProvider

	// HID provides keyboard and mouse input.
	HID HIDProvider

	// Video provides video frame access.
	Video VideoProvider

	// Audio provides audio capture and playback.
	Audio AudioProvider

	// Camera provides UVC camera output.
	Camera CameraProvider
}

// ConfigProvider provides RDP configuration access.
type ConfigProvider interface {
	// GetRDPEnabled returns whether RDP is enabled.
	GetRDPEnabled() bool

	// GetRDPPort returns the RDP listening port.
	GetRDPPort() int

	// GetRDPMaxConnections returns the maximum concurrent connections.
	GetRDPMaxConnections() int

	// GetRDPClipboardEnabled returns whether clipboard paste is enabled.
	GetRDPClipboardEnabled() bool

	// GetTLSMode returns the TLS mode ("disabled", "self-signed", "custom").
	GetTLSMode() string

	// GetHashedPassword returns the hashed password for authentication.
	GetHashedPassword() string
}

// HIDProvider provides keyboard and mouse input capabilities.
type HIDProvider interface {
	// KeypressReport sends a key press/release event.
	KeypressReport(hidCode uint8, pressed bool) error

	// AbsMouseReport sends an absolute mouse position and button state.
	AbsMouseReport(x, y int, buttons byte) error

	// WheelReport sends a mouse wheel event.
	WheelReport(vertical, horizontal int8) error

	// KeyboardMacro types a string of text.
	KeyboardMacro(text string) error

	// IsKeyboardMacroInProgress returns true if a macro is running.
	IsKeyboardMacroInProgress() bool

	// CancelKeyboardMacro cancels an in-progress macro.
	CancelKeyboardMacro()
}

// VideoProvider provides access to video frames.
type VideoProvider interface {
	// GetResolution returns the current video resolution.
	GetResolution() (width, height uint16)

	// StartVideo starts the video capture system.
	// This must be called before frames will be delivered.
	StartVideo() error

	// StopVideo stops the video capture system.
	StopVideo() error

	// SubscribeH264 returns a channel for H.264 video frames.
	// The channel is closed when the subscription ends.
	SubscribeH264() <-chan []byte

	// UnsubscribeH264 stops the H.264 subscription.
	UnsubscribeH264()

	// SubscribeJPEG returns a channel for JPEG video frames.
	// Used for bitmap mode fallback when RDPGFX is not supported.
	SubscribeJPEG() <-chan []byte

	// UnsubscribeJPEG stops the JPEG subscription.
	UnsubscribeJPEG()

	// StartJPEGEncoder starts the hardware JPEG encoder.
	// Quality is 1-99, higher is better.
	StartJPEGEncoder(quality int) error

	// StopJPEGEncoder stops the hardware JPEG encoder.
	StopJPEGEncoder() error
}

// AudioProvider provides audio capture and playback.
type AudioProvider interface {
	// SubscribeAudio returns a channel for captured HDMI audio.
	// Format: 16-bit signed PCM, stereo, 48kHz.
	SubscribeAudio() <-chan []byte

	// UnsubscribeAudio stops the audio subscription.
	UnsubscribeAudio()

	// PlayAudio plays audio data to the USB audio gadget.
	// Format: 16-bit signed PCM, stereo, 48kHz.
	PlayAudio(data []byte) error
}

// CameraProvider provides UVC camera output capabilities.
type CameraProvider interface {
	// SendFrame sends a camera frame to the UVC gadget.
	// Data should be in the specified pixel format (NV12, I420, YUY2, MJPEG, or H.264).
	SendFrame(data []byte, width, height uint32, pixelFormat uint32) error

	// IsConnected returns true if a host is connected to the UVC gadget.
	IsConnected() bool

	// SetEnabled enables or disables camera passthrough.
	// When enabled, frames sent via SendFrame will be forwarded to the UVC gadget.
	SetEnabled(enabled bool)

	// IsEnabled returns true if camera passthrough is enabled.
	IsEnabled() bool
}
