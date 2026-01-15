package kvm

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
)

// VNCServer manages the VNC server instance
type VNCServer struct {
	listener    net.Listener
	connections sync.Map // map[*VNCConnection]bool
	connCount   atomic.Int32

	// Settings
	port       int
	enabled    bool
	tlsEnabled bool

	// Lifecycle
	mu       sync.Mutex
	running  bool
	stopChan chan struct{}

	// Current video state
	width  uint16
	height uint16

	// JPEG encoder demand tracking
	jpegClientCount atomic.Int32
	jpegEncoderOn   bool
}

var (
	vncServer     *VNCServer
	vncServerOnce sync.Once
)

// GetVNCServer returns the singleton VNC server instance
func GetVNCServer() *VNCServer {
	vncServerOnce.Do(func() {
		vncServer = &VNCServer{
			port:     5900,
			enabled:  false,
			stopChan: make(chan struct{}),
		}
	})
	return vncServer
}

// Start starts the VNC server
func (s *VNCServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("VNC server already running")
	}

	// Create listener - always plain TCP
	// VeNCrypt handles TLS upgrade after protocol negotiation
	addr := fmt.Sprintf(":%d", s.port)
	listener, err := net.Listen("tcp4", addr)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}

	if s.tlsEnabled && config.TLSMode != "" && config.TLSMode != "disabled" {
		vncLogger.Info().Int("port", s.port).Msg("VNC server starting with VeNCrypt TLS support")
	} else {
		vncLogger.Info().Int("port", s.port).Msg("VNC server starting without TLS")
	}

	s.listener = listener
	s.running = true
	s.stopChan = make(chan struct{})

	// Note: JPEG encoder is started on-demand when a client needs it
	// (i.e., when a client doesn't support H.264 encoding)

	// Start accepting connections
	go s.acceptLoop()

	return nil
}

// requestJPEGEncoder is called when a client needs JPEG encoding
// It starts the encoder if this is the first client needing it
func (s *VNCServer) requestJPEGEncoder() {
	count := s.jpegClientCount.Add(1)
	vncLogger.Debug().Int32("jpegClients", count).Msg("client requesting JPEG encoder")

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.jpegEncoderOn {
		if err := nativeInstance.JpegStart(config.VNCQuality); err != nil {
			vncLogger.Warn().Err(err).Msg("failed to start JPEG encoder")
		} else {
			s.jpegEncoderOn = true
			vncLogger.Info().Msg("JPEG encoder started on-demand")
		}
	}
}

// releaseJPEGEncoder is called when a client no longer needs JPEG encoding
// It stops the encoder if no clients need it anymore
func (s *VNCServer) releaseJPEGEncoder() {
	count := s.jpegClientCount.Add(-1)
	vncLogger.Debug().Int32("jpegClients", count).Msg("client releasing JPEG encoder")

	if count <= 0 {
		s.mu.Lock()
		defer s.mu.Unlock()

		if s.jpegEncoderOn {
			_ = nativeInstance.JpegStop()
			s.jpegEncoderOn = false
			vncLogger.Info().Msg("JPEG encoder stopped (no clients need it)")
		}
	}
}

// Stop stops the VNC server
func (s *VNCServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	vncLogger.Info().Msg("stopping VNC server")

	// Signal stop
	close(s.stopChan)

	// Close listener
	if s.listener != nil {
		s.listener.Close()
	}

	// Close all connections
	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := key.(*VNCConnection); ok {
			conn.Close()
		}
		return true
	})

	// Stop JPEG encoder if running
	if s.jpegEncoderOn {
		_ = nativeInstance.JpegStop()
		s.jpegEncoderOn = false
	}
	s.jpegClientCount.Store(0)

	s.running = false
	vncLogger.Info().Msg("VNC server stopped")

	return nil
}

// IsRunning returns true if the VNC server is running
func (s *VNCServer) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// GetConnectionCount returns the number of active connections
func (s *VNCServer) GetConnectionCount() int {
	return int(s.connCount.Load())
}

// SetPort sets the VNC server port (requires restart to take effect)
func (s *VNCServer) SetPort(port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.port = port
}

// GetPort returns the VNC server port
func (s *VNCServer) GetPort() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// SetTLSEnabled sets whether TLS should be used
func (s *VNCServer) SetTLSEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tlsEnabled = enabled
}

// UpdateVideoState updates the current video resolution
func (s *VNCServer) UpdateVideoState(width, height uint16) {
	s.mu.Lock()
	s.width = width
	s.height = height
	s.mu.Unlock()

	// Notify all connections of resolution change
	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := key.(*VNCConnection); ok {
			conn.onResolutionChange(width, height)
		}
		return true
	})
}

// GetVideoState returns the current video resolution
func (s *VNCServer) GetVideoState() (uint16, uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.width, s.height
}

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

		vncLogger.Info().Str("remote", conn.RemoteAddr().String()).Msg("new VNC connection")

		// Create VNC connection handler
		vncConn := NewVNCConnection(conn, s)
		s.connections.Store(vncConn, true)
		s.connCount.Add(1)

		go func(vc *VNCConnection) {
			defer func() {
				s.connections.Delete(vc)
				s.connCount.Add(-1)
				// Release JPEG encoder if this client was using it
				if vc.needsJPEGEncoder {
					s.releaseJPEGEncoder()
				}
				// Unsubscribe from H.264 frames if subscribed
				if vc.h264Unsubscribe != nil {
					vc.h264Unsubscribe()
				}
				vncLogger.Info().Str("remote", vc.conn.RemoteAddr().String()).Msg("VNC connection closed")
			}()

			if err := vc.Handle(); err != nil {
				vncLogger.Error().Err(err).Str("remote", vc.conn.RemoteAddr().String()).Msg("VNC connection error")
			}
		}(vncConn)
	}
}

// VNCConnection represents a single VNC client connection
type VNCConnection struct {
	conn   net.Conn
	server *VNCServer

	// Protocol state
	authenticated bool
	width         uint16
	height        uint16
	pixelFormat   PixelFormat
	encodings     []int32

	// Encoding capabilities
	hasTight         bool
	hasH264          bool
	needsJPEGEncoder bool // true if this client needs JPEG (no H.264 support)

	// H.264 state
	h264ContextInitialized bool        // true after first keyframe sent
	h264FrameChan          chan []byte // channel for receiving H.264 frames
	h264Unsubscribe        func()      // function to unsubscribe from H.264 frames

	// Frame sending
	frameChan       chan []byte
	stopChan        chan struct{}
	frameRequested  atomic.Bool // True when client has requested at least one frame
	streamingActive atomic.Bool // True when continuous streaming is active

	mu sync.Mutex
}

// PixelFormat represents the VNC pixel format
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

// DefaultPixelFormat returns the default pixel format (32-bit BGRA)
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

// NewVNCConnection creates a new VNC connection handler
func NewVNCConnection(conn net.Conn, server *VNCServer) *VNCConnection {
	w, h := server.GetVideoState()
	if w == 0 {
		w = 1920
	}
	if h == 0 {
		h = 1080
	}

	return &VNCConnection{
		conn:          conn,
		server:        server,
		width:         w,
		height:        h,
		pixelFormat:   DefaultPixelFormat(),
		frameChan:     make(chan []byte, 2),
		h264FrameChan: make(chan []byte, 2),
		stopChan:      make(chan struct{}),
	}
}

// Handle handles the VNC connection
func (c *VNCConnection) Handle() error {
	defer c.conn.Close()

	// Perform RFB handshake
	if err := c.handshake(); err != nil {
		return fmt.Errorf("handshake failed: %w", err)
	}

	// Perform authentication
	if err := c.authenticate(); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Client initialization
	if err := c.clientInit(); err != nil {
		return fmt.Errorf("client init failed: %w", err)
	}

	// Server initialization
	if err := c.serverInit(); err != nil {
		return fmt.Errorf("server init failed: %w", err)
	}

	// Start frame sender goroutine
	go c.frameSender()

	// Main message loop
	return c.messageLoop()
}

// Close closes the connection
func (c *VNCConnection) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.stopChan:
		// Already closed
	default:
		close(c.stopChan)
	}
	c.conn.Close()
}

func (c *VNCConnection) onResolutionChange(width, height uint16) {
	c.mu.Lock()
	c.width = width
	c.height = height
	c.mu.Unlock()

	// Send DesktopSize pseudo-encoding if client supports it
	// This will be handled in the next frame update
}

func (c *VNCConnection) frameSender() {
	vncLogger.Debug().Str("remote", c.conn.RemoteAddr().String()).Msg("frameSender started (H.264 only)")

	for {
		select {
		case <-c.stopChan:
			vncLogger.Debug().Str("remote", c.conn.RemoteAddr().String()).Msg("frameSender stopping")
			return

		case frame := <-c.h264FrameChan:
			// Only process H.264 frames if client supports them
			c.mu.Lock()
			hasH264 := c.hasH264
			c.mu.Unlock()

			if !hasH264 {
				continue
			}

			// Only send if client has requested a frame update
			if c.frameRequested.Load() {
				c.frameRequested.Store(false)
				isKeyframe := isH264Keyframe(frame)
				if err := c.sendH264FrameUpdate(frame, isKeyframe); err != nil {
					vncLogger.Error().Err(err).Msg("failed to send H.264 frame update")
					return
				}
			}
		}
	}
}

// SendJPEGFrameDirect sends a JPEG frame directly if the client is waiting for one.
// This is called synchronously from the native callback (hot potato approach).
// Returns true if the frame was sent, false if client wasn't ready or doesn't need JPEG.
func (c *VNCConnection) SendJPEGFrameDirect(frame []byte) bool {
	// Quick checks without locking
	if !c.needsJPEGEncoder {
		return false
	}

	// VNC request-response: only send if client requested a frame
	if !c.frameRequested.CompareAndSwap(true, false) {
		return false
	}

	// Send frame synchronously
	if err := c.sendFrameUpdate(frame); err != nil {
		vncLogger.Error().Err(err).Str("remote", c.conn.RemoteAddr().String()).Msg("failed to send JPEG frame")
		return false
	}

	return true
}

// BroadcastJPEGFrame sends a JPEG frame to all connected clients that need it.
// This is called synchronously from the native callback (hot potato approach).
// The frame buffer is only valid during this call.
func (s *VNCServer) BroadcastJPEGFrame(frame []byte) {
	// Iterate over all connections and send to those that need JPEG
	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := key.(*VNCConnection); ok {
			conn.SendJPEGFrameDirect(frame)
		}
		return true
	})
}

// isH264Keyframe checks if the H.264 frame is a keyframe (IDR frame)
func isH264Keyframe(frame []byte) bool {
	// Look for NAL unit header
	// Start code: 0x00 0x00 0x00 0x01 or 0x00 0x00 0x01
	// NAL type is in lower 5 bits of the first byte after start code
	// Type 5 = IDR slice (keyframe), Type 7 = SPS, Type 8 = PPS

	for i := 0; i < len(frame)-4; i++ {
		// Check for 4-byte start code
		if frame[i] == 0 && frame[i+1] == 0 && frame[i+2] == 0 && frame[i+3] == 1 {
			if i+4 < len(frame) {
				nalType := frame[i+4] & 0x1F
				if nalType == 5 || nalType == 7 { // IDR or SPS (keyframe indicators)
					return true
				}
			}
		}
		// Check for 3-byte start code
		if frame[i] == 0 && frame[i+1] == 0 && frame[i+2] == 1 {
			if i+3 < len(frame) {
				nalType := frame[i+3] & 0x1F
				if nalType == 5 || nalType == 7 { // IDR or SPS
					return true
				}
			}
		}
	}
	return false
}

// initVNCServer initializes and starts the VNC server
func initVNCServer() {
	server := GetVNCServer()

	// Apply configuration
	server.SetPort(config.VNCPort)
	server.SetTLSEnabled(config.VNCUseTLS)

	vncLogger.Info().
		Int("port", config.VNCPort).
		Int("quality", config.VNCQuality).
		Bool("tls", config.VNCUseTLS).
		Msg("initializing VNC server")

	if err := server.Start(); err != nil {
		vncLogger.Error().Err(err).Msg("failed to start VNC server")
		return
	}

	vncLogger.Info().Int("port", config.VNCPort).Msg("VNC server started")
}
