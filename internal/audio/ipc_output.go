package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync/atomic"
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

// AudioOutputServer is now an alias for UnifiedAudioServer
type AudioOutputServer = UnifiedAudioServer

func NewAudioOutputServer() (*AudioOutputServer, error) {
	return NewUnifiedAudioServer(false) // false = output server
}

// Start method is now inherited from UnifiedAudioServer

// acceptConnections method is now inherited from UnifiedAudioServer

// startProcessorGoroutine method is now inherited from UnifiedAudioServer

// Stop method is now inherited from UnifiedAudioServer

// Close method is now inherited from UnifiedAudioServer

// SendFrame method is now inherited from UnifiedAudioServer

// GetServerStats returns server performance statistics
func (s *AudioOutputServer) GetServerStats() (total, dropped int64, bufferSize int64) {
	stats := GetFrameStats(&s.totalFrames, &s.droppedFrames)
	return stats.Total, stats.Dropped, atomic.LoadInt64(&s.bufferSize)
}

// AudioOutputClient is now an alias for UnifiedAudioClient
type AudioOutputClient = UnifiedAudioClient

func NewAudioOutputClient() *AudioOutputClient {
	return NewUnifiedAudioClient(false) // false = output client
}

// Connect method is now inherited from UnifiedAudioClient

// Disconnect method is now inherited from UnifiedAudioClient

// IsConnected method is now inherited from UnifiedAudioClient

// Close method is now inherited from UnifiedAudioClient

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

// getOutputSocketPath is now defined in unified_ipc.go
