package vnc

import (
	"github.com/rs/zerolog"
)

// Config provides VNC configuration values.
// Implemented by the kvm package's config adapter.
type Config interface {
	// TLS settings
	GetTLSMode() string

	// VNC settings
	GetVNCQuality() int
	GetVNCMaxConnections() int
	GetVNCPasteDelayMs() int
	GetVNCClipboardEnabled() bool
	GetLocalAuthPassword() string
}

// NativeEncoder provides hardware encoder control.
// Implemented by the kvm package's native adapter.
type NativeEncoder interface {
	// JPEG encoder control
	JpegStart(quality int) error
	JpegStop() error
}

// KeyboardMacroStep represents a single step in a keyboard macro.
// Used for typing clipboard text character by character.
// Field names match hidrpc.KeyboardMacroStep for easy bridging.
type KeyboardMacroStep struct {
	Modifier uint8   // HID modifier byte
	Keys     []uint8 // HID key codes to press (6 bytes)
	Delay    uint16  // Delay after this step in milliseconds
}

// HIDController provides keyboard and mouse input control.
// Implemented by the kvm package's HID adapter.
type HIDController interface {
	// Keyboard events
	KeypressReport(key uint8, down bool) error

	// Mouse events (absolute positioning)
	AbsMouseReport(x, y int, buttons byte) error

	// Scroll wheel events
	WheelReport(wheelY, wheelX int8) error

	// Keyboard macro for clipboard typing
	KeyboardMacro(steps []KeyboardMacroStep) error
	IsKeyboardMacroInProgress() bool
	CancelKeyboardMacro()
}

// TLSProvider provides TLS availability check and certificate access.
// Implemented by the kvm package's TLS adapter.
type TLSProvider interface {
	// IsTLSAvailable returns true if TLS certificates are ready
	IsTLSAvailable() bool

	// GetCertificate returns the TLS certificate for the server
	// Returns nil if not available
	GetCertificate() interface{} // *tls.Certificate or equivalent
}

// Logger is the logging interface - using zerolog directly for zero-allocation logging.
type Logger = zerolog.Logger

// Dependencies holds all external dependencies for the VNC server.
// Pass this to NewServer to inject dependencies.
type Dependencies struct {
	Config  Config
	Encoder NativeEncoder
	HID     HIDController
	TLS     TLSProvider
	Logger  *Logger

	// OnVideoNeeded is called when the first VNC client requires video frames.
	// The kvm package uses this to start the native video stream if not already running.
	OnVideoNeeded func()

	// OnVideoReleased is called when the last VNC client releases video frames.
	// The kvm package uses this to stop the native video stream if no other consumers need it.
	OnVideoReleased func()
}
