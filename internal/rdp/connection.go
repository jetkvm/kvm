package rdp

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/rdp/channels"
	"github.com/jetkvm/kvm/internal/rdp/protocol"
	"github.com/jetkvm/kvm/internal/rdp/udp"
)

// bitmapEncoderType tracks which bitmap encoder a connection started.
type bitmapEncoderType int

const (
	bitmapEncoderNone bitmapEncoderType = iota
	bitmapEncoderRGB
	bitmapEncoderJPEG
)

// rdpWriteDeadline is the timeout for individual connection writes.
// Prevents slow clients from blocking the message loop on single-core devices.
const rdpWriteDeadline = 5 * time.Second

// writeWithDeadline sets a write deadline and executes the write function.
// Prevents slow clients from blocking the message loop, which causes mouse/video lag
// that worsens with each reconnect (TCP slow start + stalls) on single-core devices.
// The deadline is not cleared after the write — each subsequent call sets a fresh
// deadline, and stale deadlines cannot fire spuriously between writes.
func (c *Connection) writeWithDeadline(writeFn func() error) error {
	if err := c.conn.SetWriteDeadline(time.Now().Add(rdpWriteDeadline)); err != nil {
		return err
	}
	return writeFn()
}

// BufferedWrite appends data to the write buffer without flushing.
// DVC fragments accumulate in the buffer and are flushed as a single
// TLS record per frame by FlushWrites(), reducing 63 syscalls to 1-2.
func (c *Connection) BufferedWrite(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.writer == nil {
		// Fallback: writer not yet initialized (pre-TLS handshake)
		_, err := c.conn.Write(data)
		return err
	}
	_, err := c.writer.Write(data)
	return err
}

// FlushWrites flushes the buffered writer, sending all accumulated DVC
// fragments as a minimal number of TLS records.
func (c *Connection) FlushWrites() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.writer == nil {
		return nil
	}
	if err := c.conn.SetWriteDeadline(time.Now().Add(rdpWriteDeadline)); err != nil {
		return err
	}
	return c.writer.Flush()
}

// vcPDUPool provides pooled buffers for virtual channel PDUs per MS-RDPBCGR 2.2.6.1.
// Audio packets can reach 4096 bytes + headers, so 8KB avoids fallback allocations.
var vcPDUPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 8192)
		return &buf
	},
}

// audinBufferPool provides pooled buffers for AUDIN audio input data.
// At 48kHz stereo 16-bit, 10ms of audio = 1920 bytes. Pool uses 2KB for alignment.
// This eliminates ~715MB of allocations per hour of audio input.
var audinBufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 2048)
		return &buf
	},
}

// audinPooledBuffer wraps a pooled buffer for audio input data.
// After processing, call Release() to return the buffer to the pool.
type audinPooledBuffer struct {
	Data []byte  // Slice of the actual data
	buf  *[]byte // Pointer to underlying buffer for pool return
}

// Release returns the buffer to the pool. Safe to call multiple times.
func (b *audinPooledBuffer) Release() {
	if b.buf != nil {
		audinBufferPool.Put(b.buf)
		b.buf = nil
		b.Data = nil
	}
}

// inputPayloadPool reduces allocations for fast-path input payloads.
// Fast-path input events are typically small (keyboard: 2 bytes, mouse: 7 bytes).
// Max realistic size: ~64 bytes for batched events. Pool uses 256 bytes for safety.
var inputPayloadPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 256)
		return &buf
	},
}

// Write buffering constants for coalescing DVC fragments.
const (
	writeBufferSize = 256 * 1024 // 256KB — fits a typical 100KB keyframe + headroom
)

// Connection represents a single RDP client connection.
type Connection struct {
	conn     net.Conn
	server   *Server
	reader   *bufio.Reader
	writer   *bufio.Writer // buffered writer for coalescing DVC fragments
	writeMu  sync.Mutex    // serializes buffered writes and flushes
	stopChan chan struct{}
	closed   atomic.Bool

	// Resolution (packed for atomic access)
	resolution atomic.Uint32

	// MCS layer state
	userID       uint16
	ioChannel    uint16
	msgChannelID uint16 // Message channel (0 if not used)
	channels     []ChannelInfo
	channelsMu   sync.RWMutex
	// Fast lookup for hot path - populated once during channel setup, never modified
	channelNames [8]string // Index = channelID - baseChannelID, supports up to 8 channels

	// Negotiated protocol (from X.224)
	selectedProtocol         uint32
	clientRequestedProtocols uint32 // Original X.224 requested protocols from client

	// Client capabilities
	clientInfo *ClientInfo

	// Frame sending
	frameRequested atomic.Bool

	// Connection phase
	phase ConnectionPhase

	// Dynamic virtual channels
	dvcManager    *channels.DVCManager
	gfxChannel    *channels.GFXChannel
	audinChannel  *channels.AudinChannel
	cameraChannel *channels.CameraChannel
	drdynvcID     uint16 // Static channel ID for drdynvc

	// Static virtual channels
	soundChannel     *channels.SoundChannel
	rdpsndID         uint16 // Static channel ID for rdpsnd
	clipboardChannel *channels.ClipboardChannel
	cliprdrdID       uint16 // Static channel ID for cliprdr

	// Modifier key tracking for paste detection
	ctrlPressed     atomic.Bool
	targetCopied    atomic.Bool // true after Ctrl+C/X on target; cleared on client FORMAT_LIST
	pasteInProgress atomic.Bool // Suppress V key events during paste

	// Pending file transfer - files received from clipboard, waiting for paste
	pendingFiles   []*channels.ClipboardFile
	pendingFilesMu sync.Mutex

	// Audio streaming (output - HDMI audio to client)
	audioChan   <-chan []byte
	audioStopCh chan struct{}

	// Audio input (AUDIN - client mic to USB gadget)
	audinDataChan chan *audinPooledBuffer
	audinStopCh   chan struct{}

	// Video streaming channels (for proper cleanup on disconnect)
	h264Chan <-chan []byte
	jpegChan <-chan []byte
	rgbChan  <-chan RGBFrame

	// Write error tracking for connection health
	consecutiveWriteErrors atomic.Int32

	// Keyframe tracking - only send frames after first keyframe
	hasReceivedKeyframe atomic.Bool

	// Graphics mode - true if RDPGFX is available, false for bitmap updates
	gfxSupported atomic.Bool

	// Track which bitmap encoder this connection started (for cleanup on disconnect).
	// Mutually exclusive: only one encoder type can be active at a time.
	activeEncoder bitmapEncoderType

	// Mouse state tracking for RDP events.
	// Accessed only from the connection's message loop goroutine (single-writer).
	// RDP sends button flags only on click events, not during moves.
	// lastMouseX/Y track the last known cursor position so button-only
	// events can re-send the correct position (HID Report ID 1 includes position).
	mouseButtons byte
	lastMouseX   int
	lastMouseY   int

	// Diagnostic counters for video frame tracking (debugging freeze issues)
	frameStats struct {
		attempted                     atomic.Uint64 // Total frames received from encoder
		sent                          atomic.Uint64 // Successfully sent via GFX channel
		dropNotReady                  atomic.Uint64 // Dropped: channel not ready
		dropNoKeyframe                atomic.Uint64 // Dropped: waiting for keyframe
		dropBackpressure              atomic.Uint64 // Dropped: backpressure
		lastLogTime                   atomic.Int64  // UnixMilli of last 30s diagnostic stats log
		lastKeyframeDropLogTime       atomic.Int64  // UnixMilli of last 1s keyframe drop log (separate to avoid interference)
		lastLogDrops                  atomic.Uint64 // Total drops at last log (for delta calculation)
		backpressureKeyframeRequested atomic.Bool   // True if keyframe requested during current backpressure episode
	}

	// Virtual channel PDU reassembly buffer for clipboard (MS-RDPBCGR 2.2.6.1)
	// Large clipboard PDUs (e.g., file contents) are fragmented across multiple packets.
	clipboardReassembly struct {
		buffer      []byte // Accumulated data from fragmented PDUs
		totalLength uint32 // Expected total length from first fragment
		mu          sync.Mutex
	}

	// Gateway mode: connection arrived via RD Gateway direct pipe.
	// Uses Go's software crypto/tls instead of hardware-accelerated OpenSSL,
	// because the in-process tsguConn has no kernel socket fd for SSL_set_fd().
	softwareTLS bool

	// Multitransport / UDP (MS-RDPEUDP2)
	clientMultitransportFlags uint32      // Client's CS_MULTITRANSPORT flags (0 if not requested)
	udpTunnel                 *udp.Tunnel // nil until UDP is established
	udpReady                  atomic.Bool // true after Soft-Sync completes
	securityCookie            [16]byte    // Random cookie for this connection
	multitransportReqID       uint32      // Correlation ID

	// Packet capture (nil if not capturing)
	capture PacketCapture
}

// ConnectionPhase represents the current protocol phase.
type ConnectionPhase int

const (
	PhaseConnection ConnectionPhase = iota
	PhaseBasicSettings
	PhaseChannelConnection
	PhaseSecurityExchange
	PhaseLicensing
	PhaseCapabilities
	PhaseActive
)

// ChannelInfo holds information about a virtual channel.
type ChannelInfo struct {
	Name    string
	ID      uint16
	Options uint32
}

// ClientInfo contains information about the connected client.
type ClientInfo struct {
	Name           string
	Version        string
	Width          uint16
	Height         uint16
	ColorDepth     uint16
	KeyboardLayout uint32
	KeyboardType   uint32
}

// NewConnection creates a new RDP connection.
func NewConnection(conn net.Conn, server *Server) *Connection {
	w, h := server.GetVideoState()

	if w == 0 {
		w = DefaultWidth
	}
	if h == 0 {
		h = DefaultHeight
	}

	c := &Connection{
		conn:     conn,
		server:   server,
		reader:   bufio.NewReader(conn),
		stopChan: make(chan struct{}),
		phase:    PhaseConnection,
	}
	c.setResolution(w, h)

	return c
}

// packResolution packs width and height into a single uint32.
func packResolution(w, h uint16) uint32 {
	return uint32(w)<<16 | uint32(h)
}

// unpackResolution unpacks width and height from a uint32.
func unpackResolution(packed uint32) (uint16, uint16) {
	return uint16(packed >> 16), uint16(packed & 0xFFFF)
}

// GetResolution returns the current resolution atomically.
func (c *Connection) GetResolution() (uint16, uint16) {
	return unpackResolution(c.resolution.Load())
}

// setResolution sets the resolution atomically.
func (c *Connection) setResolution(w, h uint16) {
	c.resolution.Store(packResolution(w, h))
}

// Handle runs the RDP connection protocol.
func (c *Connection) Handle() error {
	defer c.conn.Close()

	// Phase 1: X.224 Connection
	if err := c.handleX224Connection(); err != nil {
		return fmt.Errorf("x224 connection failed: %w", err)
	}

	// Phase 2: MCS Connect
	if err := c.handleMCSConnect(); err != nil {
		return fmt.Errorf("mcs connect failed: %w", err)
	}

	// Phase 3: MCS Channel Setup
	if err := c.handleMCSChannelSetup(); err != nil {
		return fmt.Errorf("mcs channel setup failed: %w", err)
	}

	// Phase 4: RDP Security Exchange
	if err := c.handleSecurityExchange(); err != nil {
		return fmt.Errorf("security exchange failed: %w", err)
	}

	// Phase 5: Licensing
	if err := c.handleLicensing(); err != nil {
		return fmt.Errorf("licensing failed: %w", err)
	}

	// NOTE: Multitransport bootstrapping (Initiate Multitransport Request)
	// is intentionally deferred to the active session phase. Sending it here
	// (between licensing and capabilities) causes Windows App on macOS to
	// fail — it doesn't check for SEC_TRANSPORT_REQ in the security header
	// and parses the PDU as a malformed Demand Active, disconnecting.

	// Phase 6: Capabilities Exchange
	if err := c.handleCapabilities(); err != nil {
		return fmt.Errorf("capabilities failed: %w", err)
	}

	// Enter active session
	c.phase = PhaseActive
	return c.messageLoop()
}

// messageLoop processes RDP messages after connection is established.
func (c *Connection) messageLoop() error {
	c.server.deps.Logger.Info().Str("remote", c.RemoteAddr()).
		Msg("RDP: entering active session")

	// Initialize dynamic virtual channels
	if err := c.initDynamicChannels(); err != nil {
		c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to initialize DVC")
		// Continue without DVC - basic RDP still works
	}

	// Flush the DVC capability request so it reaches the client before
	// we enter the blocking read loop. Without this, the DVC CAPS PDU
	// sits in the buffered writer and the client never receives it.
	if err := c.FlushWrites(); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: flush error after DVC init")
	}

	// Clear read deadline for maximum responsiveness - blocking reads are fastest
	// The connection will be closed on shutdown, causing the read to return with an error
	if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
		return err
	}

	for {
		// Peek at first byte to detect Fast-Path vs Slow-Path
		// This is a blocking read for maximum responsiveness
		firstByte, err := c.reader.Peek(1)
		if err != nil {
			// Connection closed or error - exit gracefully
			return nil
		}

		if firstByte[0] != 0x03 {
			// Fast-Path Input PDU (not TPKT)
			if err := c.handleFastPathInput(); err != nil {
				c.server.deps.Logger.Debug().Err(err).Msg("RDP: fast-path input error")
			}
			continue
		}

		// Slow-Path (TPKT/X.224/MCS)
		// HOT PATH: Use pooled buffer to avoid allocations
		buf, err := protocol.ReadX224DataPooled(c.reader)
		if err != nil {
			return err
		}

		pduType, err := protocol.ParseMCSPDUType(buf.Data)
		if err != nil {
			buf.Release()
			return err
		}

		switch pduType {
		case protocol.MCSSendDataRequest:
			sdr, err := protocol.ParseSendDataRequest(buf.Data)
			if err != nil {
				c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to parse SendDataRequest")
				buf.Release()
				continue
			}
			// handleDataPDU processes data synchronously (DVC handlers copy if needed)
			c.handleDataPDU(sdr)

		case protocol.MCSDisconnectUltimatum:
			buf.Release()
			c.server.deps.Logger.Info().Str("remote", c.RemoteAddr()).
				Msg("RDP: client disconnected")
			return nil

		default:
			c.server.deps.Logger.Warn().
				Str("type", pduType.String()).
				Hex("firstBytes", buf.Data[:min(len(buf.Data), 16)]).
				Msg("RDP: unhandled MCS PDU type")
		}
		buf.Release()
	}
}

// handleDataPDU handles an RDP data PDU.
func (c *Connection) handleDataPDU(sdr *protocol.SendDataRequest) {
	// Check channel
	if sdr.ChannelID == c.ioChannel {
		// I/O channel - RDP core messages
		c.handleIOChannelPDU(sdr.UserData)
	} else if c.msgChannelID != 0 && sdr.ChannelID == c.msgChannelID {
		// MCS Message Channel — Multitransport Response PDUs
		c.handleMessageChannelPDU(sdr.UserData)
	} else {
		// Virtual channel
		c.handleVirtualChannelPDU(sdr.ChannelID, sdr.UserData)
	}
}

// handleIOChannelPDU handles PDUs on the I/O channel.
func (c *Connection) handleIOChannelPDU(data []byte) {
	// Share Control Header: totalLength(2) + pduType(2) + PDUSource(2) = 6 bytes min.
	if len(data) < 6 {
		return
	}

	// Check for Multitransport Response (Basic Security Header with SEC_TRANSPORT_RSP).
	// Only check when we actually sent a Multitransport Request, because the security
	// header flags field overlaps with the Share Control totalLength field — a bitmask
	// check would falsely match legitimate PDUs whose totalLength has bit 2 set.
	if c.server.multitransportEnabled && len(data) >= 8 {
		flags := binary.LittleEndian.Uint16(data[0:2])
		flagsHi := binary.LittleEndian.Uint16(data[2:4])
		if flags == protocol.SecTransportRsp && flagsHi == 0 {
			c.handleMultitransportResponse(data[4:])
			return
		}
	}

	// Parse Share Control Header — pduType is at offset 2, not 0 (offset 0 is totalLength).
	// Low 4 bits of pduType contain the PDU type (MS-RDPBCGR 2.2.8.1.1.1.1).
	pduType := binary.LittleEndian.Uint16(data[2:4]) & 0x000F

	switch pduType {
	case protocol.PDUTypeData:
		c.handleShareDataPDU(data)
	case protocol.PDUTypeConfirmActive:
		// Already handled during capabilities
	default:
		c.server.deps.Logger.Debug().
			Uint16("pduType", uint16(pduType)).
			Msg("RDP: unhandled share control PDU")
	}
}

// handleShareDataPDU handles a Share Data PDU.
func (c *Connection) handleShareDataPDU(data []byte) {
	if len(data) < 18 {
		return
	}

	// Skip Share Control Header (6 bytes) and Share Data Header header (12 bytes)
	// PDUType2 is at offset 14
	pduType2 := data[14]

	switch pduType2 {
	case protocol.DataPDUTypeInput:
		c.handleInputPDU(data[18:])
	case protocol.DataPDUTypeShutdownRequest:
		c.server.deps.Logger.Info().Str("remote", c.RemoteAddr()).
			Msg("RDP: shutdown requested")
	case protocol.DataPDUTypeSuppressOutput:
		// Client minimized window or similar
		c.server.deps.Logger.Debug().Msg("RDP: suppress output requested")
	case protocol.DataPDUTypeRefreshRect:
		// Client wants a screen refresh
		c.frameRequested.Store(true)
	default:
		c.server.deps.Logger.Debug().
			Uint8("pduType2", pduType2).
			Msg("RDP: unhandled share data PDU")
	}
}

// Virtual channel PDU flags (MS-RDPBCGR 2.2.6.1).
const (
	channelFlagFirst = 0x00000001 // CHANNEL_FLAG_FIRST
	channelFlagLast  = 0x00000002 // CHANNEL_FLAG_LAST
	// channelFlagShowProtocol (0x00000010) reserved for future use
)

// handleVirtualChannelPDU handles PDUs on virtual channels.
func (c *Connection) handleVirtualChannelPDU(channelID uint16, data []byte) {
	// Virtual Channel PDU has an 8-byte header per MS-RDPBCGR 2.2.6.1:
	// - totalLength (4 bytes LE): total size of the original data
	// - flags (4 bytes LE): CHANNEL_FLAG_FIRST, CHANNEL_FLAG_LAST, etc.
	// - channelData (variable): the actual payload
	if len(data) < 8 {
		c.server.deps.Logger.Debug().
			Int("dataLen", len(data)).
			Msg("RDP: virtual channel PDU too short (need 8 byte header)")
		return
	}

	// Parse the 8-byte VC PDU header
	totalLength := binary.LittleEndian.Uint32(data[0:4])
	flags := binary.LittleEndian.Uint32(data[4:8])
	payload := data[8:]

	// LOCK-FREE FAST PATH: Use pre-computed channel name lookup
	// Base channel ID is 1004, so index = channelID - 1004
	const baseChannelID = protocol.ChannelMCSGlobalID + 1 // 1004
	idx := int(channelID - baseChannelID)
	var channelName string
	if idx >= 0 && idx < len(c.channelNames) {
		channelName = c.channelNames[idx]
	}

	switch channelName {
	case "drdynvc":
		// Dynamic Virtual Channel - for RDPGFX, audio, etc.
		// DVC has its own fragmentation handling, pass directly
		c.handleDrdynvc(payload)
	case "rdpsnd":
		// Audio output - typically small PDUs, pass directly
		c.handleRdpsnd(payload)
	case "cliprdr":
		// Clipboard redirection - needs reassembly for large file transfers
		c.handleClipboardWithReassembly(payload, totalLength, flags)
	case "rdpdr":
		// Device redirection
		c.server.deps.Logger.Debug().Msg("RDP: rdpdr channel data")
	default:
		c.server.deps.Logger.Debug().
			Str("channel", channelName).
			Uint16("channelID", channelID).
			Msg("RDP: unhandled virtual channel")
	}
}

// handleDrdynvc handles dynamic virtual channel setup.
func (c *Connection) handleDrdynvc(data []byte) {
	if c.dvcManager == nil {
		return
	}
	if err := c.dvcManager.HandlePDU(data); err != nil {
		// Channel closed errors are expected during normal shutdown
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: drdynvc error")
	}
}

// handleRdpsnd handles audio output channel.
func (c *Connection) handleRdpsnd(data []byte) {
	if c.soundChannel == nil {
		return
	}
	if err := c.soundChannel.HandlePDU(data); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: rdpsnd error")
	}
}

// handleCameraFormatChanges handles USB host format changes for dynamic camera format negotiation.
// When the USB host requests a different video format from the UVC gadget, this method
// re-activates the RDP camera channel with the matching format.
func (c *Connection) handleCameraFormatChanges(formatChan <-chan CameraFormatInfo) {
	for {
		select {
		case <-c.stopChan:
			return
		case fmt, ok := <-formatChan:
			if !ok {
				// Channel closed
				return
			}

			// Skip if camera channel not ready
			if c.cameraChannel == nil || !c.cameraChannel.IsReady() {
				continue
			}

			// Handle stop notification from USB host
			if fmt.Codec == "stop" {
				c.server.deps.Logger.Info().Msg("RDP: USB host stopped streaming, deactivating RDP camera")
				if err := c.cameraChannel.Deactivate(); err != nil {
					c.server.deps.Logger.Debug().Err(err).Msg("RDP: camera deactivate error")
				}
				// Disable camera passthrough so the device is released
				if c.server.deps.Camera != nil {
					c.server.deps.Camera.SetEnabled(false)
				}
				continue
			}

			// Map codec string to MS-RDPECAM pixel format constant
			var pixelFormat uint32
			switch fmt.Codec {
			case "h264":
				pixelFormat = channels.CamPixelFormatH264
			case "mjpeg":
				pixelFormat = channels.CamPixelFormatMJPEG
			default:
				c.server.deps.Logger.Debug().
					Str("codec", fmt.Codec).
					Msg("RDP: unknown camera codec requested by host, ignoring")
				continue
			}

			c.server.deps.Logger.Info().
				Str("codec", fmt.Codec).
				Int("width", fmt.Width).
				Int("height", fmt.Height).
				Int("fps", fmt.FrameRate).
				Msg("RDP: USB host started streaming, activating RDP camera")

			// Enable camera passthrough when USB host starts streaming.
			// Check closed flag to prevent racing with Close() which disables the camera.
			if c.server.deps.Camera != nil && !c.closed.Load() {
				c.server.deps.Camera.SetEnabled(true)
			}

			if err := c.cameraChannel.ActivateWithFormat(pixelFormat, fmt.Width, fmt.Height, fmt.FrameRate); err != nil {
				c.server.deps.Logger.Warn().
					Err(err).
					Str("codec", fmt.Codec).
					Msg("RDP: failed to activate camera with requested format")
			}
		}
	}
}

// Close closes the connection.
func (c *Connection) Close() {
	if c.closed.Swap(true) {
		c.server.deps.Logger.Debug().Str("remote", c.RemoteAddr()).
			Msg("RDP: Close() called but already closed")
		return
	}

	c.server.deps.Logger.Info().Str("remote", c.RemoteAddr()).
		Msg("RDP: closing connection, starting cleanup")

	// Stop audio streaming first
	c.stopAudioStream()

	// Close sound channel
	if c.soundChannel != nil {
		c.soundChannel.Close()
	}

	// Signal audio system that RDP no longer needs audio
	if c.server.deps.Audio != nil {
		c.server.deps.Audio.Disconnect()
	}

	// Close AUDIN channel first to stop data callbacks from firing
	if c.audinChannel != nil {
		c.audinChannel.Close()
	}

	// Stop AUDIN data processing goroutine
	if c.audinStopCh != nil {
		close(c.audinStopCh)
		c.audinStopCh = nil
	}

	// Drain remaining AUDIN buffers to return them to the pool
	if c.audinDataChan != nil {
		for len(c.audinDataChan) > 0 {
			if pooled := <-c.audinDataChan; pooled != nil {
				pooled.Release()
			}
		}
	}

	// Unsubscribe from camera format changes, disable passthrough, and close camera channel
	if c.server.deps.Camera != nil {
		c.server.deps.Camera.UnsubscribeFormatChanges()
		c.server.deps.Camera.SetEnabled(false) // Stop camera on client when RDP disconnects
	}
	if c.cameraChannel != nil {
		c.cameraChannel.Close()
	}

	// Close GFX channel
	if c.gfxChannel != nil {
		c.gfxChannel.Close()
	}

	// Close DVC manager (closes all remaining DVC channels)
	if c.dvcManager != nil {
		c.dvcManager.Close()
	}

	// Cleanup clipboard resources
	if c.clipboardChannel != nil {
		c.clipboardChannel.CleanupFiles()
	}

	// Cleanup pending files (not yet pasted)
	c.clearPendingFiles()

	// Release clipboard reassembly buffer (can be up to MaxClipboardReassemblySize)
	c.clipboardReassembly.mu.Lock()
	c.clipboardReassembly.buffer = nil
	c.clipboardReassembly.totalLength = 0
	c.clipboardReassembly.mu.Unlock()

	// Close UDP transport
	if c.udpTunnel != nil {
		if err := c.udpTunnel.Close(); err != nil {
			c.server.deps.Logger.Debug().Err(err).Msg("RDP: error closing UDP tunnel")
		}
		c.udpTunnel = nil
	}
	c.server.UnregisterUDPCookie(c.securityCookie)

	// Signal the message loop and streaming goroutines to exit.
	// Must happen before video cleanup so goroutines stop reading
	// from subscription channels before we unsubscribe them.
	close(c.stopChan)

	// Stop bitmap encoders that this connection started.
	// These are separate from the shared H.264 encoder managed by the native layer.
	// Without this, hardware JPEG/RGB encoders keep running after disconnect.
	if c.server.deps.Video != nil {
		switch c.activeEncoder {
		case bitmapEncoderRGB:
			if err := c.server.deps.Video.StopRGBEncoder(); err != nil {
				c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to stop RGB encoder")
			}
		case bitmapEncoderJPEG:
			if err := c.server.deps.Video.StopJPEGEncoder(); err != nil {
				c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to stop JPEG encoder")
			}
		}
		c.activeEncoder = bitmapEncoderNone
	}

	// Cleanup video subscriptions (safety net - goroutines should cleanup in defer)
	// These may already be nil if goroutines exited cleanly via stopChan
	if c.server.deps.Video != nil {
		if c.h264Chan != nil {
			c.server.deps.Video.UnsubscribeH264(c.h264Chan)
			c.h264Chan = nil
		}
		if c.jpegChan != nil {
			c.server.deps.Video.UnsubscribeJPEG(c.jpegChan)
			c.jpegChan = nil
		}
		if c.rgbChan != nil {
			c.server.deps.Video.UnsubscribeRGB(c.rgbChan)
			c.rgbChan = nil
		}
	}

	// Close packet capture session
	if c.capture != nil {
		c.capture.Close()
	}

	// Close the underlying TCP connection
	if err := c.conn.Close(); err != nil {
		c.server.deps.Logger.Debug().Err(err).Str("remote", c.RemoteAddr()).
			Msg("RDP: error closing TCP connection")
	}

	c.server.deps.Logger.Info().Str("remote", c.RemoteAddr()).
		Msg("RDP: connection cleanup complete")
}

// onResolutionChange handles resolution changes.
func (c *Connection) onResolutionChange(width, height uint16) {
	c.setResolution(width, height)

	// Update GFX surface if channel is ready
	if c.gfxChannel != nil && c.gfxChannel.IsReady() {
		if err := c.gfxChannel.UpdateResolution(width, height); err != nil {
			c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to update GFX resolution")
			return
		}
		// Reset keyframe state — the decoder needs a fresh keyframe after surface recreation.
		// Only reset on success; if UpdateResolution failed, the old surface is still valid.
		c.hasReceivedKeyframe.Store(false)
	}
}

// RemoteAddr returns the remote address.
func (c *Connection) RemoteAddr() string {
	return c.conn.RemoteAddr().String()
}
