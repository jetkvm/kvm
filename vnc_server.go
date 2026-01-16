// VNC Server Implementation
//
// This file implements the VNC server lifecycle management including:
//   - Server start/stop with graceful shutdown
//   - Connection acceptance with rate limiting
//   - JPEG encoder lifecycle (on-demand start/stop)
//   - Resolution change notifications to clients
//
// The server supports up to maxVNCConnections concurrent clients and
// integrates with the native JPEG hardware encoder for video streaming.
//
// See vnc_rfb.go for the RFB protocol implementation.

package kvm

import (
	"fmt"
	"net"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/vnctls"
)

const (
	// Maximum concurrent VNC connections for embedded device
	maxVNCConnections = 10
	// Rate limit: minimum time between new connections from same IP
	connectionRateLimitMs = 100
	// Default VNC port (standard RFB port)
	defaultVNCPort = 5900
	// Default resolution when no video signal detected
	defaultWidth  = 1920
	defaultHeight = 1080
	// Rate limit map cleanup threshold
	rateLimitCleanupThreshold = 50
	// Rate limit entry expiry duration
	rateLimitExpirySeconds = 60
	// Frame stats logging interval in seconds
	frameStatsIntervalSeconds = 5
	// JPEG encoder circuit breaker settings
	jpegEncoderMaxFailures    = 3               // failures before cooldown
	jpegEncoderCooldownPeriod = 30 * time.Second // wait before retrying
)

// VNCServer manages VNC client connections and JPEG encoder lifecycle.
//
// Lock ordering: When acquiring multiple locks, always acquire mu before rateLimitMu.
type VNCServer struct {
	listener    net.Listener
	connections sync.Map
	connCount   atomic.Int32

	port       int
	tlsEnabled bool

	mu       sync.Mutex // protects running, stopChan, width, height, jpegClientCount, jpegEncoderOn
	running  bool
	stopChan chan struct{}

	width  uint16
	height uint16

	// JPEG encoder state - protected by mu
	jpegClientCount      int32
	jpegEncoderOn        bool
	jpegEncoderFailures  int       // consecutive failures for circuit breaker
	jpegEncoderCooldown  time.Time // don't retry until after this time

	rateLimitMu  sync.Mutex           // protects lastConnTime only; acquire after mu
	lastConnTime map[string]time.Time // rate limiting per IP

	startError error // track if server failed to start for status reporting
}

var (
	vncServer     *VNCServer
	vncServerOnce sync.Once
)

func GetVNCServer() *VNCServer {
	vncServerOnce.Do(func() {
		vncServer = &VNCServer{
			port:         defaultVNCPort,
			stopChan:     make(chan struct{}),
			lastConnTime: make(map[string]time.Time),
		}
	})
	return vncServer
}

func (s *VNCServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("VNC server already running")
	}

	addr := fmt.Sprintf(":%d", s.port)
	listener, err := net.Listen("tcp4", addr)
	if err != nil {
		s.startError = err
		return fmt.Errorf("failed to create listener: %w", err)
	}

	if s.tlsEnabled && config.TLSMode != "" && config.TLSMode != "disabled" {
		vncLogger.Info().Int("port", s.port).Msg("VNC server starting with VeNCrypt TLS support")
	} else {
		vncLogger.Info().Int("port", s.port).Msg("VNC server starting without TLS")
	}

	s.listener = listener
	s.running = true
	s.startError = nil
	s.stopChan = make(chan struct{})

	go s.acceptLoop()

	return nil
}

// requestJPEGEncoder increments the JPEG client count and starts the encoder if needed.
// Returns an error if the encoder fails to start or is in cooldown due to repeated failures.
func (s *VNCServer) requestJPEGEncoder() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Circuit breaker: check if we're in cooldown after repeated failures
	if s.jpegEncoderFailures >= jpegEncoderMaxFailures {
		if time.Now().Before(s.jpegEncoderCooldown) {
			remaining := time.Until(s.jpegEncoderCooldown).Round(time.Second)
			return fmt.Errorf("JPEG encoder in cooldown after %d failures (retry in %v)", s.jpegEncoderFailures, remaining)
		}
		// Cooldown expired, reset failures and try again
		vncLogger.Info().Msg("JPEG encoder cooldown expired, retrying")
		s.jpegEncoderFailures = 0
	}

	s.jpegClientCount++
	vncLogger.Debug().Int32("jpegClients", s.jpegClientCount).Msg("client requesting JPEG encoder")

	if !s.jpegEncoderOn {
		if err := nativeInstance.JpegStart(config.VNCQuality); err != nil {
			s.jpegClientCount-- // Rollback count on failure
			s.jpegEncoderFailures++
			if s.jpegEncoderFailures >= jpegEncoderMaxFailures {
				s.jpegEncoderCooldown = time.Now().Add(jpegEncoderCooldownPeriod)
				vncLogger.Error().Err(err).Int("failures", s.jpegEncoderFailures).
					Dur("cooldown", jpegEncoderCooldownPeriod).
					Msg("JPEG encoder failed repeatedly, entering cooldown")
			} else {
				vncLogger.Error().Err(err).Int("failures", s.jpegEncoderFailures).Msg("failed to start JPEG encoder")
			}
			return fmt.Errorf("failed to start JPEG encoder: %w", err)
		}
		s.jpegEncoderOn = true
		s.jpegEncoderFailures = 0 // Reset on success
		vncLogger.Info().Int("quality", config.VNCQuality).Msg("JPEG encoder started on-demand")
	}
	return nil
}

// releaseJPEGEncoder decrements the JPEG client count and stops the encoder if no clients need it.
func (s *VNCServer) releaseJPEGEncoder() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.jpegClientCount--
	vncLogger.Debug().Int32("jpegClients", s.jpegClientCount).Msg("client releasing JPEG encoder")

	if s.jpegClientCount <= 0 {
		s.jpegClientCount = 0 // Prevent negative counts
		if s.jpegEncoderOn {
			if err := nativeInstance.JpegStop(); err != nil {
				vncLogger.Warn().Err(err).Msg("failed to stop JPEG encoder, resource may be leaked")
			} else {
				vncLogger.Info().Msg("JPEG encoder stopped (no clients need it)")
			}
			s.jpegEncoderOn = false
		}
	}
}

func (s *VNCServer) Stop() error {
	s.mu.Lock()

	if !s.running {
		s.mu.Unlock()
		return nil
	}

	vncLogger.Info().Msg("stopping VNC server")

	close(s.stopChan)

	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			vncLogger.Debug().Err(err).Msg("error closing listener")
		}
	}

	if s.jpegEncoderOn {
		if err := nativeInstance.JpegStop(); err != nil {
			// Best-effort cleanup. On restart, JpegStart will reinitialize the encoder.
			// Keeping jpegEncoderOn=true would prevent future starts, so we clear it.
			vncLogger.Warn().Err(err).Msg("failed to stop JPEG encoder during shutdown")
		}
		s.jpegEncoderOn = false
	}
	s.jpegClientCount = 0

	s.running = false
	s.mu.Unlock()

	s.rateLimitMu.Lock()
	s.lastConnTime = make(map[string]time.Time)
	s.rateLimitMu.Unlock()

	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := key.(*VNCConnection); ok {
			conn.Close()
		}
		return true
	})

	vncLogger.Info().Msg("VNC server stopped")

	return nil
}

func (s *VNCServer) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *VNCServer) GetConnectionCount() int {
	return int(s.connCount.Load())
}

func (s *VNCServer) SetPort(port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.port = port
}

func (s *VNCServer) GetPort() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

func (s *VNCServer) SetTLSEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tlsEnabled = enabled
}

func (s *VNCServer) UpdateVideoState(width, height uint16) {
	s.mu.Lock()
	oldWidth, oldHeight := s.width, s.height
	s.width = width
	s.height = height
	s.mu.Unlock()

	if oldWidth != width || oldHeight != height {
		vncLogger.Debug().
			Uint16("oldWidth", oldWidth).
			Uint16("oldHeight", oldHeight).
			Uint16("newWidth", width).
			Uint16("newHeight", height).
			Msg("VNC: video resolution changed")
	}

	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := key.(*VNCConnection); ok {
			conn.onResolutionChange(width, height)
		}
		return true
	})
}

func (s *VNCServer) GetVideoState() (uint16, uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.width, s.height
}

// checkRateLimit returns true if the connection should be rate limited
func (s *VNCServer) checkRateLimit(ip string) bool {
	s.rateLimitMu.Lock()
	defer s.rateLimitMu.Unlock()

	now := time.Now()
	if lastTime, ok := s.lastConnTime[ip]; ok {
		if now.Sub(lastTime) < time.Duration(connectionRateLimitMs)*time.Millisecond {
			return true
		}
	}
	s.lastConnTime[ip] = now

	// Clean up entries older than rateLimitExpirySeconds when map exceeds threshold
	if len(s.lastConnTime) > rateLimitCleanupThreshold {
		cutoff := now.Add(-time.Duration(rateLimitExpirySeconds) * time.Second)
		for k, v := range s.lastConnTime {
			if v.Before(cutoff) {
				delete(s.lastConnTime, k)
			}
		}
	}

	return false
}

// acceptLoop handles incoming VNC connections until the server stops.
// Each connection runs in its own goroutine with panic recovery.
func (s *VNCServer) acceptLoop() {
	for {
		select {
		case <-s.stopChan:
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.stopChan:
				return
			default:
				vncLogger.Error().Err(err).Msg("failed to accept connection")
				continue
			}
		}

		remoteAddr := conn.RemoteAddr().String()
		host, _, err := net.SplitHostPort(remoteAddr)
		if err != nil {
			vncLogger.Warn().Err(err).Str("remote", remoteAddr).Msg("failed to parse remote address, rejecting")
			if closeErr := conn.Close(); closeErr != nil && vncLogger.Debug().Enabled() {
				vncLogger.Debug().Err(closeErr).Str("remote", remoteAddr).Msg("failed to close rejected connection")
			}
			continue
		}

		if s.checkRateLimit(host) {
			vncLogger.Warn().Str("remote", remoteAddr).Msg("VNC connection rate limited")
			if closeErr := conn.Close(); closeErr != nil && vncLogger.Debug().Enabled() {
				vncLogger.Debug().Err(closeErr).Str("remote", remoteAddr).Msg("failed to close rate-limited connection")
			}
			continue
		}

		if s.connCount.Load() >= maxVNCConnections {
			vncLogger.Warn().Str("remote", remoteAddr).Int("max", maxVNCConnections).Msg("VNC connection rejected: max connections reached")
			if closeErr := conn.Close(); closeErr != nil && vncLogger.Debug().Enabled() {
				vncLogger.Debug().Err(closeErr).Str("remote", remoteAddr).Msg("failed to close max-connections connection")
			}
			continue
		}

		vncLogger.Info().Str("remote", remoteAddr).Msg("new VNC connection")

		vncConn := NewVNCConnection(conn, s)
		s.connections.Store(vncConn, true)
		s.connCount.Add(1)

		go func(vc *VNCConnection, remoteAddr string) {
			defer func() {
				if r := recover(); r != nil {
					// Include error ID for tracking and potential alerting
					// Use captured remoteAddr in case vc.conn is nil during panic
					vncLogger.Error().
						Interface("panic", r).
						Str("stack", string(debug.Stack())).
						Str("remote", remoteAddr).
						Str("errorId", "VNC_HANDLER_PANIC").
						Msg("VNC connection handler panicked - this is a bug, please report")
				}

				s.connections.Delete(vc)
				s.connCount.Add(-1)

				if vc.needsJPEGEncoder.Load() {
					s.releaseJPEGEncoder()
				}
				vncLogger.Info().Str("remote", remoteAddr).Msg("VNC connection closed")
			}()

			if err := vc.Handle(); err != nil {
				// Differentiate between normal disconnects and unexpected errors
				// EOF and timeout are expected when client disconnects
				errStr := err.Error()
				if errStr == "EOF" || strings.Contains(errStr, "timeout") || strings.Contains(errStr, "connection reset") ||
					strings.Contains(errStr, "broken pipe") || strings.Contains(errStr, "use of closed") {
					vncLogger.Debug().Err(err).Str("remote", remoteAddr).Msg("VNC connection ended")
				} else {
					vncLogger.Warn().Err(err).Str("remote", remoteAddr).Msg("VNC connection ended with unexpected error")
				}
			}
		}(vncConn, remoteAddr)
	}
}

// VNCConnection represents a single VNC client connection.
//
// Struct layout groups fields by access pattern for cache efficiency on 32-bit ARM.
// Hot path fields (atomics, conn) are placed first to minimize cache misses.
type VNCConnection struct {
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
	lastFrameLog    atomic.Int32 // Unix timestamp (valid until 2038)
	intervalSent    atomic.Int32
	intervalDropped atomic.Int32

	// === Cold path: Setup/handshake fields ===
	server           *VNCServer
	pixelFormat      PixelFormat
	pixelFormatMu    sync.Mutex // protects pixelFormat only
	needsJPEGEncoder atomic.Bool
	authFailures     int // auth failure count for rate limiting (no lock needed - single goroutine)

	// === Cold path: Rarely used buffers ===
	pixelFmtBuf  [rfbPixelFmtBufSize]byte
	encHeaderBuf [rfbEncHeaderBufSize]byte
	cutTextBuf   []byte

	// === Very cold: Logging ===
	lastPointerLogTime time.Time
	pointerEventCount  int32
}

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

// Validate checks if the pixel format has valid values
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

// packResolution packs width and height into a single uint32 for atomic access
func packResolution(w, h uint16) uint32 {
	return uint32(w)<<16 | uint32(h)
}

// unpackResolution unpacks width and height from a uint32
func unpackResolution(packed uint32) (uint16, uint16) {
	return uint16(packed >> 16), uint16(packed & 0xFFFF)
}

// getResolution returns width and height atomically (lock-free)
func (c *VNCConnection) getResolution() (uint16, uint16) {
	return unpackResolution(c.resolution.Load())
}

// setResolution sets width and height atomically
func (c *VNCConnection) setResolution(w, h uint16) {
	c.resolution.Store(packResolution(w, h))
}

func NewVNCConnection(conn net.Conn, server *VNCServer) *VNCConnection {
	w, h := server.GetVideoState()

	// Use detected resolution, or default if no video signal yet
	if w == 0 {
		w = defaultWidth
	}
	if h == 0 {
		h = defaultHeight
	}

	vc := &VNCConnection{
		conn:        conn,
		server:      server,
		pixelFormat: DefaultPixelFormat(),
		stopChan:    make(chan struct{}),
	}
	vc.setResolution(w, h)
	return vc
}

func (c *VNCConnection) Handle() error {
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

func (c *VNCConnection) Close() {
	// Atomic swap ensures exactly-once close semantics without lock contention
	if c.closed.Swap(true) {
		return // Already closed by another goroutine
	}

	// stopChan is only closed here, and atomic swap guarantees single execution
	close(c.stopChan)

	if err := c.conn.Close(); err != nil {
		vncLogger.Debug().Err(err).Msg("error closing VNC connection")
	}
}

func (c *VNCConnection) onResolutionChange(width, height uint16) {
	c.setResolution(width, height)
}

// SendJPEGFrameDirect attempts to send a JPEG frame to the client.
// Returns true if frame was sent, false if not needed or failed.
// If sending fails due to connection error, closes the connection.
func (c *VNCConnection) SendJPEGFrameDirect(frame []byte) bool {
	if c.closed.Load() {
		return false
	}

	if !c.needsJPEGEncoder.Load() {
		return false
	}

	if !c.frameRequested.CompareAndSwap(true, false) {
		// Frame dropped - client hasn't requested a new frame yet
		c.framesDropped.Add(1)
		c.intervalDropped.Add(1)
		return false
	}

	if err := c.sendFrameUpdate(frame); err != nil {
		vncLogger.Debug().Err(err).Str("remote", c.conn.RemoteAddr().String()).Msg("failed to send JPEG frame, closing connection")
		c.Close() // Close dead connection to trigger cleanup
		return false
	}

	c.framesSent.Add(1)
	c.intervalSent.Add(1)

	// Log frame stats periodically - check frame count first to avoid time.Now() syscall
	// Only check time every ~60 frames (at 60fps = ~1 second between checks)
	totalSent := c.framesSent.Load()
	if totalSent%60 == 0 {
		// Cast to int32 for 32-bit ARM efficiency (valid until 2038)
		now := int32(time.Now().Unix()) //nolint:gosec // Intentional int32 for 32-bit ARM
		lastLog := c.lastFrameLog.Load()
		if now-lastLog >= int32(frameStatsIntervalSeconds) && c.lastFrameLog.CompareAndSwap(lastLog, now) {
			// Only log if debug is enabled to avoid allocation
			if vncLogger.Debug().Enabled() {
				// Cumulative stats
				totalDropped := c.framesDropped.Load()
				total := totalSent + totalDropped
				var dropRate float64
				if total > 0 {
					dropRate = float64(totalDropped) * 100 / float64(total)
				}

				// Per-interval stats (reset after reading for next interval)
				intervalSent := c.intervalSent.Swap(0)
				intervalDropped := c.intervalDropped.Swap(0)
				intervalDuration := now - lastLog
				if intervalDuration < 1 {
					intervalDuration = 1
				}
				fps := float64(intervalSent) / float64(intervalDuration)

				vncLogger.Debug().
					Float64("fps", fps).
					Int("intervalSent", int(intervalSent)).
					Int("intervalDropped", int(intervalDropped)).
					Int("totalSent", int(totalSent)).
					Int("totalDropped", int(totalDropped)).
					Float64("dropRate", dropRate).
					Str("remote", c.conn.RemoteAddr().String()).
					Msg("VNC frame stats")
			}
		}
	}

	return true
}

// BroadcastJPEGFrame sends a JPEG frame to all connected clients that have requested one.
// Design note: This uses synchronous iteration which means a slow client can delay others.
// However, this is intentional for an embedded device because:
// 1. Each client has a 5-second write timeout (writeTimeout) preventing complete stalls
// 2. Async dispatch would create goroutine allocation GC pressure per frame
// 3. Frame channels would add memory overhead and frame duplication complexity
// 4. Maximum 10 clients (maxVNCConnections) limits worst-case latency
// If a client is consistently slow, frames are dropped via frameRequested flag backpressure.
func (s *VNCServer) BroadcastJPEGFrame(frame []byte) {
	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := key.(*VNCConnection); ok {
			conn.SendJPEGFrameDirect(frame)
		}
		return true
	})
}

// initVNCServer initializes and starts the VNC server if enabled.
// Returns an error if the server fails to start.
func initVNCServer() error {
	if !config.VNCEnabled {
		vncLogger.Info().Msg("VNC server disabled in configuration")
		return nil
	}

	// Initialize OpenSSL TLS subsystem early to check hardware crypto availability
	vnctls.Init()

	server := GetVNCServer()
	server.SetPort(config.VNCPort)
	server.SetTLSEnabled(config.VNCUseTLS)

	vncLogger.Info().
		Int("port", config.VNCPort).
		Int("quality", config.VNCQuality).
		Bool("tls", config.VNCUseTLS).
		Bool("hwCrypto", vnctls.IsHardwareCryptoEnabled()).
		Str("hwEngine", vnctls.GetHardwareCryptoEngine()).
		Int("maxConnections", maxVNCConnections).
		Msg("initializing VNC server")

	if err := server.Start(); err != nil {
		vncLogger.Error().Err(err).Msg("failed to start VNC server")
		return fmt.Errorf("failed to start VNC server: %w", err)
	}

	return nil
}
