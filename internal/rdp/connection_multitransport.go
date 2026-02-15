package rdp

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"

	"github.com/jetkvm/kvm/internal/rdp/channels"
	"github.com/jetkvm/kvm/internal/rdp/protocol"
	"github.com/jetkvm/kvm/internal/rdp/udp"
)

// Multitransport protocol constants (MS-RDPBCGR 2.2.15.1).
const (
	requestedProtocolUDPFECR = 0x0001 // Reliable UDP (MS-RDPEUDP)

	// S_OK HRESULT
	multitransportHRSuccess = 0x00000000
)

// DVC Soft-Sync constants (MS-RDPEDYC 2.2.3.1).
const (
	softSyncFlagTCPFlushed  = 0x0001
	softSyncFlagChannelList = 0x0002

	softSyncTunnelUDPFECR = 0x00000001
)

// initiateMultitransport sends the Initiate Multitransport Request PDU.
// Per MS-RDPBCGR 2.2.15.1, this PDU MUST be sent on the MCS Message Channel
// (not the I/O channel). The client's active-session parser on the I/O channel
// expects Share Control PDUs; a Basic Security Header there causes a disconnect.
func (c *Connection) initiateMultitransport() error {
	// Message channel required — client must have requested it in GCC
	if c.msgChannelID == 0 {
		return fmt.Errorf("client did not request MCS message channel")
	}

	// Generate random 16-byte security cookie
	if _, err := rand.Read(c.securityCookie[:]); err != nil {
		return fmt.Errorf("generate security cookie: %w", err)
	}

	// Generate request ID
	c.multitransportReqID = 1

	// Register cookie with server for UDP matching
	c.server.RegisterUDPCookie(c.securityCookie, c)

	// Build Initiate Multitransport Request PDU
	// Layout (28 bytes):
	//   [Basic Security Header: 4 bytes, flags=SEC_TRANSPORT_REQ]
	//   [requestId: 4 bytes LE]
	//   [requestedProtocol: 2 bytes LE]
	//   [reserved: 2 bytes]
	//   [securityCookie: 16 bytes]
	payload := make([]byte, 28)

	// Basic Security Header
	binary.LittleEndian.PutUint32(payload[0:4], protocol.SecTransportReq)

	// requestId
	binary.LittleEndian.PutUint32(payload[4:8], c.multitransportReqID)

	// requestedProtocol (UDPFECR = reliable)
	binary.LittleEndian.PutUint16(payload[8:10], requestedProtocolUDPFECR)

	// reserved
	binary.LittleEndian.PutUint16(payload[10:12], 0)

	// securityCookie
	copy(payload[12:28], c.securityCookie[:])

	// Send on MCS Message Channel (NOT the I/O channel)
	mcsPDU := protocol.BuildSendDataIndication(c.userID, c.msgChannelID, payload)
	if err := protocol.WriteMCSPDU(c.conn, mcsPDU); err != nil {
		c.server.UnregisterUDPCookie(c.securityCookie)
		return fmt.Errorf("write multitransport request: %w", err)
	}

	c.server.deps.Logger.Debug().
		Uint32("requestId", c.multitransportReqID).
		Msg("RDP: sent Initiate Multitransport Request")

	return nil
}

// handleMessageChannelPDU handles PDUs on the MCS Message Channel.
// Per MS-RDPBCGR, the Initiate Multitransport Response PDU is sent on this channel.
func (c *Connection) handleMessageChannelPDU(data []byte) {
	if len(data) < 8 {
		return
	}

	// Check for Basic Security Header with SEC_TRANSPORT_RSP
	flags := binary.LittleEndian.Uint16(data[0:2])
	flagsHi := binary.LittleEndian.Uint16(data[2:4])
	if flags == protocol.SecTransportRsp && flagsHi == 0 {
		c.handleMultitransportResponse(data[4:])
		return
	}

	c.server.deps.Logger.Debug().
		Hex("data", data[:min(len(data), 16)]).
		Msg("RDP: unhandled message channel PDU")
}

// handleMultitransportResponse processes the client's Multitransport Response.
// Called from handleMessageChannelPDU when SEC_TRANSPORT_RSP is detected.
func (c *Connection) handleMultitransportResponse(data []byte) {
	if len(data) < 8 {
		c.server.deps.Logger.Debug().
			Int("len", len(data)).
			Msg("RDP: multitransport response too short")
		return
	}

	requestID := binary.LittleEndian.Uint32(data[0:4])
	hrResponse := binary.LittleEndian.Uint32(data[4:8])

	if hrResponse != multitransportHRSuccess {
		c.server.deps.Logger.Info().
			Uint32("requestId", requestID).
			Str("hrResponse", fmt.Sprintf("0x%08x", hrResponse)).
			Str("hrName", multitransportHRName(hrResponse)).
			Msg("RDP: client rejected multitransport, continuing TCP-only")
		c.server.UnregisterUDPCookie(c.securityCookie)
		return
	}

	c.server.deps.Logger.Info().
		Uint32("requestId", requestID).
		Msg("RDP: client accepted multitransport, waiting for UDP establishment")
}

// onUDPTransportReady is called by the server when the UDP handshake +
// TLS + RDPEMT tunnel is fully established.
func (c *Connection) onUDPTransportReady(tunnel *udp.Tunnel) {
	// Guard against race with Close() — if the connection is already closing,
	// the TCP socket is shut down and we'd leak the tunnel.
	if c.closed.Load() {
		_ = tunnel.Close()
		return
	}

	c.udpTunnel = tunnel

	c.server.deps.Logger.Info().
		Str("remote", c.RemoteAddr()).
		Msg("RDP: UDP transport ready, sending Soft-Sync Request")

	// Send DVC Soft-Sync Request on TCP to migrate channels to UDP
	if err := c.sendSoftSyncRequest(); err != nil {
		c.server.deps.Logger.Warn().Err(err).
			Msg("RDP: failed to send Soft-Sync, continuing TCP-only")
		_ = tunnel.Close()
		c.udpTunnel = nil
		return
	}

	// Flush the Soft-Sync Request so the client receives it immediately.
	// Without this, it sits in the buffered writer until the next frame send.
	if err := c.FlushWrites(); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: flush error after Soft-Sync")
	}

	c.udpReady.Store(true)
	c.server.deps.Logger.Info().
		Str("remote", c.RemoteAddr()).
		Msg("RDP: DVC channels migrated to UDP")
}

// sendSoftSyncRequest sends a DVC Soft-Sync Request to migrate channels to UDP.
// Per MS-RDPEDYC 2.2.3.1.
func (c *Connection) sendSoftSyncRequest() error {
	if c.dvcManager == nil {
		return fmt.Errorf("DVC manager not initialized")
	}

	// Get list of open channel IDs to migrate
	channelIDs := c.dvcManager.GetOpenChannelIDs()

	// Build Soft-Sync Request PDU
	// Layout:
	//   [DVC header byte: 0x71 (SOFT_SYNC_REQUEST, cbChId=01)]
	//   [Pad: 1 byte]
	//   [Length: 2 bytes LE]
	//   [Flags: 2 bytes LE (TCP_FLUSHED | CHANNEL_LIST)]
	//   [NumberOfTunnels: 2 bytes LE = 1]
	//   [TunnelType: 4 bytes LE = UDPFECR]
	//   [NumDVCs: 2 bytes LE]
	//   [ChannelIDs: 4 bytes LE each]

	numDVCs := len(channelIDs)
	tunnelDataLen := 4 + 2 + (numDVCs * 4) // TunnelType + NumDVCs + channelIDs
	totalLen := 2 + 2 + 2 + tunnelDataLen  // Pad+Length+Flags+NumberOfTunnels + tunnel data

	buf := make([]byte, 1+totalLen)
	pos := 0

	// DVC header: Cmd=0x70 (SOFT_SYNC), Sp=0, CbChId=01
	buf[pos] = channels.DVCSoftSyncRequest | 0x01
	pos++

	// Pad
	buf[pos] = 0
	pos++

	// Length (total payload length including this field onward)
	payloadLen := uint16(totalLen - 1) // Exclude the pad byte
	binary.LittleEndian.PutUint16(buf[pos:pos+2], payloadLen)
	pos += 2

	// Flags
	flags := uint16(softSyncFlagTCPFlushed | softSyncFlagChannelList)
	binary.LittleEndian.PutUint16(buf[pos:pos+2], flags)
	pos += 2

	// NumberOfTunnels
	binary.LittleEndian.PutUint16(buf[pos:pos+2], 1)
	pos += 2

	// Tunnel entry: TunnelType
	binary.LittleEndian.PutUint32(buf[pos:pos+4], softSyncTunnelUDPFECR)
	pos += 4

	// Tunnel entry: NumDVCs
	binary.LittleEndian.PutUint16(buf[pos:pos+2], uint16(numDVCs))
	pos += 2

	// Tunnel entry: ChannelIDs
	for _, id := range channelIDs {
		binary.LittleEndian.PutUint32(buf[pos:pos+4], id)
		pos += 4
	}

	// Send via DVC (on TCP)
	return c.sendDVCData(buf[:pos])
}

// sendUDPDVCData writes DVC data through the RDPEMT tunnel.
// Uses the same MCS VC header framing as the TCP path.
func (c *Connection) sendUDPDVCData(data []byte) error {
	if c.udpTunnel == nil {
		return fmt.Errorf("UDP tunnel not established")
	}

	// Build MCS channel header + VC payload (same as TCP)
	vcPayloadLen := len(data)
	totalPacketLen := mcsChannelHeaderLen(vcPayloadLen) + vcPayloadLen

	packet := make([]byte, totalPacketLen)
	pos := c.buildMCSChannelHeader(packet, c.drdynvcID, vcPayloadLen)
	copy(packet[pos:], data)

	// Write through RDPEMT tunnel
	return c.udpTunnel.WriteData(packet)
}

// multitransportHRName returns a human-readable name for common HRESULT values
// returned in Initiate Multitransport Response PDUs.
func multitransportHRName(hr uint32) string {
	switch hr {
	case 0x00000000:
		return "S_OK"
	case 0x80004004:
		return "E_ABORT"
	case 0x80004005:
		return "E_FAIL"
	case 0x80070057:
		return "E_INVALIDARG"
	case 0x8000FFFF:
		return "E_UNEXPECTED"
	default:
		return "UNKNOWN"
	}
}
