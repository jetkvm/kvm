package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// Legacy aliases for backward compatibility
type OutputIPCConfig = UnifiedIPCConfig
type OutputMessageType = UnifiedMessageType
type OutputIPCMessage = UnifiedIPCMessage

// Legacy constants for backward compatibility
const (
	OutputMessageTypeOpusFrame = MessageTypeOpusFrame
	OutputMessageTypeConfig    = MessageTypeConfig
	OutputMessageTypeStop      = MessageTypeStop
	OutputMessageTypeHeartbeat = MessageTypeHeartbeat
	OutputMessageTypeAck       = MessageTypeAck
)

// Methods are now inherited from UnifiedIPCMessage

// Global shared message pool for output IPC client header reading
var globalOutputClientMessagePool = NewGenericMessagePool(Config.OutputMessagePoolSize)

// AudioOutputServer provides audio output IPC functionality
type AudioOutputServer struct {
	// Atomic counters
	bufferSize    int64 // Current buffer size (atomic)
	droppedFrames int64 // Dropped frames counter (atomic)
	totalFrames   int64 // Total frames counter (atomic)

	listener net.Listener
	conn     net.Conn
	mtx      sync.Mutex
	running  bool
	logger   zerolog.Logger

	// Message channels
	messageChan chan *OutputIPCMessage // Buffered channel for incoming messages
	processChan chan *OutputIPCMessage // Buffered channel for processing queue
	wg          sync.WaitGroup         // Wait group for goroutine coordination

	// Configuration
	socketPath  string
	magicNumber uint32
}

func NewAudioOutputServer() (*AudioOutputServer, error) {
	socketPath := getOutputSocketPath()
	logger := logging.GetDefaultLogger().With().Str("component", "audio-output-server").Logger()

	server := &AudioOutputServer{
		socketPath:  socketPath,
		magicNumber: Config.OutputMagicNumber,
		logger:      logger,
		messageChan: make(chan *OutputIPCMessage, Config.ChannelBufferSize),
		processChan: make(chan *OutputIPCMessage, Config.ChannelBufferSize),
	}

	return server, nil
}

// GetServerStats returns server performance statistics
// Start starts the audio output server
func (s *AudioOutputServer) Start() error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.running {
		return fmt.Errorf("audio output server is already running")
	}

	// Create Unix socket
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to create unix socket: %w", err)
	}

	s.listener = listener
	s.running = true

	// Start goroutines
	s.wg.Add(1)
	go s.acceptConnections()

	s.logger.Info().Str("socket_path", s.socketPath).Msg("Audio output server started")
	return nil
}

// Stop stops the audio output server
func (s *AudioOutputServer) Stop() {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if !s.running {
		return
	}

	s.running = false

	if s.listener != nil {
		s.listener.Close()
	}

	if s.conn != nil {
		s.conn.Close()
	}

	// Close channels
	close(s.messageChan)
	close(s.processChan)

	s.wg.Wait()
	s.logger.Info().Msg("Audio output server stopped")
}

// acceptConnections handles incoming connections
func (s *AudioOutputServer) acceptConnections() {
	defer s.wg.Done()

	for s.running {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.running {
				s.logger.Error().Err(err).Msg("Failed to accept connection")
			}
			return
		}

		s.mtx.Lock()
		s.conn = conn
		s.mtx.Unlock()

		s.logger.Info().Msg("Client connected to audio output server")
		// Only handle one connection at a time for simplicity
		for s.running && s.conn != nil {
			// Keep connection alive until stopped or disconnected
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// SendFrame sends an audio frame to the client
func (s *AudioOutputServer) SendFrame(frame []byte) error {
	s.mtx.Lock()
	conn := s.conn
	s.mtx.Unlock()

	if conn == nil {
		return fmt.Errorf("no client connected")
	}

	msg := &OutputIPCMessage{
		Magic:     s.magicNumber,
		Type:      OutputMessageTypeOpusFrame,
		Length:    uint32(len(frame)),
		Timestamp: time.Now().UnixNano(),
		Data:      frame,
	}

	return s.writeMessage(conn, msg)
}

// writeMessage writes a message to the connection
func (s *AudioOutputServer) writeMessage(conn net.Conn, msg *OutputIPCMessage) error {
	header := EncodeMessageHeader(msg.Magic, uint8(msg.Type), msg.Length, msg.Timestamp)

	if _, err := conn.Write(header); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	if msg.Length > 0 && msg.Data != nil {
		if _, err := conn.Write(msg.Data); err != nil {
			return fmt.Errorf("failed to write data: %w", err)
		}
	}

	atomic.AddInt64(&s.totalFrames, 1)
	return nil
}

func (s *AudioOutputServer) GetServerStats() (total, dropped int64, bufferSize int64) {
	return atomic.LoadInt64(&s.totalFrames), atomic.LoadInt64(&s.droppedFrames), atomic.LoadInt64(&s.bufferSize)
}

// AudioOutputClient provides audio output IPC client functionality
type AudioOutputClient struct {
	// Atomic counters
	droppedFrames int64 // Atomic counter for dropped frames
	totalFrames   int64 // Atomic counter for total frames

	conn        net.Conn
	mtx         sync.Mutex
	running     bool
	logger      zerolog.Logger
	socketPath  string
	magicNumber uint32
	bufferPool  *AudioBufferPool // Buffer pool for memory optimization

	// Health monitoring
	autoReconnect bool // Enable automatic reconnection
}

func NewAudioOutputClient() *AudioOutputClient {
	socketPath := getOutputSocketPath()
	logger := logging.GetDefaultLogger().With().Str("component", "audio-output-client").Logger()

	return &AudioOutputClient{
		socketPath:    socketPath,
		magicNumber:   Config.OutputMagicNumber,
		logger:        logger,
		bufferPool:    NewAudioBufferPool(Config.MaxFrameSize),
		autoReconnect: true,
	}
}

// Connect connects to the audio output server
func (c *AudioOutputClient) Connect() error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if c.running {
		return fmt.Errorf("audio output client is already connected")
	}

	conn, err := net.Dial("unix", c.socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to audio output server: %w", err)
	}

	c.conn = conn
	c.running = true
	c.logger.Info().Str("socket_path", c.socketPath).Msg("Connected to audio output server")
	return nil
}

// Disconnect disconnects from the audio output server
func (c *AudioOutputClient) Disconnect() {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if !c.running {
		return
	}

	c.running = false

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	c.logger.Info().Msg("Disconnected from audio output server")
}

// IsConnected returns whether the client is connected
func (c *AudioOutputClient) IsConnected() bool {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.running && c.conn != nil
}

func (c *AudioOutputClient) ReceiveFrame() ([]byte, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if !c.running || c.conn == nil {
		return nil, fmt.Errorf("not connected to audio output server")
	}

	// Get optimized message from pool for header reading
	optMsg := globalOutputClientMessagePool.Get()
	defer globalOutputClientMessagePool.Put(optMsg)

	// Read header
	if _, err := io.ReadFull(c.conn, optMsg.header[:]); err != nil {
		return nil, fmt.Errorf("failed to read IPC message header from audio output server: %w", err)
	}

	// Parse header
	magic := binary.LittleEndian.Uint32(optMsg.header[0:4])
	if magic != outputMagicNumber {
		return nil, fmt.Errorf("invalid magic number in IPC message: got 0x%x, expected 0x%x", magic, outputMagicNumber)
	}

	msgType := OutputMessageType(optMsg.header[4])
	if msgType != OutputMessageTypeOpusFrame {
		return nil, fmt.Errorf("unexpected message type: %d", msgType)
	}

	size := binary.LittleEndian.Uint32(optMsg.header[5:9])
	maxFrameSize := Config.OutputMaxFrameSize
	if int(size) > maxFrameSize {
		return nil, fmt.Errorf("received frame size validation failed: got %d bytes, maximum allowed %d bytes", size, maxFrameSize)
	}

	// Read frame data using buffer pool to avoid allocation
	frame := c.bufferPool.Get()
	frame = frame[:size] // Resize to actual frame size
	if size > 0 {
		if _, err := io.ReadFull(c.conn, frame); err != nil {
			c.bufferPool.Put(frame) // Return buffer on error
			return nil, fmt.Errorf("failed to read frame data: %w", err)
		}
	}

	// Note: Caller is responsible for returning frame to pool via PutAudioFrameBuffer()

	atomic.AddInt64(&c.totalFrames, 1)
	return frame, nil
}

// GetClientStats returns client performance statistics
func (c *AudioOutputClient) GetClientStats() (total, dropped int64) {
	stats := GetFrameStats(&c.totalFrames, &c.droppedFrames)
	return stats.Total, stats.Dropped
}

// Helper functions
// getOutputSocketPath is defined in ipc_unified.go
