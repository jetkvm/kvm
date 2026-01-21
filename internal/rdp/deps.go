// Package rdp implements an RDP server for JetKVM.
package rdp

import (
	"crypto/tls"
	"net"

	"github.com/rs/zerolog"
)

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

	// TLS provides TLS/SSL connection upgrading.
	// If nil, the server will use Go's standard crypto/tls.
	TLS TLSProvider
}

// TLSProvider provides TLS connection upgrading with optional hardware acceleration.
type TLSProvider interface {
	// UpgradeServerConn upgrades a net.Conn to a TLS server connection.
	// Returns a TLSConn that provides the encrypted connection.
	// Uses hardware acceleration when available.
	UpgradeServerConn(conn net.Conn) (TLSConn, error)

	// UpgradeServerConnForCredSSP upgrades a net.Conn to a TLS server connection
	// for CredSSP/NLA authentication. This returns a Go *tls.Conn because CredSSP
	// requires access to the TLS session binding for pubKeyAuth calculation.
	// Does not use hardware acceleration.
	UpgradeServerConnForCredSSP(conn net.Conn) (CredSSPTLSConn, error)

	// IsHardwareAccelerated returns true if hardware crypto acceleration is available.
	IsHardwareAccelerated() bool

	// HardwareEngine returns the name of the hardware crypto engine in use.
	HardwareEngine() string
}

// CredSSPTLSConn is a TLS connection that supports CredSSP authentication.
// This must be a Go *tls.Conn for access to ConnectionState().
type CredSSPTLSConn interface {
	net.Conn
	// ConnectionState returns the TLS connection state.
	// This is required for CredSSP pubKeyAuth calculation.
	ConnectionState() tls.ConnectionState
}

// TLSConn represents a TLS connection with introspection capabilities.
type TLSConn interface {
	net.Conn
	// GetCipherName returns the name of the negotiated cipher suite.
	GetCipherName() string
	// GetProtocolVersion returns the negotiated TLS version string.
	GetProtocolVersion() string
	// IsHardwareAccelerated returns true if hardware crypto is being used.
	IsHardwareAccelerated() bool
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

	// GetRDPVideoEnabled returns whether H.264 video (RDPGFX) is enabled.
	GetRDPVideoEnabled() bool

	// GetRDPAudioEnabled returns whether audio output to client (RDPSND) is enabled.
	GetRDPAudioEnabled() bool

	// GetRDPMicEnabled returns whether microphone input from client (AUDIN) is enabled.
	GetRDPMicEnabled() bool

	// GetRDPCameraEnabled returns whether webcam redirection from client is enabled.
	GetRDPCameraEnabled() bool

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

// RGBFrameFormat indicates the pixel format of the frame data
type RGBFrameFormat int

const (
	// RGBFrameFormatYUV422 indicates YUV422 YUYV format (needs software conversion)
	RGBFrameFormatYUV422 RGBFrameFormat = iota
	// RGBFrameFormatBGRX indicates BGRX format (ready to use, from RGA hardware)
	RGBFrameFormatBGRX
)

// RGBFrame represents a video frame for RDP bitmap mode.
// When RGA hardware acceleration is available, Format will be RGBFrameFormatBGRX
// and Data contains ready-to-use BGRX pixels. Otherwise, Format is RGBFrameFormatYUV422
// and Data needs software conversion.
type RGBFrame struct {
	Data   []byte
	Width  uint32
	Height uint32
	Format RGBFrameFormat
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

	// SubscribeRGB returns a channel for raw BGRX frames from RGA hardware.
	// This provides the fastest bitmap updates by bypassing JPEG encode/decode.
	SubscribeRGB() <-chan RGBFrame

	// UnsubscribeRGB stops the RGB subscription.
	UnsubscribeRGB()

	// StartRGBEncoder starts the RGA hardware RGB encoder.
	// This converts YUV422 to BGRX in hardware with zero CPU overhead.
	StartRGBEncoder() error

	// StopRGBEncoder stops the RGA hardware RGB encoder.
	StopRGBEncoder() error
}

// AudioProvider provides audio capture and playback.
type AudioProvider interface {
	// Connect signals that an RDP client needs audio.
	// This ensures audio capture is running.
	Connect()

	// Disconnect signals that an RDP client no longer needs audio.
	Disconnect()

	// SubscribeAudio returns a channel for captured HDMI audio.
	// Format: 16-bit signed PCM, stereo, 48kHz.
	SubscribeAudio() <-chan []byte

	// UnsubscribeAudio stops the audio subscription.
	UnsubscribeAudio()

	// PlayAudio plays audio data to the USB audio gadget.
	// Format: 16-bit signed PCM, stereo, 48kHz.
	PlayAudio(data []byte) error

	// EnableAudioInput enables the audio input subsystem for RDP mic passthrough.
	// This is called when the AUDIN channel becomes ready.
	EnableAudioInput() error
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
