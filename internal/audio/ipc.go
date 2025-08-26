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

var (
	outputMagicNumber uint32 = GetConfig().OutputMagicNumber // "JKOU" (JetKVM Output)
	outputSocketName         = "audio_output.sock"
)

// Output IPC constants are now centralized in config_constants.go
// outputMaxFrameSize, outputWriteTimeout, outputMaxDroppedFrames, outputHeaderSize, outputMessagePoolSize

// OutputMessageType represents the type of IPC message
type OutputMessageType uint8

const (
	OutputMessageTypeOpusFrame OutputMessageType = iota
	OutputMessageTypeConfig
	OutputMessageTypeStop
	OutputMessageTypeHeartbeat
	OutputMessageTypeAck
)

// OutputIPCMessage represents a message sent over IPC
type OutputIPCMessage struct {
	Magic     uint32
	Type      OutputMessageType
	Length    uint32
	Timestamp int64
	Data      []byte
}

// Implement IPCMessage interface
func (msg *OutputIPCMessage) GetMagic() uint32 {
	return msg.Magic
}

func (msg *OutputIPCMessage) GetType() uint8 {
	return uint8(msg.Type)
}

func (msg *OutputIPCMessage) GetLength() uint32 {
	return msg.Length
}

func (msg *OutputIPCMessage) GetTimestamp() int64 {
	return msg.Timestamp
}

func (msg *OutputIPCMessage) GetData() []byte {
	return msg.Data
}

// Global shared message pool for output IPC client header reading
var globalOutputClientMessagePool = NewGenericMessagePool(GetConfig().OutputMessagePoolSize)

type AudioOutputServer struct {
	// Atomic fields MUST be first for ARM32 alignment (int64 fields need 8-byte alignment)
	bufferSize    int64 // Current buffer size (atomic)
	droppedFrames int64 // Dropped frames counter (atomic)
	totalFrames   int64 // Total frames counter (atomic)

	listener net.Listener
	conn     net.Conn
	mtx      sync.Mutex
	running  bool

	// Advanced message handling
	messageChan chan *OutputIPCMessage // Buffered channel for incoming messages
	stopChan    chan struct{}          // Stop signal
	wg          sync.WaitGroup         // Wait group for goroutine coordination

	// Latency monitoring
	latencyMonitor    *LatencyMonitor
	adaptiveOptimizer *AdaptiveOptimizer

	// Socket buffer configuration
	socketBufferConfig SocketBufferConfig
}

func NewAudioOutputServer() (*AudioOutputServer, error) {
	socketPath := getOutputSocketPath()
	// Remove existing socket if any
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create unix socket: %w", err)
	}

	// Initialize with adaptive buffer size (start with 500 frames)
	initialBufferSize := int64(GetConfig().InitialBufferFrames)

	// Initialize latency monitoring
	latencyConfig := DefaultLatencyConfig()
	logger := zerolog.New(os.Stderr).With().Timestamp().Str("component", "audio-server").Logger()
	latencyMonitor := NewLatencyMonitor(latencyConfig, logger)

	// Initialize adaptive buffer manager with default config
	bufferConfig := DefaultAdaptiveBufferConfig()
	bufferManager := NewAdaptiveBufferManager(bufferConfig)

	// Initialize adaptive optimizer
	optimizerConfig := DefaultOptimizerConfig()
	adaptiveOptimizer := NewAdaptiveOptimizer(latencyMonitor, bufferManager, optimizerConfig, logger)

	// Initialize socket buffer configuration
	socketBufferConfig := DefaultSocketBufferConfig()

	return &AudioOutputServer{
		listener:           listener,
		messageChan:        make(chan *OutputIPCMessage, initialBufferSize),
		stopChan:           make(chan struct{}),
		bufferSize:         initialBufferSize,
		latencyMonitor:     latencyMonitor,
		adaptiveOptimizer:  adaptiveOptimizer,
		socketBufferConfig: socketBufferConfig,
	}, nil
}

func (s *AudioOutputServer) Start() error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.running {
		return fmt.Errorf("server already running")
	}

	s.running = true

	// Start latency monitoring and adaptive optimization
	if s.latencyMonitor != nil {
		s.latencyMonitor.Start()
	}
	if s.adaptiveOptimizer != nil {
		s.adaptiveOptimizer.Start()
	}

	// Start message processor goroutine
	s.startProcessorGoroutine()

	// Accept connections in a goroutine
	go s.acceptConnections()

	return nil
}

// acceptConnections accepts incoming connections
func (s *AudioOutputServer) acceptConnections() {
	logger := logging.GetDefaultLogger().With().Str("component", "audio-server").Logger()
	for s.running {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.running {
				// Log warning and retry on accept failure
				logger.Warn().Err(err).Msg("Failed to accept connection, retrying")
				continue
			}
			return
		}

		// Configure socket buffers for optimal performance
		if err := ConfigureSocketBuffers(conn, s.socketBufferConfig); err != nil {
			// Log warning but don't fail - socket buffer optimization is not critical
			logger.Warn().Err(err).Msg("Failed to configure socket buffers, continuing with defaults")
		} else {
			// Record socket buffer metrics for monitoring
			RecordSocketBufferMetrics(conn, "audio-output")
		}

		s.mtx.Lock()
		// Close existing connection if any
		if s.conn != nil {
			s.conn.Close()
			s.conn = nil
		}
		s.conn = conn
		s.mtx.Unlock()
	}
}

// startProcessorGoroutine starts the message processor
func (s *AudioOutputServer) startProcessorGoroutine() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case msg := <-s.messageChan:
				// Process message (currently just frame sending)
				if msg.Type == OutputMessageTypeOpusFrame {
					if err := s.sendFrameToClient(msg.Data); err != nil {
						// Log error but continue processing
						atomic.AddInt64(&s.droppedFrames, 1)
					}
				}
			case <-s.stopChan:
				return
			}
		}
	}()
}

func (s *AudioOutputServer) Stop() {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if !s.running {
		return
	}

	s.running = false

	// Stop latency monitoring and adaptive optimization
	if s.adaptiveOptimizer != nil {
		s.adaptiveOptimizer.Stop()
	}
	if s.latencyMonitor != nil {
		s.latencyMonitor.Stop()
	}

	// Signal processor to stop
	close(s.stopChan)
	s.wg.Wait()

	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
}

func (s *AudioOutputServer) Close() error {
	s.Stop()
	if s.listener != nil {
		s.listener.Close()
	}
	// Remove socket file
	os.Remove(getOutputSocketPath())
	return nil
}

func (s *AudioOutputServer) SendFrame(frame []byte) error {
	maxFrameSize := GetConfig().OutputMaxFrameSize
	if len(frame) > maxFrameSize {
		return fmt.Errorf("output frame size validation failed: got %d bytes, maximum allowed %d bytes", len(frame), maxFrameSize)
	}

	start := time.Now()

	// Create IPC message
	msg := &OutputIPCMessage{
		Magic:     outputMagicNumber,
		Type:      OutputMessageTypeOpusFrame,
		Length:    uint32(len(frame)),
		Timestamp: start.UnixNano(),
		Data:      frame,
	}

	// Try to send via message channel (non-blocking)
	select {
	case s.messageChan <- msg:
		atomic.AddInt64(&s.totalFrames, 1)

		// Record latency for monitoring
		if s.latencyMonitor != nil {
			processingTime := time.Since(start)
			s.latencyMonitor.RecordLatency(processingTime, "ipc_send")
		}

		return nil
	default:
		// Channel full, drop frame to prevent blocking
		atomic.AddInt64(&s.droppedFrames, 1)
		return fmt.Errorf("output message channel full (capacity: %d) - frame dropped to prevent blocking", cap(s.messageChan))
	}
}

// sendFrameToClient sends frame data directly to the connected client
// Global shared message pool for output IPC server
var globalOutputServerMessagePool = NewGenericMessagePool(GetConfig().OutputMessagePoolSize)

func (s *AudioOutputServer) sendFrameToClient(frame []byte) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.conn == nil {
		return fmt.Errorf("no audio output client connected to server")
	}

	start := time.Now()

	// Create output IPC message
	msg := &OutputIPCMessage{
		Magic:     outputMagicNumber,
		Type:      OutputMessageTypeOpusFrame,
		Length:    uint32(len(frame)),
		Timestamp: start.UnixNano(),
		Data:      frame,
	}

	// Use shared WriteIPCMessage function
	err := WriteIPCMessage(s.conn, msg, globalOutputServerMessagePool, &s.droppedFrames)
	if err != nil {
		return err
	}

	// Record latency for monitoring
	if s.latencyMonitor != nil {
		writeLatency := time.Since(start)
		s.latencyMonitor.RecordLatency(writeLatency, "ipc_write")
	}

	return nil
}

// GetServerStats returns server performance statistics
func (s *AudioOutputServer) GetServerStats() (total, dropped int64, bufferSize int64) {
	stats := GetFrameStats(&s.totalFrames, &s.droppedFrames)
	return stats.Total, stats.Dropped, atomic.LoadInt64(&s.bufferSize)
}

type AudioOutputClient struct {
	// Atomic fields MUST be first for ARM32 alignment (int64 fields need 8-byte alignment)
	droppedFrames int64 // Atomic counter for dropped frames
	totalFrames   int64 // Atomic counter for total frames

	conn    net.Conn
	mtx     sync.Mutex
	running bool
}

func NewAudioOutputClient() *AudioOutputClient {
	return &AudioOutputClient{}
}

// Connect connects to the audio output server
func (c *AudioOutputClient) Connect() error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if c.running {
		return nil // Already connected
	}

	socketPath := getOutputSocketPath()
	// Try connecting multiple times as the server might not be ready
	// Reduced retry count and delay for faster startup
	for i := 0; i < 8; i++ {
		conn, err := net.Dial("unix", socketPath)
		if err == nil {
			c.conn = conn
			c.running = true
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

	return fmt.Errorf("failed to connect to audio output server at %s after %d retries", socketPath, 8)
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
}

// IsConnected returns whether the client is connected
func (c *AudioOutputClient) IsConnected() bool {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.running && c.conn != nil
}

func (c *AudioOutputClient) Close() error {
	c.Disconnect()
	return nil
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
	maxFrameSize := GetConfig().OutputMaxFrameSize
	if int(size) > maxFrameSize {
		return nil, fmt.Errorf("received frame size validation failed: got %d bytes, maximum allowed %d bytes", size, maxFrameSize)
	}

	// Read frame data
	frame := make([]byte, size)
	if size > 0 {
		if _, err := io.ReadFull(c.conn, frame); err != nil {
			return nil, fmt.Errorf("failed to read frame data: %w", err)
		}
	}

	atomic.AddInt64(&c.totalFrames, 1)
	return frame, nil
}

// GetClientStats returns client performance statistics
func (c *AudioOutputClient) GetClientStats() (total, dropped int64) {
	stats := GetFrameStats(&c.totalFrames, &c.droppedFrames)
	return stats.Total, stats.Dropped
}

// Helper functions

// getOutputSocketPath returns the path to the output socket
func getOutputSocketPath() string {
	if path := os.Getenv("JETKVM_AUDIO_OUTPUT_SOCKET"); path != "" {
		return path
	}
	return filepath.Join("/var/run", outputSocketName)
}
