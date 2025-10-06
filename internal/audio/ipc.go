package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

// Buffer pool for zero-allocation writes
var writeBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, ipcHeaderSize+ipcMaxFrameSize)
		return &buf
	},
}

// IPC Protocol constants (matches C implementation in ipc_protocol.h)
const (
	ipcMagicOutput   = 0x4A4B4F55 // "JKOU" - Output (device → browser)
	ipcMagicInput    = 0x4A4B4D49 // "JKMI" - Input (browser → device)
	ipcHeaderSize    = 9          // Reduced from 17 (removed 8-byte timestamp)
	ipcMaxFrameSize  = 1024       // 128kbps @ 20ms = ~600 bytes worst case with VBR+FEC
	ipcMsgTypeOpus   = 0
	ipcMsgTypeConfig = 1
	ipcMsgTypeStop   = 3
	connectTimeout   = 5 * time.Second
	readTimeout      = 2 * time.Second
)

// IPCClient manages Unix socket communication with audio subprocess
type IPCClient struct {
	socketPath  string
	magicNumber uint32
	conn        net.Conn
	mu          sync.Mutex
	logger      zerolog.Logger
	readBuf     []byte // Reusable buffer for reads (single reader per client)
}

// NewIPCClient creates a new IPC client
// For output: socketPath="/var/run/audio_output.sock", magic=ipcMagicOutput
// For input:  socketPath="/var/run/audio_input.sock", magic=ipcMagicInput
func NewIPCClient(name, socketPath string, magicNumber uint32) *IPCClient {
	logger := logging.GetDefaultLogger().With().Str("component", name+"-ipc").Logger()

	return &IPCClient{
		socketPath:  socketPath,
		magicNumber: magicNumber,
		logger:      logger,
		readBuf:     make([]byte, ipcMaxFrameSize),
	}
}

// Connect establishes connection to the subprocess
func (c *IPCClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	conn, err := net.DialTimeout("unix", c.socketPath, connectTimeout)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", c.socketPath, err)
	}

	c.conn = conn
	c.logger.Debug().Str("socket", c.socketPath).Msg("connected to subprocess")
	return nil
}

// Disconnect closes the connection
func (c *IPCClient) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
		c.logger.Debug().Msg("disconnected from subprocess")
	}
}

// IsConnected returns true if currently connected
func (c *IPCClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

// ReadMessage reads a complete IPC message (header + payload)
// Returns message type, payload data, and error
// IMPORTANT: The returned payload slice is only valid until the next ReadMessage call.
// Callers must use the data immediately or copy if retention is needed.
func (c *IPCClient) ReadMessage() (uint8, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return 0, nil, fmt.Errorf("not connected")
	}

	// Set read deadline
	if err := c.conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		return 0, nil, fmt.Errorf("failed to set read deadline: %w", err)
	}

	// Read 9-byte header
	var header [ipcHeaderSize]byte
	if _, err := io.ReadFull(c.conn, header[:]); err != nil {
		return 0, nil, fmt.Errorf("failed to read header: %w", err)
	}

	// Parse header (little-endian)
	magic := binary.LittleEndian.Uint32(header[0:4])
	msgType := header[4]
	length := binary.LittleEndian.Uint32(header[5:9])

	// Validate magic number
	if magic != c.magicNumber {
		return 0, nil, fmt.Errorf("invalid magic: got 0x%X, expected 0x%X", magic, c.magicNumber)
	}

	// Validate length
	if length > ipcMaxFrameSize {
		return 0, nil, fmt.Errorf("message too large: %d bytes", length)
	}

	// Read payload if present
	if length == 0 {
		return msgType, nil, nil
	}

	// Read directly into reusable buffer (zero-allocation)
	if _, err := io.ReadFull(c.conn, c.readBuf[:length]); err != nil {
		return 0, nil, fmt.Errorf("failed to read payload: %w", err)
	}

	// Return slice of readBuf - caller must use immediately, data is only valid until next ReadMessage
	// This avoids allocation in hot path (50 frames/sec)
	return msgType, c.readBuf[:length], nil
}

// WriteMessage writes a complete IPC message
func (c *IPCClient) WriteMessage(msgType uint8, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	length := uint32(len(payload))
	if length > ipcMaxFrameSize {
		return fmt.Errorf("payload too large: %d bytes", length)
	}

	// Get buffer from pool for zero-allocation write
	bufPtr := writeBufferPool.Get().(*[]byte)
	defer writeBufferPool.Put(bufPtr)
	buf := *bufPtr

	// Build header in pooled buffer (9 bytes, little-endian)
	binary.LittleEndian.PutUint32(buf[0:4], c.magicNumber)
	buf[4] = msgType
	binary.LittleEndian.PutUint32(buf[5:9], length)

	// Copy payload after header
	copy(buf[ipcHeaderSize:], payload)

	// Write header + payload atomically
	if _, err := c.conn.Write(buf[:ipcHeaderSize+length]); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	return nil
}
