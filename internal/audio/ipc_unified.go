package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// Unified IPC constants
var (
	outputMagicNumber uint32 = GetConfig().OutputMagicNumber // "JKOU" (JetKVM Output)
	inputMagicNumber  uint32 = GetConfig().InputMagicNumber  // "JKMI" (JetKVM Microphone Input)
	outputSocketName         = "audio_output.sock"
	inputSocketName          = "audio_input.sock"
	headerSize               = 17 // Fixed header size: 4+1+4+8 bytes
)

// Header buffer pool to reduce allocation overhead
var headerBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, headerSize)
		return &buf
	},
}

// UnifiedMessageType represents the type of IPC message for both input and output
type UnifiedMessageType uint8

const (
	MessageTypeOpusFrame UnifiedMessageType = iota
	MessageTypeConfig
	MessageTypeOpusConfig
	MessageTypeStop
	MessageTypeHeartbeat
	MessageTypeAck
)

// UnifiedIPCMessage represents a message sent over IPC for both input and output
type UnifiedIPCMessage struct {
	Magic     uint32
	Type      UnifiedMessageType
	Length    uint32
	Timestamp int64
	Data      []byte
}

// Implement IPCMessage interface
func (msg *UnifiedIPCMessage) GetMagic() uint32 {
	return msg.Magic
}

func (msg *UnifiedIPCMessage) GetType() uint8 {
	return uint8(msg.Type)
}

func (msg *UnifiedIPCMessage) GetLength() uint32 {
	return msg.Length
}

func (msg *UnifiedIPCMessage) GetTimestamp() int64 {
	return msg.Timestamp
}

func (msg *UnifiedIPCMessage) GetData() []byte {
	return msg.Data
}

// UnifiedIPCConfig represents configuration for audio
type UnifiedIPCConfig struct {
	SampleRate int
	Channels   int
	FrameSize  int
}

// UnifiedIPCOpusConfig represents Opus-specific configuration
type UnifiedIPCOpusConfig struct {
	SampleRate int
	Channels   int
	FrameSize  int
	Bitrate    int
	Complexity int
	VBR        int
	SignalType int
	Bandwidth  int
	DTX        int
}

// UnifiedAudioServer provides common functionality for both input and output servers
type UnifiedAudioServer struct {
	// Atomic counters for performance monitoring
	bufferSize    int64 // Current buffer size (atomic)
	droppedFrames int64 // Dropped frames counter (atomic)
	totalFrames   int64 // Total frames counter (atomic)

	listener net.Listener
	conn     net.Conn
	mtx      sync.Mutex
	running  bool
	logger   zerolog.Logger

	// Message channels
	messageChan chan *UnifiedIPCMessage // Buffered channel for incoming messages
	processChan chan *UnifiedIPCMessage // Buffered channel for processing queue
	wg          sync.WaitGroup          // Wait group for goroutine coordination

	// Configuration
	socketPath         string
	magicNumber        uint32
	socketBufferConfig SocketBufferConfig

	// Performance monitoring
	latencyMonitor    *LatencyMonitor
	adaptiveOptimizer *AdaptiveOptimizer
}

// NewUnifiedAudioServer creates a new unified audio server
func NewUnifiedAudioServer(isInput bool) (*UnifiedAudioServer, error) {
	var socketPath string
	var magicNumber uint32
	var componentName string

	if isInput {
		socketPath = getInputSocketPath()
		magicNumber = inputMagicNumber
		componentName = "audio-input-server"
	} else {
		socketPath = getOutputSocketPath()
		magicNumber = outputMagicNumber
		componentName = "audio-output-server"
	}

	logger := logging.GetDefaultLogger().With().Str("component", componentName).Logger()

	server := &UnifiedAudioServer{
		logger:             logger,
		socketPath:         socketPath,
		magicNumber:        magicNumber,
		messageChan:        make(chan *UnifiedIPCMessage, GetConfig().ChannelBufferSize),
		processChan:        make(chan *UnifiedIPCMessage, GetConfig().ChannelBufferSize),
		socketBufferConfig: DefaultSocketBufferConfig(),
		latencyMonitor:     nil,
		adaptiveOptimizer:  nil,
	}

	return server, nil
}

// Start starts the unified audio server
func (s *UnifiedAudioServer) Start() error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.running {
		return fmt.Errorf("server already running")
	}

	// Remove existing socket file
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	// Create listener
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}

	s.listener = listener
	s.running = true

	// Start goroutines
	s.wg.Add(3)
	go s.acceptConnections()
	go s.startReaderGoroutine()
	go s.startProcessorGoroutine()

	s.logger.Info().Str("socket_path", s.socketPath).Msg("Unified audio server started")
	return nil
}

// Stop stops the unified audio server
func (s *UnifiedAudioServer) Stop() {
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

	// Wait for goroutines to finish
	s.wg.Wait()

	// Remove socket file
	os.Remove(s.socketPath)

	s.logger.Info().Msg("Unified audio server stopped")
}

// acceptConnections handles incoming connections
func (s *UnifiedAudioServer) acceptConnections() {
	defer s.wg.Done()

	for s.running {
		conn, err := AcceptConnectionWithRetry(s.listener, 3, 100*time.Millisecond)
		if err != nil {
			if s.running {
				s.logger.Error().Err(err).Msg("Failed to accept connection")
			}
			continue
		}

		s.mtx.Lock()
		if s.conn != nil {
			s.conn.Close()
		}
		s.conn = conn
		s.mtx.Unlock()

		s.logger.Info().Msg("Client connected")
	}
}

// startReaderGoroutine handles reading messages from the connection
func (s *UnifiedAudioServer) startReaderGoroutine() {
	defer s.wg.Done()

	for s.running {
		s.mtx.Lock()
		conn := s.conn
		s.mtx.Unlock()

		if conn == nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		msg, err := s.readMessage(conn)
		if err != nil {
			if s.running {
				s.logger.Error().Err(err).Msg("Failed to read message")
			}
			continue
		}

		select {
		case s.messageChan <- msg:
		default:
			atomic.AddInt64(&s.droppedFrames, 1)
			s.logger.Warn().Msg("Message channel full, dropping message")
		}
	}
}

// startProcessorGoroutine handles processing messages
func (s *UnifiedAudioServer) startProcessorGoroutine() {
	defer s.wg.Done()

	for msg := range s.messageChan {
		select {
		case s.processChan <- msg:
			atomic.AddInt64(&s.totalFrames, 1)
		default:
			atomic.AddInt64(&s.droppedFrames, 1)
			s.logger.Warn().Msg("Process channel full, dropping message")
		}
	}
}

// readMessage reads a message from the connection
func (s *UnifiedAudioServer) readMessage(conn net.Conn) (*UnifiedIPCMessage, error) {
	// Get header buffer from pool
	headerPtr := headerBufferPool.Get().(*[]byte)
	header := *headerPtr
	defer headerBufferPool.Put(headerPtr)

	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, fmt.Errorf("failed to read header: %w", err)
	}

	// Parse header
	magic := binary.LittleEndian.Uint32(header[0:4])
	if magic != s.magicNumber {
		return nil, fmt.Errorf("invalid magic number: expected %d, got %d", s.magicNumber, magic)
	}

	msgType := UnifiedMessageType(header[4])
	length := binary.LittleEndian.Uint32(header[5:9])
	timestamp := int64(binary.LittleEndian.Uint64(header[9:17]))

	// Validate length
	if length > uint32(GetConfig().MaxFrameSize) {
		return nil, fmt.Errorf("message too large: %d bytes", length)
	}

	// Read data
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

// SendFrame sends a frame to the connected client
func (s *UnifiedAudioServer) SendFrame(frame []byte) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if !s.running || s.conn == nil {
		return fmt.Errorf("no client connected")
	}

	start := time.Now()

	// Create message
	msg := &UnifiedIPCMessage{
		Magic:     s.magicNumber,
		Type:      MessageTypeOpusFrame,
		Length:    uint32(len(frame)),
		Timestamp: start.UnixNano(),
		Data:      frame,
	}

	// Write message to connection
	err := s.writeMessage(s.conn, msg)
	if err != nil {
		atomic.AddInt64(&s.droppedFrames, 1)
		return err
	}

	// Record latency for monitoring
	if s.latencyMonitor != nil {
		writeLatency := time.Since(start)
		s.latencyMonitor.RecordLatency(writeLatency, "ipc_write")
	}

	atomic.AddInt64(&s.totalFrames, 1)
	return nil
}

// writeMessage writes a message to the connection
func (s *UnifiedAudioServer) writeMessage(conn net.Conn, msg *UnifiedIPCMessage) error {
	// Get header buffer from pool
	headerPtr := headerBufferPool.Get().(*[]byte)
	header := *headerPtr
	defer headerBufferPool.Put(headerPtr)

	binary.LittleEndian.PutUint32(header[0:4], msg.Magic)
	header[4] = uint8(msg.Type)
	binary.LittleEndian.PutUint32(header[5:9], msg.Length)
	binary.LittleEndian.PutUint64(header[9:17], uint64(msg.Timestamp))

	if _, err := conn.Write(header); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	// Write data if present
	if msg.Length > 0 && msg.Data != nil {
		if _, err := conn.Write(msg.Data); err != nil {
			return fmt.Errorf("failed to write data: %w", err)
		}
	}

	return nil
}

// UnifiedAudioClient provides common functionality for both input and output clients
type UnifiedAudioClient struct {
	// Atomic fields first for ARM32 alignment
	droppedFrames int64 // Atomic counter for dropped frames
	totalFrames   int64 // Atomic counter for total frames

	conn        net.Conn
	mtx         sync.Mutex
	running     bool
	logger      zerolog.Logger
	socketPath  string
	magicNumber uint32
	bufferPool  *AudioBufferPool // Buffer pool for memory optimization
}

// NewUnifiedAudioClient creates a new unified audio client
func NewUnifiedAudioClient(isInput bool) *UnifiedAudioClient {
	var socketPath string
	var magicNumber uint32
	var componentName string

	if isInput {
		socketPath = getInputSocketPath()
		magicNumber = inputMagicNumber
		componentName = "audio-input-client"
	} else {
		socketPath = getOutputSocketPath()
		magicNumber = outputMagicNumber
		componentName = "audio-output-client"
	}

	logger := logging.GetDefaultLogger().With().Str("component", componentName).Logger()

	return &UnifiedAudioClient{
		logger:      logger,
		socketPath:  socketPath,
		magicNumber: magicNumber,
		bufferPool:  NewAudioBufferPool(GetConfig().MaxFrameSize),
	}
}

// Connect connects the client to the server
func (c *UnifiedAudioClient) Connect() error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if c.running {
		return nil // Already connected
	}

	// Ensure clean state before connecting
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	// Try connecting multiple times as the server might not be ready
	// Reduced retry count and delay for faster startup
	for i := 0; i < 10; i++ {
		conn, err := net.Dial("unix", c.socketPath)
		if err == nil {
			c.conn = conn
			c.running = true
			// Reset frame counters on successful connection
			atomic.StoreInt64(&c.totalFrames, 0)
			atomic.StoreInt64(&c.droppedFrames, 0)
			c.logger.Info().Str("socket_path", c.socketPath).Msg("Connected to server")
			return nil
		}
		// Exponential backoff starting from config
		backoffStart := GetConfig().BackoffStart
		delay := time.Duration(backoffStart.Nanoseconds()*(1<<uint(i/3))) * time.Nanosecond
		maxDelay := GetConfig().MaxRetryDelay
		if delay > maxDelay {
			delay = maxDelay
		}
		time.Sleep(delay)
	}

	// Ensure clean state on connection failure
	c.conn = nil
	c.running = false
	return fmt.Errorf("failed to connect to audio server after 10 attempts")
}

// Disconnect disconnects the client from the server
func (c *UnifiedAudioClient) Disconnect() {
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

	c.logger.Info().Msg("Disconnected from server")
}

// IsConnected returns whether the client is connected
func (c *UnifiedAudioClient) IsConnected() bool {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.running && c.conn != nil
}

// GetFrameStats returns frame statistics
func (c *UnifiedAudioClient) GetFrameStats() (total, dropped int64) {
	total = atomic.LoadInt64(&c.totalFrames)
	dropped = atomic.LoadInt64(&c.droppedFrames)
	return total, dropped
}

// Helper functions for socket paths
func getInputSocketPath() string {
	return filepath.Join(os.TempDir(), inputSocketName)
}

func getOutputSocketPath() string {
	return filepath.Join(os.TempDir(), outputSocketName)
}
