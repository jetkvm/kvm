package audio

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
)

// Component name constant for logging
const (
	AudioInputClientComponent = "audio-input-client"
)

// Constants are now defined in unified_ipc.go
var (
	maxFrameSize    = Config.MaxFrameSize    // Maximum Opus frame size
	messagePoolSize = Config.MessagePoolSize // Pre-allocated message pool size
)


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
		backoffStart := Config.BackoffStart
		delay := time.Duration(backoffStart.Nanoseconds()*(1<<uint(i/3))) * time.Nanosecond
		maxDelay := Config.MaxRetryDelay
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
		msg := &UnifiedIPCMessage{
			Magic:     inputMagicNumber,
			Type:      MessageTypeStop,
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
	// Fast path validation
	if len(frame) == 0 {
		return nil
	}

	aic.mtx.Lock()
	if !aic.running || aic.conn == nil {
		aic.mtx.Unlock()
		return fmt.Errorf("not connected")
	}

	// Direct message creation without timestamp overhead
	msg := &UnifiedIPCMessage{
		Magic:  inputMagicNumber,
		Type:   MessageTypeOpusFrame,
		Length: uint32(len(frame)),
		Data:   frame,
	}

	err := aic.writeMessage(msg)
	aic.mtx.Unlock()
	return err
}

// SendFrameZeroCopy sends a zero-copy Opus frame to the audio input server
func (aic *AudioInputClient) SendFrameZeroCopy(frame *ZeroCopyAudioFrame) error {
	aic.mtx.Lock()
	defer aic.mtx.Unlock()

	if !aic.running || aic.conn == nil {
		return fmt.Errorf("not connected to audio input server")
	}

	if frame == nil {
		return nil // Nil frame, ignore
	}

	frameLen := frame.Length()
	if frameLen == 0 {
		return nil // Empty frame, ignore
	}

	// Inline frame validation to reduce function call overhead
	if frameLen > maxFrameSize {
		return ErrFrameDataTooLarge
	}

	// Use zero-copy data directly
	msg := &UnifiedIPCMessage{
		Magic:     inputMagicNumber,
		Type:      MessageTypeOpusFrame,
		Length:    uint32(frameLen),
		Timestamp: time.Now().UnixNano(),
		Data:      frame.Data(), // Zero-copy data access
	}

	return aic.writeMessage(msg)
}

// SendConfig sends a configuration update to the audio input server
func (aic *AudioInputClient) SendConfig(config UnifiedIPCConfig) error {
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

	// Serialize config using common function
	data := EncodeAudioConfig(config.SampleRate, config.Channels, config.FrameSize)

	msg := &UnifiedIPCMessage{
		Magic:     inputMagicNumber,
		Type:      MessageTypeConfig,
		Length:    uint32(len(data)),
		Timestamp: time.Now().UnixNano(),
		Data:      data,
	}

	return aic.writeMessage(msg)
}

// SendOpusConfig sends a complete Opus encoder configuration update to the audio input server
func (aic *AudioInputClient) SendOpusConfig(config UnifiedIPCOpusConfig) error {
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

	// Serialize Opus configuration using common function
	data := EncodeOpusConfig(config.SampleRate, config.Channels, config.FrameSize, config.Bitrate, config.Complexity, config.VBR, config.SignalType, config.Bandwidth, config.DTX)

	msg := &UnifiedIPCMessage{
		Magic:     inputMagicNumber,
		Type:      MessageTypeOpusConfig,
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

	msg := &UnifiedIPCMessage{
		Magic:     inputMagicNumber,
		Type:      MessageTypeHeartbeat,
		Length:    0,
		Timestamp: time.Now().UnixNano(),
	}

	return aic.writeMessage(msg)
}

// writeMessage writes a message to the server
// Global shared message pool for input IPC clients
var globalInputMessagePool = NewGenericMessagePool(messagePoolSize)

func (aic *AudioInputClient) writeMessage(msg *UnifiedIPCMessage) error {
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

