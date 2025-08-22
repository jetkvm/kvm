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
)

const (
	inputMagicNumber uint32 = 0x4A4B4D49 // "JKMI" (JetKVM Microphone Input)
	inputSocketName         = "audio_input.sock"
	maxFrameSize            = 4096 // Maximum Opus frame size
	writeTimeout            = 5 * time.Millisecond // Non-blocking write timeout
	maxDroppedFrames        = 100 // Maximum consecutive dropped frames before reconnect
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
	messageChan    chan *InputIPCMessage // Buffered channel for incoming messages
	processChan    chan *InputIPCMessage // Buffered channel for processing queue
	stopChan       chan struct{}         // Stop signal for all goroutines
	wg             sync.WaitGroup        // Wait group for goroutine coordination
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

	// Initialize with adaptive buffer size (start with 1000 frames)
	initialBufferSize := int64(1000)

	return &AudioInputServer{
		listener:    listener,
		messageChan: make(chan *InputIPCMessage, initialBufferSize),
		processChan: make(chan *InputIPCMessage, initialBufferSize),
		stopChan:    make(chan struct{}),
		bufferSize:  initialBufferSize,
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
	// Read header (magic + type + length + timestamp)
	headerSize := 4 + 1 + 4 + 8 // uint32 + uint8 + uint32 + int64
	header := make([]byte, headerSize)

	_, err := io.ReadFull(conn, header)
	if err != nil {
		return nil, err
	}

	// Parse header
	msg := &InputIPCMessage{}
	msg.Magic = binary.LittleEndian.Uint32(header[0:4])
	msg.Type = InputMessageType(header[4])
	msg.Length = binary.LittleEndian.Uint32(header[5:9])
	msg.Timestamp = int64(binary.LittleEndian.Uint64(header[9:17]))

	// Validate magic number
	if msg.Magic != inputMagicNumber {
		return nil, fmt.Errorf("invalid magic number: %x", msg.Magic)
	}

	// Validate message length
	if msg.Length > maxFrameSize {
		return nil, fmt.Errorf("message too large: %d bytes", msg.Length)
	}

	// Read data if present
	if msg.Length > 0 {
		msg.Data = make([]byte, msg.Length)
		_, err = io.ReadFull(conn, msg.Data)
		if err != nil {
			return nil, err
		}
	}

	return msg, nil
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

// writeMessage writes a message to the connection
func (ais *AudioInputServer) writeMessage(conn net.Conn, msg *InputIPCMessage) error {
	// Prepare header
	headerSize := 4 + 1 + 4 + 8
	header := make([]byte, headerSize)

	binary.LittleEndian.PutUint32(header[0:4], msg.Magic)
	header[4] = byte(msg.Type)
	binary.LittleEndian.PutUint32(header[5:9], msg.Length)
	binary.LittleEndian.PutUint64(header[9:17], uint64(msg.Timestamp))

	// Write header
	_, err := conn.Write(header)
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
	// Atomic fields must be first for proper alignment on ARM
	droppedFrames int64 // Atomic counter for dropped frames
	totalFrames   int64 // Atomic counter for total frames
	
	conn         net.Conn
	mtx          sync.Mutex
	running      bool
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
	for i := 0; i < 5; i++ {
		conn, err := net.Dial("unix", socketPath)
		if err == nil {
			aic.conn = conn
			aic.running = true
			return nil
		}
		time.Sleep(time.Second)
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

	// Prepare header
	headerSize := 4 + 1 + 4 + 8
	header := make([]byte, headerSize)

	binary.LittleEndian.PutUint32(header[0:4], msg.Magic)
	header[4] = byte(msg.Type)
	binary.LittleEndian.PutUint32(header[5:9], msg.Length)
	binary.LittleEndian.PutUint64(header[9:17], uint64(msg.Timestamp))

	// Use non-blocking write with timeout
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	// Create a channel to signal write completion
	done := make(chan error, 1)
	go func() {
		// Write header
		_, err := aic.conn.Write(header)
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
		defer ais.wg.Done()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		
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
						processingTime := time.Since(start).Nanoseconds()
						
						// Calculate end-to-end latency using message timestamp
						if msg.Type == InputMessageTypeOpusFrame && msg.Timestamp > 0 {
							msgTime := time.Unix(0, msg.Timestamp)
							endToEndLatency := time.Since(msgTime).Nanoseconds()
							// Use exponential moving average for end-to-end latency tracking
							currentAvg := atomic.LoadInt64(&ais.processingTime)
							// Weight: 90% historical, 10% current (for smoother averaging)
							newAvg := (currentAvg*9 + endToEndLatency) / 10
							atomic.StoreInt64(&ais.processingTime, newAvg)
						} else {
							// Fallback to processing time only
							currentAvg := atomic.LoadInt64(&ais.processingTime)
							newAvg := (currentAvg + processingTime) / 2
							atomic.StoreInt64(&ais.processingTime, newAvg)
						}
						
						if err != nil {
							atomic.AddInt64(&ais.droppedFrames, 1)
						}
					default:
						// No more messages to process
						goto adaptiveBuffering
					}
				}
				
				adaptiveBuffering:
				// Adaptive buffer sizing based on processing time
				avgTime := atomic.LoadInt64(&ais.processingTime)
				currentSize := atomic.LoadInt64(&ais.bufferSize)
				
				if avgTime > 10*1000*1000 { // > 10ms processing time
					// Increase buffer size
					newSize := currentSize * 2
					if newSize > 1000 {
						newSize = 1000
					}
					atomic.StoreInt64(&ais.bufferSize, newSize)
				} else if avgTime < 1*1000*1000 { // < 1ms processing time
					// Decrease buffer size
					newSize := currentSize / 2
					if newSize < 50 {
						newSize = 50
					}
					atomic.StoreInt64(&ais.bufferSize, newSize)
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

// Helper functions

// getInputSocketPath returns the path to the input socket
func getInputSocketPath() string {
	if path := os.Getenv("JETKVM_AUDIO_INPUT_SOCKET"); path != "" {
		return path
	}
	return filepath.Join("/var/run", inputSocketName)
}