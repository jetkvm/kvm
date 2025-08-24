package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
)

const (
	inputMagicNumber uint32 = 0x4A4B4D49 // "JKMI" (JetKVM Microphone Input)
	inputSocketName         = "audio_input.sock"
	maxFrameSize            = 4096                  // Maximum Opus frame size
	writeTimeout            = 15 * time.Millisecond // Non-blocking write timeout (increased for high load)
	maxDroppedFrames        = 100                   // Maximum consecutive dropped frames before reconnect
	headerSize              = 17                    // Fixed header size: 4+1+4+8 bytes
	messagePoolSize         = 256                   // Pre-allocated message pool size
)

// InputMessageType represents the type of IPC message
type InputMessageType uint8

const (
	InputMessageTypeOpusFrame InputMessageType = iota
	InputMessageTypeConfig
	InputMessageTypeStop
	InputMessageTypeHeartbeat
	InputMessageTypeAck
)

// InputIPCMessage represents a message sent over IPC
type InputIPCMessage struct {
	Magic     uint32
	Type      InputMessageType
	Length    uint32
	Timestamp int64
	Data      []byte
}

// OptimizedIPCMessage represents an optimized message with pre-allocated buffers
type OptimizedIPCMessage struct {
	header [headerSize]byte // Pre-allocated header buffer
	data   []byte           // Reusable data buffer
	msg    InputIPCMessage  // Embedded message
}

// MessagePool manages a pool of reusable messages to reduce allocations
type MessagePool struct {
	// Atomic fields MUST be first for ARM32 alignment (int64 fields need 8-byte alignment)
	hitCount  int64 // Pool hit counter (atomic)
	missCount int64 // Pool miss counter (atomic)

	// Other fields
	pool chan *OptimizedIPCMessage
	// Memory optimization fields
	preallocated []*OptimizedIPCMessage // Pre-allocated messages for immediate use
	preallocSize int                    // Number of pre-allocated messages
	maxPoolSize  int                    // Maximum pool size to prevent memory bloat
	mutex        sync.RWMutex           // Protects preallocated slice
}

// Global message pool instance
var globalMessagePool = &MessagePool{
	pool: make(chan *OptimizedIPCMessage, messagePoolSize),
}

var messagePoolInitOnce sync.Once

// initializeMessagePool initializes the message pool with pre-allocated messages
func initializeMessagePool() {
	messagePoolInitOnce.Do(func() {
		// Pre-allocate 30% of pool size for immediate availability
		preallocSize := messagePoolSize * 30 / 100
		globalMessagePool.preallocSize = preallocSize
		globalMessagePool.maxPoolSize = messagePoolSize * 2 // Allow growth up to 2x
		globalMessagePool.preallocated = make([]*OptimizedIPCMessage, 0, preallocSize)

		// Pre-allocate messages to reduce initial allocation overhead
		for i := 0; i < preallocSize; i++ {
			msg := &OptimizedIPCMessage{
				data: make([]byte, 0, maxFrameSize),
			}
			globalMessagePool.preallocated = append(globalMessagePool.preallocated, msg)
		}

		// Fill the channel pool with remaining messages
		for i := preallocSize; i < messagePoolSize; i++ {
			globalMessagePool.pool <- &OptimizedIPCMessage{
				data: make([]byte, 0, maxFrameSize),
			}
		}
	})
}

// Get retrieves a message from the pool
func (mp *MessagePool) Get() *OptimizedIPCMessage {
	initializeMessagePool()
	// First try pre-allocated messages for fastest access
	mp.mutex.Lock()
	if len(mp.preallocated) > 0 {
		msg := mp.preallocated[len(mp.preallocated)-1]
		mp.preallocated = mp.preallocated[:len(mp.preallocated)-1]
		mp.mutex.Unlock()
		atomic.AddInt64(&mp.hitCount, 1)
		return msg
	}
	mp.mutex.Unlock()

	// Try channel pool next
	select {
	case msg := <-mp.pool:
		atomic.AddInt64(&mp.hitCount, 1)
		return msg
	default:
		// Pool exhausted, create new message
		atomic.AddInt64(&mp.missCount, 1)
		return &OptimizedIPCMessage{
			data: make([]byte, 0, maxFrameSize),
		}
	}
}

// Put returns a message to the pool
func (mp *MessagePool) Put(msg *OptimizedIPCMessage) {
	// Reset the message for reuse
	msg.data = msg.data[:0]
	msg.msg = InputIPCMessage{}

	// First try to return to pre-allocated pool for fastest reuse
	mp.mutex.Lock()
	if len(mp.preallocated) < mp.preallocSize {
		mp.preallocated = append(mp.preallocated, msg)
		mp.mutex.Unlock()
		return
	}
	mp.mutex.Unlock()

	// Try channel pool next
	select {
	case mp.pool <- msg:
		// Successfully returned to pool
	default:
		// Pool full, let GC handle it
	}
}

// InputIPCConfig represents configuration for audio input
type InputIPCConfig struct {
	SampleRate int
	Channels   int
	FrameSize  int
}

// AudioInputServer handles IPC communication for audio input processing
type AudioInputServer struct {
	// Atomic fields must be first for proper alignment on ARM
	bufferSize     int64 // Current buffer size (atomic)
	processingTime int64 // Average processing time in nanoseconds (atomic)
	droppedFrames  int64 // Dropped frames counter (atomic)
	totalFrames    int64 // Total frames counter (atomic)

	listener net.Listener
	conn     net.Conn
	mtx      sync.Mutex
	running  bool

	// Triple-goroutine architecture
	messageChan chan *InputIPCMessage // Buffered channel for incoming messages
	processChan chan *InputIPCMessage // Buffered channel for processing queue
	stopChan    chan struct{}         // Stop signal for all goroutines
	wg          sync.WaitGroup        // Wait group for goroutine coordination

	// Socket buffer configuration
	socketBufferConfig SocketBufferConfig
}

// NewAudioInputServer creates a new audio input server
func NewAudioInputServer() (*AudioInputServer, error) {
	socketPath := getInputSocketPath()
	// Remove existing socket if any
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create unix socket: %w", err)
	}

	// Get initial buffer size from adaptive buffer manager
	adaptiveManager := GetAdaptiveBufferManager()
	initialBufferSize := int64(adaptiveManager.GetInputBufferSize())

	// Initialize socket buffer configuration
	socketBufferConfig := DefaultSocketBufferConfig()

	return &AudioInputServer{
		listener:           listener,
		messageChan:        make(chan *InputIPCMessage, initialBufferSize),
		processChan:        make(chan *InputIPCMessage, initialBufferSize),
		stopChan:           make(chan struct{}),
		bufferSize:         initialBufferSize,
		socketBufferConfig: socketBufferConfig,
	}, nil
}

// Start starts the audio input server
func (ais *AudioInputServer) Start() error {
	ais.mtx.Lock()
	defer ais.mtx.Unlock()

	if ais.running {
		return fmt.Errorf("server already running")
	}

	ais.running = true

	// Start triple-goroutine architecture
	ais.startReaderGoroutine()
	ais.startProcessorGoroutine()
	ais.startMonitorGoroutine()

	// Accept connections in a goroutine
	go ais.acceptConnections()

	return nil
}

// Stop stops the audio input server
func (ais *AudioInputServer) Stop() {
	ais.mtx.Lock()
	defer ais.mtx.Unlock()

	if !ais.running {
		return
	}

	ais.running = false

	// Signal all goroutines to stop
	close(ais.stopChan)
	ais.wg.Wait()

	if ais.conn != nil {
		ais.conn.Close()
		ais.conn = nil
	}

	if ais.listener != nil {
		ais.listener.Close()
	}
}

// Close closes the server and cleans up resources
func (ais *AudioInputServer) Close() {
	ais.Stop()
	// Remove socket file
	os.Remove(getInputSocketPath())
}

// acceptConnections accepts incoming connections
func (ais *AudioInputServer) acceptConnections() {
	for ais.running {
		conn, err := ais.listener.Accept()
		if err != nil {
			if ais.running {
				// Only log error if we're still supposed to be running
				continue
			}
			return
		}

		// Configure socket buffers for optimal performance
		if err := ConfigureSocketBuffers(conn, ais.socketBufferConfig); err != nil {
			// Log warning but don't fail - socket buffer optimization is not critical
			logger := logging.GetDefaultLogger().With().Str("component", "audio-input-server").Logger()
			logger.Warn().Err(err).Msg("Failed to configure socket buffers, continuing with defaults")
		} else {
			// Record socket buffer metrics for monitoring
			RecordSocketBufferMetrics(conn, "audio-input")
		}

		ais.mtx.Lock()
		// Close existing connection if any
		if ais.conn != nil {
			ais.conn.Close()
		}
		ais.conn = conn
		ais.mtx.Unlock()

		// Handle this connection
		go ais.handleConnection(conn)
	}
}

// handleConnection handles a single client connection
func (ais *AudioInputServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Connection is now handled by the reader goroutine
	// Just wait for connection to close or stop signal
	for {
		select {
		case <-ais.stopChan:
			return
		default:
			// Check if connection is still alive
			if ais.conn == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// readMessage reads a complete message from the connection
func (ais *AudioInputServer) readMessage(conn net.Conn) (*InputIPCMessage, error) {
	// Get optimized message from pool
	optMsg := globalMessagePool.Get()
	defer globalMessagePool.Put(optMsg)

	// Read header directly into pre-allocated buffer
	_, err := io.ReadFull(conn, optMsg.header[:])
	if err != nil {
		return nil, err
	}

	// Parse header using optimized access
	msg := &optMsg.msg
	msg.Magic = binary.LittleEndian.Uint32(optMsg.header[0:4])
	msg.Type = InputMessageType(optMsg.header[4])
	msg.Length = binary.LittleEndian.Uint32(optMsg.header[5:9])
	msg.Timestamp = int64(binary.LittleEndian.Uint64(optMsg.header[9:17]))

	// Validate magic number
	if msg.Magic != inputMagicNumber {
		return nil, fmt.Errorf("invalid magic number: %x", msg.Magic)
	}

	// Validate message length
	if msg.Length > maxFrameSize {
		return nil, fmt.Errorf("message too large: %d bytes", msg.Length)
	}

	// Read data if present using pooled buffer
	if msg.Length > 0 {
		// Ensure buffer capacity
		if cap(optMsg.data) < int(msg.Length) {
			optMsg.data = make([]byte, msg.Length)
		} else {
			optMsg.data = optMsg.data[:msg.Length]
		}

		_, err = io.ReadFull(conn, optMsg.data)
		if err != nil {
			return nil, err
		}
		msg.Data = optMsg.data
	}

	// Return a copy of the message (data will be copied by caller if needed)
	result := &InputIPCMessage{
		Magic:     msg.Magic,
		Type:      msg.Type,
		Length:    msg.Length,
		Timestamp: msg.Timestamp,
	}

	if msg.Length > 0 {
		// Copy data to ensure it's not affected by buffer reuse
		result.Data = make([]byte, msg.Length)
		copy(result.Data, msg.Data)
	}

	return result, nil
}

// processMessage processes a received message
func (ais *AudioInputServer) processMessage(msg *InputIPCMessage) error {
	switch msg.Type {
	case InputMessageTypeOpusFrame:
		return ais.processOpusFrame(msg.Data)
	case InputMessageTypeConfig:
		return ais.processConfig(msg.Data)
	case InputMessageTypeStop:
		return fmt.Errorf("stop message received")
	case InputMessageTypeHeartbeat:
		return ais.sendAck()
	default:
		return fmt.Errorf("unknown message type: %d", msg.Type)
	}
}

// processOpusFrame processes an Opus audio frame
func (ais *AudioInputServer) processOpusFrame(data []byte) error {
	if len(data) == 0 {
		return nil // Empty frame, ignore
	}

	// Process the Opus frame using CGO
	_, err := CGOAudioDecodeWrite(data)
	return err
}

// processConfig processes a configuration update
func (ais *AudioInputServer) processConfig(data []byte) error {
	// Acknowledge configuration receipt
	return ais.sendAck()
}

// sendAck sends an acknowledgment message
func (ais *AudioInputServer) sendAck() error {
	ais.mtx.Lock()
	defer ais.mtx.Unlock()

	if ais.conn == nil {
		return fmt.Errorf("no connection")
	}

	msg := &InputIPCMessage{
		Magic:     inputMagicNumber,
		Type:      InputMessageTypeAck,
		Length:    0,
		Timestamp: time.Now().UnixNano(),
	}

	return ais.writeMessage(ais.conn, msg)
}

// writeMessage writes a message to the connection using optimized buffers
func (ais *AudioInputServer) writeMessage(conn net.Conn, msg *InputIPCMessage) error {
	// Get optimized message from pool for header preparation
	optMsg := globalMessagePool.Get()
	defer globalMessagePool.Put(optMsg)

	// Prepare header in pre-allocated buffer
	binary.LittleEndian.PutUint32(optMsg.header[0:4], msg.Magic)
	optMsg.header[4] = byte(msg.Type)
	binary.LittleEndian.PutUint32(optMsg.header[5:9], msg.Length)
	binary.LittleEndian.PutUint64(optMsg.header[9:17], uint64(msg.Timestamp))

	// Write header
	_, err := conn.Write(optMsg.header[:])
	if err != nil {
		return err
	}

	// Write data if present
	if msg.Length > 0 && msg.Data != nil {
		_, err = conn.Write(msg.Data)
		if err != nil {
			return err
		}
	}

	return nil
}

// AudioInputClient handles IPC communication from the main process
type AudioInputClient struct {
	// Atomic fields MUST be first for ARM32 alignment (int64 fields need 8-byte alignment)
	droppedFrames int64 // Atomic counter for dropped frames
	totalFrames   int64 // Atomic counter for total frames

	conn    net.Conn
	mtx     sync.Mutex
	running bool
}

// NewAudioInputClient creates a new audio input client
func NewAudioInputClient() *AudioInputClient {
	return &AudioInputClient{}
}

// Connect connects to the audio input server
func (aic *AudioInputClient) Connect() error {
	aic.mtx.Lock()
	defer aic.mtx.Unlock()

	if aic.running {
		return nil // Already connected
	}

	socketPath := getInputSocketPath()
	// Try connecting multiple times as the server might not be ready
	// Reduced retry count and delay for faster startup
	for i := 0; i < 10; i++ {
		conn, err := net.Dial("unix", socketPath)
		if err == nil {
			aic.conn = conn
			aic.running = true
			return nil
		}
		// Exponential backoff starting at 50ms
		delay := time.Duration(50*(1<<uint(i/3))) * time.Millisecond
		if delay > 500*time.Millisecond {
			delay = 500 * time.Millisecond
		}
		time.Sleep(delay)
	}

	return fmt.Errorf("failed to connect to audio input server")
}

// Disconnect disconnects from the audio input server
func (aic *AudioInputClient) Disconnect() {
	aic.mtx.Lock()
	defer aic.mtx.Unlock()

	if !aic.running {
		return
	}

	aic.running = false

	if aic.conn != nil {
		// Send stop message
		msg := &InputIPCMessage{
			Magic:     inputMagicNumber,
			Type:      InputMessageTypeStop,
			Length:    0,
			Timestamp: time.Now().UnixNano(),
		}
		_ = aic.writeMessage(msg) // Ignore errors during shutdown

		aic.conn.Close()
		aic.conn = nil
	}
}

// SendFrame sends an Opus frame to the audio input server
func (aic *AudioInputClient) SendFrame(frame []byte) error {
	aic.mtx.Lock()
	defer aic.mtx.Unlock()

	if !aic.running || aic.conn == nil {
		return fmt.Errorf("not connected")
	}

	if len(frame) == 0 {
		return nil // Empty frame, ignore
	}

	if len(frame) > maxFrameSize {
		return fmt.Errorf("frame too large: %d bytes", len(frame))
	}

	msg := &InputIPCMessage{
		Magic:     inputMagicNumber,
		Type:      InputMessageTypeOpusFrame,
		Length:    uint32(len(frame)),
		Timestamp: time.Now().UnixNano(),
		Data:      frame,
	}

	return aic.writeMessage(msg)
}

// SendFrameZeroCopy sends a zero-copy Opus frame to the audio input server
func (aic *AudioInputClient) SendFrameZeroCopy(frame *ZeroCopyAudioFrame) error {
	aic.mtx.Lock()
	defer aic.mtx.Unlock()

	if !aic.running || aic.conn == nil {
		return fmt.Errorf("not connected")
	}

	if frame == nil || frame.Length() == 0 {
		return nil // Empty frame, ignore
	}

	if frame.Length() > maxFrameSize {
		return fmt.Errorf("frame too large: %d bytes", frame.Length())
	}

	// Use zero-copy data directly
	msg := &InputIPCMessage{
		Magic:     inputMagicNumber,
		Type:      InputMessageTypeOpusFrame,
		Length:    uint32(frame.Length()),
		Timestamp: time.Now().UnixNano(),
		Data:      frame.Data(), // Zero-copy data access
	}

	return aic.writeMessage(msg)
}

// SendConfig sends a configuration update to the audio input server
func (aic *AudioInputClient) SendConfig(config InputIPCConfig) error {
	aic.mtx.Lock()
	defer aic.mtx.Unlock()

	if !aic.running || aic.conn == nil {
		return fmt.Errorf("not connected")
	}

	// Serialize config (simple binary format)
	data := make([]byte, 12) // 3 * int32
	binary.LittleEndian.PutUint32(data[0:4], uint32(config.SampleRate))
	binary.LittleEndian.PutUint32(data[4:8], uint32(config.Channels))
	binary.LittleEndian.PutUint32(data[8:12], uint32(config.FrameSize))

	msg := &InputIPCMessage{
		Magic:     inputMagicNumber,
		Type:      InputMessageTypeConfig,
		Length:    uint32(len(data)),
		Timestamp: time.Now().UnixNano(),
		Data:      data,
	}

	return aic.writeMessage(msg)
}

// SendHeartbeat sends a heartbeat message
func (aic *AudioInputClient) SendHeartbeat() error {
	aic.mtx.Lock()
	defer aic.mtx.Unlock()

	if !aic.running || aic.conn == nil {
		return fmt.Errorf("not connected")
	}

	msg := &InputIPCMessage{
		Magic:     inputMagicNumber,
		Type:      InputMessageTypeHeartbeat,
		Length:    0,
		Timestamp: time.Now().UnixNano(),
	}

	return aic.writeMessage(msg)
}

// writeMessage writes a message to the server
func (aic *AudioInputClient) writeMessage(msg *InputIPCMessage) error {
	// Increment total frames counter
	atomic.AddInt64(&aic.totalFrames, 1)

	// Get optimized message from pool for header preparation
	optMsg := globalMessagePool.Get()
	defer globalMessagePool.Put(optMsg)

	// Prepare header in pre-allocated buffer
	binary.LittleEndian.PutUint32(optMsg.header[0:4], msg.Magic)
	optMsg.header[4] = byte(msg.Type)
	binary.LittleEndian.PutUint32(optMsg.header[5:9], msg.Length)
	binary.LittleEndian.PutUint64(optMsg.header[9:17], uint64(msg.Timestamp))

	// Use non-blocking write with timeout
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	// Create a channel to signal write completion
	done := make(chan error, 1)
	go func() {
		// Write header using pre-allocated buffer
		_, err := aic.conn.Write(optMsg.header[:])
		if err != nil {
			done <- err
			return
		}

		// Write data if present
		if msg.Length > 0 && msg.Data != nil {
			_, err = aic.conn.Write(msg.Data)
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
			atomic.AddInt64(&aic.droppedFrames, 1)
			return err
		}
		return nil
	case <-ctx.Done():
		// Timeout occurred - drop frame to prevent blocking
		atomic.AddInt64(&aic.droppedFrames, 1)
		return fmt.Errorf("write timeout - frame dropped")
	}
}

// IsConnected returns whether the client is connected
func (aic *AudioInputClient) IsConnected() bool {
	aic.mtx.Lock()
	defer aic.mtx.Unlock()
	return aic.running && aic.conn != nil
}

// GetFrameStats returns frame statistics
func (aic *AudioInputClient) GetFrameStats() (total, dropped int64) {
	return atomic.LoadInt64(&aic.totalFrames), atomic.LoadInt64(&aic.droppedFrames)
}

// GetDropRate returns the current frame drop rate as a percentage
func (aic *AudioInputClient) GetDropRate() float64 {
	total := atomic.LoadInt64(&aic.totalFrames)
	dropped := atomic.LoadInt64(&aic.droppedFrames)
	if total == 0 {
		return 0.0
	}
	return float64(dropped) / float64(total) * 100.0
}

// ResetStats resets frame statistics
func (aic *AudioInputClient) ResetStats() {
	atomic.StoreInt64(&aic.totalFrames, 0)
	atomic.StoreInt64(&aic.droppedFrames, 0)
}

// startReaderGoroutine starts the message reader goroutine
func (ais *AudioInputServer) startReaderGoroutine() {
	ais.wg.Add(1)
	go func() {
		defer ais.wg.Done()
		for {
			select {
			case <-ais.stopChan:
				return
			default:
				if ais.conn != nil {
					msg, err := ais.readMessage(ais.conn)
					if err != nil {
						continue // Connection error, retry
					}
					// Send to message channel with non-blocking write
					select {
					case ais.messageChan <- msg:
						atomic.AddInt64(&ais.totalFrames, 1)
					default:
						// Channel full, drop message
						atomic.AddInt64(&ais.droppedFrames, 1)
					}
				}
			}
		}
	}()
}

// startProcessorGoroutine starts the message processor goroutine
func (ais *AudioInputServer) startProcessorGoroutine() {
	ais.wg.Add(1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		// Set high priority for audio processing
		logger := logging.GetDefaultLogger().With().Str("component", "audio-input-processor").Logger()
		if err := SetAudioThreadPriority(); err != nil {
			logger.Warn().Err(err).Msg("Failed to set audio processing priority")
		}
		defer func() {
			if err := ResetThreadPriority(); err != nil {
				logger.Warn().Err(err).Msg("Failed to reset thread priority")
			}
		}()

		defer ais.wg.Done()
		for {
			select {
			case <-ais.stopChan:
				return
			case msg := <-ais.messageChan:
				// Intelligent frame dropping: prioritize recent frames
				if msg.Type == InputMessageTypeOpusFrame {
					// Check if processing queue is getting full
					queueLen := len(ais.processChan)
					bufferSize := int(atomic.LoadInt64(&ais.bufferSize))

					if queueLen > bufferSize*3/4 {
						// Drop oldest frames, keep newest
						select {
						case <-ais.processChan: // Remove oldest
							atomic.AddInt64(&ais.droppedFrames, 1)
						default:
						}
					}
				}

				// Send to processing queue
				select {
				case ais.processChan <- msg:
				default:
					// Processing queue full, drop frame
					atomic.AddInt64(&ais.droppedFrames, 1)
				}
			}
		}
	}()
}

// startMonitorGoroutine starts the performance monitoring goroutine
func (ais *AudioInputServer) startMonitorGoroutine() {
	ais.wg.Add(1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		// Set I/O priority for monitoring
		logger := logging.GetDefaultLogger().With().Str("component", "audio-input-monitor").Logger()
		if err := SetAudioIOThreadPriority(); err != nil {
			logger.Warn().Err(err).Msg("Failed to set audio I/O priority")
		}
		defer func() {
			if err := ResetThreadPriority(); err != nil {
				logger.Warn().Err(err).Msg("Failed to reset thread priority")
			}
		}()

		defer ais.wg.Done()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		// Buffer size update ticker (less frequent)
		bufferUpdateTicker := time.NewTicker(500 * time.Millisecond)
		defer bufferUpdateTicker.Stop()

		for {
			select {
			case <-ais.stopChan:
				return
			case <-ticker.C:
				// Process frames from processing queue
				for {
					select {
					case msg := <-ais.processChan:
						start := time.Now()
						err := ais.processMessage(msg)
						processingTime := time.Since(start)

						// Calculate end-to-end latency using message timestamp
						var latency time.Duration
						if msg.Type == InputMessageTypeOpusFrame && msg.Timestamp > 0 {
							msgTime := time.Unix(0, msg.Timestamp)
							latency = time.Since(msgTime)
							// Use exponential moving average for end-to-end latency tracking
							currentAvg := atomic.LoadInt64(&ais.processingTime)
							// Weight: 90% historical, 10% current (for smoother averaging)
							newAvg := (currentAvg*9 + latency.Nanoseconds()) / 10
							atomic.StoreInt64(&ais.processingTime, newAvg)
						} else {
							// Fallback to processing time only
							latency = processingTime
							currentAvg := atomic.LoadInt64(&ais.processingTime)
							newAvg := (currentAvg + processingTime.Nanoseconds()) / 2
							atomic.StoreInt64(&ais.processingTime, newAvg)
						}

						// Report latency to adaptive buffer manager
						ais.ReportLatency(latency)

						if err != nil {
							atomic.AddInt64(&ais.droppedFrames, 1)
						}
					default:
						// No more messages to process
						goto checkBufferUpdate
					}
				}

			checkBufferUpdate:
				// Check if we need to update buffer size
				select {
				case <-bufferUpdateTicker.C:
					// Update buffer size from adaptive buffer manager
					ais.UpdateBufferSize()
				default:
					// No buffer update needed
				}
			}
		}
	}()
}

// GetServerStats returns server performance statistics
func (ais *AudioInputServer) GetServerStats() (total, dropped int64, avgProcessingTime time.Duration, bufferSize int64) {
	return atomic.LoadInt64(&ais.totalFrames),
		atomic.LoadInt64(&ais.droppedFrames),
		time.Duration(atomic.LoadInt64(&ais.processingTime)),
		atomic.LoadInt64(&ais.bufferSize)
}

// UpdateBufferSize updates the buffer size from adaptive buffer manager
func (ais *AudioInputServer) UpdateBufferSize() {
	adaptiveManager := GetAdaptiveBufferManager()
	newSize := int64(adaptiveManager.GetInputBufferSize())
	atomic.StoreInt64(&ais.bufferSize, newSize)
}

// ReportLatency reports processing latency to adaptive buffer manager
func (ais *AudioInputServer) ReportLatency(latency time.Duration) {
	adaptiveManager := GetAdaptiveBufferManager()
	adaptiveManager.UpdateLatency(latency)
}

// GetMessagePoolStats returns detailed statistics about the message pool
func (mp *MessagePool) GetMessagePoolStats() MessagePoolStats {
	mp.mutex.RLock()
	preallocatedCount := len(mp.preallocated)
	mp.mutex.RUnlock()

	hitCount := atomic.LoadInt64(&mp.hitCount)
	missCount := atomic.LoadInt64(&mp.missCount)
	totalRequests := hitCount + missCount

	var hitRate float64
	if totalRequests > 0 {
		hitRate = float64(hitCount) / float64(totalRequests) * 100
	}

	// Calculate channel pool size
	channelPoolSize := len(mp.pool)

	return MessagePoolStats{
		MaxPoolSize:       mp.maxPoolSize,
		ChannelPoolSize:   channelPoolSize,
		PreallocatedCount: int64(preallocatedCount),
		PreallocatedMax:   int64(mp.preallocSize),
		HitCount:          hitCount,
		MissCount:         missCount,
		HitRate:           hitRate,
	}
}

// MessagePoolStats provides detailed message pool statistics
type MessagePoolStats struct {
	MaxPoolSize       int
	ChannelPoolSize   int
	PreallocatedCount int64
	PreallocatedMax   int64
	HitCount          int64
	MissCount         int64
	HitRate           float64 // Percentage
}

// GetGlobalMessagePoolStats returns statistics for the global message pool
func GetGlobalMessagePoolStats() MessagePoolStats {
	return globalMessagePool.GetMessagePoolStats()
}

// Helper functions

// getInputSocketPath returns the path to the input socket
func getInputSocketPath() string {
	if path := os.Getenv("JETKVM_AUDIO_INPUT_SOCKET"); path != "" {
		return path
	}
	return filepath.Join("/var/run", inputSocketName)
}
