package kvm

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
)

type VNCServer struct {
	listener    net.Listener
	connections sync.Map
	connCount   atomic.Int32

	port       int
	tlsEnabled bool

	mu       sync.Mutex
	running  bool
	stopChan chan struct{}

	width  uint16
	height uint16

	jpegClientCount atomic.Int32
	jpegEncoderOn   bool
}

var (
	vncServer     *VNCServer
	vncServerOnce sync.Once
)

func GetVNCServer() *VNCServer {
	vncServerOnce.Do(func() {
		vncServer = &VNCServer{
			port:     5900,
			stopChan: make(chan struct{}),
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

	go s.acceptLoop()

	return nil
}

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

func (s *VNCServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	vncLogger.Info().Msg("stopping VNC server")

	close(s.stopChan)

	if s.listener != nil {
		s.listener.Close()
	}

	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := key.(*VNCConnection); ok {
			conn.Close()
		}
		return true
	})

	if s.jpegEncoderOn {
		_ = nativeInstance.JpegStop()
		s.jpegEncoderOn = false
	}
	s.jpegClientCount.Store(0)

	s.running = false
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
	s.width = width
	s.height = height
	s.mu.Unlock()

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

		vncConn := NewVNCConnection(conn, s)
		s.connections.Store(vncConn, true)
		s.connCount.Add(1)

		go func(vc *VNCConnection) {
			defer func() {
				s.connections.Delete(vc)
				s.connCount.Add(-1)

				needsJPEG := vc.needsJPEGEncoder.Load()
				vc.mu.Lock()
				unsubscribe := vc.h264Unsubscribe
				vc.mu.Unlock()

				if needsJPEG {
					s.releaseJPEGEncoder()
				}
				if unsubscribe != nil {
					unsubscribe()
				}
				vncLogger.Info().Str("remote", vc.conn.RemoteAddr().String()).Msg("VNC connection closed")
			}()

			if err := vc.Handle(); err != nil {
				vncLogger.Error().Err(err).Str("remote", vc.conn.RemoteAddr().String()).Msg("VNC connection error")
			}
		}(vncConn)
	}
}

type VNCConnection struct {
	conn   net.Conn
	server *VNCServer

	authenticated bool
	width         uint16
	height        uint16
	pixelFormat   PixelFormat
	encodings     []int32

	hasTight         bool
	hasH264          bool
	needsJPEGEncoder atomic.Bool

	h264ContextInitialized bool
	h264FrameChan          chan []byte
	h264Unsubscribe        func()

	stopChan       chan struct{}
	frameRequested atomic.Bool

	mu sync.Mutex

	msgBuf       [1]byte
	keyBuf       [7]byte
	pointerBuf   [5]byte
	fbReqBuf     [9]byte
	pixelFmtBuf  [19]byte
	encHeaderBuf [3]byte
	encBuf       [4]byte
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
		h264FrameChan: make(chan []byte, 2),
		stopChan:      make(chan struct{}),
	}
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

	go c.frameSender()

	return c.messageLoop()
}

func (c *VNCConnection) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.stopChan:
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
}

func (c *VNCConnection) frameSender() {
	vncLogger.Debug().Str("remote", c.conn.RemoteAddr().String()).Msg("frameSender started")

	for {
		select {
		case <-c.stopChan:
			return

		case frame := <-c.h264FrameChan:
			c.mu.Lock()
			hasH264 := c.hasH264
			c.mu.Unlock()

			if !hasH264 {
				continue
			}

			if c.frameRequested.CompareAndSwap(true, false) {
				isKeyframe := isH264Keyframe(frame)
				if err := c.sendH264FrameUpdate(frame, isKeyframe); err != nil {
					vncLogger.Error().Err(err).Msg("failed to send H.264 frame update")
					return
				}
			}
		}
	}
}

func (c *VNCConnection) SendJPEGFrameDirect(frame []byte) bool {
	if !c.needsJPEGEncoder.Load() {
		return false
	}

	if !c.frameRequested.CompareAndSwap(true, false) {
		return false
	}

	if err := c.sendFrameUpdate(frame); err != nil {
		vncLogger.Error().Err(err).Str("remote", c.conn.RemoteAddr().String()).Msg("failed to send JPEG frame")
		return false
	}

	return true
}

func (s *VNCServer) BroadcastJPEGFrame(frame []byte) {
	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := key.(*VNCConnection); ok {
			conn.SendJPEGFrameDirect(frame)
		}
		return true
	})
}

func isH264Keyframe(frame []byte) bool {
	// NAL type 5 = IDR slice (keyframe), type 7 = SPS
	n := len(frame)
	for i := 0; i < n-4; i++ {
		if frame[i] == 0 && frame[i+1] == 0 {
			if frame[i+2] == 0 && frame[i+3] == 1 && i+4 < n {
				nalType := frame[i+4] & 0x1F
				if nalType == 5 || nalType == 7 {
					return true
				}
			} else if frame[i+2] == 1 {
				nalType := frame[i+3] & 0x1F
				if nalType == 5 || nalType == 7 {
					return true
				}
			}
		}
	}
	return false
}

func initVNCServer() {
	server := GetVNCServer()
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
