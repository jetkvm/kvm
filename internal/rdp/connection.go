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
)

// vcPDUPool provides pooled buffers for virtual channel PDUs per MS-RDPBCGR 2.2.6.1.
// Audio packets can reach 4096 bytes + headers, so 8KB avoids fallback allocations.
var vcPDUPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 8192)
		return &buf
	},
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

// Connection represents a single RDP client connection.
type Connection struct {
	conn     net.Conn
	server   *Server
	reader   *bufio.Reader
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
	pasteInProgress atomic.Bool // Suppress V key events during paste

	// Audio streaming (output - HDMI audio to client)
	audioChan   <-chan []byte
	audioStopCh chan struct{}

	// Audio input (AUDIN - client mic to USB gadget)
	// Uses buffered channel to avoid blocking DVC message loop
	audinDataChan chan []byte
	audinStopCh   chan struct{}

	// Write error tracking for connection health
	consecutiveWriteErrors atomic.Int32

	// Keyframe tracking - only send frames after first keyframe
	hasReceivedKeyframe atomic.Bool

	// Graphics mode - true if RDPGFX is available, false for bitmap updates
	gfxSupported atomic.Bool

	// Mouse button state tracking for RDP events
	// RDP sends button flags only on click events, not during moves
	mouseButtons byte

	// Diagnostic counters for video frame tracking (debugging freeze issues)
	frameStats struct {
		attempted        atomic.Uint64 // Total frames received from encoder
		sent             atomic.Uint64 // Successfully sent via GFX channel
		dropNotReady     atomic.Uint64 // Dropped: channel not ready
		dropNoKeyframe   atomic.Uint64 // Dropped: waiting for keyframe
		dropBackpressure atomic.Uint64 // Dropped: backpressure
		lastLogTime      atomic.Int64  // UnixMilli of last stats log
	}
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
	} else {
		// Virtual channel
		c.handleVirtualChannelPDU(sdr.ChannelID, sdr.UserData)
	}
}

// handleIOChannelPDU handles PDUs on the I/O channel.
func (c *Connection) handleIOChannelPDU(data []byte) {
	if len(data) < 2 {
		return
	}

	// Parse Share Control Header
	pduType := binary.LittleEndian.Uint16(data[0:2]) & 0x000F

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

	// Skip 8-byte VC PDU header (totalLength + flags), pass payload directly
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
		c.handleDrdynvc(payload)
	case "rdpsnd":
		// Audio output
		c.handleRdpsnd(payload)
	case "cliprdr":
		// Clipboard redirection
		c.handleClipboard(payload)
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
		c.server.deps.Logger.Warn().Err(err).Msg("RDP: drdynvc error")
	}
}

// handleRdpsnd handles audio output channel.
func (c *Connection) handleRdpsnd(data []byte) {
	if c.soundChannel == nil {
		return
	}
	if err := c.soundChannel.HandlePDU(data); err != nil {
		c.server.deps.Logger.Warn().Err(err).Msg("RDP: rdpsnd error")
	}
}

// handleClipboard handles clipboard channel.
func (c *Connection) handleClipboard(data []byte) {
	if c.clipboardChannel == nil {
		return
	}
	if err := c.clipboardChannel.HandlePDU(data); err != nil {
		c.server.deps.Logger.Warn().Err(err).Msg("RDP: cliprdr error")
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
				Msg("RDP: USB host requested new camera format, re-activating RDP camera")

			if err := c.cameraChannel.ActivateWithFormat(pixelFormat); err != nil {
				c.server.deps.Logger.Warn().
					Err(err).
					Str("codec", fmt.Codec).
					Msg("RDP: failed to re-activate camera with new format")
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

	// Stop AUDIN data processing goroutine
	if c.audinStopCh != nil {
		close(c.audinStopCh)
		c.audinStopCh = nil
	}

	// Close AUDIN channel
	if c.audinChannel != nil {
		c.audinChannel.Close()
	}

	// Unsubscribe from camera format changes and close camera channel
	if c.server.deps.Camera != nil {
		c.server.deps.Camera.UnsubscribeFormatChanges()
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

	// Signal the message loop to exit
	close(c.stopChan)

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
			c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to update GFX resolution")
		}
	}
}

// RemoteAddr returns the remote address.
func (c *Connection) RemoteAddr() string {
	return c.conn.RemoteAddr().String()
}
