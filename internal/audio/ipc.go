package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

const (
	outputMagicNumber      uint32 = 0x4A4B4F55 // "JKOU" (JetKVM Output)
	outputSocketName              = "audio_output.sock"
	outputMaxFrameSize            = 4096                  // Maximum Opus frame size
	outputWriteTimeout            = 10 * time.Millisecond // Non-blocking write timeout (increased for high load)
	outputMaxDroppedFrames        = 50                    // Maximum consecutive dropped frames
	outputHeaderSize              = 17                    // Fixed header size: 4+1+4+8 bytes
	outputMessagePoolSize         = 128                   // Pre-allocated message pool size
)

// OutputMessageType represents the type of IPC message
type OutputMessageType uint8

const (
	OutputMessageTypeOpusFrame OutputMessageType = iota
	OutputMessageTypeConfig
	OutputMessageTypeStop
	OutputMessageTypeHeartbeat
	OutputMessageTypeAck
)

// OutputIPCMessage represents an IPC message for audio output
type OutputIPCMessage struct {
	Magic     uint32
	Type      OutputMessageType
	Length    uint32
	Timestamp int64
	Data      []byte
}

// OutputOptimizedMessage represents a pre-allocated message for zero-allocation operations
type OutputOptimizedMessage struct {
	header [outputHeaderSize]byte // Pre-allocated header buffer
	data   []byte                 // Reusable data buffer
}

// OutputMessagePool manages pre-allocated messages for zero-allocation IPC
type OutputMessagePool struct {
	pool chan *OutputOptimizedMessage
}

// NewOutputMessagePool creates a new message pool
func NewOutputMessagePool(size int) *OutputMessagePool {
	pool := &OutputMessagePool{
		pool: make(chan *OutputOptimizedMessage, size),
	}

	// Pre-allocate messages
	for i := 0; i < size; i++ {
		msg := &OutputOptimizedMessage{
			data: make([]byte, outputMaxFrameSize),
		}
		pool.pool <- msg
	}

	return pool
}

// Get retrieves a message from the pool
func (p *OutputMessagePool) Get() *OutputOptimizedMessage {
	select {
	case msg := <-p.pool:
		return msg
	default:
		// Pool exhausted, create new message
		return &OutputOptimizedMessage{
			data: make([]byte, outputMaxFrameSize),
		}
	}
}

// Put returns a message to the pool
func (p *OutputMessagePool) Put(msg *OutputOptimizedMessage) {
	select {
	case p.pool <- msg:
		// Successfully returned to pool
	default:
		// Pool full, let GC handle it
	}
}

// Global message pool for output IPC
var globalOutputMessagePool = NewOutputMessagePool(outputMessagePoolSize)

type AudioServer struct {
	// Atomic fields must be first for proper alignment on ARM
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
}

func NewAudioServer() (*AudioServer, error) {
	socketPath := getOutputSocketPath()
	// Remove existing socket if any
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create unix socket: %w", err)
	}

	// Initialize with adaptive buffer size (start with 500 frames)
	initialBufferSize := int64(500)

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

	return &AudioServer{
		listener:          listener,
		messageChan:       make(chan *OutputIPCMessage, initialBufferSize),
		stopChan:          make(chan struct{}),
		bufferSize:        initialBufferSize,
		latencyMonitor:    latencyMonitor,
		adaptiveOptimizer: adaptiveOptimizer,
	}, nil
}

func (s *AudioServer) Start() error {
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
func (s *AudioServer) acceptConnections() {
	for s.running {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.running {
				// Only log error if we're still supposed to be running
				continue
			}
			return
		}

		s.mtx.Lock()
		// Close existing connection if any
		if s.conn != nil {
			s.conn.Close()
		}
		s.conn = conn
		s.mtx.Unlock()
	}
}

// startProcessorGoroutine starts the message processor
func (s *AudioServer) startProcessorGoroutine() {
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

func (s *AudioServer) Stop() {
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

func (s *AudioServer) Close() error {
	s.Stop()
	if s.listener != nil {
		s.listener.Close()
	}
	// Remove socket file
	os.Remove(getOutputSocketPath())
	return nil
}

func (s *AudioServer) SendFrame(frame []byte) error {
	if len(frame) > outputMaxFrameSize {
		return fmt.Errorf("frame size %d exceeds maximum %d", len(frame), outputMaxFrameSize)
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
		return fmt.Errorf("message channel full - frame dropped")
	}
}

// sendFrameToClient sends frame data directly to the connected client
func (s *AudioServer) sendFrameToClient(frame []byte) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.conn == nil {
		return fmt.Errorf("no client connected")
	}

	start := time.Now()

	// Get optimized message from pool
	optMsg := globalOutputMessagePool.Get()
	defer globalOutputMessagePool.Put(optMsg)

	// Prepare header in pre-allocated buffer
	binary.LittleEndian.PutUint32(optMsg.header[0:4], outputMagicNumber)
	optMsg.header[4] = byte(OutputMessageTypeOpusFrame)
	binary.LittleEndian.PutUint32(optMsg.header[5:9], uint32(len(frame)))
	binary.LittleEndian.PutUint64(optMsg.header[9:17], uint64(start.UnixNano()))

	// Use non-blocking write with timeout
	ctx, cancel := context.WithTimeout(context.Background(), outputWriteTimeout)
	defer cancel()

	// Create a channel to signal write completion
	done := make(chan error, 1)
	go func() {
		// Write header using pre-allocated buffer
		_, err := s.conn.Write(optMsg.header[:])
		if err != nil {
			done <- err
			return
		}

		// Write frame data
		if len(frame) > 0 {
			_, err = s.conn.Write(frame)
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()

	// Wait for completion or timeout
	select {
	case err := <-done:
		if err != nil {
			atomic.AddInt64(&s.droppedFrames, 1)
			return err
		}
		// Record latency for monitoring
		if s.latencyMonitor != nil {
			writeLatency := time.Since(start)
			s.latencyMonitor.RecordLatency(writeLatency, "ipc_write")
		}
		return nil
	case <-ctx.Done():
		// Timeout occurred - drop frame to prevent blocking
		atomic.AddInt64(&s.droppedFrames, 1)
		return fmt.Errorf("write timeout - frame dropped")
	}
}

// GetServerStats returns server performance statistics
func (s *AudioServer) GetServerStats() (total, dropped int64, bufferSize int64) {
	return atomic.LoadInt64(&s.totalFrames),
		atomic.LoadInt64(&s.droppedFrames),
		atomic.LoadInt64(&s.bufferSize)
}

type AudioClient struct {
	// Atomic fields must be first for proper alignment on ARM
	droppedFrames int64 // Atomic counter for dropped frames
	totalFrames   int64 // Atomic counter for total frames

	conn    net.Conn
	mtx     sync.Mutex
	running bool
}

func NewAudioClient() *AudioClient {
	return &AudioClient{}
}

// Connect connects to the audio output server
func (c *AudioClient) Connect() error {
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
		// Exponential backoff starting at 50ms
		delay := time.Duration(50*(1<<uint(i/3))) * time.Millisecond
		if delay > 400*time.Millisecond {
			delay = 400 * time.Millisecond
		}
		time.Sleep(delay)
	}

	return fmt.Errorf("failed to connect to audio output server")
}

// Disconnect disconnects from the audio output server
func (c *AudioClient) Disconnect() {
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
func (c *AudioClient) IsConnected() bool {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.running && c.conn != nil
}

func (c *AudioClient) Close() error {
	c.Disconnect()
	return nil
}

func (c *AudioClient) ReceiveFrame() ([]byte, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	if !c.running || c.conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	// Get optimized message from pool for header reading
	optMsg := globalOutputMessagePool.Get()
	defer globalOutputMessagePool.Put(optMsg)

	// Read header
	if _, err := io.ReadFull(c.conn, optMsg.header[:]); err != nil {
		return nil, fmt.Errorf("failed to read header: %w", err)
	}

	// Parse header
	magic := binary.LittleEndian.Uint32(optMsg.header[0:4])
	if magic != outputMagicNumber {
		return nil, fmt.Errorf("invalid magic number: %x", magic)
	}

	msgType := OutputMessageType(optMsg.header[4])
	if msgType != OutputMessageTypeOpusFrame {
		return nil, fmt.Errorf("unexpected message type: %d", msgType)
	}

	size := binary.LittleEndian.Uint32(optMsg.header[5:9])
	if size > outputMaxFrameSize {
		return nil, fmt.Errorf("frame size %d exceeds maximum %d", size, outputMaxFrameSize)
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
func (c *AudioClient) GetClientStats() (total, dropped int64) {
	return atomic.LoadInt64(&c.totalFrames),
		atomic.LoadInt64(&c.droppedFrames)
}

// Helper functions

// getOutputSocketPath returns the path to the output socket
func getOutputSocketPath() string {
	if path := os.Getenv("JETKVM_AUDIO_OUTPUT_SOCKET"); path != "" {
		return path
	}
	return filepath.Join("/var/run", outputSocketName)
}
