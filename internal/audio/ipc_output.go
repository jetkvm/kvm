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

// Global shared message pool for output IPC client header reading
var globalOutputClientMessagePool = NewGenericMessagePool(Config.OutputMessagePoolSize)

// AudioOutputServer provides audio output IPC functionality
type AudioOutputServer struct {
	bufferSize    int64
	droppedFrames int64
	totalFrames   int64

	listener net.Listener
	conn     net.Conn
	mtx      sync.Mutex
	running  bool
	logger   zerolog.Logger

	messageChan chan *UnifiedIPCMessage
	processChan chan *UnifiedIPCMessage
	wg          sync.WaitGroup

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
		messageChan: make(chan *UnifiedIPCMessage, Config.ChannelBufferSize),
		processChan: make(chan *UnifiedIPCMessage, Config.ChannelBufferSize),
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
		s.listener = nil
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
		// Start message processing for this connection
		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

// handleConnection processes messages from a client connection
func (s *AudioOutputServer) handleConnection(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	for s.running {
		msg, err := s.readMessage(conn)
		if err != nil {
			if s.running {
				s.logger.Error().Err(err).Msg("Failed to read message from client")
			}
			return
		}

		if err := s.processMessage(msg); err != nil {
			s.logger.Error().Err(err).Msg("Failed to process message")
		}
	}
}

// readMessage reads a message from the connection
func (s *AudioOutputServer) readMessage(conn net.Conn) (*UnifiedIPCMessage, error) {
	header := make([]byte, 17)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, fmt.Errorf("failed to read header: %w", err)
	}

	magic := binary.LittleEndian.Uint32(header[0:4])
	if magic != s.magicNumber {
		return nil, fmt.Errorf("invalid magic number: expected %d, got %d", s.magicNumber, magic)
	}

	msgType := UnifiedMessageType(header[4])
	length := binary.LittleEndian.Uint32(header[5:9])
	timestamp := int64(binary.LittleEndian.Uint64(header[9:17]))

	var data []byte
	if length > 0 {
		data = make([]byte, length)
		if _, err := io.ReadFull(conn, data); err != nil {
			return nil, fmt.Errorf("failed to read data: %w", err)
		}
	}

	return &UnifiedIPCMessage{
		Magic:     magic,
		Type:      msgType,
		Length:    length,
		Timestamp: timestamp,
		Data:      data,
	}, nil
}

// processMessage processes a received message
func (s *AudioOutputServer) processMessage(msg *UnifiedIPCMessage) error {
	switch msg.Type {
	case MessageTypeOpusConfig:
		return s.processOpusConfig(msg.Data)
	case MessageTypeStop:
		s.logger.Info().Msg("Received stop message")
		return nil
	case MessageTypeHeartbeat:
		s.logger.Debug().Msg("Received heartbeat")
		return nil
	default:
		s.logger.Warn().Int("type", int(msg.Type)).Msg("Unknown message type")
		return nil
	}
}

// processOpusConfig processes Opus configuration updates
func (s *AudioOutputServer) processOpusConfig(data []byte) error {
	// Validate configuration data size (9 * int32 = 36 bytes)
	if len(data) != 36 {
		return fmt.Errorf("invalid Opus configuration data size: expected 36 bytes, got %d", len(data))
	}

	// Decode Opus configuration
	config := UnifiedIPCOpusConfig{
		SampleRate: int(binary.LittleEndian.Uint32(data[0:4])),
		Channels:   int(binary.LittleEndian.Uint32(data[4:8])),
		FrameSize:  int(binary.LittleEndian.Uint32(data[8:12])),
		Bitrate:    int(binary.LittleEndian.Uint32(data[12:16])),
		Complexity: int(binary.LittleEndian.Uint32(data[16:20])),
		VBR:        int(binary.LittleEndian.Uint32(data[20:24])),
		SignalType: int(binary.LittleEndian.Uint32(data[24:28])),
		Bandwidth:  int(binary.LittleEndian.Uint32(data[28:32])),
		DTX:        int(binary.LittleEndian.Uint32(data[32:36])),
	}

	s.logger.Info().Interface("config", config).Msg("Received Opus configuration update")

	// Ensure we're running in the audio server subprocess
	if !isAudioServerProcess() {
		s.logger.Warn().Msg("Opus configuration update ignored - not running in audio server subprocess")
		return nil
	}

	// Check if audio output streaming is currently active
	if atomic.LoadInt32(&outputStreamingRunning) == 0 {
		s.logger.Info().Msg("Audio output streaming not active, configuration will be applied when streaming starts")
		return nil
	}

	// Ensure capture is initialized before updating encoder parameters
	// The C function requires both encoder and capture_initialized to be true
	if err := cgoAudioInit(); err != nil {
		s.logger.Debug().Err(err).Msg("Audio capture already initialized or initialization failed")
		// Continue anyway - capture may already be initialized
	}

	// Apply configuration using CGO function (only if audio system is running)
	vbrConstraint := Config.CGOOpusVBRConstraint
	if err := updateOpusEncoderParams(config.Bitrate, config.Complexity, config.VBR, vbrConstraint, config.SignalType, config.Bandwidth, config.DTX); err != nil {
		s.logger.Error().Err(err).Msg("Failed to update Opus encoder parameters - encoder may not be initialized")
		return err
	}

	s.logger.Info().Msg("Opus encoder parameters updated successfully")
	return nil
}

// SendFrame sends an audio frame to the client
func (s *AudioOutputServer) SendFrame(frame []byte) error {
	s.mtx.Lock()
	conn := s.conn
	s.mtx.Unlock()

	if conn == nil {
		return fmt.Errorf("no client connected")
	}

	// Zero-cost trace logging for frame transmission
	if s.logger.GetLevel() <= zerolog.TraceLevel {
		totalFrames := atomic.LoadInt64(&s.totalFrames)
		if totalFrames <= 5 || totalFrames%1000 == 1 {
			s.logger.Trace().
				Int("frame_size", len(frame)).
				Int64("total_frames_sent", totalFrames).
				Msg("Sending audio frame to output client")
		}
	}

	msg := &UnifiedIPCMessage{
		Magic:     s.magicNumber,
		Type:      MessageTypeOpusFrame,
		Length:    uint32(len(frame)),
		Timestamp: time.Now().UnixNano(),
		Data:      frame,
	}

	return s.writeMessage(conn, msg)
}

// writeMessage writes a message to the connection
func (s *AudioOutputServer) writeMessage(conn net.Conn, msg *UnifiedIPCMessage) error {
	header := make([]byte, 17)
	EncodeMessageHeader(header, msg.Magic, uint8(msg.Type), msg.Length, msg.Timestamp)

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
	droppedFrames int64
	totalFrames   int64

	conn        net.Conn
	mtx         sync.Mutex
	running     bool
	logger      zerolog.Logger
	socketPath  string
	magicNumber uint32
	bufferPool  *AudioBufferPool

	autoReconnect bool
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

	msgType := UnifiedMessageType(optMsg.header[4])
	if msgType != MessageTypeOpusFrame {
		return nil, fmt.Errorf("unexpected message type: %d", msgType)
	}

	size := binary.LittleEndian.Uint32(optMsg.header[5:9])
	timestamp := int64(binary.LittleEndian.Uint64(optMsg.header[9:17]))
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

	// Zero-cost trace logging for frame reception
	if c.logger.GetLevel() <= zerolog.TraceLevel {
		totalFrames := atomic.LoadInt64(&c.totalFrames)
		if totalFrames <= 5 || totalFrames%1000 == 1 {
			c.logger.Trace().
				Int("frame_size", int(size)).
				Int64("timestamp", timestamp).
				Int64("total_frames_received", totalFrames).
				Msg("Received audio frame from output server")
		}
	}

	return frame, nil
}

// SendOpusConfig sends Opus configuration to the audio output server
func (c *AudioOutputClient) SendOpusConfig(config UnifiedIPCOpusConfig) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if !c.running || c.conn == nil {
		return fmt.Errorf("not connected to audio output server")
	}

	// Validate configuration parameters
	if config.SampleRate <= 0 || config.Channels <= 0 || config.FrameSize <= 0 || config.Bitrate <= 0 {
		return fmt.Errorf("invalid Opus configuration: SampleRate=%d, Channels=%d, FrameSize=%d, Bitrate=%d",
			config.SampleRate, config.Channels, config.FrameSize, config.Bitrate)
	}

	// Serialize Opus configuration using common function
	data := EncodeOpusConfig(config.SampleRate, config.Channels, config.FrameSize, config.Bitrate, config.Complexity, config.VBR, config.SignalType, config.Bandwidth, config.DTX)

	msg := &UnifiedIPCMessage{
		Magic:     c.magicNumber,
		Type:      MessageTypeOpusConfig,
		Length:    uint32(len(data)),
		Timestamp: time.Now().UnixNano(),
		Data:      data,
	}

	return c.writeMessage(msg)
}

// writeMessage writes a message to the connection
func (c *AudioOutputClient) writeMessage(msg *UnifiedIPCMessage) error {
	header := make([]byte, 17)
	EncodeMessageHeader(header, msg.Magic, uint8(msg.Type), msg.Length, msg.Timestamp)

	if _, err := c.conn.Write(header); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	if msg.Length > 0 && msg.Data != nil {
		if _, err := c.conn.Write(msg.Data); err != nil {
			return fmt.Errorf("failed to write data: %w", err)
		}
	}

	atomic.AddInt64(&c.totalFrames, 1)
	return nil
}

// GetClientStats returns client performance statistics
func (c *AudioOutputClient) GetClientStats() (total, dropped int64) {
	stats := GetFrameStats(&c.totalFrames, &c.droppedFrames)
	return stats.Total, stats.Dropped
}

// Helper functions
// getOutputSocketPath is defined in ipc_unified.go
