package rdp

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/rdp/channels"
	"github.com/jetkvm/kvm/internal/rdp/credssp"
	"github.com/jetkvm/kvm/internal/rdp/protocol"
)

// tileBufferPool reduces allocations for bitmap tile data.
// Max tile size: 256x256x4 = 262144 bytes (256KB per tile)
var tileBufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, bitmapTileSize*bitmapTileSize*4)
		return &buf
	},
}

// vcPDUPool reduces allocations for Virtual Channel PDU data.
// Max size: 8 (VC header) + ~1609 (DVC fragment) = ~1617 bytes, rounded to 2KB.
// This pool is used by sendDVCData for zero-allocation hot path.
var vcPDUPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 2048)
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

// handleX224Connection handles the X.224 connection request/confirm.
func (c *Connection) handleX224Connection() error {
	c.server.deps.Logger.Debug().Str("remote", c.RemoteAddr()).
		Msg("RDP: starting X.224 connection")

	// Set deadline for handshake
	if err := c.conn.SetReadDeadline(time.Now().Add(protocol.HandshakeTimeout)); err != nil {
		return err
	}

	// Read TPKT + X.224 Connection Request
	payload, err := protocol.ReadTPKTPayload(c.reader)
	if err != nil {
		return fmt.Errorf("read connection request: %w", err)
	}

	cr, err := protocol.ParseConnectionRequest(payload)
	if err != nil {
		return fmt.Errorf("parse connection request: %w", err)
	}

	// Temporary WARN-level logging for debugging
	c.server.deps.Logger.Warn().
		Str("cookie", cr.Cookie).
		Bool("clientRequestsTLS", cr.RequestsTLS()).
		Bool("clientRequestsCredSSP", cr.RequestsCredSSP()).
		Bool("serverTLSEnabled", c.server.tlsEnabled).
		Bool("serverTLSProviderSet", c.server.deps.TLS != nil).
		Msg("RDP: X.224 connection request received")

	// Build and send Connection Confirm
	// Note: We prefer TLS over CredSSP because CredSSP/NLA requires computing pubKeyAuth
	// which needs the NTLM session key. For a "permissive" server that doesn't validate
	// passwords, we can't derive the correct session key. TLS-only mode still provides
	// encryption but doesn't require NLA authentication.
	selectedProto := uint32(protocol.ProtocolRDP)
	if c.server.tlsEnabled && c.server.deps.TLS != nil {
		// Prefer TLS over CredSSP - simpler and doesn't require pubKeyAuth
		if cr.RequestsTLS() {
			selectedProto = protocol.ProtocolTLS
		} else if cr.RequestsCredSSP() {
			// Fall back to CredSSP only if client doesn't support plain TLS
			selectedProto = protocol.ProtocolCredSSP
		}
	}

	// Build connection confirm - must include RDP_NEG_RSP if client sent RDP_NEG_REQ
	clientSentNegReq := cr.NegReq != nil
	// Set negotiation flags based on what we support
	// Per MS-RDPBCGR 2.2.1.2.1 (RDP Negotiation Response):
	// - EXTENDED_CLIENT_DATA_SUPPORTED (0x01): Server supports Extended Client Data Blocks
	// - DYNVC_GFX_PROTOCOL_SUPPORTED (0x02): Server supports Graphics Pipeline Extension
	// NOTE: RDP_NEG_REQ flags and RDP_NEG_RSP flags have DIFFERENT meanings!
	// We should NOT intersect them - just set what our server supports.
	negFlags := uint8(protocol.NegFlagExtendedClientDataSupported | protocol.NegFlagDynvcGfxProtocolSupported)
	cc := protocol.BuildConnectionConfirm(selectedProto, negFlags, clientSentNegReq)

	// Log the negotiation details
	c.server.deps.Logger.Warn().
		Uint8("negFlags", negFlags).
		Uint32("selectedProto", selectedProto).
		Msg("RDP: X.224 negotiation flags")

	// Log the exact bytes being sent for debugging
	c.server.deps.Logger.Warn().
		Int("ccLen", len(cc)).
		Str("ccHex", fmt.Sprintf("%x", cc)).
		Msg("RDP: X.224 Connection Confirm bytes")

	if err := protocol.WriteTPKT(c.conn, cc); err != nil {
		return fmt.Errorf("write connection confirm: %w", err)
	}

	// Store the selected protocol for later use in MCS Connect
	c.selectedProtocol = selectedProto
	// Store the client's original requested protocols for SC_CORE
	if cr.NegReq != nil {
		c.clientRequestedProtocols = cr.NegReq.RequestedProto
	}

	// Temporary WARN-level logging for debugging
	c.server.deps.Logger.Warn().
		Uint32("selectedProtocol", selectedProto).
		Bool("negRspIncluded", clientSentNegReq).
		Msg("RDP: X.224 connection confirm sent")

	// If TLS or CredSSP was negotiated, upgrade to TLS first
	if selectedProto == protocol.ProtocolTLS || selectedProto == protocol.ProtocolCredSSP {
		c.server.deps.Logger.Warn().Msg("RDP: upgrading connection to TLS")

		// Set deadline before TLS handshake
		if err := c.conn.SetDeadline(time.Now().Add(protocol.HandshakeTimeout)); err != nil {
			return fmt.Errorf("set TLS deadline: %w", err)
		}

		// CredSSP requires Go's *tls.Conn for TLS session binding in pubKeyAuth.
		// For plain TLS mode, we can use hardware-accelerated TLS if available.
		if selectedProto == protocol.ProtocolCredSSP {
			// CredSSP mode: use Go's crypto/tls (required for session binding)
			credsspConn, err := c.server.deps.TLS.UpgradeServerConnForCredSSP(c.conn)
			if err != nil {
				return fmt.Errorf("TLS handshake failed: %w", err)
			}

			state := credsspConn.ConnectionState()
			c.server.deps.Logger.Warn().
				Str("version", tlsVersionString(state.Version)).
				Str("cipher", tls.CipherSuiteName(state.CipherSuite)).
				Bool("hwAccel", false).
				Msg("RDP: TLS handshake complete (CredSSP mode)")

			c.server.deps.Logger.Warn().Msg("RDP: starting CredSSP/NLA authentication")

			// Type assert to *tls.Conn for CredSSP handler
			goTLSConn, ok := credsspConn.(*tls.Conn)
			if !ok {
				return fmt.Errorf("CredSSP requires Go's *tls.Conn")
			}

			handler := credssp.NewHandler(goTLSConn)
			handler.SetDebugLog(func(format string, args ...any) {
				c.server.deps.Logger.Warn().Msgf(format, args...)
			})

			username, err := handler.Authenticate()
			if err != nil {
				return fmt.Errorf("CredSSP authentication failed: %w", err)
			}

			c.server.deps.Logger.Warn().
				Str("username", username).
				Msg("RDP: CredSSP/NLA authentication complete")

			// Replace connection and reader with TLS versions
			c.conn = credsspConn
			c.reader = bufio.NewReader(credsspConn)
		} else {
			// Plain TLS mode: use hardware-accelerated TLS if available
			tlsConn, err := c.server.deps.TLS.UpgradeServerConn(c.conn)
			if err != nil {
				return fmt.Errorf("TLS handshake failed: %w", err)
			}

			c.server.deps.Logger.Warn().
				Str("version", tlsConn.GetProtocolVersion()).
				Str("cipher", tlsConn.GetCipherName()).
				Bool("hwAccel", tlsConn.IsHardwareAccelerated()).
				Msg("RDP: TLS handshake complete")

			// Replace connection and reader with TLS versions
			c.conn = tlsConn
			c.reader = bufio.NewReader(tlsConn)
		}
	}

	// Clear both read and write deadlines that were set during TLS handshake.
	// IMPORTANT: SetDeadline clears both, while SetReadDeadline only clears read.
	// If we only clear read deadline, writes will start failing after HandshakeTimeout (30s).
	if err := c.conn.SetDeadline(time.Time{}); err != nil {
		return err
	}

	c.phase = PhaseBasicSettings
	return nil
}

// tlsVersionString returns a human-readable TLS version string.
func tlsVersionString(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("unknown (0x%04x)", version)
	}
}

// handleMCSConnect handles the MCS Connect-Initial/Response exchange.
func (c *Connection) handleMCSConnect() error {
	c.server.deps.Logger.Debug().Str("remote", c.RemoteAddr()).
		Msg("RDP: starting MCS connect")

	// Set deadline
	if err := c.conn.SetReadDeadline(time.Now().Add(protocol.NegotiationTimeout)); err != nil {
		return err
	}

	// Read MCS Connect-Initial (wrapped in X.224 Data)
	mcsData, err := protocol.ReadX224Data(c.reader)
	if err != nil {
		return fmt.Errorf("read connect-initial: %w", err)
	}

	ci, err := protocol.ParseConnectInitial(mcsData)
	if err != nil {
		c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to parse MCS Connect-Initial")
		return fmt.Errorf("parse connect-initial: %w", err)
	}
	c.server.deps.Logger.Warn().
		Int("userDataLen", len(ci.UserData)).
		Msg("RDP: MCS Connect-Initial parsed successfully")

	// Log client's domain parameters for debugging
	c.server.deps.Logger.Warn().
		Int("targetMaxChannels", ci.TargetParams.MaxChannelIDs).
		Int("targetMaxUsers", ci.TargetParams.MaxUserIDs).
		Int("targetMaxPDU", ci.TargetParams.MaxMCSPDUSize).
		Int("minMaxChannels", ci.MinParams.MaxChannelIDs).
		Int("minMaxUsers", ci.MinParams.MaxUserIDs).
		Int("maxMaxChannels", ci.MaxParams.MaxChannelIDs).
		Int("maxMaxUsers", ci.MaxParams.MaxUserIDs).
		Msg("RDP: client domain parameters")

	// Parse GCC user data
	ccr, err := protocol.ParseConferenceCreateRequest(ci.UserData)
	if err != nil {
		c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to parse GCC data, continuing")
	}

	// Log what client data blocks were parsed
	if ccr != nil {
		c.server.deps.Logger.Warn().
			Bool("hasCoreData", ccr.CoreData != nil).
			Bool("hasSecurityData", ccr.SecurityData != nil).
			Bool("hasNetworkData", ccr.NetworkData != nil).
			Bool("hasMsgChannelData", ccr.MsgChannelData != nil).
			Msg("RDP: client GCC data blocks")

		if ccr.CoreData != nil {
			c.server.deps.Logger.Warn().
				Uint16("earlyCapFlags", ccr.CoreData.EarlyCapabilityFlags).
				Uint32("version", ccr.CoreData.Version).
				Msg("RDP: client core data details")
		}
	}

	// Extract client info
	if ccr != nil && ccr.CoreData != nil {
		c.clientInfo = &ClientInfo{
			Name:           ccr.CoreData.GetClientName(),
			Version:        ccr.CoreData.RDPVersion(),
			Width:          ccr.CoreData.DesktopWidth,
			Height:         ccr.CoreData.DesktopHeight,
			ColorDepth:     ccr.CoreData.HighColorDepth,
			KeyboardLayout: ccr.CoreData.KeyboardLayout,
			KeyboardType:   ccr.CoreData.KeyboardType,
		}

		c.server.deps.Logger.Info().
			Str("client", c.clientInfo.Name).
			Str("version", c.clientInfo.Version).
			Uint16("width", c.clientInfo.Width).
			Uint16("height", c.clientInfo.Height).
			Msg("RDP: client connected")
	}

	// Extract channel definitions
	if ccr != nil && ccr.NetworkData != nil {
		// Log ALL raw channels from client for debugging
		rawNames := make([]string, len(ccr.NetworkData.Channels))
		for i, ch := range ccr.NetworkData.Channels {
			rawNames[i] = fmt.Sprintf("[%d]=%q", i, ch.Name.String())
		}
		c.server.deps.Logger.Warn().
			Uint32("rawChannelCount", ccr.NetworkData.ChannelCount).
			Int("parsedChannels", len(ccr.NetworkData.Channels)).
			Strs("rawChannelNames", rawNames).
			Msg("RDP: client NetworkData (raw)")

		// Only include channels with non-empty names (per xrdp fix for Jump Desktop)
		// Some clients send empty channel names for disabled channels
		c.channels = make([]ChannelInfo, 0, len(ccr.NetworkData.Channels))
		for _, ch := range ccr.NetworkData.Channels {
			name := ch.Name.String()
			if name != "" {
				c.channels = append(c.channels, ChannelInfo{
					Name:    name,
					Options: ch.Options,
				})
			}
		}

		// Log filtered channel info
		channelNames := make([]string, len(c.channels))
		for i, ch := range c.channels {
			channelNames[i] = ch.Name
		}
		c.server.deps.Logger.Warn().
			Int("filteredCount", len(c.channels)).
			Strs("filteredNames", channelNames).
			Msg("RDP: client requested virtual channels (after filtering)")
	}

	// Build MCS Connect-Response
	// Use RDP 5.0 version for maximum compatibility with older clients like Jump Desktop
	// Jump Desktop is RDP 5.0 and may not handle newer version numbers correctly
	// Per MS-RDPBCGR 2.2.1.4.2, clientRequestedProtocols should be set to
	// the value from the client's CS_CORE. In practice, Windows Desktop seems to
	// expect it to match the original X.224 requested protocols.
	clientReqProto := c.clientRequestedProtocols
	if clientReqProto == 0 {
		clientReqProto = c.selectedProtocol // Fallback
	}
	serverCore := &protocol.ServerCoreData{
		Version:              0x00080004, // RDP 5.0/5.1/5.2 - maximum compatibility
		ClientRequestedProto: clientReqProto,
		EarlyCapFlags:        0, // No early cap flags for RDP 5.0 compatibility
	}

	// Assign channel IDs to filtered channels only
	// Note: MS-RDPBCGR says channelCount SHOULD match client, but xrdp only returns
	// IDs for valid channels (per PR #615 Jump Desktop fix)
	baseChannelID := uint16(protocol.ChannelMCSGlobalID + 1) // 1004
	channelIDs := make([]uint16, len(c.channels))
	for i := range c.channels {
		c.channels[i].ID = baseChannelID + uint16(i)
		channelIDs[i] = c.channels[i].ID
		// Populate fast lookup array (lock-free hot path)
		if i < len(c.channelNames) {
			c.channelNames[i] = c.channels[i].Name
		}
	}

	// Determine message channel ID if client requested it
	var msgChannelID uint16
	if ccr != nil && ccr.MsgChannelData != nil {
		// Assign message channel ID after virtual channels
		msgChannelID = baseChannelID + uint16(len(c.channels))
		c.msgChannelID = msgChannelID
		c.server.deps.Logger.Debug().
			Uint32("clientFlags", ccr.MsgChannelData.Flags).
			Uint16("msgChannelID", msgChannelID).
			Msg("RDP: client requested message channel")
	}

	serverNetwork := &protocol.ServerNetworkData{
		MCSChannelID: protocol.ChannelMCSGlobalID,
		ChannelCount: uint16(len(c.channels)), // Only return valid channels
		ChannelIDs:   channelIDs,
		MsgChannelID: msgChannelID,
	}

	// No encryption for now (TLS handles security)
	serverSecurity := &protocol.ServerSecurityData{
		EncryptionMethod: 0,
		EncryptionLevel:  0,
	}

	gccResponse := protocol.BuildConferenceCreateResponse(serverCore, serverNetwork, serverSecurity)
	domainParams := protocol.DefaultDomainParameters()

	// Build the full MCS Connect-Response for logging
	mcsResponse := protocol.BuildConnectResponse(protocol.MCSResultSuccessful, 0, domainParams, gccResponse)

	c.server.deps.Logger.Warn().
		Int("gccResponseLen", len(gccResponse)).
		Uint32("clientReqProto", clientReqProto).
		Uint16("mcsChannelID", serverNetwork.MCSChannelID).
		Uint16("channelCount", serverNetwork.ChannelCount).
		Interface("channelIDs", serverNetwork.ChannelIDs).
		Uint16("msgChannelID", serverNetwork.MsgChannelID).
		Str("gccResponseHex", fmt.Sprintf("% 02X", gccResponse)).
		Int("mcsResponseLen", len(mcsResponse)).
		Str("mcsResponseHex", fmt.Sprintf("% 02X", mcsResponse)).
		Msg("RDP: building MCS Connect-Response")

	if err := protocol.WriteX224Data(c.conn, mcsResponse); err != nil {
		return fmt.Errorf("write connect-response: %w", err)
	}

	c.server.deps.Logger.Warn().Msg("RDP: sent MCS Connect-Response successfully")

	// Clear deadline
	if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
		return err
	}

	c.phase = PhaseChannelConnection
	return nil
}

// handleMCSChannelSetup handles MCS Erect Domain, Attach User, and Channel Join.
func (c *Connection) handleMCSChannelSetup() error {
	c.server.deps.Logger.Warn().Str("remote", c.RemoteAddr()).
		Msg("RDP: starting MCS channel setup, waiting for Erect Domain Request...")

	// Set deadline
	if err := c.conn.SetReadDeadline(time.Now().Add(protocol.NegotiationTimeout)); err != nil {
		return err
	}

	// 1. Receive Erect Domain Request
	data, err := protocol.ReadX224Data(c.reader)
	if err != nil {
		c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to read Erect Domain Request")
		return fmt.Errorf("read erect domain: %w", err)
	}
	c.server.deps.Logger.Warn().Int("dataLen", len(data)).Msg("RDP: received data for Erect Domain")

	pduType, err := protocol.ParseMCSPDUType(data)
	if err != nil {
		return err
	}
	if pduType != protocol.MCSErectDomainRequest {
		return fmt.Errorf("expected ErectDomainRequest, got %s", pduType)
	}

	c.server.deps.Logger.Debug().Msg("RDP: received erect domain request")

	// 2. Receive Attach User Request
	data, err = protocol.ReadX224Data(c.reader)
	if err != nil {
		c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to read Attach User Request")
		return fmt.Errorf("read attach user: %w", err)
	}

	c.server.deps.Logger.Warn().
		Int("dataLen", len(data)).
		Str("dataHex", fmt.Sprintf("% X", data)).
		Msg("RDP: received Attach User Request")

	pduType, err = protocol.ParseMCSPDUType(data)
	if err != nil {
		return err
	}
	if pduType != protocol.MCSAttachUserRequest {
		return fmt.Errorf("expected AttachUserRequest, got %s", pduType)
	}

	// Assign user ID and send confirm
	c.userID = protocol.MCSUserIDBase
	confirm := protocol.BuildAttachUserConfirm(protocol.MCSResultSuccessful, c.userID)

	c.server.deps.Logger.Warn().
		Uint16("userID", c.userID).
		Int("confirmLen", len(confirm)).
		Str("confirmHex", fmt.Sprintf("% X", confirm)).
		Msg("RDP: sending Attach User Confirm")

	if err := protocol.WriteMCSPDU(c.conn, confirm); err != nil {
		return fmt.Errorf("write attach user confirm: %w", err)
	}

	c.server.deps.Logger.Warn().Uint16("userID", c.userID).
		Msg("RDP: sent attach user confirm successfully")

	// 3. Handle Channel Join Requests
	// Client will join: user channel, I/O channel, and virtual channels
	// Note: c.channels only contains channels with non-empty names (filtered earlier)
	expectedJoins := 2 + len(c.channels) // user + IO + virtual channels
	if c.msgChannelID != 0 {
		expectedJoins++ // message channel
	}

	c.server.deps.Logger.Warn().
		Int("expectedJoins", expectedJoins).
		Int("virtualChannels", len(c.channels)).
		Uint16("msgChannelID", c.msgChannelID).
		Msg("RDP: waiting for channel join requests")

	for i := range expectedJoins {
		c.server.deps.Logger.Warn().Int("joinIndex", i).Msg("RDP: waiting for channel join request")
		data, err = protocol.ReadX224Data(c.reader)
		if err != nil {
			c.server.deps.Logger.Warn().Err(err).Int("joinIndex", i).Msg("RDP: failed to read channel join")
			return fmt.Errorf("read channel join: %w", err)
		}

		pduType, err = protocol.ParseMCSPDUType(data)
		if err != nil {
			return err
		}
		if pduType != protocol.MCSChannelJoinRequest {
			return fmt.Errorf("expected ChannelJoinRequest, got %s", pduType)
		}

		joinReq, err := protocol.ParseChannelJoinRequest(data)
		if err != nil {
			return err
		}

		// Track I/O channel
		if joinReq.ChannelID == protocol.ChannelMCSGlobalID {
			c.ioChannel = joinReq.ChannelID
		}

		// Send confirm
		confirm := protocol.BuildChannelJoinConfirm(
			protocol.MCSResultSuccessful,
			joinReq.UserID,
			joinReq.ChannelID,
		)
		if err := protocol.WriteMCSPDU(c.conn, confirm); err != nil {
			return fmt.Errorf("write channel join confirm: %w", err)
		}

		c.server.deps.Logger.Debug().
			Uint16("channelID", joinReq.ChannelID).
			Msg("RDP: confirmed channel join")
	}

	// Clear deadline
	if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
		return err
	}

	c.phase = PhaseSecurityExchange
	return nil
}

// handleSecurityExchange handles RDP security exchange.
func (c *Connection) handleSecurityExchange() error {
	c.server.deps.Logger.Debug().Str("remote", c.RemoteAddr()).
		Msg("RDP: starting security exchange")

	// When using TLS, this phase is simplified
	// We may receive a Security Exchange PDU or go straight to Client Info

	// Set deadline
	if err := c.conn.SetReadDeadline(time.Now().Add(protocol.NegotiationTimeout)); err != nil {
		return err
	}

	// Read the next PDU - this should be Client Info or Security Exchange
	data, err := protocol.ReadX224Data(c.reader)
	if err != nil {
		return fmt.Errorf("read security/info: %w", err)
	}

	// Parse MCS Send Data Request to get the payload
	pduType, err := protocol.ParseMCSPDUType(data)
	if err != nil {
		return err
	}

	if pduType != protocol.MCSSendDataRequest {
		return fmt.Errorf("expected SendDataRequest, got %s", pduType)
	}

	sdr, err := protocol.ParseSendDataRequest(data)
	if err != nil {
		return err
	}

	c.server.deps.Logger.Debug().
		Uint16("channelID", sdr.ChannelID).
		Int("dataLen", len(sdr.UserData)).
		Msg("RDP: received security/client info PDU")

	// TODO: Parse Client Info PDU for username, domain, etc.
	// For now, we proceed to licensing

	// Clear deadline
	if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
		return err
	}

	c.phase = PhaseLicensing
	return nil
}

// handleLicensing handles RDP licensing.
func (c *Connection) handleLicensing() error {
	c.server.deps.Logger.Debug().Str("remote", c.RemoteAddr()).
		Msg("RDP: handling licensing")

	// Send License Error PDU with STATUS_VALID_CLIENT
	// This tells the client that no license is required

	// Build License Error PDU
	// Header: SEC_LICENSE_PKT flag + License PDU
	licenseError := buildLicenseErrorPDU()

	// Wrap in MCS Send Data Indication
	mcsPDU := protocol.BuildSendDataIndication(c.userID, c.ioChannel, licenseError)

	c.server.deps.Logger.Warn().
		Int("licenseLen", len(licenseError)).
		Int("mcsLen", len(mcsPDU)).
		Uint16("ioChannel", c.ioChannel).
		Str("licenseHex", fmt.Sprintf("% X", licenseError)).
		Msg("RDP: sending license error PDU")

	if err := protocol.WriteMCSPDU(c.conn, mcsPDU); err != nil {
		return fmt.Errorf("write license error: %w", err)
	}

	c.server.deps.Logger.Warn().Msg("RDP: sent license error (valid client)")

	c.phase = PhaseCapabilities
	return nil
}

// buildLicenseErrorPDU builds a License Error PDU with STATUS_VALID_CLIENT.
// The PDU includes a 4-byte basic security header (flags + flagsHi) followed
// by the licensing PDU preamble and error message.
func buildLicenseErrorPDU() []byte {
	buf := make([]byte, 20)

	// Basic Security Header (4 bytes per MS-RDPBCGR 2.2.8.1.1.2)
	// flags = SEC_LICENSE_PKT (0x0080), no encryption
	buf[0] = 0x80 // flags low (SEC_LICENSE_PKT)
	buf[1] = 0x00 // flags high
	buf[2] = 0x00 // flagsHi low
	buf[3] = 0x00 // flagsHi high

	// License PDU Preamble (4 bytes per MS-RDPELE 2.2.1.1)
	buf[4] = 0xFF // bMsgType = ERROR_ALERT (0xFF)
	buf[5] = 0x02 // flags = PREAMBLE_VERSION_2_0 (0x02) - Windows 2000, compatible with RDP 5.0
	buf[6] = 0x10 // wMsgSize low (16 = size of preamble + message body)
	buf[7] = 0x00 // wMsgSize high

	// License Error Message (12 bytes per MS-RDPELE 2.2.2.7.1)
	// dwErrorCode = STATUS_VALID_CLIENT (0x00000007)
	buf[8] = 0x07
	buf[9] = 0x00
	buf[10] = 0x00
	buf[11] = 0x00

	// dwStateTransition = ST_NO_TRANSITION (0x00000002)
	buf[12] = 0x02
	buf[13] = 0x00
	buf[14] = 0x00
	buf[15] = 0x00

	// bbErrorInfo = LICENSE_BINARY_BLOB with BB_ANY_BLOB type
	// Per MS-RDPELE 2.2.2.3, when wBlobLen is 0, wBlobType SHOULD be ignored.
	// However, FreeRDP and some clients expect BB_ANY_BLOB (0x0000) for empty blobs.
	buf[16] = 0x00 // wBlobType low (BB_ANY_BLOB = 0x0000)
	buf[17] = 0x00 // wBlobType high
	buf[18] = 0x00 // wBlobLen low
	buf[19] = 0x00 // wBlobLen high

	return buf
}

// handleCapabilities handles capability exchange.
func (c *Connection) handleCapabilities() error {
	c.server.deps.Logger.Debug().Str("remote", c.RemoteAddr()).
		Msg("RDP: exchanging capabilities")

	// Send Demand Active PDU
	demandActive := c.buildDemandActivePDU()

	// Wrap in MCS Send Data Indication
	mcsPDU := protocol.BuildSendDataIndication(c.userID, c.ioChannel, demandActive)

	// Log first 64 bytes of the demand active for debugging
	hexLen := 64
	if len(demandActive) < hexLen {
		hexLen = len(demandActive)
	}
	c.server.deps.Logger.Warn().
		Int("demandActiveLen", len(demandActive)).
		Int("mcsLen", len(mcsPDU)).
		Uint16("ioChannel", c.ioChannel).
		Uint16("userID", c.userID).
		Str("demandActiveFirst64", fmt.Sprintf("% X", demandActive[:hexLen])).
		Msg("RDP: sending demand active PDU")

	if err := protocol.WriteMCSPDU(c.conn, mcsPDU); err != nil {
		return fmt.Errorf("write demand active: %w", err)
	}

	c.server.deps.Logger.Warn().Msg("RDP: sent demand active")

	// Set deadline for client response
	if err := c.conn.SetReadDeadline(time.Now().Add(protocol.NegotiationTimeout)); err != nil {
		return err
	}

	// Read Confirm Active PDU
	data, err := protocol.ReadX224Data(c.reader)
	if err != nil {
		return fmt.Errorf("read confirm active: %w", err)
	}

	pduType, err := protocol.ParseMCSPDUType(data)
	if err != nil {
		return err
	}

	if pduType != protocol.MCSSendDataRequest {
		return fmt.Errorf("expected SendDataRequest, got %s", pduType)
	}

	sdr, err := protocol.ParseSendDataRequest(data)
	if err != nil {
		return err
	}

	// TODO: Parse Confirm Active and extract client capabilities
	c.server.deps.Logger.Debug().
		Int("dataLen", len(sdr.UserData)).
		Msg("RDP: received confirm active")

	// Clear deadline
	if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
		return err
	}

	// Send finalization PDUs: Synchronize, Control (Cooperate), Control (Granted), Font Map
	if err := c.sendFinalizationPDUs(); err != nil {
		return fmt.Errorf("finalization failed: %w", err)
	}

	return nil
}

// buildDemandActivePDU builds a Demand Active PDU with server capabilities.
func (c *Connection) buildDemandActivePDU() []byte {
	// This is a simplified Demand Active PDU
	// A full implementation would include many capability sets

	w, h := c.GetResolution()

	// For now, build a minimal set of capabilities
	// Share control header + Share ID + capabilities

	// Pre-allocate buffer
	buf := make([]byte, 0, 1024)

	// Share Control Header will be added after we know the size
	// For now, build the PDU data

	// Share ID (4 bytes) - MS-RDPBCGR says: 0x000103EA + userID
	shareIDVal := uint32(0x000103EA) + uint32(c.userID)
	shareID := []byte{byte(shareIDVal), byte(shareIDVal >> 8), byte(shareIDVal >> 16), byte(shareIDVal >> 24)}

	// Length of source descriptor - use "RDP" as required by Jump Desktop
	// MS-RDPBCGR 2.2.1.13.1.1 says "typically a string such as 'RDP'"
	sourceDesc := []byte("RDP\x00")
	sourceDescLen := uint16(len(sourceDesc))

	// Capabilities
	caps := c.buildCapabilitySets(w, h)

	// Number of capability sets
	numCaps := countCapabilitySets(caps)

	// lengthCombinedCapabilities = numberCapabilities(2) + pad2Octets(2) + capabilitySets
	combinedLen := 2 + 2 + len(caps)

	// Calculate total length for share control header
	// Share Control Header (6) + shareId(4) + lengthSourceDescriptor(2) + lengthCombinedCapabilities(2) +
	// sourceDescriptor + numberCapabilities(2) + pad2Octets(2) + capabilitySets + sessionId(4)
	pduDataLen := 4 + 2 + 2 + len(sourceDesc) + 2 + 2 + len(caps) + 4
	totalLen := 6 + pduDataLen

	// Log for debugging
	c.server.deps.Logger.Warn().
		Int("totalLen", totalLen).
		Int("pduDataLen", pduDataLen).
		Int("sourceDescLen", int(sourceDescLen)).
		Int("combinedLen", combinedLen).
		Int("numCaps", numCaps).
		Int("capsLen", len(caps)).
		Msg("RDP: building Demand Active PDU")

	// Share Control Header
	buf = append(buf, byte(totalLen), byte(totalLen>>8)) // totalLength
	buf = append(buf, 0x11, 0x00)                        // pduType (PDUTYPE_DEMANDACTIVEPDU = 0x0011)
	// PDUSource - use IO channel (FreeRDP expects this)
	buf = append(buf, byte(c.ioChannel), byte(c.ioChannel>>8))

	// Share ID
	buf = append(buf, shareID...)

	// Source descriptor length
	buf = append(buf, byte(sourceDescLen), byte(sourceDescLen>>8))

	// Capability sets combined length
	buf = append(buf, byte(combinedLen), byte(combinedLen>>8))

	// Source descriptor
	buf = append(buf, sourceDesc...)

	// Number of capability sets
	buf = append(buf, byte(numCaps), byte(numCaps>>8))

	// Padding
	buf = append(buf, 0x00, 0x00)

	// Capability sets
	buf = append(buf, caps...)

	// Session ID (4 bytes) - required by FreeRDP and modern clients
	buf = append(buf, 0x00, 0x00, 0x00, 0x00)

	return buf
}

// buildCapabilitySets builds the server capability sets.
// Note: FreeRDP explicitly rejects certain capabilities from servers (Brush, GlyphCache, etc.)
// Only send capabilities that are expected from servers per MS-RDPBCGR.
func (c *Connection) buildCapabilitySets(width, height uint16) []byte {
	buf := make([]byte, 0, 512)

	// General Capability Set (required per MS-RDPBCGR 2.2.7)
	buf = append(buf, buildGeneralCapability()...)

	// Bitmap Capability Set (required per MS-RDPBCGR 2.2.7)
	buf = append(buf, buildBitmapCapability(width, height)...)

	// Order Capability Set (required per MS-RDPBCGR 2.2.7, even if no order support)
	buf = append(buf, buildOrderCapability()...)

	// Pointer Capability Set (required per MS-RDPBCGR 2.2.7)
	buf = append(buf, buildPointerCapability()...)

	// Input Capability Set (may be required by older clients)
	buf = append(buf, buildInputCapability()...)

	// Multifragment Update Capability Set - required for large updates (> 16KB)
	// Windows Desktop client specifically checks for this capability
	buf = append(buf, buildMultifragUpdateCapability()...)

	// Large Pointer Capability Set - for large cursor support
	buf = append(buf, buildLargePointerCapability()...)

	// TEMPORARILY DISABLED - testing if this breaks DVC
	// // Sound Capability Set - required for RDPSND audio output
	// buf = append(buf, buildSoundCapability()...)

	return buf
}

// countCapabilitySets counts the number of capability sets in a buffer.
func countCapabilitySets(caps []byte) int {
	count := 0
	pos := 0
	for pos+4 <= len(caps) {
		length := int(caps[pos+2]) | int(caps[pos+3])<<8
		if length < 4 || pos+length > len(caps) {
			break
		}
		count++
		pos += length
	}
	return count
}

// Individual capability set builders

func buildGeneralCapability() []byte {
	buf := make([]byte, 24)
	// Type
	buf[0] = byte(protocol.CapabilityGeneral)
	buf[1] = byte(protocol.CapabilityGeneral >> 8)
	// Length
	buf[2] = 24
	buf[3] = 0
	// OS major/minor type (Windows)
	buf[4] = 1
	buf[6] = 3
	// Protocol version
	buf[8] = 0x00
	buf[9] = 0x02
	// Compression types
	buf[12] = 0x00
	// Extra flags - enable FASTPATH_OUTPUT_SUPPORTED for larger bitmap updates
	// FASTPATH was added in RDP 5.1; modern clients like Windows Desktop support it
	extraFlags := protocol.ExtraFlagsFastPathOutputSupported | protocol.ExtraFlagsNoBitmapCompressionHdr
	buf[14] = byte(extraFlags)
	buf[15] = byte(extraFlags >> 8)
	// Update capability
	buf[16] = 0x00
	// Remote unshare
	buf[18] = 0x00
	// Compression level
	buf[20] = 0x00
	// Refresh rect support
	buf[22] = 0x01
	// Suppress output support
	buf[23] = 0x01
	return buf
}

func buildBitmapCapability(width, height uint16) []byte {
	buf := make([]byte, 28)
	// Type
	buf[0] = byte(protocol.CapabilityBitmap)
	buf[1] = byte(protocol.CapabilityBitmap >> 8)
	// Length
	buf[2] = 28
	buf[3] = 0
	// Preferred BPP
	buf[4] = 32
	buf[5] = 0
	// Receive 1-bit palettes
	buf[6] = 1
	// Receive 4-bit palettes
	buf[8] = 1
	// Receive 8-bit palettes
	buf[10] = 1
	// Desktop width
	buf[12] = byte(width)
	buf[13] = byte(width >> 8)
	// Desktop height
	buf[14] = byte(height)
	buf[15] = byte(height >> 8)
	// Pad
	buf[16] = 0
	buf[17] = 0
	// Desktop resize (2 bytes at offset 18-19)
	buf[18] = 1
	buf[19] = 0
	// Bitmap compression (2 bytes at offset 20-21)
	buf[20] = 1
	buf[21] = 0
	// High color flags (1 byte at offset 22) - 0 = no specific color depth preference
	buf[22] = 0
	// Drawing flags (1 byte at offset 23)
	buf[23] = 0x01 // DRAW_ALLOW_DYNAMIC_COLOR_FIDELITY
	// Multiple rectangle support (2 bytes at offset 24-25)
	buf[24] = 1
	buf[25] = 0
	// Pad
	buf[26] = 0
	buf[27] = 0
	return buf
}

func buildOrderCapability() []byte {
	buf := make([]byte, 88)
	// Type
	buf[0] = byte(protocol.CapabilityOrder)
	buf[1] = byte(protocol.CapabilityOrder >> 8)
	// Length
	buf[2] = 88
	buf[3] = 0
	// Terminal descriptor (16 bytes) - all zeros
	// Pad (4 bytes)
	// Desktop save X granularity
	buf[24] = 1
	buf[25] = 0
	// Desktop save Y granularity
	buf[26] = 20
	buf[27] = 0
	// Pad
	// Maximum order level
	buf[30] = 1
	buf[31] = 0
	// Number of fonts
	buf[32] = 0
	buf[33] = 0
	// Order flags
	buf[34] = 0x22 // NEGOTIATEORDERSUPPORT | COLORINDEXSUPPORT
	buf[35] = 0
	// Order support (32 bytes) - minimal support
	// Order support extra flags
	buf[68] = 0
	buf[69] = 0
	// Pad
	// Desktop save size
	buf[76] = 0
	buf[77] = 0
	buf[78] = 0x04 // 480KB
	buf[79] = 0
	// Pad
	// Text ANSI code page
	buf[84] = 0xE4 // 1252
	buf[85] = 0x04
	// Pad
	return buf
}

func buildPointerCapability() []byte {
	// Per MS-RDPBCGR 2.2.7.1.5, the Pointer Capability Set sent in a Demand Active PDU
	// MUST include the pointerCacheSize field, making it 10 bytes minimum.
	buf := make([]byte, 10)
	// Type
	buf[0] = byte(protocol.CapabilityPointer)
	buf[1] = byte(protocol.CapabilityPointer >> 8)
	// Length (10 bytes including pointerCacheSize)
	buf[2] = 10
	buf[3] = 0
	// Color pointer flag (1 = supported)
	buf[4] = 1
	buf[5] = 0
	// Color pointer cache size (25 is the default)
	buf[6] = 25
	buf[7] = 0
	// Pointer cache size (25 is the default) - REQUIRED in server Demand Active PDU
	buf[8] = 25
	buf[9] = 0
	return buf
}

func buildInputCapability() []byte {
	buf := make([]byte, 88)
	// Type
	buf[0] = byte(protocol.CapabilityInput)
	buf[1] = byte(protocol.CapabilityInput >> 8)
	// Length
	buf[2] = 88
	buf[3] = 0
	// Input flags - no FASTPATH for RDP 5.0 compatibility
	// 0x25 = SCANCODES (0x01) | MOUSEX (0x04) | UNICODE (0x20)
	buf[4] = 0x25
	buf[5] = 0
	// Pad
	// Keyboard layout (US)
	buf[8] = 0x09
	buf[9] = 0x04
	// Keyboard type (IBM enhanced 101/102)
	buf[12] = 4
	buf[13] = 0
	buf[14] = 0
	buf[15] = 0
	// Keyboard subtype
	buf[16] = 0
	// Keyboard function keys
	buf[20] = 12
	// IME filename (64 bytes) - zeros
	return buf
}

// buildMultifragUpdateCapability builds the Multifragment Update capability set.
// Per MS-RDPBCGR 2.2.7.2.6 - Required for updates larger than 16KB.
func buildMultifragUpdateCapability() []byte {
	buf := make([]byte, 8)
	// Type
	buf[0] = byte(protocol.CapabilityMultifragUpdate)
	buf[1] = byte(protocol.CapabilityMultifragUpdate >> 8)
	// Length
	buf[2] = 8
	buf[3] = 0
	// MaxRequestSize - maximum size of a single update in bytes
	// 0x3E8000 = 4,096,000 bytes (roughly 4MB) - sufficient for 1080p frames
	binary.LittleEndian.PutUint32(buf[4:8], 0x3E8000)
	return buf
}

// buildLargePointerCapability builds the Large Pointer capability set.
// Per MS-RDPBCGR 2.2.7.2.7 - For large cursor support (> 32x32).
func buildLargePointerCapability() []byte {
	buf := make([]byte, 6)
	// Type
	buf[0] = byte(protocol.CapabilityLargePointer)
	buf[1] = byte(protocol.CapabilityLargePointer >> 8)
	// Length
	buf[2] = 6
	buf[3] = 0
	// LargePointerSupportFlags
	// 0x0001 = LARGE_POINTER_FLAG_96x96 - support 96x96 pointers
	buf[4] = 0x01
	buf[5] = 0x00
	return buf
}

// sendFinalizationPDUs sends the synchronize, control, and font map PDUs.
func (c *Connection) sendFinalizationPDUs() error {
	// Read and respond to client finalization PDUs
	// Client sends: Synchronize, Control (Cooperate), Control (Request Control), Font List

	for i := range 4 {
		data, err := protocol.ReadX224Data(c.reader)
		if err != nil {
			return fmt.Errorf("read finalization PDU %d: %w", i, err)
		}

		pduType, err := protocol.ParseMCSPDUType(data)
		if err != nil {
			return err
		}

		if pduType != protocol.MCSSendDataRequest {
			return fmt.Errorf("expected SendDataRequest, got %s", pduType)
		}

		c.server.deps.Logger.Debug().
			Int("pdu", i).
			Msg("RDP: received finalization PDU")
	}

	// Send our finalization PDUs
	// Synchronize
	syncPDU := c.buildSynchronizePDU()
	mcsPDU := protocol.BuildSendDataIndication(c.userID, c.ioChannel, syncPDU)
	if err := protocol.WriteMCSPDU(c.conn, mcsPDU); err != nil {
		return err
	}

	// Control (Cooperate)
	controlPDU := c.buildControlPDU(0x04) // CTRLACTION_COOPERATE
	mcsPDU = protocol.BuildSendDataIndication(c.userID, c.ioChannel, controlPDU)
	if err := protocol.WriteMCSPDU(c.conn, mcsPDU); err != nil {
		return err
	}

	// Control (Granted)
	controlPDU = c.buildControlPDU(0x02) // CTRLACTION_GRANTED_CONTROL
	mcsPDU = protocol.BuildSendDataIndication(c.userID, c.ioChannel, controlPDU)
	if err := protocol.WriteMCSPDU(c.conn, mcsPDU); err != nil {
		return err
	}

	// Font Map
	fontMapPDU := c.buildFontMapPDU()
	mcsPDU = protocol.BuildSendDataIndication(c.userID, c.ioChannel, fontMapPDU)
	if err := protocol.WriteMCSPDU(c.conn, mcsPDU); err != nil {
		return err
	}

	c.server.deps.Logger.Debug().Msg("RDP: sent finalization PDUs")

	return nil
}

func (c *Connection) buildSynchronizePDU() []byte {
	buf := make([]byte, 22)
	// Share control header
	buf[0] = 22 // totalLength
	buf[1] = 0
	buf[2] = 0x17 // PDUTYPE_DATAPDU
	buf[3] = 0x00
	buf[4] = byte(c.userID)
	buf[5] = byte(c.userID >> 8)
	// Share data header
	buf[6] = 0x66 // ShareID
	buf[7] = 0x72
	buf[8] = 0x65
	buf[9] = 0x64
	buf[10] = 0 // Pad
	buf[11] = 1 // StreamID
	buf[12] = 4 // UncompressedLength (4 bytes: messageType + targetUser)
	buf[13] = 0
	buf[14] = protocol.DataPDUTypeSynchronize // PDUType2
	buf[15] = 0                               // Compression type
	buf[16] = 0                               // Compressed length
	buf[17] = 0
	// Synchronize data
	buf[18] = 1 // messageType (CYCLESYNCREADY = 1)
	buf[19] = 0
	buf[20] = byte(c.userID) // targetUser
	buf[21] = byte(c.userID >> 8)
	return buf
}

func (c *Connection) buildControlPDU(action uint16) []byte {
	buf := make([]byte, 26)
	// Share control header
	buf[0] = 26 // totalLength
	buf[1] = 0
	buf[2] = 0x17 // PDUTYPE_DATAPDU
	buf[3] = 0x00
	buf[4] = byte(c.userID)
	buf[5] = byte(c.userID >> 8)
	// Share data header
	buf[6] = 0x66 // ShareID
	buf[7] = 0x72
	buf[8] = 0x65
	buf[9] = 0x64
	buf[10] = 0 // Pad
	buf[11] = 1 // StreamID
	buf[12] = 8 // UncompressedLength (8 bytes: action + grantId + controlId)
	buf[13] = 0
	buf[14] = protocol.DataPDUTypeControl // PDUType2
	buf[15] = 0                           // Compression type
	buf[16] = 0                           // Compressed length
	buf[17] = 0
	// Control data
	buf[18] = byte(action) // action
	buf[19] = byte(action >> 8)
	buf[20] = 0 // grantId
	buf[21] = 0
	buf[22] = 0 // controlId
	buf[23] = 0
	buf[24] = 0
	buf[25] = 0
	return buf
}

func (c *Connection) buildFontMapPDU() []byte {
	buf := make([]byte, 26)
	// Share control header
	buf[0] = 26 // totalLength
	buf[1] = 0
	buf[2] = 0x17 // PDUTYPE_DATAPDU
	buf[3] = 0x00
	buf[4] = byte(c.userID)
	buf[5] = byte(c.userID >> 8)
	// Share data header
	buf[6] = 0x66 // ShareID
	buf[7] = 0x72
	buf[8] = 0x65
	buf[9] = 0x64
	buf[10] = 0 // Pad
	buf[11] = 1 // StreamID
	buf[12] = 8 // UncompressedLength (8 bytes: font map data)
	buf[13] = 0
	buf[14] = protocol.DataPDUTypeFontMap // PDUType2
	buf[15] = 0                           // Compression type
	buf[16] = 0                           // Compressed length
	buf[17] = 0
	// Font map data
	buf[18] = 0 // numberEntries
	buf[19] = 0
	buf[20] = 0 // totalNumEntries
	buf[21] = 0
	buf[22] = 0x03 // mapFlags (FONTMAP_FIRST | FONTMAP_LAST)
	buf[23] = 0
	buf[24] = 0x04 // entrySize
	buf[25] = 0
	return buf
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
		data, err := protocol.ReadX224Data(c.reader)
		if err != nil {
			return err
		}

		pduType, err := protocol.ParseMCSPDUType(data)
		if err != nil {
			return err
		}

		switch pduType {
		case protocol.MCSSendDataRequest:
			sdr, err := protocol.ParseSendDataRequest(data)
			if err != nil {
				c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to parse SendDataRequest")
				continue
			}
			c.handleDataPDU(sdr)

		case protocol.MCSDisconnectUltimatum:
			c.server.deps.Logger.Info().Str("remote", c.RemoteAddr()).
				Msg("RDP: client disconnected")
			return nil

		default:
			c.server.deps.Logger.Warn().
				Str("type", pduType.String()).
				Hex("firstBytes", data[:min(len(data), 16)]).
				Msg("RDP: unhandled MCS PDU type")
		}
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

// handleInputPDU handles input events from the client.
func (c *Connection) handleInputPDU(data []byte) {
	if len(data) < 4 {
		return
	}

	// Input PDU contains multiple input events
	numEvents := int(data[0]) | int(data[1])<<8
	pos := 4 // Skip numEvents and pad

	for i := 0; i < numEvents && pos+6 <= len(data); i++ {
		// Each event: 4 bytes time, 2 bytes type, variable data
		// eventTime := binary.LittleEndian.Uint32(data[pos : pos+4])
		eventType := binary.LittleEndian.Uint16(data[pos+4 : pos+6])
		pos += 6

		switch eventType {
		case 0x0001: // INPUT_EVENT_MOUSE
			if pos+6 > len(data) {
				break
			}
			c.handleMouseEvent(data[pos : pos+6])
			pos += 6
		case 0x0004: // INPUT_EVENT_SCANCODE
			if pos+6 > len(data) {
				break
			}
			c.handleScancodeEvent(data[pos : pos+6])
			pos += 6
		case 0x0005: // INPUT_EVENT_UNICODE
			if pos+6 > len(data) {
				break
			}
			c.handleUnicodeEvent(data[pos : pos+6])
			pos += 6
		case 0x0008: // INPUT_EVENT_MOUSEX (extended mouse)
			if pos+6 > len(data) {
				break
			}
			c.handleMouseXEvent(data[pos : pos+6])
			pos += 6
		default:
			c.server.deps.Logger.Debug().
				Uint16("eventType", eventType).
				Msg("RDP: unhandled input event type")
			return // Unknown event, can't determine size to continue
		}
	}
}

// handleMouseEvent handles basic mouse input.
// RDP mouse flags (MS-RDPBCGR 2.2.8.1.1.3.1.1.3):
//   - PTRFLAGS_HWHEEL (0x0400): Horizontal wheel event
//   - PTRFLAGS_WHEEL (0x0200): Vertical wheel event
//   - PTRFLAGS_WHEEL_NEGATIVE (0x0100): Wheel rotation is negative
//   - PTRFLAGS_MOVE (0x0800): Mouse moved
//   - PTRFLAGS_DOWN (0x8000): Button is being pressed (vs released)
//   - PTRFLAGS_BUTTON1 (0x1000): Left button
//   - PTRFLAGS_BUTTON2 (0x2000): Right button
//   - PTRFLAGS_BUTTON3 (0x4000): Middle button
func (c *Connection) handleMouseEvent(data []byte) {
	if len(data) < 6 {
		return
	}

	pointerFlags := binary.LittleEndian.Uint16(data[0:2])
	xPos := binary.LittleEndian.Uint16(data[2:4])
	yPos := binary.LittleEndian.Uint16(data[4:6])

	// Convert to absolute HID coordinates
	w, h := c.GetResolution()
	if w == 0 || h == 0 {
		return
	}

	absX := int(xPos) * 32767 / int(w)
	absY := int(yPos) * 32767 / int(h)

	// Track button state based on RDP events
	// RDP sends button flags with PTRFLAGS_DOWN on press, without on release
	// Button flags are only set during actual button events, not during moves
	hasButtonEvent := (pointerFlags & 0x7000) != 0 // Any of BUTTON1/2/3
	if hasButtonEvent {
		isDown := (pointerFlags & 0x8000) != 0 // PTRFLAGS_DOWN
		if isDown {
			// Button press - set bits
			if pointerFlags&0x1000 != 0 { // PTRFLAGS_BUTTON1
				c.mouseButtons |= 0x01 // Left
			}
			if pointerFlags&0x2000 != 0 { // PTRFLAGS_BUTTON2
				c.mouseButtons |= 0x02 // Right
			}
			if pointerFlags&0x4000 != 0 { // PTRFLAGS_BUTTON3
				c.mouseButtons |= 0x04 // Middle
			}
		} else {
			// Button release - clear bits
			if pointerFlags&0x1000 != 0 { // PTRFLAGS_BUTTON1
				c.mouseButtons &^= 0x01 // Left
			}
			if pointerFlags&0x2000 != 0 { // PTRFLAGS_BUTTON2
				c.mouseButtons &^= 0x02 // Right
			}
			if pointerFlags&0x4000 != 0 { // PTRFLAGS_BUTTON3
				c.mouseButtons &^= 0x04 // Middle
			}
		}
	}

	if err := c.server.deps.HID.AbsMouseReport(absX, absY, c.mouseButtons); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: mouse report failed")
	}

	// Handle vertical wheel (PTRFLAGS_WHEEL = 0x0200)
	// RDP wheel delta: WHEEL_DELTA (120) = one notch. Lower 8 bits contain magnitude.
	// HID wheel: signed 8-bit value, typically 1-3 per notch.
	// We scale by dividing by 40 (120/40 ≈ 3 HID units per notch) and ensure
	// small movements aren't lost. Minimum movement is ±1 if delta is non-zero.
	if pointerFlags&0x0200 != 0 {
		delta := int(pointerFlags & 0x00FF)
		if pointerFlags&0x0100 != 0 { // PTRFLAGS_WHEEL_NEGATIVE
			delta = -delta
		}
		// Scale to small values like VNC uses (±1 to ±3)
		// RDP deltas can be large (120+ per notch), so we scale down significantly
		wheelY := int8(delta / 120) // One notch = 1 HID unit
		if wheelY == 0 && delta != 0 {
			if delta > 0 {
				wheelY = 1
			} else {
				wheelY = -1
			}
		}
		// Cap to reasonable range
		if wheelY > 3 {
			wheelY = 3
		} else if wheelY < -3 {
			wheelY = -3
		}
		if err := c.server.deps.HID.WheelReport(wheelY, 0); err != nil {
			c.server.deps.Logger.Debug().Err(err).Msg("RDP: vertical wheel report failed")
		}
	}

	// Handle horizontal wheel (PTRFLAGS_HWHEEL = 0x0400)
	if pointerFlags&0x0400 != 0 {
		delta := int(pointerFlags & 0x00FF)
		if pointerFlags&0x0100 != 0 { // PTRFLAGS_WHEEL_NEGATIVE
			delta = -delta
		}
		// Scale to small values like VNC uses (±1 to ±3)
		wheelX := int8(delta / 120)
		if wheelX == 0 && delta != 0 {
			if delta > 0 {
				wheelX = 1
			} else {
				wheelX = -1
			}
		}
		// Cap to reasonable range
		if wheelX > 3 {
			wheelX = 3
		} else if wheelX < -3 {
			wheelX = -3
		}
		if err := c.server.deps.HID.WheelReport(0, wheelX); err != nil {
			c.server.deps.Logger.Debug().Err(err).Msg("RDP: horizontal wheel report failed")
		}
	}
}

// handleMouseXEvent handles extended mouse input.
func (c *Connection) handleMouseXEvent(data []byte) {
	// Similar to handleMouseEvent but with extended button support
	c.handleMouseEvent(data)
}

// handleScancodeEvent handles keyboard scancode input.
func (c *Connection) handleScancodeEvent(data []byte) {
	if len(data) < 6 {
		return
	}

	keyboardFlags := binary.LittleEndian.Uint16(data[0:2])
	scancode := binary.LittleEndian.Uint16(data[2:4])

	// Key up or down
	pressed := keyboardFlags&0x8000 == 0 // KBDFLAGS_RELEASE

	// Track Ctrl key state for paste/copy detection
	// Left Ctrl: scancode 0x1D, Right Ctrl: scancode 0x1D with extended flag
	if scancode == 0x1D {
		c.ctrlPressed.Store(pressed)
	}

	// Handle clipboard-related key combinations (only on key down)
	if pressed && c.ctrlPressed.Load() && c.clipboardChannel != nil && c.server.deps.Config.GetRDPClipboardEnabled() {
		switch scancode {
		case 0x2E, 0x2D: // C key (0x2E) or X key (0x2D) - copy/cut
			// Clear RDP clipboard when user copies on managed PC
			// This prevents stale clipboard data from being pasted
			c.clipboardChannel.ClearClipboardText()
			c.server.deps.Logger.Debug().
				Uint16("scancode", scancode).
				Msg("RDP: copy/cut detected, cleared clipboard")
			// Don't return - still forward the key for native copy/cut

		case 0x2F: // V key - paste
			text := c.clipboardChannel.GetClipboardText()
			if text != nil {
				c.server.deps.Logger.Debug().
					Int("textLen", len(text)).
					Msg("RDP: pasting clipboard text via keyboard")

				c.pasteInProgress.Store(true)

				// Type the clipboard text using keyboard macro
				if err := c.server.deps.HID.KeyboardMacro(string(text)); err != nil {
					c.server.deps.Logger.Debug().Err(err).Msg("RDP: clipboard paste failed")
				}
				return // Don't forward the V key down
			}
		}
	}

	// Handle V key release after paste - suppress to avoid orphan key-up
	if scancode == 0x2F && !pressed && c.pasteInProgress.Load() {
		c.pasteInProgress.Store(false)
		return // Don't forward the V key up
	}

	// Convert scancode to HID code
	hidCode := scancodeToHID(scancode, keyboardFlags)
	if hidCode == 0 {
		return
	}

	if err := c.server.deps.HID.KeypressReport(hidCode, pressed); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: key report failed")
	}
}

// handleUnicodeEvent handles Unicode keyboard input.
func (c *Connection) handleUnicodeEvent(data []byte) {
	if len(data) < 6 {
		return
	}

	keyboardFlags := binary.LittleEndian.Uint16(data[0:2])
	unicodeCode := binary.LittleEndian.Uint16(data[2:4])

	// Only handle key down for Unicode
	if keyboardFlags&0x8000 != 0 { // KBDFLAGS_RELEASE
		return
	}

	// Use keyboard macro for Unicode input
	if err := c.server.deps.HID.KeyboardMacro(string(rune(unicodeCode))); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: unicode input failed")
	}
}

// handleFastPathInput reads and handles a Fast-Path Input PDU.
// Fast-Path format: header(1) + length(1-2) + events(variable)
// MS-RDPBCGR 2.2.8.1.2
func (c *Connection) handleFastPathInput() error {
	// Read header byte
	header, err := c.reader.ReadByte()
	if err != nil {
		return fmt.Errorf("read fast-path header: %w", err)
	}

	// Extract fields from header
	// Bits 0-1: action (0 = Fast-Path)
	// Bits 2-3: number of events (0 means events in payload)
	// Bits 4-5: secFlags
	// Bits 6-7: reserved
	numEvents := int((header >> 2) & 0x03)
	// secFlags := (header >> 4) & 0x03

	// Read length (1 or 2 bytes)
	length1, err := c.reader.ReadByte()
	if err != nil {
		return fmt.Errorf("read fast-path length: %w", err)
	}

	var totalLength int
	if length1&0x80 != 0 {
		// Two-byte length
		length2, err := c.reader.ReadByte()
		if err != nil {
			return fmt.Errorf("read fast-path length2: %w", err)
		}
		totalLength = int(length1&0x7F)<<8 | int(length2)
	} else {
		totalLength = int(length1)
	}

	// Calculate payload size (total - header - length bytes)
	headerSize := 2 // header + length1
	if length1&0x80 != 0 {
		headerSize = 3 // header + length1 + length2
	}
	payloadSize := totalLength - headerSize
	if payloadSize < 0 {
		return fmt.Errorf("invalid fast-path length: %d", totalLength)
	}

	if payloadSize == 0 {
		return nil
	}

	// Read payload using pool for zero-allocation hot path
	var payload []byte
	var poolBuf *[]byte
	if payloadSize <= 256 {
		// Use pool for typical small payloads
		poolBuf = inputPayloadPool.Get().(*[]byte)
		payload = (*poolBuf)[:payloadSize]
	} else {
		// Rare: large payload, allocate (shouldn't happen in practice)
		payload = make([]byte, payloadSize)
	}

	if _, err := io.ReadFull(c.reader, payload); err != nil {
		if poolBuf != nil {
			inputPayloadPool.Put(poolBuf)
		}
		return fmt.Errorf("read fast-path payload: %w", err)
	}

	// Process events, then return buffer to pool
	defer func() {
		if poolBuf != nil {
			inputPayloadPool.Put(poolBuf)
		}
	}()

	// If numEvents is 0, first byte of payload is the event count
	pos := 0
	if numEvents == 0 {
		if len(payload) < 1 {
			return nil
		}
		numEvents = int(payload[0])
		pos = 1
	}

	// Process each event
	for i := 0; i < numEvents && pos < len(payload); i++ {
		if pos >= len(payload) {
			break
		}

		eventHeader := payload[pos]
		pos++

		// Bits 0-4: event flags
		// Bits 5-7: event code
		eventCode := (eventHeader >> 5) & 0x07
		eventFlags := eventHeader & 0x1F

		switch eventCode {
		case 0: // FASTPATH_INPUT_EVENT_SCANCODE
			if pos >= len(payload) {
				break
			}
			scancode := payload[pos]
			pos++
			c.handleFastPathScancode(scancode, eventFlags)

		case 1: // FASTPATH_INPUT_EVENT_MOUSE
			if pos+6 > len(payload) {
				break
			}
			c.handleFastPathMouse(payload[pos : pos+6])
			pos += 6

		case 2: // FASTPATH_INPUT_EVENT_MOUSEX
			if pos+6 > len(payload) {
				break
			}
			c.handleFastPathMouse(payload[pos : pos+6])
			pos += 6

		case 3: // FASTPATH_INPUT_EVENT_SYNC
			// Synchronize event (caps lock, num lock, etc.)
			// Just skip it for now

		case 4: // FASTPATH_INPUT_EVENT_UNICODE
			if pos+2 > len(payload) {
				break
			}
			unicodeCode := binary.LittleEndian.Uint16(payload[pos : pos+2])
			pos += 2
			c.handleFastPathUnicode(unicodeCode, eventFlags)

		case 5: // FASTPATH_INPUT_EVENT_QOE_TIMESTAMP
			if pos+4 > len(payload) {
				break
			}
			pos += 4 // Skip timestamp

		default:
			// Unknown event type, cannot continue safely
			return nil
		}
	}

	return nil
}

// handleFastPathScancode processes a fast-path keyboard event.
func (c *Connection) handleFastPathScancode(scancode byte, flags byte) {
	// Flags: bit 0 = release, bit 1 = extended
	released := flags&0x01 != 0
	extended := flags&0x02 != 0
	pressed := !released

	var kbdFlags uint16
	if released {
		kbdFlags |= 0x8000 // KBDFLAGS_RELEASE
	}
	if extended {
		kbdFlags |= 0x0100 // KBDFLAGS_EXTENDED
	}

	// Track Ctrl key state for paste/copy detection
	// Left Ctrl: scancode 0x1D, Right Ctrl: scancode 0x1D with extended flag
	if scancode == 0x1D {
		c.ctrlPressed.Store(pressed)
	}

	// Handle clipboard-related key combinations (only on key down)
	if pressed && c.ctrlPressed.Load() && c.clipboardChannel != nil && c.server.deps.Config.GetRDPClipboardEnabled() {
		switch scancode {
		case 0x2E, 0x2D: // C key (0x2E) or X key (0x2D) - copy/cut
			c.clipboardChannel.ClearClipboardText()
			c.server.deps.Logger.Debug().
				Uint8("scancode", scancode).
				Msg("RDP: copy/cut detected, cleared clipboard")
			// Don't return - still forward the key for native copy/cut

		case 0x2F: // V key - paste
			text := c.clipboardChannel.GetClipboardText()
			if text != nil {
				c.server.deps.Logger.Debug().
					Int("textLen", len(text)).
					Msg("RDP: pasting clipboard text via keyboard")

				c.pasteInProgress.Store(true)

				// Type the clipboard text using keyboard macro
				if err := c.server.deps.HID.KeyboardMacro(string(text)); err != nil {
					c.server.deps.Logger.Debug().Err(err).Msg("RDP: clipboard paste failed")
				}
				return // Don't forward the V key down
			}
		}
	}

	// Handle V key release after paste - suppress to avoid orphan key-up
	if scancode == 0x2F && released && c.pasteInProgress.Load() {
		c.pasteInProgress.Store(false)
		return // Don't forward the V key up
	}

	hidCode := scancodeToHID(uint16(scancode), kbdFlags)
	if hidCode == 0 {
		return
	}

	if err := c.server.deps.HID.KeypressReport(hidCode, pressed); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: fast-path key report failed")
	}
}

// handleFastPathMouse processes a fast-path mouse event.
func (c *Connection) handleFastPathMouse(data []byte) {
	// Same format as slow-path mouse: flags(2) + x(2) + y(2)
	c.handleMouseEvent(data)
}

// handleFastPathUnicode processes a fast-path unicode event.
func (c *Connection) handleFastPathUnicode(unicodeCode uint16, flags byte) {
	released := flags&0x01 != 0
	if released {
		return // Only handle key down
	}

	if err := c.server.deps.HID.KeyboardMacro(string(rune(unicodeCode))); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: fast-path unicode input failed")
	}
}

// scancodeToHID converts an RDP scancode to HID code.
// This is a simplified version - a full implementation would handle all scancodes.
func scancodeToHID(scancode uint16, flags uint16) uint8 {
	// Extended key flag
	extended := flags&0x0100 != 0

	// Basic scancode to HID mapping
	// This is the US keyboard layout mapping
	var hidCode uint8

	switch scancode {
	case 0x01:
		hidCode = 0x29 // Escape
	case 0x02:
		hidCode = 0x1E // 1
	case 0x03:
		hidCode = 0x1F // 2
	case 0x04:
		hidCode = 0x20 // 3
	case 0x05:
		hidCode = 0x21 // 4
	case 0x06:
		hidCode = 0x22 // 5
	case 0x07:
		hidCode = 0x23 // 6
	case 0x08:
		hidCode = 0x24 // 7
	case 0x09:
		hidCode = 0x25 // 8
	case 0x0A:
		hidCode = 0x26 // 9
	case 0x0B:
		hidCode = 0x27 // 0
	case 0x0C:
		hidCode = 0x2D // -
	case 0x0D:
		hidCode = 0x2E // =
	case 0x0E:
		hidCode = 0x2A // Backspace
	case 0x0F:
		hidCode = 0x2B // Tab
	case 0x10:
		hidCode = 0x14 // Q
	case 0x11:
		hidCode = 0x1A // W
	case 0x12:
		hidCode = 0x08 // E
	case 0x13:
		hidCode = 0x15 // R
	case 0x14:
		hidCode = 0x17 // T
	case 0x15:
		hidCode = 0x1C // Y
	case 0x16:
		hidCode = 0x18 // U
	case 0x17:
		hidCode = 0x0C // I
	case 0x18:
		hidCode = 0x12 // O
	case 0x19:
		hidCode = 0x13 // P
	case 0x1A:
		hidCode = 0x2F // [
	case 0x1B:
		hidCode = 0x30 // ]
	case 0x1C:
		if extended {
			hidCode = 0x58 // Numpad Enter
		} else {
			hidCode = 0x28 // Enter
		}
	case 0x1D:
		if extended {
			hidCode = 0xE4 // Right Control
		} else {
			hidCode = 0xE0 // Left Control
		}
	case 0x1E:
		hidCode = 0x04 // A
	case 0x1F:
		hidCode = 0x16 // S
	case 0x20:
		hidCode = 0x07 // D
	case 0x21:
		hidCode = 0x09 // F
	case 0x22:
		hidCode = 0x0A // G
	case 0x23:
		hidCode = 0x0B // H
	case 0x24:
		hidCode = 0x0D // J
	case 0x25:
		hidCode = 0x0E // K
	case 0x26:
		hidCode = 0x0F // L
	case 0x27:
		hidCode = 0x33 // ;
	case 0x28:
		hidCode = 0x34 // '
	case 0x29:
		hidCode = 0x35 // `
	case 0x2A:
		hidCode = 0xE1 // Left Shift
	case 0x2B:
		hidCode = 0x31 // backslash
	case 0x2C:
		hidCode = 0x1D // Z
	case 0x2D:
		hidCode = 0x1B // X
	case 0x2E:
		hidCode = 0x06 // C
	case 0x2F:
		hidCode = 0x19 // V
	case 0x30:
		hidCode = 0x05 // B
	case 0x31:
		hidCode = 0x11 // N
	case 0x32:
		hidCode = 0x10 // M
	case 0x33:
		hidCode = 0x36 // ,
	case 0x34:
		hidCode = 0x37 // .
	case 0x35:
		if extended {
			hidCode = 0x54 // Numpad /
		} else {
			hidCode = 0x38 // /
		}
	case 0x36:
		hidCode = 0xE5 // Right Shift
	case 0x37:
		hidCode = 0x55 // Numpad *
	case 0x38:
		if extended {
			hidCode = 0xE6 // Right Alt
		} else {
			hidCode = 0xE2 // Left Alt
		}
	case 0x39:
		hidCode = 0x2C // Space
	case 0x3A:
		hidCode = 0x39 // Caps Lock
	case 0x3B:
		hidCode = 0x3A // F1
	case 0x3C:
		hidCode = 0x3B // F2
	case 0x3D:
		hidCode = 0x3C // F3
	case 0x3E:
		hidCode = 0x3D // F4
	case 0x3F:
		hidCode = 0x3E // F5
	case 0x40:
		hidCode = 0x3F // F6
	case 0x41:
		hidCode = 0x40 // F7
	case 0x42:
		hidCode = 0x41 // F8
	case 0x43:
		hidCode = 0x42 // F9
	case 0x44:
		hidCode = 0x43 // F10
	case 0x45:
		hidCode = 0x53 // Num Lock
	case 0x46:
		hidCode = 0x47 // Scroll Lock
	case 0x47:
		if extended {
			hidCode = 0x4A // Home
		} else {
			hidCode = 0x5F // Numpad 7
		}
	case 0x48:
		if extended {
			hidCode = 0x52 // Up Arrow
		} else {
			hidCode = 0x60 // Numpad 8
		}
	case 0x49:
		if extended {
			hidCode = 0x4B // Page Up
		} else {
			hidCode = 0x61 // Numpad 9
		}
	case 0x4A:
		hidCode = 0x56 // Numpad -
	case 0x4B:
		if extended {
			hidCode = 0x50 // Left Arrow
		} else {
			hidCode = 0x5C // Numpad 4
		}
	case 0x4C:
		hidCode = 0x5D // Numpad 5
	case 0x4D:
		if extended {
			hidCode = 0x4F // Right Arrow
		} else {
			hidCode = 0x5E // Numpad 6
		}
	case 0x4E:
		hidCode = 0x57 // Numpad +
	case 0x4F:
		if extended {
			hidCode = 0x4D // End
		} else {
			hidCode = 0x59 // Numpad 1
		}
	case 0x50:
		if extended {
			hidCode = 0x51 // Down Arrow
		} else {
			hidCode = 0x5A // Numpad 2
		}
	case 0x51:
		if extended {
			hidCode = 0x4E // Page Down
		} else {
			hidCode = 0x5B // Numpad 3
		}
	case 0x52:
		if extended {
			hidCode = 0x49 // Insert
		} else {
			hidCode = 0x62 // Numpad 0
		}
	case 0x53:
		if extended {
			hidCode = 0x4C // Delete
		} else {
			hidCode = 0x63 // Numpad .
		}
	case 0x57:
		hidCode = 0x44 // F11
	case 0x58:
		hidCode = 0x45 // F12
	case 0x5B:
		hidCode = 0xE3 // Left GUI (Windows)
	case 0x5C:
		hidCode = 0xE7 // Right GUI (Windows)
	case 0x5D:
		hidCode = 0x65 // Application (Menu)
	}

	return hidCode
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

	// Parse VC PDU header
	// totalLength := binary.LittleEndian.Uint32(data[0:4])
	// flags := binary.LittleEndian.Uint32(data[4:8])
	// For now, we don't handle chunked data (FIRST without LAST), just pass payload
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

// handleClipboard handles clipboard channel.
func (c *Connection) handleClipboard(data []byte) {
	if c.clipboardChannel == nil {
		return
	}

	if err := c.clipboardChannel.HandlePDU(data); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: cliprdr error")
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

	// Close camera channel
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

// initDynamicChannels initializes the DRDYNVC, RDPGFX, and RDPSND channels.
func (c *Connection) initDynamicChannels() error {
	// Find static channel IDs
	c.channelsMu.RLock()
	for _, ch := range c.channels {
		switch ch.Name {
		case "drdynvc":
			c.drdynvcID = ch.ID
		case "rdpsnd":
			c.rdpsndID = ch.ID
		case "cliprdr":
			c.cliprdrdID = ch.ID
		}
	}
	c.channelsMu.RUnlock()

	// NOTE: Channel initialization is deferred until AFTER DVC setup
	// to avoid interfering with DVC capability exchange.
	// RDPSND is still needed for audio output even when DVC is available.

	if c.drdynvcID == 0 {
		// Client doesn't support dynamic channels
		// Initialize RDPSND and clipboard now
		if c.rdpsndID != 0 {
			c.initSoundChannel()
		}
		if c.cliprdrdID != 0 {
			c.initClipboardChannel()
		}
		return nil
	}

	// Create DVC manager with send callback
	c.dvcManager = channels.NewDVCManager(func(data []byte) error {
		return c.sendDVCData(data)
	})

	// NOTE: Logger disabled for maximum performance - DVC is hot path

	// Set up synchronous callback for when capability response is received.
	// This runs in the message loop context, ensuring proper sequencing of create requests and responses.
	c.dvcManager.SetOnCapabilityReceived(func() {
		c.server.deps.Logger.Warn().
			Uint16("version", c.dvcManager.GetNegotiatedVersion()).
			Msg("RDP: DVC capability received (synchronous callback), creating channels")
		c.initDVCChannelsSync()
	})

	// Send capability request
	c.server.deps.Logger.Warn().
		Uint16("drdynvcID", c.drdynvcID).
		Uint16("userID", c.userID).
		Uint16("rdpsndID", c.rdpsndID).
		Msg("RDP: sending DVC capability request (both IDs for comparison)")
	if err := c.dvcManager.SendCapabilityRequest(); err != nil {
		c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to send DVC capability request")
		return err
	}
	c.server.deps.Logger.Warn().Msg("RDP: DVC capability request sent successfully")

	return nil
}

// initDVCChannelsSync creates DVC channels synchronously.
// Called from the capability response handler in the message loop context.
func (c *Connection) initDVCChannelsSync() {
	// Initialize RDPSND for audio output (static channel, not DVC)
	if c.rdpsndID != 0 {
		c.initSoundChannel()
	}

	// Initialize clipboard channel AFTER DVC capability exchange completes
	// to avoid any interference
	if c.cliprdrdID != 0 {
		c.initClipboardChannel()
	}

	// Create RDPGFX channel only if video is enabled in config
	if !c.server.deps.Config.GetRDPVideoEnabled() {
		c.server.deps.Logger.Info().Msg("RDP: H.264 video disabled in config, skipping RDPGFX channel")
	} else {
		c.gfxChannel = channels.NewGFXChannel(c.dvcManager)

		// Set callback to initialize surface when channel is ready
		c.gfxChannel.SetReadyCallback(func(g *channels.GFXChannel) {
			w, h := c.GetResolution()
			c.server.deps.Logger.Warn().
				Uint16("width", w).
				Uint16("height", h).
				Bool("avc420", g.SupportsAVC420()).
				Bool("avc444", g.SupportsAVC444()).
				Msg("RDP: RDPGFX channel ready, initializing surface")

			if err := g.Initialize(w, h); err != nil {
				c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to initialize GFX surface")
				return
			}

			// Mark RDPGFX as supported
			c.gfxSupported.Store(true)

			// Start video capture now that the channel is ready
			if c.server.deps.Video != nil {
				if err := c.server.deps.Video.StartVideo(); err != nil {
					c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to start video capture")
				} else {
					c.server.deps.Logger.Warn().Msg("RDP: video capture started (RDPGFX mode)")
				}
			}
		})

		if err := c.gfxChannel.Open(); err != nil {
			c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to open RDPGFX channel")
			// Non-fatal - we can still work without graphics channel
		}
	}

	// Create AUDIN channel for microphone input
	c.audinChannel = channels.NewAudinChannel(c.dvcManager)

	// Set logger for debugging (Warn level to ensure visibility)
	c.audinChannel.SetLogger(func(msg string, args ...interface{}) {
		c.server.deps.Logger.Warn().Msgf(msg, args...)
	})

	// Set ready callback for AUDIN
	c.audinChannel.SetReadyCallback(func(a *channels.AudinChannel) {
		fmt, ok := a.GetSelectedFormat()
		if !ok {
			return
		}

		c.server.deps.Logger.Info().
			Uint16("channels", fmt.Channels).
			Uint32("sampleRate", fmt.SamplesPerSec).
			Uint16("bitsPerSample", fmt.BitsPerSample).
			Msg("RDP: AUDIN channel ready for microphone input")

		// Automatically enable audio input when RDP client has mic enabled
		// This initializes the USB audio gadget playback without requiring manual UI toggle
		if c.server.deps.Audio != nil {
			if err := c.server.deps.Audio.EnableAudioInput(); err != nil {
				c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to enable audio input for AUDIN")
			} else {
				c.server.deps.Logger.Info().Msg("RDP: audio input enabled for AUDIN")
			}
		}
	})

	// Create AUDIN async buffer and processing goroutine
	// This prevents AUDIN data processing from blocking the DVC message loop
	// which would delay GFX frame ACKs and cause video stuttering
	c.audinDataChan = make(chan []byte, 30) // Buffer for ~300ms at 10ms packets
	c.audinStopCh = make(chan struct{})
	go c.audinDataLoop()

	// Set data callback to forward audio to buffer (non-blocking)
	c.audinChannel.SetDataCallback(func(data []byte) {
		// Make a copy since the underlying buffer may be reused by the connection
		dataCopy := make([]byte, len(data))
		copy(dataCopy, data)

		// Non-blocking send - drop if buffer is full
		select {
		case c.audinDataChan <- dataCopy:
			// Data queued successfully
		default:
			// Buffer full, drop the audio packet (acceptable for real-time audio)
		}
	})

	if err := c.audinChannel.Open(); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to open AUDIN channel")
		// Non-fatal - we can still work without audio input
	}

	// Create camera channel for webcam redirection
	c.cameraChannel = channels.NewCameraChannel(c.dvcManager)

	// Set ready callback for camera
	c.cameraChannel.SetReadyCallback(func(cam *channels.CameraChannel) {
		cameras := cam.GetCameras()
		c.server.deps.Logger.Info().
			Int("numCameras", len(cameras)).
			Msg("RDP: Camera channel ready")

		// Activate first camera if UVC gadget is connected
		if c.server.deps.Camera != nil && c.server.deps.Camera.IsConnected() {
			if err := cam.Activate(); err != nil {
				c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to activate camera")
			}
		}
	})

	// Set frame callback to forward to UVC gadget
	c.cameraChannel.SetFrameCallback(func(frame []byte, width, height, pixelFormat uint32) {
		if c.server.deps.Camera == nil {
			return
		}
		if err := c.server.deps.Camera.SendFrame(frame, width, height, pixelFormat); err != nil {
			c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to send camera frame")
		}
	})

	if err := c.cameraChannel.Open(); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to open camera channel")
		// Non-fatal - we can still work without camera
	}

	c.server.deps.Logger.Warn().Msg("RDP: dynamic virtual channels initialized")

	// Start a goroutine to check if RDPGFX becomes ready, otherwise fall back to bitmap mode
	go c.checkGFXReadinessAndFallback()
}

// checkGFXReadinessAndFallback waits for RDPGFX to become ready.
// If it doesn't become ready within timeout, falls back to bitmap updates.
func (c *Connection) checkGFXReadinessAndFallback() {
	// Wait up to 3 seconds for RDPGFX to become ready
	timeout := time.After(3 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-timeout:
			// RDPGFX didn't become ready - fall back to bitmap mode
			if !c.gfxSupported.Load() {
				c.server.deps.Logger.Warn().Msg("RDP: RDPGFX not supported, falling back to bitmap updates")
				if c.server.deps.Video != nil {
					jpegChan := c.server.deps.Video.SubscribeJPEG()
					c.startBitmapStreaming(jpegChan)
				}
			}
			return
		case <-ticker.C:
			if c.gfxSupported.Load() {
				// RDPGFX is ready, no need for fallback
				return
			}
		}
	}
}

// sendDVCData sends data on the drdynvc static channel.
// Note: The single Write() call is atomic at the TLS level,
// so concurrent calls to this function are safe.
//
// HOT PATH: Zero allocations using pooled buffers for typical packet sizes.
func (c *Connection) sendDVCData(data []byte) error {
	if c.drdynvcID == 0 {
		return nil
	}

	// Use zero-allocation hot path for typical packet sizes
	return c.sendDVCDataHotPath(data)
}

// sendDVCDataHotPath sends DVC data using zero-allocation hot path.
// Uses pooled buffers to avoid heap allocations on every packet.
// Falls back to sendDVCDataFallback for packets too large for the pool.
func (c *Connection) sendDVCDataHotPath(data []byte) error {
	if c.drdynvcID == 0 {
		return nil
	}

	// Packet layout:
	// [TPKT header 4][X.224 header 3][MCS header 6-8][VC header 8][DVC data]
	// MCS header is 6 bytes for data < 128, 8 bytes otherwise
	const (
		tpktHeaderLen    = 4
		x224HeaderLen    = 3
		mcsHeaderBaseLen = 6
		vcHeaderLen      = 8
		channelFlagFirst = 0x01
		channelFlagLast  = 0x02
	)

	vcPayloadLen := len(data)
	mcsLenFieldSize := 1
	if vcPayloadLen+vcHeaderLen >= 128 {
		mcsLenFieldSize = 2
	}
	mcsHeaderLen := mcsHeaderBaseLen + mcsLenFieldSize

	totalPacketLen := tpktHeaderLen + x224HeaderLen + mcsHeaderLen + vcHeaderLen + vcPayloadLen

	// Get buffer from pool
	bufPtr := vcPDUPool.Get().(*[]byte)
	buf := *bufPtr

	if totalPacketLen > len(buf) {
		// Rare path: packet too large for pool
		vcPDUPool.Put(bufPtr)
		return c.sendDVCDataFallback(data)
	}
	defer vcPDUPool.Put(bufPtr)

	packet := buf[:totalPacketLen]
	pos := 0

	// TPKT header (4 bytes, big-endian length)
	packet[pos] = protocol.TPKTVersion
	packet[pos+1] = 0
	binary.BigEndian.PutUint16(packet[pos+2:pos+4], uint16(totalPacketLen))
	pos += tpktHeaderLen

	// X.224 Data TPDU header (3 bytes)
	packet[pos] = 2                    // LI
	packet[pos+1] = protocol.X224Data  // Code
	packet[pos+2] = protocol.X224DataEOT // EOT
	pos += x224HeaderLen

	// MCS Send Data Indication header
	packet[pos] = byte(protocol.MCSSendDataIndication << 2)
	relativeUserID := c.userID - protocol.MCSUserIDBase
	binary.BigEndian.PutUint16(packet[pos+1:pos+3], relativeUserID)
	binary.BigEndian.PutUint16(packet[pos+3:pos+5], c.drdynvcID)
	packet[pos+5] = 0x70 // High priority, begin+end segment
	pos += 6

	// MCS length field (PER encoded)
	mcsDataLen := vcHeaderLen + vcPayloadLen
	if mcsDataLen < 128 {
		packet[pos] = byte(mcsDataLen)
		pos++
	} else {
		packet[pos] = byte(0x80 | (mcsDataLen >> 8))
		packet[pos+1] = byte(mcsDataLen)
		pos += 2
	}

	// VC PDU header (8 bytes, little-endian)
	binary.LittleEndian.PutUint32(packet[pos:pos+4], uint32(vcPayloadLen))
	binary.LittleEndian.PutUint32(packet[pos+4:pos+8], channelFlagFirst|channelFlagLast)
	pos += vcHeaderLen

	// DVC data payload
	copy(packet[pos:], data)

	// Single write to connection
	_, err := c.conn.Write(packet)
	return err
}

// sendDVCDataFallback handles oversized packets that don't fit in the pool.
func (c *Connection) sendDVCDataFallback(data []byte) error {
	const (
		channelFlagFirst = 0x01
		channelFlagLast  = 0x02
		vcHeaderSize     = 8
	)

	// Debug: Log what we're sending

	vcPDU := make([]byte, vcHeaderSize+len(data))
	binary.LittleEndian.PutUint32(vcPDU[0:4], uint32(len(data)))
	binary.LittleEndian.PutUint32(vcPDU[4:8], channelFlagFirst|channelFlagLast)
	copy(vcPDU[8:], data)

	mcsPDU := protocol.BuildSendDataIndication(c.userID, c.drdynvcID, vcPDU)
	return protocol.WriteMCSPDU(c.conn, mcsPDU)
}

// SendFrame sends an H.264 video frame to the client.
// The frame should contain raw H.264 NAL units.
func (c *Connection) SendFrame(frame []byte) {
	// Don't send to closed connections
	if c.closed.Load() {
		return
	}

	if c.gfxChannel == nil {
		c.frameRequested.Store(false)
		return
	}

	if !c.gfxChannel.IsReady() {
		c.frameRequested.Store(false)
		return
	}

	// Detect keyframe by checking NAL unit type
	isKeyframe := isH264Keyframe(frame)

	// Don't send non-keyframes until we've sent a keyframe first
	// The H.264 decoder needs SPS/PPS (which come with keyframes) before it can decode
	if !isKeyframe && !c.hasReceivedKeyframe.Load() {
		c.frameRequested.Store(false)
		return
	}

	// Track that we've sent a keyframe
	if isKeyframe {
		c.hasReceivedKeyframe.Store(true)
	}

	// Send via RDPGFX with zero-copy
	if err := c.gfxChannel.SendH264Frame(frame, isKeyframe); err != nil {
		if err == channels.ErrGFXBackpressure {
			// Too many frames pending - skip this one
			// CRITICAL: When ANY frame is dropped (keyframe OR P-frame), we must
			// wait for the next keyframe. Dropping a P-frame breaks the reference
			// chain, causing subsequent P-frames to decode as green/corrupted.
			c.hasReceivedKeyframe.Store(false)
			c.server.deps.Logger.Debug().
				Bool("keyframe", isKeyframe).
				Msg("RDP: frame dropped due to backpressure, waiting for next keyframe")
		} else if err == channels.ErrGFXNoCodec {
			// Client doesn't support AVC420/AVC444 - log once and stop sending frames
			// This is not a transient error, so don't count against write errors
			if c.hasReceivedKeyframe.CompareAndSwap(true, false) {
				c.server.deps.Logger.Warn().Msg("RDP: client does not support H.264 (AVC420/AVC444), video disabled")
			}
		} else {
			c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to send frame")

			// Track consecutive write errors
			errCount := c.consecutiveWriteErrors.Add(1)
			if errCount >= 5 {
				// Too many consecutive write errors - connection is unhealthy
				c.server.deps.Logger.Warn().
					Int32("errorCount", errCount).
					Msg("RDP: closing connection due to consecutive write errors")
				go c.Close() // Close asynchronously to avoid blocking
			}
		}
	} else {
		// Reset error counter on successful send
		c.consecutiveWriteErrors.Store(0)

		// Check if connection appears stale (no acks received for a while)
		if c.gfxChannel.IsConnectionStale() {
			c.server.deps.Logger.Warn().
				Int("pendingFrames", c.gfxChannel.GetPendingFrames()).
				Msg("RDP: closing stale connection (no frame acks received)")
			go c.Close()
		}
	}

	c.frameRequested.Store(false)
}

// isH264Keyframe checks if the frame contains an IDR (keyframe).
// This is a fast check that looks for NAL unit type 5 (IDR) or 7 (SPS).
func isH264Keyframe(data []byte) bool {
	if len(data) < 5 {
		return false
	}

	// Look for start codes and check NAL type
	for i := 0; i < len(data)-4; i++ {
		// Check for 3-byte or 4-byte start code
		if data[i] == 0 && data[i+1] == 0 {
			var nalType byte
			if data[i+2] == 1 {
				// 3-byte start code
				nalType = data[i+3] & 0x1F
			} else if data[i+2] == 0 && data[i+3] == 1 && i+4 < len(data) {
				// 4-byte start code
				nalType = data[i+4] & 0x1F
			} else {
				continue
			}

			// NAL type 5 = IDR, NAL type 7 = SPS (precedes keyframe)
			if nalType == 5 || nalType == 7 {
				return true
			}
		}
	}

	return false
}

// initSoundChannel initializes the RDPSND static channel.
func (c *Connection) initSoundChannel() {
	// Create sound channel with send callback
	// RDPSND requires Virtual Channel PDU header per MS-RDPBCGR 2.2.6.1
	c.soundChannel = channels.NewSoundChannel(func(data []byte) error {
		// Build Virtual Channel PDU: header (8 bytes) + data
		const (
			channelFlagFirst = 0x01
			channelFlagLast  = 0x02
		)

		// Debug: Log what we're sending (only for first packet which is SNDC_FORMATS)
		if len(data) > 0 && data[0] == 0x07 { // SNDCFormats = 0x07
			c.server.deps.Logger.Warn().
				Uint16("rdpsndID", c.rdpsndID).
				Uint16("userID", c.userID).
				Int("dataLen", len(data)).
				Hex("firstBytes", data[:min(len(data), 16)]).
				Msg("RDP: initSoundChannel - sending SNDC_FORMATS")
		}

		vcPDU := make([]byte, 8+len(data))
		binary.LittleEndian.PutUint32(vcPDU[0:4], uint32(len(data)))                // totalLength
		binary.LittleEndian.PutUint32(vcPDU[4:8], channelFlagFirst|channelFlagLast) // flags
		copy(vcPDU[8:], data)

		mcsPDU := protocol.BuildSendDataIndication(c.userID, c.rdpsndID, vcPDU)
		return protocol.WriteMCSPDU(c.conn, mcsPDU)
	})

	// Set ready callback to start audio streaming
	c.soundChannel.SetReadyCallback(func(s *channels.SoundChannel) {
		fmt, ok := s.GetSelectedFormat()
		if !ok {
			return
		}

		c.server.deps.Logger.Info().
			Uint16("channels", fmt.Channels).
			Uint32("sampleRate", fmt.SamplesPerSec).
			Uint16("bitsPerSample", fmt.BitsPerSample).
			Msg("RDP: RDPSND channel ready, starting audio stream")

		// Signal audio system that RDP needs audio
		if c.server.deps.Audio != nil {
			c.server.deps.Audio.Connect()
		}

		// Start audio streaming goroutine
		c.startAudioStream()
	})

	// Start format negotiation
	if err := c.soundChannel.Start(); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to start rdpsnd")
	}
}

// initClipboardChannel initializes the CLIPRDR static channel.
func (c *Connection) initClipboardChannel() {
	// Create clipboard channel with send callback
	// CLIPRDR requires Virtual Channel PDU header per MS-RDPBCGR 2.2.6.1
	c.clipboardChannel = channels.NewClipboardChannel(func(data []byte) error {
		return c.sendClipboardData(data)
	})

	// NOTE: Logger disabled for maximum performance

	// Start clipboard channel (sends Capabilities and Monitor Ready)
	if err := c.clipboardChannel.Start(); err != nil {
		c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to start cliprdr")
	} else {
		c.server.deps.Logger.Warn().Msg("RDP: clipboard channel initialized")
	}
}

// sendClipboardData sends data on the cliprdr channel with proper VC PDU header.
// Per MS-RDPBCGR 2.2.6.1, virtual channel data must include the VC PDU header.
func (c *Connection) sendClipboardData(data []byte) error {
	if c.cliprdrdID == 0 {
		return nil
	}

	// Build Virtual Channel PDU per MS-RDPBCGR 2.2.6.1
	// Header: totalLength (4 bytes LE) + flags (4 bytes LE) + data
	const (
		channelFlagFirst = 0x01
		channelFlagLast  = 0x02
	)

	vcPDU := make([]byte, 8+len(data))
	binary.LittleEndian.PutUint32(vcPDU[0:4], uint32(len(data)))                // totalLength
	binary.LittleEndian.PutUint32(vcPDU[4:8], channelFlagFirst|channelFlagLast) // flags (single chunk)
	copy(vcPDU[8:], data)

	c.server.deps.Logger.Warn().
		Int("dataLen", len(data)).
		Str("dataHex", fmt.Sprintf("% X", data[:min(len(data), 20)])).
		Msg("RDP: sending clipboard data")

	// Wrap in MCS Send Data Indication
	mcsPDU := protocol.BuildSendDataIndication(c.userID, c.cliprdrdID, vcPDU)
	return protocol.WriteMCSPDU(c.conn, mcsPDU)
}

// startAudioStream starts the audio streaming goroutine.
func (c *Connection) startAudioStream() {
	if c.server.deps.Audio == nil {
		c.server.deps.Logger.Debug().Msg("RDP: audio provider not available")
		return
	}

	// Subscribe to audio
	c.audioChan = c.server.deps.Audio.SubscribeAudio()
	if c.audioChan == nil {
		c.server.deps.Logger.Debug().Msg("RDP: failed to subscribe to audio")
		return
	}

	c.audioStopCh = make(chan struct{})

	go c.audioStreamLoop()
}

// audioStreamLoop reads audio from the provider and sends to the client.
func (c *Connection) audioStreamLoop() {
	defer func() {
		if c.server.deps.Audio != nil {
			c.server.deps.Audio.UnsubscribeAudio()
		}
	}()

	for {
		select {
		case <-c.stopChan:
			return
		case <-c.audioStopCh:
			return
		case audioData, ok := <-c.audioChan:
			if !ok {
				return
			}

			if c.soundChannel == nil || !c.soundChannel.IsReady() {
				continue
			}

			// Send audio in chunks
			if err := c.soundChannel.SendAudioChunked(audioData); err != nil {
				if err == channels.ErrSoundBackpressure {
					// Too many blocks pending - skip this one
					c.server.deps.Logger.Debug().Msg("RDP: audio dropped due to backpressure")
				} else if err != channels.ErrSoundNotReady {
					c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to send audio")
				}
			}
		}
	}
}

// stopAudioStream stops the audio streaming.
func (c *Connection) stopAudioStream() {
	if c.audioStopCh != nil {
		close(c.audioStopCh)
		c.audioStopCh = nil
	}
}

// audinDataLoop processes AUDIN audio data asynchronously.
// This runs in its own goroutine to prevent blocking the DVC message loop.
func (c *Connection) audinDataLoop() {
	for {
		select {
		case <-c.audinStopCh:
			return
		case data, ok := <-c.audinDataChan:
			if !ok {
				return
			}
			if c.server.deps.Audio == nil {
				continue
			}
			if err := c.server.deps.Audio.PlayAudio(data); err != nil {
				c.server.deps.Logger.Debug().Err(err).Int("len", len(data)).Msg("RDP: failed to play AUDIN audio")
			}
		}
	}
}

// SendBitmapUpdate sends a bitmap update PDU to the client.
// This is used for clients that don't support RDPGFX (like Jump Desktop).
// The frame should be JPEG data which will be decoded and sent as RGB bitmap.
//
// Tile size for bitmap updates is limited by MCS PDU encoding:
// Tile size for Fast-Path bitmap updates with fragmentation support.
// With fragmentation enabled (via Multifragment Update Capability), we can use
// larger tiles that get split across multiple PDUs automatically.
// Using 256x256 tiles for better visual experience and reduced overhead.
// Each tile: 256x256 at 32bpp = 262144 bytes (fragmented into ~16KB chunks)
const bitmapTileSize = 256

// bgrxBufferPool reduces allocations for YUV→BGRX conversion.
// Max size: 1920x1080x4 = ~8MB (handles 1080p)
var bgrxBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 1920*1080*4)
		return &buf
	},
}

// convertYUV422ToBGRX converts YUV422 YUYV format to BGRX.
// Input: YUYV packed (2 pixels in 4 bytes: Y0 U0 Y1 V0)
// Output: BGRX (4 bytes per pixel: B G R X)
// This is optimized for speed with integer arithmetic (no floats).
func convertYUV422ToBGRX(yuv []byte, width, height int) []byte {
	// Get pooled buffer
	bufPtr := bgrxBufferPool.Get().(*[]byte)
	bgrx := *bufPtr

	// Ensure buffer is large enough
	bgrxSize := width * height * 4
	if len(bgrx) < bgrxSize {
		bgrx = make([]byte, bgrxSize)
		*bufPtr = bgrx
	}

	// YUV422 YUYV: 2 bytes per pixel average (4 bytes for 2 pixels)
	// Process 2 pixels at a time
	yuvStride := width * 2
	bgrxStride := width * 4

	for row := 0; row < height; row++ {
		yuvOffset := row * yuvStride
		bgrxOffset := row * bgrxStride

		for col := 0; col < width; col += 2 {
			// Extract YUYV components (4 bytes = 2 pixels)
			y0 := int(yuv[yuvOffset])
			u := int(yuv[yuvOffset+1])
			y1 := int(yuv[yuvOffset+2])
			v := int(yuv[yuvOffset+3])
			yuvOffset += 4

			// Precompute UV terms (integer approximation of BT.601)
			// R = Y + 1.402*(V-128) ≈ Y + (359*(V-128))>>8
			// G = Y - 0.344*(U-128) - 0.714*(V-128) ≈ Y - (88*(U-128) + 183*(V-128))>>8
			// B = Y + 1.772*(U-128) ≈ Y + (454*(U-128))>>8
			u128 := u - 128
			v128 := v - 128

			rComp := (359 * v128) >> 8
			gComp := (88*u128 + 183*v128) >> 8
			bComp := (454 * u128) >> 8

			// Pixel 0
			r0 := y0 + rComp
			g0 := y0 - gComp
			b0 := y0 + bComp

			// Clamp to [0, 255]
			if r0 < 0 {
				r0 = 0
			} else if r0 > 255 {
				r0 = 255
			}
			if g0 < 0 {
				g0 = 0
			} else if g0 > 255 {
				g0 = 255
			}
			if b0 < 0 {
				b0 = 0
			} else if b0 > 255 {
				b0 = 255
			}

			// BGRX format
			bgrx[bgrxOffset] = byte(b0)
			bgrx[bgrxOffset+1] = byte(g0)
			bgrx[bgrxOffset+2] = byte(r0)
			bgrx[bgrxOffset+3] = 0xFF
			bgrxOffset += 4

			// Pixel 1
			r1 := y1 + rComp
			g1 := y1 - gComp
			b1 := y1 + bComp

			// Clamp to [0, 255]
			if r1 < 0 {
				r1 = 0
			} else if r1 > 255 {
				r1 = 255
			}
			if g1 < 0 {
				g1 = 0
			} else if g1 > 255 {
				g1 = 255
			}
			if b1 < 0 {
				b1 = 0
			} else if b1 > 255 {
				b1 = 255
			}

			bgrx[bgrxOffset] = byte(b1)
			bgrx[bgrxOffset+1] = byte(g1)
			bgrx[bgrxOffset+2] = byte(r1)
			bgrx[bgrxOffset+3] = 0xFF
			bgrxOffset += 4
		}
	}

	return bgrx[:bgrxSize]
}

// releaseBGRXBuffer returns a BGRX buffer to the pool.
// Must be called after the buffer is no longer needed.
func releaseBGRXBuffer(buf []byte) {
	bgrxBufferPool.Put(&buf)
}

func (c *Connection) SendBitmapUpdate(jpegData []byte) error {
	if c.closed.Load() {
		return nil
	}

	// Decode JPEG
	img, err := jpeg.Decode(bytes.NewReader(jpegData))
	if err != nil {
		return fmt.Errorf("failed to decode JPEG: %w", err)
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Send bitmap update using tiles - optimized path for YCbCr (common JPEG format)
	return c.sendTiledBitmapUpdateFast(img, width, height)
}

// tileRect represents a single bitmap tile for RDP updates.
type tileRect struct {
	left, top     int
	right, bottom int
	data          []byte
}

// sendTiledBitmapUpdateFast is an optimized version that:
// - Uses buffer pooling to reduce GC pressure
// - Sends tiles immediately without building a full list
// - Optimizes YCbCr→BGRX conversion with direct pixel access
func (c *Connection) sendTiledBitmapUpdateFast(img image.Image, width, height int) error {
	// Calculate tile grid
	tilesX := (width + bitmapTileSize - 1) / bitmapTileSize
	tilesY := (height + bitmapTileSize - 1) / bitmapTileSize

	// Get pooled buffer for tile data (reused across tiles)
	bufPtr := tileBufferPool.Get().(*[]byte)
	tileBuffer := *bufPtr
	defer tileBufferPool.Put(bufPtr)

	// Try to get direct pixel access based on image type
	var ycbcr *image.YCbCr
	var rgba *image.RGBA
	var nrgba *image.NRGBA

	switch v := img.(type) {
	case *image.YCbCr:
		ycbcr = v
	case *image.RGBA:
		rgba = v
	case *image.NRGBA:
		nrgba = v
	}

	// Process and send each tile immediately
	var tile tileRect
	for ty := 0; ty < tilesY; ty++ {
		for tx := 0; tx < tilesX; tx++ {
			// Calculate tile bounds
			left := tx * bitmapTileSize
			top := ty * bitmapTileSize
			right := left + bitmapTileSize - 1
			bottom := top + bitmapTileSize - 1

			// Clamp to image bounds
			if right >= width {
				right = width - 1
			}
			if bottom >= height {
				bottom = height - 1
			}

			tileW := right - left + 1
			tileH := bottom - top + 1
			dataSize := tileW * tileH * 4

			// Use appropriate conversion based on image type
			if ycbcr != nil {
				convertYCbCrTileToBGRX(ycbcr, left, top, tileW, tileH, tileBuffer)
			} else if rgba != nil {
				convertRGBATileToBGRX(rgba, left, top, tileW, tileH, tileBuffer)
			} else if nrgba != nil {
				convertNRGBATileToBGRX(nrgba, left, top, tileW, tileH, tileBuffer)
			} else {
				// Fallback to generic (slower) conversion
				convertGenericTileToBGRX(img, left, top, tileW, tileH, tileBuffer)
			}

			// Setup tile for sending
			tile.left = left
			tile.top = top
			tile.right = right
			tile.bottom = bottom
			tile.data = tileBuffer[:dataSize]

			// Send this tile via Fast-Path (supports fragmentation for large tiles)
			if err := c.sendFastPathBitmapUpdate([]tileRect{tile}); err != nil {
				return err
			}
		}
	}

	return nil
}

// convertYCbCrTileToBGRX converts a tile from YCbCr to BGRX format (bottom-up scanlines).
// This is the fast path for JPEG images which are typically YCbCr.
func convertYCbCrTileToBGRX(img *image.YCbCr, left, top, tileW, tileH int, dst []byte) {
	for y := 0; y < tileH; y++ {
		srcY := top + y
		dstY := tileH - 1 - y // RDP expects bottom-up
		dstRowOffset := dstY * tileW * 4

		for x := 0; x < tileW; x++ {
			srcX := left + x
			r, g, b := color.YCbCrToRGB(
				img.Y[img.YOffset(srcX, srcY)],
				img.Cb[img.COffset(srcX, srcY)],
				img.Cr[img.COffset(srcX, srcY)],
			)
			offset := dstRowOffset + x*4
			dst[offset+0] = b
			dst[offset+1] = g
			dst[offset+2] = r
			dst[offset+3] = 0
		}
	}
}

// convertRGBATileToBGRX converts a tile from RGBA to BGRX format (bottom-up scanlines).
func convertRGBATileToBGRX(img *image.RGBA, left, top, tileW, tileH int, dst []byte) {
	stride := img.Stride
	pix := img.Pix
	minX := img.Rect.Min.X
	minY := img.Rect.Min.Y

	for y := 0; y < tileH; y++ {
		srcY := top + y - minY
		dstY := tileH - 1 - y
		srcRowOffset := srcY * stride
		dstRowOffset := dstY * tileW * 4

		for x := 0; x < tileW; x++ {
			srcX := left + x - minX
			srcOffset := srcRowOffset + srcX*4
			dstOffset := dstRowOffset + x*4

			dst[dstOffset+0] = pix[srcOffset+2] // B
			dst[dstOffset+1] = pix[srcOffset+1] // G
			dst[dstOffset+2] = pix[srcOffset+0] // R
			dst[dstOffset+3] = 0                // X
		}
	}
}

// convertNRGBATileToBGRX converts a tile from NRGBA to BGRX format (bottom-up scanlines).
func convertNRGBATileToBGRX(img *image.NRGBA, left, top, tileW, tileH int, dst []byte) {
	stride := img.Stride
	pix := img.Pix
	minX := img.Rect.Min.X
	minY := img.Rect.Min.Y

	for y := 0; y < tileH; y++ {
		srcY := top + y - minY
		dstY := tileH - 1 - y
		srcRowOffset := srcY * stride
		dstRowOffset := dstY * tileW * 4

		for x := 0; x < tileW; x++ {
			srcX := left + x - minX
			srcOffset := srcRowOffset + srcX*4
			dstOffset := dstRowOffset + x*4

			dst[dstOffset+0] = pix[srcOffset+2] // B
			dst[dstOffset+1] = pix[srcOffset+1] // G
			dst[dstOffset+2] = pix[srcOffset+0] // R
			dst[dstOffset+3] = 0                // X
		}
	}
}

// convertGenericTileToBGRX is the fallback for any image type.
// It uses the image.Image interface which is slower but always works.
func convertGenericTileToBGRX(img image.Image, left, top, tileW, tileH int, dst []byte) {
	for y := 0; y < tileH; y++ {
		srcY := top + y
		dstY := tileH - 1 - y
		dstRowOffset := dstY * tileW * 4

		for x := 0; x < tileW; x++ {
			r, g, b, _ := img.At(left+x, srcY).RGBA()
			offset := dstRowOffset + x*4
			dst[offset+0] = byte(b >> 8)
			dst[offset+1] = byte(g >> 8)
			dst[offset+2] = byte(r >> 8)
			dst[offset+3] = 0
		}
	}
}

// sendFastPathBitmapUpdate sends a bitmap update via Fast-Path with fragmentation support.
// Per MS-RDPBCGR 2.2.9.1.2 and 2.2.9.1.2.1.2 (TS_FP_UPDATE_BITMAP).
// This bypasses the MCS 16KB limit by using fragmentation for large updates.
func (c *Connection) sendFastPathBitmapUpdate(tiles []tileRect) error {
	// Build the bitmap update data (same format as slow-path)
	// TS_UPDATE_BITMAP_DATA: updateType(2) + numberRectangles(2) + rectangles
	totalDataLen := 4 // updateType + numberRectangles
	for _, tile := range tiles {
		totalDataLen += 18 + len(tile.data) // TS_BITMAP_DATA header + pixel data
	}

	bitmapData := make([]byte, totalDataLen)
	binary.LittleEndian.PutUint16(bitmapData[0:2], protocol.UpdateTypeBitmap)
	binary.LittleEndian.PutUint16(bitmapData[2:4], uint16(len(tiles)))
	pos := 4

	for _, tile := range tiles {
		tileW := tile.right - tile.left + 1
		tileH := tile.bottom - tile.top + 1

		binary.LittleEndian.PutUint16(bitmapData[pos:pos+2], uint16(tile.left))
		binary.LittleEndian.PutUint16(bitmapData[pos+2:pos+4], uint16(tile.top))
		binary.LittleEndian.PutUint16(bitmapData[pos+4:pos+6], uint16(tile.right))
		binary.LittleEndian.PutUint16(bitmapData[pos+6:pos+8], uint16(tile.bottom))
		binary.LittleEndian.PutUint16(bitmapData[pos+8:pos+10], uint16(tileW))
		binary.LittleEndian.PutUint16(bitmapData[pos+10:pos+12], uint16(tileH))
		binary.LittleEndian.PutUint16(bitmapData[pos+12:pos+14], 32)
		binary.LittleEndian.PutUint16(bitmapData[pos+14:pos+16], protocol.BitmapCompressionNone)
		binary.LittleEndian.PutUint16(bitmapData[pos+16:pos+18], uint16(len(tile.data)))
		pos += 18
		copy(bitmapData[pos:], tile.data)
		pos += len(tile.data)
	}

	// Maximum fragment size (leave room for Fast-Path headers)
	// Fast-Path PDU max = 16383, headers = ~10 bytes, so use 16000 for safety
	const maxFragmentSize = 16000

	if len(bitmapData) <= maxFragmentSize {
		// Single fragment - no fragmentation needed
		return c.sendFastPathFragment(bitmapData, protocol.FastPathFragSingle)
	}

	// Fragment the data
	offset := 0
	remaining := len(bitmapData)
	isFirst := true

	for remaining > 0 {
		chunkSize := remaining
		if chunkSize > maxFragmentSize {
			chunkSize = maxFragmentSize
		}

		var fragFlag byte
		if isFirst {
			fragFlag = protocol.FastPathFragFirst
			isFirst = false
		} else if remaining-chunkSize > 0 {
			fragFlag = protocol.FastPathFragNext
		} else {
			fragFlag = protocol.FastPathFragLast
		}

		if err := c.sendFastPathFragment(bitmapData[offset:offset+chunkSize], fragFlag); err != nil {
			return err
		}

		offset += chunkSize
		remaining -= chunkSize
	}

	return nil
}

// sendFastPathFragment sends a single Fast-Path fragment.
func (c *Connection) sendFastPathFragment(data []byte, fragFlag byte) error {
	// Fast-Path Update structure (MS-RDPBCGR 2.2.9.1.2.1):
	// - updateHeader (1 byte): updateCode(4 bits) | fragmentation(2 bits) | compression(2 bits)
	//   compression = 0: no compression, NO size field
	//   compression = 2 (0x80): no compression, HAS size field (required for fragmentation)
	// - compressionFlags (1 byte): only present if compression != 0
	// - size (2 bytes): only present if compression != 0
	// - updateData (variable)

	// Set compression = 2 (0x80) to indicate size field is present
	updateHeader := protocol.FastPathUpdateBitmap | fragFlag | 0x80

	// Fast-Path PDU structure:
	// - fpOutputHeader (1 byte): action(2 bits) | reserved(4 bits) | flags(2 bits)
	// - length1 (1 byte)
	// - length2 (1 byte, optional)
	// - fpOutputUpdates (variable)

	// Calculate total PDU length
	// Update: header(1) + compressionFlags(1) + size(2) + data
	updateLen := 1 + 1 + 2 + len(data)

	// PDU: fpOutputHeader(1) + length(1-2) + update
	pduLen := 1 + updateLen
	if pduLen >= 128 {
		pduLen++ // Need 2-byte length encoding
	}
	pduLen++ // length1 byte

	buf := make([]byte, pduLen)
	pos := 0

	// fpOutputHeader: action = 0 (Fast-Path), no encryption
	buf[pos] = protocol.FastPathActionFastPath
	pos++

	// Length encoding
	totalLen := pduLen
	if totalLen < 128 {
		buf[pos] = byte(totalLen)
		pos++
	} else {
		buf[pos] = byte(0x80 | (totalLen >> 8))
		buf[pos+1] = byte(totalLen)
		pos += 2
	}

	// Fast-Path Update header
	buf[pos] = updateHeader
	pos++

	// compressionFlags (required when compression bits != 0)
	buf[pos] = 0x00 // No compression
	pos++

	// Size field (2 bytes, little-endian) - size of the update data
	binary.LittleEndian.PutUint16(buf[pos:pos+2], uint16(len(data)))
	pos += 2

	// Update data
	copy(buf[pos:], data)

	// Write directly to connection (no TPKT/X224 wrapper for Fast-Path)
	_, err := c.conn.Write(buf)
	return err
}

// startBitmapStreaming starts streaming bitmap updates when RDPGFX is not available.
// This uses RGA hardware YUV→BGRX conversion for maximum performance.
func (c *Connection) startBitmapStreaming(jpegChan <-chan []byte) {
	c.server.deps.Logger.Warn().Msg("RDP: starting bitmap streaming with raw YUV422 frames")

	// Start video capture if not already started
	if c.server.deps.Video != nil {
		if err := c.server.deps.Video.StartVideo(); err != nil {
			c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to start video capture")
		}

		// Start raw frame encoder (outputs YUV422, Go converts to BGRX)
		if err := c.server.deps.Video.StartRGBEncoder(); err != nil {
			c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to start raw frame encoder, falling back to JPEG")
			// Fall back to JPEG if raw frames fail
			if err := c.server.deps.Video.StartJPEGEncoder(50); err != nil {
				c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to start JPEG encoder")
			} else {
				c.server.deps.Logger.Warn().Msg("RDP: using JPEG fallback for bitmap mode")
				c.startJPEGBitmapStreaming(jpegChan)
				return
			}
		} else {
			c.server.deps.Logger.Warn().Msg("RDP: raw YUV422 encoder started for bitmap mode")
		}
	}

	// Subscribe to YUV422 frames (misnamed rgbChan for historical reasons)
	rgbChan := c.server.deps.Video.SubscribeRGB()

	go func() {
		// Send frames as they arrive - no artificial rate limiting
		// Native code produces frames at up to 60fps
		frameCount := 0
		startTime := time.Now()

		for {
			select {
			case <-c.stopChan:
				elapsed := time.Since(startTime).Seconds()
				fps := float64(frameCount) / elapsed
				c.server.deps.Logger.Debug().Int("framesSent", frameCount).Float64("avgFps", fps).Msg("RDP: RGB bitmap streaming stopped")
				return
			case frame := <-rgbChan:
				if c.closed.Load() {
					continue
				}

				if frameCount == 0 {
					c.server.deps.Logger.Info().
						Int("frameSize", len(frame.Data)).
						Uint32("width", frame.Width).
						Uint32("height", frame.Height).
						Msg("RDP: first YUV422 frame received")
				}

				// Convert YUV422 YUYV to BGRX (native outputs YUV, Go converts)
				bgrxData := convertYUV422ToBGRX(frame.Data, int(frame.Width), int(frame.Height))

				if err := c.SendRGBBitmapUpdate(bgrxData, int(frame.Width), int(frame.Height)); err != nil {
					c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to send RGB bitmap update")
				} else {
					frameCount++
					if frameCount == 1 || frameCount%100 == 0 {
						elapsed := time.Since(startTime).Seconds()
						fps := float64(frameCount) / elapsed
						c.server.deps.Logger.Debug().Int("frameCount", frameCount).Float64("avgFps", fps).Msg("RDP: RGB bitmap update sent")
					}
				}
				// Return buffer to pool (data was copied in SendRGBBitmapUpdate)
				releaseBGRXBuffer(bgrxData)
			}
		}
	}()
}

// startJPEGBitmapStreaming is the fallback for when RGA is not available.
func (c *Connection) startJPEGBitmapStreaming(jpegChan <-chan []byte) {
	go func() {
		frameCount := 0
		startTime := time.Now()

		for {
			select {
			case <-c.stopChan:
				elapsed := time.Since(startTime).Seconds()
				fps := float64(frameCount) / elapsed
				c.server.deps.Logger.Debug().Int("framesSent", frameCount).Float64("avgFps", fps).Msg("RDP: JPEG bitmap streaming stopped")
				return
			case frame := <-jpegChan:
				if c.closed.Load() {
					continue
				}

				if err := c.SendBitmapUpdate(frame); err != nil {
					c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to send JPEG bitmap update")
				} else {
					frameCount++
					if frameCount%100 == 0 {
						elapsed := time.Since(startTime).Seconds()
						fps := float64(frameCount) / elapsed
						c.server.deps.Logger.Debug().Int("frameCount", frameCount).Float64("avgFps", fps).Msg("RDP: JPEG bitmap update sent")
					}
				}
			}
		}
	}()
}

// rgbTileSize is smaller than bitmapTileSize to fit within Fast-Path reassembly limits.
// 64x64 at 32bpp = 16384 bytes per tile.
const rgbTileSize = 64

// maxTilesPerRGBUpdate is the maximum tiles per bitmap update to stay under 64KB reassembly limit.
// 64x64 tile = 16384 bytes + 18 byte header = 16402 bytes per tile
// 3 tiles = ~49KB, safely under 64KB
const maxTilesPerRGBUpdate = 3

// rgbTileBufferPool provides reusable buffers for RGB tile data.
var rgbTileBufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, rgbTileSize*rgbTileSize*4)
		return &buf
	},
}

// SendRGBBitmapUpdate sends a raw BGRX frame as RDP bitmap updates.
// This is the fast path - no JPEG decode needed, just tile and send.
// The data is expected in BGRX format (4 bytes per pixel, top-down).
func (c *Connection) SendRGBBitmapUpdate(bgrxData []byte, width, height int) error {
	if c.closed.Load() {
		return nil
	}

	// Verify data size
	expectedSize := width * height * 4
	if len(bgrxData) < expectedSize {
		return fmt.Errorf("insufficient data: expected %d bytes, got %d", expectedSize, len(bgrxData))
	}

	// Calculate tile grid using smaller tiles for Fast-Path
	tilesX := (width + rgbTileSize - 1) / rgbTileSize
	tilesY := (height + rgbTileSize - 1) / rgbTileSize

	// Get pooled buffer for tile data
	bufPtr := rgbTileBufferPool.Get().(*[]byte)
	tileBuffer := *bufPtr
	defer rgbTileBufferPool.Put(bufPtr)

	// Batch tiles to reduce PDU overhead while staying under reassembly limits
	var tiles []tileRect

	for ty := 0; ty < tilesY; ty++ {
		for tx := 0; tx < tilesX; tx++ {
			left := tx * rgbTileSize
			top := ty * rgbTileSize
			right := left + rgbTileSize - 1
			bottom := top + rgbTileSize - 1

			// Clamp to image bounds
			if right >= width {
				right = width - 1
			}
			if bottom >= height {
				bottom = height - 1
			}

			tileW := right - left + 1
			tileH := bottom - top + 1
			tileSize := tileW * tileH * 4

			// Copy tile data with vertical flip (RDP expects bottom-up scanlines)
			for y := 0; y < tileH; y++ {
				srcY := top + y
				dstY := tileH - 1 - y
				srcOffset := (srcY*width + left) * 4
				dstOffset := dstY * tileW * 4
				copy(tileBuffer[dstOffset:dstOffset+tileW*4], bgrxData[srcOffset:srcOffset+tileW*4])
			}

			// Copy tile data to new slice
			tileData := make([]byte, tileSize)
			copy(tileData, tileBuffer[:tileSize])

			tiles = append(tiles, tileRect{
				left:   left,
				top:    top,
				right:  right,
				bottom: bottom,
				data:   tileData,
			})

			// Send batch when we reach the limit
			if len(tiles) >= maxTilesPerRGBUpdate {
				if err := c.sendFastPathBitmapUpdate(tiles); err != nil {
					return err
				}
				tiles = tiles[:0] // Reset slice, keep capacity
			}
		}
	}

	// Send any remaining tiles
	if len(tiles) > 0 {
		return c.sendFastPathBitmapUpdate(tiles)
	}

	return nil
}
