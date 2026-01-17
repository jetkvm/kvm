package vnc

import (
	"bytes"
	"sync"
	"time"
)

// bufferPool provides reusable buffers for ServerInit message assembly.
// This avoids allocations on the hot path during connection setup.
var bufferPool = sync.Pool{
	New: func() interface{} {
		return bytes.NewBuffer(make([]byte, 0, serverInitBufSize))
	},
}

// =============================================================================
// RFB Security Types
// =============================================================================

// rfbSecurityType represents RFB security types used during authentication.
type rfbSecurityType uint8

const (
	secTypeNone     rfbSecurityType = 1  // No authentication
	secTypeVNCAuth  rfbSecurityType = 2  // VNC challenge-response (DES)
	secTypeVeNCrypt rfbSecurityType = 19 // VeNCrypt extension (TLS wrapper)
)

// =============================================================================
// VeNCrypt Security Subtypes
// =============================================================================

// veNCryptSubtype represents VeNCrypt security subtypes after TLS negotiation.
type veNCryptSubtype uint32

const (
	veNCryptTLSNone   veNCryptSubtype = 257 // TLS + No auth
	veNCryptTLSVnc    veNCryptSubtype = 258 // TLS + VNC challenge-response
	veNCryptTLSPlain  veNCryptSubtype = 259 // TLS + plaintext username/password
	veNCryptX509None  veNCryptSubtype = 260 // X509 + No auth
	veNCryptX509Vnc   veNCryptSubtype = 261 // X509 + VNC challenge-response
	veNCryptX509Plain veNCryptSubtype = 262 // X509 + plaintext username/password
)

// =============================================================================
// RFB Client Message Types
// =============================================================================

// rfbClientMsgType represents messages sent by VNC clients.
type rfbClientMsgType uint8

const (
	msgSetPixelFormat           rfbClientMsgType = 0
	msgSetEncodings             rfbClientMsgType = 2
	msgFramebufferUpdateRequest rfbClientMsgType = 3
	msgKeyEvent                 rfbClientMsgType = 4
	msgPointerEvent             rfbClientMsgType = 5
	msgClientCutText            rfbClientMsgType = 6
)

// =============================================================================
// RFB Server Message Types
// =============================================================================

// rfbServerMsgType represents messages sent by VNC server.
type rfbServerMsgType uint8

const (
	msgFramebufferUpdate rfbServerMsgType = 0
)

// =============================================================================
// RFB Encoding Types
// =============================================================================

// rfbEncodingType represents pixel data encoding methods.
type rfbEncodingType int32

const (
	encodingTight rfbEncodingType = 7 // Tight encoding with JPEG compression
)

// =============================================================================
// Protocol Constants
// =============================================================================

const (
	// RFB Protocol version 3.8 - required for VeNCrypt security type support
	rfbProtocolVersion = "RFB 003.008\n"

	// Tight encoding compression control byte:
	// Bits 7-4: compression type (0x9 = JPEG compression)
	// Bits 3-0: stream reset flags (0 = no stream resets)
	tightJPEG = 0x90

	// Authentication failure delay to prevent brute-force attacks
	authFailureDelay = 2 * time.Second

	// Maximum auth failures before disconnecting
	maxAuthFailures = 3

	// Timeouts for various operations
	handshakeTimeout = 30 * time.Second
	readTimeout      = 60 * time.Second
	writeTimeout     = 5 * time.Second

	// VNC auth challenge/response size (DES block size * 2)
	vncChallengeSize = 16

	// Maximum plaintext auth credential length
	maxCredentialLength = 256

	// Maximum clipboard text size (1MB to prevent OOM)
	maxCutTextLength = 1024 * 1024

	// Tight encoding compact length thresholds
	tightLen1Byte = 128
	tightLen2Byte = 16384

	// Buffer pool capacity for ServerInit message
	serverInitBufSize = 256

	// JPEG Start of Image marker bytes
	jpegSOIByte0 = 0xFF
	jpegSOIByte1 = 0xD8

	// Pre-allocated buffer sizes for RFB protocol messages
	rfbMsgBufSize         = 1  // Message type byte
	rfbKeyBufSize         = 7  // Key event
	rfbPointerBufSize     = 5  // Pointer event
	rfbFBReqBufSize       = 9  // FB update request
	rfbPixelFmtBufSize    = 19 // SetPixelFormat
	rfbEncHeaderBufSize   = 3  // SetEncodings header
	rfbEncBufSize         = 4  // Single encoding value
	rfbFrameHeaderBufSize = 20 // Frame header

	// USB HID absolute positioning maximum
	hidAbsoluteMax = 32767
)

// =============================================================================
// Server Constants
// =============================================================================

const (
	// Maximum concurrent VNC connections for embedded device
	MaxConnections = 10

	// Rate limit: minimum time between new connections from same IP
	connectionRateLimitMs = 100

	// Default VNC port (standard RFB port)
	DefaultPort = 5900

	// Default resolution when no video signal detected
	DefaultWidth  = 1920
	DefaultHeight = 1080

	// Rate limit map cleanup threshold
	rateLimitCleanupThreshold = 50

	// Rate limit entry expiry duration
	rateLimitExpirySeconds = 60

	// Frame stats logging interval in seconds
	frameStatsIntervalSeconds = 5

	// JPEG encoder circuit breaker settings
	jpegEncoderMaxFailures    = 3
	jpegEncoderCooldownPeriod = 30 * time.Second
)
