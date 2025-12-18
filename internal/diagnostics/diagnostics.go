// Package diagnostics provides comprehensive system diagnostics logging
// for crash analysis, debugging, and troubleshooting.
package diagnostics

import (
	"io"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

var diagLogger = logging.GetSubsystemLogger("diagnostics")

// DataChannelInfo contains information about a WebRTC data channel.
type DataChannelInfo struct {
	Label string
	State string
}

// SessionInfo contains session-related diagnostic information.
type SessionInfo struct {
	ActiveSessions     int
	HasCurrentSession  bool
	ICEConnectionState string
	SignalingState     string
	ConnectionState    string
	DataChannels       []DataChannelInfo
}

// Options configures the Diagnostics instance.
type Options struct {
	// GetSessionInfo returns session diagnostics. Optional.
	GetSessionInfo func() SessionInfo
	// Writer is an optional output destination. If set, logs go here instead of default.
	Writer io.Writer
}

// Diagnostics provides comprehensive system diagnostics logging.
type Diagnostics struct {
	logger  *zerolog.Logger
	options Options
}

// New creates a new Diagnostics instance using the default diagnostics logger.
// If opts.Writer is set, logs are written there instead of the default logger.
func New(opts Options) *Diagnostics {
	if opts.Writer != nil {
		logger := zerolog.New(opts.Writer)
		return &Diagnostics{logger: &logger, options: opts}
	}
	return NewWithLogger(diagLogger, opts)
}

// NewWithLogger creates a new Diagnostics instance with a custom logger.
func NewWithLogger(logger *zerolog.Logger, opts Options) *Diagnostics {
	return &Diagnostics{
		logger:  logger,
		options: opts,
	}
}

// LogAll runs all diagnostic checks and logs the results.
// The phase parameter distinguishes context (e.g., "crash" vs "handshake" vs "download").
func (d *Diagnostics) LogAll(phase string) {
	d.logger.Error().Str("phase", phase).Msg("=== DIAGNOSTICS ===")

	d.LogSystemInfo()
	d.LogInputDevices()
	d.LogI2CInfo()
	d.LogDeviceFiles()
	d.LogUSBGadget()
	d.LogNetworking()
	d.LogSessionInfo()
	d.LogGoRuntime()
	d.LogKernelInfo()
	d.LogDmesgTail()
}
