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
