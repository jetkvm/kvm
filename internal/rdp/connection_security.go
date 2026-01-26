package rdp

import (
	"fmt"
	"time"

	"github.com/jetkvm/kvm/internal/rdp/protocol"
)

// RDP connection security, licensing, and capability exchange handlers.
// This file contains all functions related to the RDP security and capability negotiation.

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

	if err := protocol.WriteMCSPDU(c.conn, mcsPDU); err != nil {
		return fmt.Errorf("write license error: %w", err)
	}

	c.server.deps.Logger.Debug().Msg("RDP: sent license (valid client)")

	c.phase = PhaseCapabilities
	return nil
}

// licensePDUValidClient is a pre-built License Error PDU with STATUS_VALID_CLIENT.
// This is a static PDU that never changes, so we build it once at package init.
// Layout (20 bytes total):
//   - Basic Security Header (4 bytes): SEC_LICENSE_PKT flag
//   - License PDU Preamble (4 bytes): ERROR_ALERT, PREAMBLE_VERSION_2_0
//   - License Error Message (12 bytes): STATUS_VALID_CLIENT, ST_NO_TRANSITION, empty blob
var licensePDUValidClient = []byte{
	// Basic Security Header (MS-RDPBCGR 2.2.8.1.1.2)
	0x80, 0x00, 0x00, 0x00, // flags = SEC_LICENSE_PKT (0x0080)
	// License PDU Preamble (MS-RDPELE 2.2.1.1)
	0xFF, 0x02, 0x10, 0x00, // ERROR_ALERT, VERSION_2_0, wMsgSize=16
	// License Error Message (MS-RDPELE 2.2.2.7.1)
	0x07, 0x00, 0x00, 0x00, // dwErrorCode = STATUS_VALID_CLIENT
	0x02, 0x00, 0x00, 0x00, // dwStateTransition = ST_NO_TRANSITION
	0x00, 0x00, 0x00, 0x00, // bbErrorInfo = BB_ANY_BLOB, wBlobLen=0
}

// buildLicenseErrorPDU returns the pre-built License Error PDU.
// This is safe to return directly as the caller only reads it.
func buildLicenseErrorPDU() []byte {
	return licensePDUValidClient
}

// handleCapabilities handles capability exchange.
func (c *Connection) handleCapabilities() error {
	c.server.deps.Logger.Debug().Str("remote", c.RemoteAddr()).
		Msg("RDP: exchanging capabilities")

	// Send Demand Active PDU
	demandActive := c.buildDemandActivePDU()

	// Wrap in MCS Send Data Indication
	mcsPDU := protocol.BuildSendDataIndication(c.userID, c.ioChannel, demandActive)

	if err := protocol.WriteMCSPDU(c.conn, mcsPDU); err != nil {
		return fmt.Errorf("write demand active: %w", err)
	}

	c.server.deps.Logger.Debug().Msg("RDP: sent demand active")

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
