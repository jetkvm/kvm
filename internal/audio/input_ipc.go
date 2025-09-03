package audio

import (
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
	"github.com/rs/zerolog"
)

var (
	inputMagicNumber uint32 = GetConfig().InputMagicNumber // "JKMI" (JetKVM Microphone Input)
	inputSocketName         = "audio_input.sock"
)

const (
	headerSize = 17 // Fixed header size: 4+1+4+8 bytes - matches GetConfig().HeaderSize
)

var (
	maxFrameSize    = GetConfig().MaxFrameSize    // Maximum Opus frame size
	messagePoolSize = GetConfig().MessagePoolSize // Pre-allocated message pool size
)

// InputMessageType represents the type of IPC message
type InputMessageType uint8

const (
	InputMessageTypeOpusFrame InputMessageType = iota
	InputMessageTypeConfig
	InputMessageTypeOpusConfig
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

// Implement IPCMessage interface
func (msg *InputIPCMessage) GetMagic() uint32 {
	return msg.Magic
}

func (msg *InputIPCMessage) GetType() uint8 {
	return uint8(msg.Type)
}

func (msg *InputIPCMessage) GetLength() uint32 {
	return msg.Length
}

func (msg *InputIPCMessage) GetTimestamp() int64 {
	return msg.Timestamp
}

func (msg *InputIPCMessage) GetData() []byte {
	return msg.Data
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

// initializeMessagePool initializes the global message pool with pre-allocated messages
func initializeMessagePool() {
	messagePoolInitOnce.Do(func() {
		preallocSize := messagePoolSize / 4 // 25% pre-allocated for immediate use
		globalMessagePool.preallocSize = preallocSize
		globalMessagePool.maxPoolSize = messagePoolSize * GetConfig().PoolGrowthMultiplier // Allow growth up to 2x
		globalMessagePool.preallocated = make([]*OptimizedIPCMessage, 0, preallocSize)

		// Pre-allocate messages for immediate use
		for i := 0; i < preallocSize; i++ {
			msg := &OptimizedIPCMessage{
				data: make([]byte, 0, maxFrameSize),
			}
			globalMessagePool.preallocated = append(globalMessagePool.preallocated, msg)
		}

		// Fill the channel with remaining messages
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
		// Reset message for reuse
		msg.data = msg.data[:0]
		msg.msg = InputIPCMessage{}
		return msg
	}
	mp.mutex.Unlock()

	// Try channel pool next
	select {
	case msg := <-mp.pool:
		atomic.AddInt64(&mp.hitCount, 1)
		// Reset message for reuse and ensure proper capacity
		msg.data = msg.data[:0]
		msg.msg = InputIPCMessage{}
		// Ensure data buffer has sufficient capacity
		if cap(msg.data) < maxFrameSize {
			msg.data = make([]byte, 0, maxFrameSize)
		}
		return msg
	default:
		// Pool exhausted, create new message with exact capacity
		atomic.AddInt64(&mp.missCount, 1)
		return &OptimizedIPCMessage{
			data: make([]byte, 0, maxFrameSize),
		}
	}
}

// Put returns a message to the pool
func (mp *MessagePool) Put(msg *OptimizedIPCMessage) {
	if msg == nil {
		return
	}

	// Validate buffer capacity - reject if too small or too large
	if cap(msg.data) < maxFrameSize/2 || cap(msg.data) > maxFrameSize*2 {
		return // Let GC handle oversized or undersized buffers
	}

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

// InputIPCOpusConfig contains complete Opus encoder configuration
type InputIPCOpusConfig struct {
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

// AudioInputServer handles IPC communication for audio input processing
type AudioInputServer struct {
	// Atomic fields MUST be first for ARM32 alignment (int64 fields need 8-byte alignment)
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

	// Reset counters on start
	atomic.StoreInt64(&ais.totalFrames, 0)
	atomic.StoreInt64(&ais.droppedFrames, 0)
	atomic.StoreInt64(&ais.processingTime, 0)

	// Start triple-goroutine architecture
	ais.startReaderGoroutine()
	ais.startProcessorGoroutine()
	ais.startMonitorGoroutine()

	// Submit the connection acceptor to the audio reader pool
	if !SubmitAudioReaderTask(ais.acceptConnections) {
		// If the pool is full or shutting down, fall back to direct goroutine creation
		logger := logging.GetDefaultLogger().With().Str("component", AudioInputClientComponent).Logger()
		logger.Warn().Msg("Audio reader pool full or shutting down, falling back to direct goroutine creation")

		go ais.acceptConnections()
	}

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
				// Log error and continue accepting
				logger := logging.GetDefaultLogger().With().Str("component", "audio-input").Logger()
				logger.Warn().Err(err).Msg("failed to accept connection, retrying")
				continue
			}
			return
		}

		// Configure socket buffers for optimal performance
		if err := ConfigureSocketBuffers(conn, ais.socketBufferConfig); err != nil {
			// Log warning but don't fail - socket buffer optimization is not critical
			logger := logging.GetDefaultLogger().With().Str("component", "audio-input").Logger()
			logger.Warn().Err(err).Msg("failed to configure socket buffers, using defaults")
		} else {
			// Record socket buffer metrics for monitoring
			RecordSocketBufferMetrics(conn, "audio-input")
		}

		ais.mtx.Lock()
		// Close existing connection if any to prevent resource leaks
		if ais.conn != nil {
			ais.conn.Close()
			ais.conn = nil
		}
		ais.conn = conn
		ais.mtx.Unlock()

		// Handle this connection using the goroutine pool
		if !SubmitAudioReaderTask(func() { ais.handleConnection(conn) }) {
			// If the pool is full or shutting down, fall back to direct goroutine creation
			logger := logging.GetDefaultLogger().With().Str("component", AudioInputClientComponent).Logger()
			logger.Warn().Msg("Audio reader pool full or shutting down, falling back to direct goroutine creation")

			go ais.handleConnection(conn)
		}
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
			time.Sleep(GetConfig().DefaultSleepDuration)
		}
	}
}

// readMessage reads a message from the connection using optimized pooled buffers with validation.
//
// Validation Rules:
//   - Magic number must match InputMagicNumber ("JKMI" - JetKVM Microphone Input)
//   - Message length must not exceed MaxFrameSize (default: 4096 bytes)
//   - Header size is fixed at 17 bytes (4+1+4+8: Magic+Type+Length+Timestamp)
//   - Data length validation prevents buffer overflow attacks
//
// Message Format:
//   - Magic (4 bytes): Identifies valid JetKVM audio messages
//   - Type (1 byte): InputMessageType (OpusFrame, Config, Stop, Heartbeat, Ack)
//   - Length (4 bytes): Data payload size in bytes
//   - Timestamp (8 bytes): Message timestamp for latency tracking
//   - Data (variable): Message payload up to MaxFrameSize
//
// Error Conditions:
//   - Invalid magic number: Rejects non-JetKVM messages
//   - Message too large: Prevents memory exhaustion
//   - Connection errors: Network/socket failures
//   - Incomplete reads: Partial message reception
//
// The function uses pooled buffers for efficient memory management and
// ensures all messages conform to the JetKVM audio protocol specification.
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
		return nil, fmt.Errorf("invalid magic number: got 0x%x, expected 0x%x", msg.Magic, inputMagicNumber)
	}

	// Validate message length
	if msg.Length > uint32(maxFrameSize) {
		return nil, fmt.Errorf("message too large: got %d bytes, maximum allowed %d bytes", msg.Length, maxFrameSize)
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
	case InputMessageTypeOpusConfig:
		return ais.processOpusConfig(msg.Data)
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

	// Use ultra-fast validation for critical audio path
	if err := ValidateAudioFrame(data); err != nil {
		logger := logging.GetDefaultLogger().With().Str("component", AudioInputServerComponent).Logger()
		logger.Error().Err(err).Msg("Frame validation failed")
		return fmt.Errorf("input frame validation failed: %w", err)
	}

	// Get cached config for optimal performance
	cache := GetCachedConfig()
	cache.Update()

	// Get a PCM buffer from the pool for optimized decode-write
	pcmBuffer := GetBufferFromPool(cache.GetMaxPCMBufferSize())
	defer ReturnBufferToPool(pcmBuffer)

	// Process the Opus frame using optimized CGO implementation with separate buffers
	_, err := CGOAudioDecodeWrite(data, pcmBuffer)
	return err
}

// processConfig processes a configuration update
func (ais *AudioInputServer) processConfig(data []byte) error {
	// Validate configuration data
	if len(data) == 0 {
		return fmt.Errorf("empty configuration data")
	}

	// Basic validation for configuration size
	if err := ValidateBufferSize(len(data)); err != nil {
		logger := logging.GetDefaultLogger().With().Str("component", AudioInputServerComponent).Logger()
		logger.Error().Err(err).Msg("Configuration buffer validation failed")
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Acknowledge configuration receipt
	return ais.sendAck()
}

// processOpusConfig processes a complete Opus encoder configuration update
func (ais *AudioInputServer) processOpusConfig(data []byte) error {
	logger := logging.GetDefaultLogger().With().Str("component", AudioInputServerComponent).Logger()

	// Validate configuration data size (9 * int32 = 36 bytes)
	if len(data) != 36 {
		return fmt.Errorf("invalid Opus configuration data size: expected 36 bytes, got %d", len(data))
	}

	// Deserialize Opus configuration
	config := InputIPCOpusConfig{
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

	logger.Info().Interface("config", config).Msg("applying dynamic Opus encoder configuration")

	// Apply the Opus encoder configuration dynamically
	err := CGOUpdateOpusEncoderParams(
		config.Bitrate,
		config.Complexity,
		config.VBR,
		0, // VBR constraint - using default
		config.SignalType,
		config.Bandwidth,
		config.DTX,
	)

	if err != nil {
		logger.Error().Err(err).Msg("failed to apply Opus encoder configuration")
		return fmt.Errorf("failed to apply Opus configuration: %w", err)
	}

	logger.Info().Msg("Opus encoder configuration applied successfully")
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

// Global shared message pool for input IPC server
var globalInputServerMessagePool = NewGenericMessagePool(messagePoolSize)

// writeMessage writes a message to the connection using shared common utilities
func (ais *AudioInputServer) writeMessage(conn net.Conn, msg *InputIPCMessage) error {
	// Use shared WriteIPCMessage function with global message pool
	return WriteIPCMessage(conn, msg, globalInputServerMessagePool, &ais.droppedFrames)
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

	// Ensure clean state before connecting
	if aic.conn != nil {
		aic.conn.Close()
		aic.conn = nil
	}

	socketPath := getInputSocketPath()
	// Try connecting multiple times as the server might not be ready
	// Reduced retry count and delay for faster startup
	for i := 0; i < 10; i++ {
		conn, err := net.Dial("unix", socketPath)
		if err == nil {
			aic.conn = conn
			aic.running = true
			// Reset frame counters on successful connection
			atomic.StoreInt64(&aic.totalFrames, 0)
			atomic.StoreInt64(&aic.droppedFrames, 0)
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
	aic.conn = nil
	aic.running = false
	return fmt.Errorf("failed to connect to audio input server after 10 attempts")
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
		return fmt.Errorf("not connected to audio input server")
	}

	if len(frame) == 0 {
		return nil // Empty frame, ignore
	}

	// Validate frame data before sending
	if err := ValidateAudioFrame(frame); err != nil {
		logger := logging.GetDefaultLogger().With().Str("component", AudioInputClientComponent).Logger()
		logger.Error().Err(err).Msg("Frame validation failed")
		return fmt.Errorf("input frame validation failed: %w", err)
	}

	if len(frame) > maxFrameSize {
		return fmt.Errorf("frame too large: got %d bytes, maximum allowed %d bytes", len(frame), maxFrameSize)
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
		return fmt.Errorf("not connected to audio input server")
	}

	if frame == nil || frame.Length() == 0 {
		return nil // Empty frame, ignore
	}

	// Validate zero-copy frame before sending
	if err := ValidateZeroCopyFrame(frame); err != nil {
		logger := logging.GetDefaultLogger().With().Str("component", AudioInputClientComponent).Logger()
		logger.Error().Err(err).Msg("Zero-copy frame validation failed")
		return fmt.Errorf("input frame validation failed: %w", err)
	}

	if frame.Length() > maxFrameSize {
		return fmt.Errorf("frame too large: got %d bytes, maximum allowed %d bytes", frame.Length(), maxFrameSize)
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
		return fmt.Errorf("not connected to audio input server")
	}

	// Validate configuration parameters
	if err := ValidateInputIPCConfig(config.SampleRate, config.Channels, config.FrameSize); err != nil {
		logger := logging.GetDefaultLogger().With().Str("component", AudioInputClientComponent).Logger()
		logger.Error().Err(err).Msg("Configuration validation failed")
		return fmt.Errorf("input configuration validation failed: %w", err)
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

// SendOpusConfig sends a complete Opus encoder configuration update to the audio input server
func (aic *AudioInputClient) SendOpusConfig(config InputIPCOpusConfig) error {
	aic.mtx.Lock()
	defer aic.mtx.Unlock()

	if !aic.running || aic.conn == nil {
		return fmt.Errorf("not connected to audio input server")
	}

	// Validate configuration parameters
	if config.SampleRate <= 0 || config.Channels <= 0 || config.FrameSize <= 0 || config.Bitrate <= 0 {
		return fmt.Errorf("invalid Opus configuration: SampleRate=%d, Channels=%d, FrameSize=%d, Bitrate=%d",
			config.SampleRate, config.Channels, config.FrameSize, config.Bitrate)
	}

	// Serialize Opus configuration (9 * int32 = 36 bytes)
	data := make([]byte, 36)
	binary.LittleEndian.PutUint32(data[0:4], uint32(config.SampleRate))
	binary.LittleEndian.PutUint32(data[4:8], uint32(config.Channels))
	binary.LittleEndian.PutUint32(data[8:12], uint32(config.FrameSize))
	binary.LittleEndian.PutUint32(data[12:16], uint32(config.Bitrate))
	binary.LittleEndian.PutUint32(data[16:20], uint32(config.Complexity))
	binary.LittleEndian.PutUint32(data[20:24], uint32(config.VBR))
	binary.LittleEndian.PutUint32(data[24:28], uint32(config.SignalType))
	binary.LittleEndian.PutUint32(data[28:32], uint32(config.Bandwidth))
	binary.LittleEndian.PutUint32(data[32:36], uint32(config.DTX))

	msg := &InputIPCMessage{
		Magic:     inputMagicNumber,
		Type:      InputMessageTypeOpusConfig,
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
		return fmt.Errorf("not connected to audio input server")
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
// Global shared message pool for input IPC clients
var globalInputMessagePool = NewGenericMessagePool(messagePoolSize)

func (aic *AudioInputClient) writeMessage(msg *InputIPCMessage) error {
	// Increment total frames counter
	atomic.AddInt64(&aic.totalFrames, 1)

	// Use shared WriteIPCMessage function with global message pool
	return WriteIPCMessage(aic.conn, msg, globalInputMessagePool, &aic.droppedFrames)
}

// IsConnected returns whether the client is connected
func (aic *AudioInputClient) IsConnected() bool {
	aic.mtx.Lock()
	defer aic.mtx.Unlock()
	return aic.running && aic.conn != nil
}

// GetFrameStats returns frame statistics
func (aic *AudioInputClient) GetFrameStats() (total, dropped int64) {
	stats := GetFrameStats(&aic.totalFrames, &aic.droppedFrames)
	return stats.Total, stats.Dropped
}

// GetDropRate returns the current frame drop rate as a percentage
func (aic *AudioInputClient) GetDropRate() float64 {
	stats := GetFrameStats(&aic.totalFrames, &aic.droppedFrames)
	return CalculateDropRate(stats)
}

// ResetStats resets frame statistics
func (aic *AudioInputClient) ResetStats() {
	ResetFrameStats(&aic.totalFrames, &aic.droppedFrames)
}

// startReaderGoroutine starts the message reader using the goroutine pool
func (ais *AudioInputServer) startReaderGoroutine() {
	ais.wg.Add(1)

	// Create a reader task that will run in the goroutine pool
	readerTask := func() {
		defer ais.wg.Done()

		// Enhanced error tracking and recovery
		var consecutiveErrors int
		var lastErrorTime time.Time
		maxConsecutiveErrors := GetConfig().MaxConsecutiveErrors
		errorResetWindow := GetConfig().RestartWindow // Use existing restart window
		baseBackoffDelay := GetConfig().RetryDelay
		maxBackoffDelay := GetConfig().MaxRetryDelay

		logger := logging.GetDefaultLogger().With().Str("component", AudioInputClientComponent).Logger()

		for {
			select {
			case <-ais.stopChan:
				return
			default:
				if ais.conn != nil {
					msg, err := ais.readMessage(ais.conn)
					if err != nil {
						// Enhanced error handling with progressive backoff
						now := time.Now()

						// Reset error counter if enough time has passed
						if now.Sub(lastErrorTime) > errorResetWindow {
							consecutiveErrors = 0
						}

						consecutiveErrors++
						lastErrorTime = now

						// Log error with context
						logger.Warn().Err(err).
							Int("consecutive_errors", consecutiveErrors).
							Msg("Failed to read message from input connection")

						// Progressive backoff based on error count
						if consecutiveErrors > 1 {
							backoffDelay := time.Duration(consecutiveErrors-1) * baseBackoffDelay
							if backoffDelay > maxBackoffDelay {
								backoffDelay = maxBackoffDelay
							}
							time.Sleep(backoffDelay)
						}

						// If too many consecutive errors, close connection to force reconnect
						if consecutiveErrors >= maxConsecutiveErrors {
							logger.Error().
								Int("consecutive_errors", consecutiveErrors).
								Msg("Too many consecutive read errors, closing connection")

							ais.mtx.Lock()
							if ais.conn != nil {
								ais.conn.Close()
								ais.conn = nil
							}
							ais.mtx.Unlock()

							consecutiveErrors = 0 // Reset for next connection
						}
						continue
					}

					// Reset error counter on successful read
					if consecutiveErrors > 0 {
						consecutiveErrors = 0
						logger.Info().Msg("Input connection recovered")
					}

					// Send to message channel with non-blocking write
					select {
					case ais.messageChan <- msg:
						atomic.AddInt64(&ais.totalFrames, 1)
					default:
						// Channel full, drop message
						atomic.AddInt64(&ais.droppedFrames, 1)
						logger.Warn().Msg("Message channel full, dropping frame")
					}
				} else {
					// No connection, wait briefly before checking again
					time.Sleep(GetConfig().DefaultSleepDuration)
				}
			}
		}
	}

	// Submit the reader task to the audio reader pool
	logger := logging.GetDefaultLogger().With().Str("component", AudioInputClientComponent).Logger()
	if !SubmitAudioReaderTask(readerTask) {
		// If the pool is full or shutting down, fall back to direct goroutine creation
		logger.Warn().Msg("Audio reader pool full or shutting down, falling back to direct goroutine creation")

		go readerTask()
	}
}

// startProcessorGoroutine starts the message processor using the goroutine pool
func (ais *AudioInputServer) startProcessorGoroutine() {
	ais.wg.Add(1)

	// Create a processor task that will run in the goroutine pool
	processorTask := func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		// Set high priority for audio processing
		logger := logging.GetDefaultLogger().With().Str("component", AudioInputClientComponent).Logger()
		if err := SetAudioThreadPriority(); err != nil {
			logger.Warn().Err(err).Msg("Failed to set audio processing priority")
		}
		defer func() {
			if err := ResetThreadPriority(); err != nil {
				logger.Warn().Err(err).Msg("Failed to reset thread priority")
			}
		}()

		// Enhanced error tracking for processing
		var processingErrors int
		var lastProcessingError time.Time
		maxProcessingErrors := GetConfig().MaxConsecutiveErrors
		errorResetWindow := GetConfig().RestartWindow

		defer ais.wg.Done()
		for {
			select {
			case <-ais.stopChan:
				return
			case msg := <-ais.messageChan:
				// Process message with error handling
				start := time.Now()
				err := ais.processMessageWithRecovery(msg, logger)
				processingTime := time.Since(start)

				if err != nil {
					// Track processing errors
					now := time.Now()
					if now.Sub(lastProcessingError) > errorResetWindow {
						processingErrors = 0
					}

					processingErrors++
					lastProcessingError = now

					logger.Warn().Err(err).
						Int("processing_errors", processingErrors).
						Dur("processing_time", processingTime).
						Msg("Failed to process input message")

					// If too many processing errors, drop frames more aggressively
					if processingErrors >= maxProcessingErrors {
						logger.Error().
							Int("processing_errors", processingErrors).
							Msg("Too many processing errors, entering aggressive drop mode")

						// Clear processing queue to recover
						for len(ais.processChan) > 0 {
							select {
							case <-ais.processChan:
								atomic.AddInt64(&ais.droppedFrames, 1)
							default:
								break
							}
						}
						processingErrors = 0 // Reset after clearing queue
					}
					continue
				}

				// Reset error counter on successful processing
				if processingErrors > 0 {
					processingErrors = 0
					logger.Info().Msg("Input processing recovered")
				}

				// Update processing time metrics
				atomic.StoreInt64(&ais.processingTime, processingTime.Nanoseconds())
			}
		}
	}

	// Submit the processor task to the audio processor pool
	logger := logging.GetDefaultLogger().With().Str("component", AudioInputClientComponent).Logger()
	if !SubmitAudioProcessorTask(processorTask) {
		// If the pool is full or shutting down, fall back to direct goroutine creation
		logger.Warn().Msg("Audio processor pool full or shutting down, falling back to direct goroutine creation")

		go processorTask()
	}
}

// processMessageWithRecovery processes a message with enhanced error recovery
func (ais *AudioInputServer) processMessageWithRecovery(msg *InputIPCMessage, logger zerolog.Logger) error {
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
				logger.Debug().Msg("Dropped oldest frame to make room")
			default:
			}
		}
	}

	// Send to processing queue with timeout
	select {
	case ais.processChan <- msg:
		return nil
	case <-time.After(GetConfig().WriteTimeout):
		// Processing queue full and timeout reached, drop frame
		atomic.AddInt64(&ais.droppedFrames, 1)
		return fmt.Errorf("processing queue timeout")
	default:
		// Processing queue full, drop frame immediately
		atomic.AddInt64(&ais.droppedFrames, 1)
		return fmt.Errorf("processing queue full")
	}
}

// startMonitorGoroutine starts the performance monitoring using the goroutine pool
func (ais *AudioInputServer) startMonitorGoroutine() {
	ais.wg.Add(1)

	// Create a monitor task that will run in the goroutine pool
	monitorTask := func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		// Set I/O priority for monitoring
		logger := logging.GetDefaultLogger().With().Str("component", AudioInputClientComponent).Logger()
		if err := SetAudioIOThreadPriority(); err != nil {
			logger.Warn().Err(err).Msg("Failed to set audio I/O priority")
		}
		defer func() {
			if err := ResetThreadPriority(); err != nil {
				logger.Warn().Err(err).Msg("Failed to reset thread priority")
			}
		}()

		defer ais.wg.Done()
		ticker := time.NewTicker(GetConfig().DefaultTickerInterval)
		defer ticker.Stop()

		// Buffer size update ticker (less frequent)
		bufferUpdateTicker := time.NewTicker(GetConfig().BufferUpdateInterval)
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
	}

	// Submit the monitor task to the audio processor pool
	logger := logging.GetDefaultLogger().With().Str("component", AudioInputClientComponent).Logger()
	if !SubmitAudioProcessorTask(monitorTask) {
		// If the pool is full or shutting down, fall back to direct goroutine creation
		logger.Warn().Msg("Audio processor pool full or shutting down, falling back to direct goroutine creation")

		go monitorTask()
	}
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
		hitRate = float64(hitCount) / float64(totalRequests) * GetConfig().PercentageMultiplier
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
