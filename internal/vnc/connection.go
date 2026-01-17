package vnc

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Connection represents a single VNC client connection.
//
// Struct layout groups fields by access pattern for cache efficiency on 32-bit ARM.
// Hot path fields (atomics, conn) are placed first to minimize cache misses.
type Connection struct {
	// === Hot path: Control atomics and connection ===
	resolution     atomic.Uint32 // packed: high=width, low=height
	frameRequested atomic.Bool
	closed         atomic.Bool
	hasTight       atomic.Bool
	conn           net.Conn
	stopChan       chan struct{}

	// === Hot path: Frame header buffer ===
	frameHeaderBuf [rfbFrameHeaderBufSize]byte

	// === Hot path: Input message buffers ===
	msgBuf     [rfbMsgBufSize]byte
	keyBuf     [rfbKeyBufSize]byte
	pointerBuf [rfbPointerBufSize]byte
	fbReqBuf   [rfbFBReqBufSize]byte
	encBuf     [rfbEncBufSize]byte

	// === Diagnostic counters (int32 for lock-free atomics on 32-bit ARM) ===
	framesSent      atomic.Int32
	framesDropped   atomic.Int32
	lastFrameLog    atomic.Int32 // elapsed seconds since startTime
	intervalSent    atomic.Int32
	intervalDropped atomic.Int32
	startTime       time.Time

	// === Cold path: Setup/handshake fields ===
	server           *Server
	pixelFormat      PixelFormat
	pixelFormatMu    sync.Mutex // protects pixelFormat only
	needsJPEGEncoder atomic.Bool
	authFailures     int // auth failure count (single goroutine, no lock needed)

	// === Cold path: Rarely used buffers ===
	pixelFmtBuf  [rfbPixelFmtBufSize]byte
	encHeaderBuf [rfbEncHeaderBufSize]byte
	cutTextBuf   []byte

	// === Cold path: Clipboard for paste-on-demand ===
	clipboardText []byte
	clipboardMu   sync.Mutex // protects clipboardText
	ctrlDown      bool       // Left or Right Control is pressed
	metaDown      bool       // Left or Right Meta/Super is pressed
	shiftDown     bool       // Left or Right Shift is pressed

	// === Very cold: Logging (message loop only, no lock needed) ===
	lastPointerLogTime time.Time
	pointerEventCount  int32
}

// PixelFormat represents the VNC pixel format.
type PixelFormat struct {
	BitsPerPixel uint8
	Depth        uint8
	BigEndian    uint8
	TrueColor    uint8
	RedMax       uint16
	GreenMax     uint16
	BlueMax      uint16
	RedShift     uint8
	GreenShift   uint8
	BlueShift    uint8
}

// Validate checks if the pixel format has valid values.
func (pf PixelFormat) Validate() error {
	if pf.BitsPerPixel != 8 && pf.BitsPerPixel != 16 && pf.BitsPerPixel != 32 {
		return fmt.Errorf("invalid bits per pixel: %d (must be 8, 16, or 32)", pf.BitsPerPixel)
	}
	if pf.Depth > pf.BitsPerPixel {
		return fmt.Errorf("invalid depth: %d (cannot exceed bits per pixel %d)", pf.Depth, pf.BitsPerPixel)
	}
	if pf.TrueColor != 0 && pf.TrueColor != 1 {
		return fmt.Errorf("invalid true color flag: %d (must be 0 or 1)", pf.TrueColor)
	}
	if pf.BigEndian != 0 && pf.BigEndian != 1 {
		return fmt.Errorf("invalid big endian flag: %d (must be 0 or 1)", pf.BigEndian)
	}
	return nil
}

// DefaultPixelFormat returns the default 32-bit BGRA pixel format.
func DefaultPixelFormat() PixelFormat {
	return PixelFormat{
		BitsPerPixel: 32,
		Depth:        24,
		BigEndian:    0,
		TrueColor:    1,
		RedMax:       255,
		GreenMax:     255,
		BlueMax:      255,
		RedShift:     16,
		GreenShift:   8,
		BlueShift:    0,
	}
}

// packResolution packs width and height into a single uint32 for atomic access.
func packResolution(w, h uint16) uint32 {
	return uint32(w)<<16 | uint32(h)
}

// unpackResolution unpacks width and height from a uint32.
func unpackResolution(packed uint32) (uint16, uint16) {
	return uint16(packed >> 16), uint16(packed & 0xFFFF)
}

// GetResolution returns width and height atomically (lock-free).
func (c *Connection) GetResolution() (uint16, uint16) {
	return unpackResolution(c.resolution.Load())
}

// setResolution sets width and height atomically.
func (c *Connection) setResolution(w, h uint16) {
	c.resolution.Store(packResolution(w, h))
}

// NewConnection creates a new VNC connection.
func NewConnection(conn net.Conn, server *Server) *Connection {
	w, h := server.GetVideoState()

	// Use detected resolution, or default if no video signal yet
	if w == 0 {
		w = DefaultWidth
	}
	if h == 0 {
		h = DefaultHeight
	}

	vc := &Connection{
		conn:        conn,
		server:      server,
		pixelFormat: DefaultPixelFormat(),
		stopChan:    make(chan struct{}),
		startTime:   time.Now(),
	}
	vc.setResolution(w, h)
	return vc
}

// Handle runs the VNC connection protocol.
func (c *Connection) Handle() error {
	defer c.conn.Close()

	if err := c.handshake(); err != nil {
		return fmt.Errorf("handshake failed: %w", err)
	}

	if err := c.authenticate(); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	if err := c.clientInit(); err != nil {
		return fmt.Errorf("client init failed: %w", err)
	}

	if err := c.serverInit(); err != nil {
		return fmt.Errorf("server init failed: %w", err)
	}

	return c.messageLoop()
}

// Close closes the connection.
func (c *Connection) Close() {
	// Atomic swap ensures exactly-once close semantics
	if c.closed.Swap(true) {
		return // Already closed
	}

	close(c.stopChan)

	if err := c.conn.Close(); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("error closing VNC connection")
	}
}

// onResolutionChange updates the connection's resolution.
func (c *Connection) onResolutionChange(width, height uint16) {
	c.setResolution(width, height)
}

// RemoteAddr returns the remote address of the connection.
func (c *Connection) RemoteAddr() string {
	return c.conn.RemoteAddr().String()
}

// NeedsJPEGEncoder returns true if the connection needs the JPEG encoder.
func (c *Connection) NeedsJPEGEncoder() bool {
	return c.needsJPEGEncoder.Load()
}
