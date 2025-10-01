package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// Unified IPC constants
var (
	outputMagicNumber uint32 = Config.OutputMagicNumber // "JKOU" (JetKVM Output)
	inputMagicNumber  uint32 = Config.InputMagicNumber  // "JKMI" (JetKVM Microphone Input)
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
	socketPath     string
	magicNumber    uint32
	sendBufferSize int
	recvBufferSize int
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
		messageChan:        make(chan *UnifiedIPCMessage, Config.ChannelBufferSize),
		processChan:        make(chan *UnifiedIPCMessage, Config.ChannelBufferSize),
		sendBufferSize:     Config.SocketOptimalBuffer,
		recvBufferSize:     Config.SocketOptimalBuffer,
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

	// Remove existing socket file with retry logic
	for i := 0; i < 3; i++ {
		if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
			s.logger.Warn().Err(err).Int("attempt", i+1).Msg("failed to remove existing socket file, retrying")
			time.Sleep(100 * time.Millisecond)
			continue
		}
		break
	}

	// Create listener with retry on address already in use
	var listener net.Listener
	var err error
	for i := 0; i < 3; i++ {
		listener, err = net.Listen("unix", s.socketPath)
		if err == nil {
			break
		}

		// If address is still in use, try to remove socket file again
		if strings.Contains(err.Error(), "address already in use") {
			s.logger.Warn().Err(err).Int("attempt", i+1).Msg("socket address in use, attempting cleanup and retry")
			os.Remove(s.socketPath)
			time.Sleep(200 * time.Millisecond)
			continue
		}

		return fmt.Errorf("failed to create unix socket: %w", err)
	}

	if err != nil {
		return fmt.Errorf("failed to create unix socket after retries: %w", err)
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
	if length > uint32(Config.MaxFrameSize) {
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
		// Silently drop frames when no client is connected
		// This prevents "no client connected" warnings during startup and quality changes
		atomic.AddInt64(&s.droppedFrames, 1)
		return nil // Return nil to avoid flooding logs with connection warnings
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

	atomic.AddInt64(&s.totalFrames, 1)
	return nil
}

// writeMessage writes a message to the connection
func (s *UnifiedAudioServer) writeMessage(conn net.Conn, msg *UnifiedIPCMessage) error {
	header := make([]byte, 17)
	EncodeMessageHeader(header, msg.Magic, uint8(msg.Type), msg.Length, msg.Timestamp)

	// Optimize: Use single write for header+data to reduce system calls
	if msg.Length > 0 && msg.Data != nil {
		// Pre-allocate combined buffer to avoid copying
		combined := make([]byte, len(header)+len(msg.Data))
		copy(combined, header)
		copy(combined[len(header):], msg.Data)
		if _, err := conn.Write(combined); err != nil {
			return fmt.Errorf("failed to write message: %w", err)
		}
	} else {
		if _, err := conn.Write(header); err != nil {
			return fmt.Errorf("failed to write header: %w", err)
		}
	}

	return nil
}

// UnifiedAudioClient provides common functionality for both input and output clients
type UnifiedAudioClient struct {
	// Atomic counters for frame statistics
	droppedFrames int64 // Atomic counter for dropped frames
	totalFrames   int64 // Atomic counter for total frames

	conn        net.Conn
	mtx         sync.Mutex
	running     bool
	logger      zerolog.Logger
	socketPath  string
	magicNumber uint32
	bufferPool  *AudioBufferPool // Buffer pool for memory optimization

	// Connection health monitoring
	lastHealthCheck   time.Time
	connectionErrors  int64 // Atomic counter for connection errors
	autoReconnect     bool  // Enable automatic reconnection
	healthCheckTicker *time.Ticker
	stopHealthCheck   chan struct{}
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
		logger:          logger,
		socketPath:      socketPath,
		magicNumber:     magicNumber,
		bufferPool:      NewAudioBufferPool(Config.MaxFrameSize),
		autoReconnect:   true, // Enable automatic reconnection by default
		stopHealthCheck: make(chan struct{}),
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
	// Use configurable retry parameters for better control
	maxAttempts := Config.MaxConnectionAttempts
	initialDelay := Config.ConnectionRetryDelay
	maxDelay := Config.MaxConnectionRetryDelay
	backoffFactor := Config.ConnectionBackoffFactor

	for i := 0; i < maxAttempts; i++ {
		// Set connection timeout for each attempt
		conn, err := net.DialTimeout("unix", c.socketPath, Config.ConnectionTimeoutDelay)
		if err == nil {
			c.conn = conn
			c.running = true
			// Reset frame counters on successful connection
			atomic.StoreInt64(&c.totalFrames, 0)
			atomic.StoreInt64(&c.droppedFrames, 0)
			atomic.StoreInt64(&c.connectionErrors, 0)
			c.lastHealthCheck = time.Now()
			// Start health check monitoring if auto-reconnect is enabled
			if c.autoReconnect {
				c.startHealthCheck()
			}
			c.logger.Info().Str("socket_path", c.socketPath).Int("attempt", i+1).Msg("Connected to server")
			return nil
		}

		// Log connection attempt failure
		c.logger.Debug().Err(err).Str("socket_path", c.socketPath).Int("attempt", i+1).Int("max_attempts", maxAttempts).Msg("Connection attempt failed")

		// Don't sleep after the last attempt
		if i < maxAttempts-1 {
			// Calculate adaptive delay based on connection failure patterns
			delay := c.calculateAdaptiveDelay(i, initialDelay, maxDelay, backoffFactor)
			time.Sleep(delay)
		}
	}

	// Ensure clean state on connection failure
	c.conn = nil
	c.running = false
	return fmt.Errorf("failed to connect to audio server after %d attempts", Config.MaxConnectionAttempts)
}

// Disconnect disconnects the client from the server
func (c *UnifiedAudioClient) Disconnect() {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if !c.running {
		return
	}

	c.running = false

	// Stop health check monitoring
	c.stopHealthCheckMonitoring()

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
	return
}

// startHealthCheck starts the connection health monitoring
func (c *UnifiedAudioClient) startHealthCheck() {
	if c.healthCheckTicker != nil {
		c.healthCheckTicker.Stop()
	}

	c.healthCheckTicker = time.NewTicker(Config.HealthCheckInterval)
	go func() {
		for {
			select {
			case <-c.healthCheckTicker.C:
				c.performHealthCheck()
			case <-c.stopHealthCheck:
				return
			}
		}
	}()
}

// stopHealthCheckMonitoring stops the health check monitoring
func (c *UnifiedAudioClient) stopHealthCheckMonitoring() {
	if c.healthCheckTicker != nil {
		c.healthCheckTicker.Stop()
		c.healthCheckTicker = nil
	}
	select {
	case c.stopHealthCheck <- struct{}{}:
	default:
	}
}

// performHealthCheck checks the connection health and attempts reconnection if needed
func (c *UnifiedAudioClient) performHealthCheck() {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if !c.running || c.conn == nil {
		return
	}

	// Simple health check: try to get connection info
	if tcpConn, ok := c.conn.(*net.UnixConn); ok {
		if _, err := tcpConn.File(); err != nil {
			// Connection is broken
			atomic.AddInt64(&c.connectionErrors, 1)
			c.logger.Warn().Err(err).Msg("Connection health check failed, attempting reconnection")

			// Close the broken connection
			c.conn.Close()
			c.conn = nil
			c.running = false

			// Attempt reconnection
			go func() {
				time.Sleep(Config.ReconnectionInterval)
				if err := c.Connect(); err != nil {
					c.logger.Error().Err(err).Msg("Failed to reconnect during health check")
				}
			}()
		}
	}

	c.lastHealthCheck = time.Now()
}

// SetAutoReconnect enables or disables automatic reconnection
func (c *UnifiedAudioClient) SetAutoReconnect(enabled bool) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	c.autoReconnect = enabled
	if !enabled {
		c.stopHealthCheckMonitoring()
	} else if c.running {
		c.startHealthCheck()
	}
}

// GetConnectionErrors returns the number of connection errors
func (c *UnifiedAudioClient) GetConnectionErrors() int64 {
	return atomic.LoadInt64(&c.connectionErrors)
}

// calculateAdaptiveDelay calculates retry delay based on system load and failure patterns
func (c *UnifiedAudioClient) calculateAdaptiveDelay(attempt int, initialDelay, maxDelay time.Duration, backoffFactor float64) time.Duration {
	// Base exponential backoff
	baseDelay := time.Duration(float64(initialDelay.Nanoseconds()) * math.Pow(backoffFactor, float64(attempt)))

	// Get connection error history for adaptive adjustment
	errorCount := atomic.LoadInt64(&c.connectionErrors)

	// Adjust delay based on recent connection errors
	// More errors = longer delays to avoid overwhelming the server
	adaptiveFactor := 1.0
	if errorCount > 5 {
		adaptiveFactor = 1.5 // 50% longer delays after many errors
	} else if errorCount > 10 {
		adaptiveFactor = 2.0 // Double delays after excessive errors
	}

	// Apply adaptive factor
	adaptiveDelay := time.Duration(float64(baseDelay.Nanoseconds()) * adaptiveFactor)

	// Ensure we don't exceed maximum delay
	if adaptiveDelay > maxDelay {
		adaptiveDelay = maxDelay
	}

	// Add small random jitter to avoid thundering herd
	jitter := time.Duration(float64(adaptiveDelay.Nanoseconds()) * 0.1 * (0.5 + float64(attempt%3)/6.0))
	adaptiveDelay += jitter

	return adaptiveDelay
}

// Helper functions for socket paths
func getInputSocketPath() string {
	return filepath.Join("/var/run", inputSocketName)
}

func getOutputSocketPath() string {
	return filepath.Join("/var/run", outputSocketName)
}
