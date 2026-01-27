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

	// TLSEnabled indicates if TLS is configured for HTTPS services.
	// Used by clipboard file server to determine whether to use HTTPS.
	TLSEnabled bool

	// GetCertificate returns a TLS certificate for the given ClientHelloInfo.
	// Used by clipboard file server for HTTPS.
	GetCertificate func(*tls.ClientHelloInfo) (*tls.Certificate, error)

	// USBStorage provides USB mass storage for clipboard file transfer.
	// If nil, USB transfer method is not available.
	USBStorage USBStorageProvider

	// ClipboardStore provides file storage for network-based clipboard transfer.
	// Files are served via the main HTTPS server on port 443.
	ClipboardStore ClipboardStoreProvider

	// OnSessionStart is called when an RDP client enters active session.
	// Used to track active sessions for sleep mode prevention.
	OnSessionStart func()

	// OnSessionEnd is called when an RDP client disconnects.
	// Used to track active sessions for sleep mode prevention.
	OnSessionEnd func()
}

// TLSProvider provides TLS connection upgrading with optional hardware acceleration.
type TLSProvider interface {
	// UpgradeServerConn upgrades a net.Conn to a TLS server connection.
	// Returns a TLSConn that provides the encrypted connection.
	// Uses hardware acceleration when available for both TLS and CredSSP modes.
	// CredSSP only needs net.Conn - the server public key is provided separately.
	UpgradeServerConn(conn net.Conn) (TLSConn, error)

	// GetServerCertificate returns the server's TLS certificate for a given SNI.
	// This is used by CredSSP to compute pubKeyAuth over the server's public key.
	GetServerCertificate(serverName string) *tls.Certificate

	// IsHardwareAccelerated returns true if hardware crypto acceleration is available.
	IsHardwareAccelerated() bool

	// HardwareEngine returns the name of the hardware crypto engine in use.
	HardwareEngine() string
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

	// GetRDPCameraTranscodeEnabled returns whether H.264→MJPEG software transcoding is enabled.
	// This is a BETA feature with high CPU usage.
	GetRDPCameraTranscodeEnabled() bool

	// GetCameraFrameRate returns the camera frame rate setting (e.g., 15, 24, 30).
	GetCameraFrameRate() int

	// GetCameraMjpegQuality returns the MJPEG quality setting (0-100).
	GetCameraMjpegQuality() int

	// GetTLSMode returns the TLS mode ("disabled", "self-signed", "custom").
	GetTLSMode() string

	// GetHashedPassword returns the hashed password for authentication.
	GetHashedPassword() string

	// GetLocalAuthPassword returns the plaintext password for NTLM authentication.
	// This is the same password used for VNC authentication.
	GetLocalAuthPassword() string

	// GetRDPUsername returns the expected username for RDP authentication.
	// If empty, any username is accepted.
	GetRDPUsername() string

	// GetRDPDomain returns the expected domain for RDP authentication.
	// If empty, any domain is accepted.
	GetRDPDomain() string

	// GetRDPTargetOS returns the target OS for clipboard commands.
	// Values: "windows", "linux", "macos"
	GetRDPTargetOS() string

	// GetRDPFileTransferEnabled returns whether file clipboard transfer is enabled.
	GetRDPFileTransferEnabled() bool

	// GetRDPFileTransferMethod returns the file transfer method.
	// Values: "auto", "network", "usb", "base64"
	GetRDPFileTransferMethod() string

	// GetRDPFileTransferMaxMB returns the maximum file size in MB.
	GetRDPFileTransferMaxMB() int

	// GetRDPFileTransferTTLSec returns the file TTL in seconds (default 300).
	GetRDPFileTransferTTLSec() int

	// GetRDPFileTransferCleanupSec returns the cleanup interval in seconds (default 60).
	GetRDPFileTransferCleanupSec() int

	// GetRDPNetworkCmdWindows returns custom download command for Windows.
	GetRDPNetworkCmdWindows() string

	// GetRDPNetworkCmdLinux returns custom download command for Linux.
	GetRDPNetworkCmdLinux() string

	// GetRDPNetworkCmdMacOS returns custom download command for macOS.
	GetRDPNetworkCmdMacOS() string

	// GetRDPBase64CmdWindows returns custom decode command for Windows.
	GetRDPBase64CmdWindows() string

	// GetRDPBase64CmdLinux returns custom decode command for Linux.
	GetRDPBase64CmdLinux() string

	// GetRDPBase64CmdMacOS returns custom decode command for macOS.
	GetRDPBase64CmdMacOS() string
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
//
// Memory management: Call Release() after processing to return the buffer to the pool.
type RGBFrame struct {
	Data       []byte
	Width      uint32
	Height     uint32
	Format     RGBFrameFormat
	OnRelease  func() // Called to return buffer to pool (exported for cross-package use)
}

// Release returns the frame's buffer to the pool.
// Must be called after the frame data is no longer needed.
// Safe to call multiple times or on frames without a release callback.
func (f *RGBFrame) Release() {
	if f.OnRelease != nil {
		f.OnRelease()
		f.OnRelease = nil
	}
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
	SubscribeH264() <-chan []byte

	// UnsubscribeH264 removes the subscription and releases resources.
	UnsubscribeH264(ch <-chan []byte)

	// SubscribeJPEG returns a channel for JPEG video frames.
	// Used for bitmap mode fallback when RDPGFX is not supported.
	SubscribeJPEG() <-chan []byte

	// UnsubscribeJPEG removes the subscription and releases resources.
	UnsubscribeJPEG(ch <-chan []byte)

	// StartJPEGEncoder starts the hardware JPEG encoder.
	// Quality is 1-99, higher is better.
	StartJPEGEncoder(quality int) error

	// StopJPEGEncoder stops the hardware JPEG encoder.
	StopJPEGEncoder() error

	// SubscribeRGB returns a channel for raw BGRX frames from RGA hardware.
	SubscribeRGB() <-chan RGBFrame

	// UnsubscribeRGB removes the subscription and releases resources.
	UnsubscribeRGB(ch <-chan RGBFrame)

	// StartRGBEncoder starts the RGA hardware RGB encoder.
	// This converts YUV422 to BGRX in hardware with zero CPU overhead.
	StartRGBEncoder() error

	// StopRGBEncoder stops the RGA hardware RGB encoder.
	StopRGBEncoder() error

	// RequestKeyframe requests the encoder to produce an immediate keyframe.
	// This is used after frame drops to minimize video recovery time.
	RequestKeyframe()
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

	// UnsubscribeAudio removes the subscription and releases resources.
	UnsubscribeAudio(ch <-chan []byte)

	// PlayAudio plays audio data to the USB audio gadget.
	// Format: 16-bit signed PCM, stereo, 48kHz.
	PlayAudio(data []byte) error

	// EnableAudioInput enables the audio input subsystem for RDP mic passthrough.
	// This is called when the AUDIN channel becomes ready.
	EnableAudioInput() error
}

// USBStorageProvider provides USB mass storage capabilities for clipboard file transfer.
type USBStorageProvider interface {
	// IsAvailable returns true if USB mass storage can be used (not already mounted).
	IsAvailable() bool

	// MountFile mounts a file as USB mass storage (disk mode).
	// The file should be in the images folder.
	MountFile(filename string) error

	// Unmount unmounts the current USB mass storage.
	Unmount() error

	// GetImagesFolder returns the path to the images folder.
	GetImagesFolder() string
}

// ClipboardStoreProvider provides file storage for network clipboard transfer.
type ClipboardStoreProvider interface {
	// AddFile adds a file to the store and returns a download token.
	AddFile(path, originalName string) (token string, err error)

	// RemoveFile removes a file from the store.
	RemoveFile(token string)
}

// CameraFormatInfo describes the video format requested by the USB host.
type CameraFormatInfo struct {
	Codec     string // "h264" or "mjpeg"
	Width     int
	Height    int
	FrameRate int
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

	// SubscribeFormatChanges returns a channel that receives notifications
	// when the USB host requests a different camera format.
	// Returns nil if format subscription is not supported.
	SubscribeFormatChanges() <-chan CameraFormatInfo

	// UnsubscribeFormatChanges stops the format change subscription.
	UnsubscribeFormatChanges()
}
