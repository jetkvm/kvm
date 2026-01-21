package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MCS implements the T.125 Multipoint Communication Service protocol.
// This is the channel multiplexing layer for RDP.

var (
	ErrMCSInvalidPDU    = errors.New("mcs: invalid PDU")
	ErrMCSInvalidTag    = errors.New("mcs: invalid BER tag")
	ErrMCSChannelFull   = errors.New("mcs: maximum channels reached")
	ErrMCSInvalidResult = errors.New("mcs: invalid result code")
)

// DomainParameters contains MCS domain parameters.
type DomainParameters struct {
	MaxChannelIDs   int
	MaxUserIDs      int
	MaxTokenIDs     int
	NumPriorities   int
	MinThroughput   int
	MaxHeight       int
	MaxMCSPDUSize   int
	ProtocolVersion int
}

// DefaultDomainParameters returns typical domain parameters for an RDP server.
// Values are conservative to maximize client compatibility.
func DefaultDomainParameters() DomainParameters {
	return DomainParameters{
		MaxChannelIDs:   34, // Matches typical Windows Server values
		MaxUserIDs:      2,  // Single user
		MaxTokenIDs:     0,  // Not using tokens
		NumPriorities:   1,
		MinThroughput:   0,
		MaxHeight:       1,
		MaxMCSPDUSize:   65535, // Maximum PDU size
		ProtocolVersion: 2,
	}
}

// ConnectInitial represents an MCS Connect-Initial PDU.
type ConnectInitial struct {
	CallingDomain []byte
	CalledDomain  []byte
	UpwardFlag    bool
	TargetParams  DomainParameters
	MinParams     DomainParameters
	MaxParams     DomainParameters
	UserData      []byte // Contains GCC Conference Create Request
}

// ConnectResponse represents an MCS Connect-Response PDU.
type ConnectResponse struct {
	Result          int
	CalledConnectID int
	DomainParams    DomainParameters
	UserData        []byte // Contains GCC Conference Create Response
}

// ParseConnectInitial parses an MCS Connect-Initial PDU.
func ParseConnectInitial(data []byte) (*ConnectInitial, error) {
	r := NewBERReader(data)

	// Read application tag 101 (Connect-Initial)
	// In BER, application tags > 30 use long-form encoding:
	// First byte: 0x7F (class=application|constructed + tag=31 meaning "long form")
	// Second byte: actual tag number (101 = 0x65)
	tag, err := r.ReadByte()
	if err != nil {
		return nil, err
	}

	// Check for long-form application tag (0x7F means tag number follows)
	if tag == 0x7F {
		// Read the actual tag number
		tagNum, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if tagNum != 101 {
			return nil, fmt.Errorf("%w: expected tag 101, got %d", ErrMCSInvalidTag, tagNum)
		}
	} else if tag != (BERClassApplication | BERConstructed | 31) {
		return nil, fmt.Errorf("%w: expected Connect-Initial tag 0x7F, got 0x%02X", ErrMCSInvalidTag, tag)
	}

	// Read length
	if _, err := r.ReadLength(); err != nil {
		return nil, err
	}

	ci := &ConnectInitial{}

	// Calling Domain Selector (OCTET STRING)
	ci.CallingDomain, err = r.ReadOctetString()
	if err != nil {
		return nil, fmt.Errorf("mcs: read calling domain: %w", err)
	}

	// Called Domain Selector (OCTET STRING)
	ci.CalledDomain, err = r.ReadOctetString()
	if err != nil {
		return nil, fmt.Errorf("mcs: read called domain: %w", err)
	}

	// Upward Flag (BOOLEAN)
	ci.UpwardFlag, err = r.ReadBoolean()
	if err != nil {
		return nil, fmt.Errorf("mcs: read upward flag: %w", err)
	}

	// Target Parameters (DomainParameters)
	ci.TargetParams, err = parseDomainParameters(r)
	if err != nil {
		return nil, fmt.Errorf("mcs: read target params: %w", err)
	}

	// Minimum Parameters (DomainParameters)
	ci.MinParams, err = parseDomainParameters(r)
	if err != nil {
		return nil, fmt.Errorf("mcs: read min params: %w", err)
	}

	// Maximum Parameters (DomainParameters)
	ci.MaxParams, err = parseDomainParameters(r)
	if err != nil {
		return nil, fmt.Errorf("mcs: read max params: %w", err)
	}

	// User Data (OCTET STRING) - contains GCC payload
	ci.UserData, err = r.ReadOctetString()
	if err != nil {
		return nil, fmt.Errorf("mcs: read user data: %w", err)
	}

	return ci, nil
}

// parseDomainParameters parses DomainParameters from a BER reader.
func parseDomainParameters(r *BERReader) (DomainParameters, error) {
	// DomainParameters is a SEQUENCE
	tag, length, err := r.ReadTagAndLength()
	if err != nil {
		return DomainParameters{}, err
	}

	if tag != (BERTagSequence | BERConstructed) {
		return DomainParameters{}, fmt.Errorf("%w: expected SEQUENCE for DomainParameters", ErrMCSInvalidTag)
	}

	// Read individual integers within the sequence
	startPos := r.pos
	dp := DomainParameters{}

	dp.MaxChannelIDs, err = r.ReadInteger()
	if err != nil {
		return dp, err
	}

	dp.MaxUserIDs, err = r.ReadInteger()
	if err != nil {
		return dp, err
	}

	dp.MaxTokenIDs, err = r.ReadInteger()
	if err != nil {
		return dp, err
	}

	dp.NumPriorities, err = r.ReadInteger()
	if err != nil {
		return dp, err
	}

	dp.MinThroughput, err = r.ReadInteger()
	if err != nil {
		return dp, err
	}

	dp.MaxHeight, err = r.ReadInteger()
	if err != nil {
		return dp, err
	}

	dp.MaxMCSPDUSize, err = r.ReadInteger()
	if err != nil {
		return dp, err
	}

	dp.ProtocolVersion, err = r.ReadInteger()
	if err != nil {
		return dp, err
	}

	// Ensure we consumed exactly the expected length
	consumed := r.pos - startPos
	if consumed < length {
		// Skip any remaining bytes
		if err := r.Skip(length - consumed); err != nil {
			return dp, err
		}
	}

	return dp, nil
}

// BuildConnectResponse builds an MCS Connect-Response PDU.
func BuildConnectResponse(result int, connectID int, params DomainParameters, userData []byte) []byte {
	w := NewBERWriter()

	// Build the content first to know its size
	content := NewBERWriter()

	// Result (ENUMERATED)
	content.WriteByte(BERTagEnumerated)
	content.WriteByte(1)
	content.WriteByte(byte(result))

	// CalledConnectID (INTEGER)
	content.WriteInteger(connectID)

	// DomainParameters (SEQUENCE)
	dpBytes := encodeDomainParameters(params)
	content.WriteBytes(dpBytes)

	// UserData (OCTET STRING)
	content.WriteOctetString(userData)

	// Write the application tag for Connect-Response (102 = 0x66)
	// Tag 102 requires long-form encoding
	w.WriteByte(0x7F) // Long-form application tag
	w.WriteByte(102)
	w.WriteLength(content.Len())
	w.WriteBytes(content.Bytes())

	return w.Bytes()
}

// encodeDomainParameters encodes DomainParameters as a BER SEQUENCE.
func encodeDomainParameters(dp DomainParameters) []byte {
	content := NewBERWriter()
	content.WriteInteger(dp.MaxChannelIDs)
	content.WriteInteger(dp.MaxUserIDs)
	content.WriteInteger(dp.MaxTokenIDs)
	content.WriteInteger(dp.NumPriorities)
	content.WriteInteger(dp.MinThroughput)
	content.WriteInteger(dp.MaxHeight)
	content.WriteInteger(dp.MaxMCSPDUSize)
	content.WriteInteger(dp.ProtocolVersion)

	w := NewBERWriter()
	w.WriteSequence(content.Len())
	w.WriteBytes(content.Bytes())

	return w.Bytes()
}

// MCSPDUType represents the type of an MCS PDU.
type MCSPDUType uint8

// String returns a string representation of the MCS PDU type.
func (t MCSPDUType) String() string {
	switch t {
	case MCSErectDomainRequest:
		return "ErectDomainRequest"
	case MCSAttachUserRequest:
		return "AttachUserRequest"
	case MCSAttachUserConfirm:
		return "AttachUserConfirm"
	case MCSChannelJoinRequest:
		return "ChannelJoinRequest"
	case MCSChannelJoinConfirm:
		return "ChannelJoinConfirm"
	case MCSSendDataRequest:
		return "SendDataRequest"
	case MCSSendDataIndication:
		return "SendDataIndication"
	case MCSDisconnectUltimatum:
		return "DisconnectUltimatum"
	default:
		return fmt.Sprintf("Unknown(%d)", t)
	}
}

// ParseMCSPDUType reads the MCS PDU type from the first byte.
// MCS PDUs use PER encoding where the type is in the high bits.
func ParseMCSPDUType(data []byte) (MCSPDUType, error) {
	if len(data) < 1 {
		return 0, ErrMCSInvalidPDU
	}

	// PER encoding: type is in bits 7-2 (shifted right by 2)
	pduType := MCSPDUType(data[0] >> 2)
	return pduType, nil
}

// ErectDomainRequest represents an MCS Erect Domain Request.
type ErectDomainRequest struct {
	SubHeight   int
	SubInterval int
}

// ParseErectDomainRequest parses an Erect Domain Request PDU.
func ParseErectDomainRequest(data []byte) (*ErectDomainRequest, error) {
	if len(data) < 5 {
		return nil, ErrMCSInvalidPDU
	}

	// Skip type byte, read sub-height and sub-interval (each 2 bytes, PER encoded)
	return &ErectDomainRequest{
		SubHeight:   int(binary.BigEndian.Uint16(data[1:3])),
		SubInterval: int(binary.BigEndian.Uint16(data[3:5])),
	}, nil
}

// AttachUserRequest is an empty MCS Attach User Request.
type AttachUserRequest struct{}

// ParseAttachUserRequest parses an Attach User Request PDU.
func ParseAttachUserRequest(data []byte) (*AttachUserRequest, error) {
	// Just the type byte, nothing else
	return &AttachUserRequest{}, nil
}

// BuildAttachUserConfirm builds an Attach User Confirm PDU.
// Format: choice(1) + result(1) + userId(2) = 4 bytes
// The choice byte uses (type << 2) | 2 to indicate the optional initiator field is present.
// User ID is sent as absolute value (matching xrdp exactly for Jump Desktop compatibility).
func BuildAttachUserConfirm(result int, userID uint16) []byte {
	if result == MCSResultSuccessful {
		buf := make([]byte, 4)
		// Use |2 to indicate optional initiator field is present (matches xrdp)
		buf[0] = byte((MCSAttachUserConfirm << 2) | 2) // 0x2E
		buf[1] = byte(result)                          // 0x00 = success
		// User ID as absolute value (xrdp sends this way and Jump Desktop expects it)
		buf[2] = byte(userID >> 8)
		buf[3] = byte(userID)
		return buf
	}

	// No user ID on failure - no presence bit since initiator is not present
	buf := make([]byte, 2)
	buf[0] = byte(MCSAttachUserConfirm << 2) // 0x2C
	buf[1] = byte(result)
	return buf
}

// ChannelJoinRequest represents an MCS Channel Join Request.
type ChannelJoinRequest struct {
	UserID    uint16
	ChannelID uint16
}

// ParseChannelJoinRequest parses a Channel Join Request PDU.
func ParseChannelJoinRequest(data []byte) (*ChannelJoinRequest, error) {
	if len(data) < 5 {
		return nil, ErrMCSInvalidPDU
	}

	// PER encoding:
	// - Type (1 byte)
	// - User ID (2 bytes, relative to MCSUserIDBase)
	// - Channel ID (2 bytes)
	return &ChannelJoinRequest{
		UserID:    binary.BigEndian.Uint16(data[1:3]) + MCSUserIDBase,
		ChannelID: binary.BigEndian.Uint16(data[3:5]),
	}, nil
}

// BuildChannelJoinConfirm builds a Channel Join Confirm PDU.
// Format: choice(1) + result(1) + userId(2) + requested(2) + channelId(2) = 8 bytes
// The choice byte uses |2 to indicate the optional channelId field is present on success.
// User ID is sent relative to MCSUserIDBase (per T.125 PER encoding).
func BuildChannelJoinConfirm(result int, userID, channelID uint16) []byte {
	relativeUserID := userID - MCSUserIDBase

	if result == MCSResultSuccessful {
		buf := make([]byte, 8)
		// Use |2 to indicate optional channelId field is present (matches xrdp: 0x3E)
		buf[0] = byte((MCSChannelJoinConfirm << 2) | 2) // 0x3E
		buf[1] = byte(result)
		binary.BigEndian.PutUint16(buf[2:4], relativeUserID)
		binary.BigEndian.PutUint16(buf[4:6], channelID) // requested
		binary.BigEndian.PutUint16(buf[6:8], channelID) // channelId (confirmed)
		return buf
	}

	// Failure - no channelId field, no presence bit
	buf := make([]byte, 6)
	buf[0] = byte(MCSChannelJoinConfirm << 2) // 0x3C (no presence bit)
	buf[1] = byte(result)
	binary.BigEndian.PutUint16(buf[2:4], relativeUserID)
	binary.BigEndian.PutUint16(buf[4:6], channelID) // requested only
	return buf
}

// SendDataRequest represents an MCS Send Data Request.
type SendDataRequest struct {
	UserID       uint16
	ChannelID    uint16
	DataPriority uint8
	Segmentation uint8
	UserData     []byte
}

// ParseSendDataRequest parses a Send Data Request PDU.
func ParseSendDataRequest(data []byte) (*SendDataRequest, error) {
	if len(data) < 8 {
		return nil, ErrMCSInvalidPDU
	}

	// PER encoding:
	// - Type (1 byte)
	// - User ID (2 bytes, relative to MCSUserIDBase)
	// - Channel ID (2 bytes)
	// - Priority + Segmentation (1 byte)
	// - Length (1-2 bytes, PER encoded)
	// - User Data

	sdr := &SendDataRequest{
		UserID:       binary.BigEndian.Uint16(data[1:3]) + MCSUserIDBase,
		ChannelID:    binary.BigEndian.Uint16(data[3:5]),
		DataPriority: (data[5] >> 6) & 0x03,
		Segmentation: (data[5] >> 4) & 0x03,
	}

	// Parse PER-encoded length
	pos := 6
	length := int(data[pos])
	pos++

	if length&0x80 != 0 {
		// Two-byte length
		if pos >= len(data) {
			return nil, ErrMCSInvalidPDU
		}
		length = ((length & 0x7F) << 8) | int(data[pos])
		pos++
	}

	if pos+length > len(data) {
		return nil, ErrMCSInvalidPDU
	}

	sdr.UserData = data[pos : pos+length]
	return sdr, nil
}

// BuildSendDataIndication builds a Send Data Indication PDU.
func BuildSendDataIndication(userID, channelID uint16, data []byte) []byte {
	// Calculate length encoding size
	lengthSize := 1
	if len(data) >= 128 {
		lengthSize = 2
	}

	buf := make([]byte, 6+lengthSize+len(data))

	// Type
	buf[0] = byte(MCSSendDataIndication << 2)

	// User ID (PER encoded relative to MCSUserIDBase)
	relativeUserID := userID - MCSUserIDBase
	binary.BigEndian.PutUint16(buf[1:3], relativeUserID)

	// Channel ID
	binary.BigEndian.PutUint16(buf[3:5], channelID)

	// Priority (high) + Segmentation (begin+end)
	buf[5] = 0x70 // High priority, begin+end segment

	// Length (PER encoded)
	pos := 6
	if len(data) < 128 {
		buf[pos] = byte(len(data))
		pos++
	} else {
		buf[pos] = byte(0x80 | (len(data) >> 8))
		buf[pos+1] = byte(len(data))
		pos += 2
	}

	// Data
	copy(buf[pos:], data)

	return buf
}

// BuildDisconnectUltimatum builds a Disconnect Ultimatum PDU.
func BuildDisconnectUltimatum(reason int) []byte {
	// PER encoding: Type + Reason
	buf := make([]byte, 2)
	buf[0] = byte(MCSDisconnectUltimatum << 2)
	buf[1] = byte(reason)
	return buf
}

// WriteConnectResponse writes an MCS Connect-Response wrapped in X.224 and TPKT.
func WriteConnectResponse(w io.Writer, result int, connectID int, params DomainParameters, userData []byte) error {
	mcsData := BuildConnectResponse(result, connectID, params, userData)
	return WriteX224Data(w, mcsData)
}

// WriteMCSPDU writes an MCS PDU wrapped in X.224 and TPKT.
// HOT PATH: Uses pooled buffers for zero-allocation packet building.
func WriteMCSPDU(w io.Writer, pdu []byte) error {
	return WriteX224DataPooled(w, pdu)
}

// WriteSendDataIndicationPooled writes a complete Send Data Indication PDU with all
// layers (TPKT + X.224 + MCS + data) built in a single pooled buffer.
// HOT PATH: Zero allocations for typical RDP data packets.
func WriteSendDataIndicationPooled(w io.Writer, userID, channelID uint16, data []byte) error {
	// Calculate MCS header size (6-8 bytes depending on length encoding)
	lengthSize := 1
	if len(data) >= 128 {
		lengthSize = 2
	}
	mcsHeaderSize := 6 + lengthSize

	// Total packet: TPKT(4) + X.224(3) + MCS header + data
	totalLen := TPKTHeaderLength + X224DataHeaderLen + mcsHeaderSize + len(data)

	if totalLen > MaxTPKTLength {
		return ErrTPKTLengthTooLarge
	}

	// Get buffer from pool
	bufPtr := packetPool.Get().(*[]byte)
	buf := *bufPtr

	// Check if buffer is large enough
	if totalLen > len(buf) {
		// Rare path: packet too large for pooled buffer, fall back to allocation
		packetPool.Put(bufPtr)
		mcsPDU := BuildSendDataIndication(userID, channelID, data)
		return WriteMCSPDU(w, mcsPDU)
	}

	// Return buffer to pool after use
	defer packetPool.Put(bufPtr)

	packet := buf[:totalLen]

	// TPKT header (4 bytes)
	packet[0] = TPKTVersion
	packet[1] = 0 // reserved
	binary.BigEndian.PutUint16(packet[2:4], uint16(totalLen))

	// X.224 Data TPDU header (3 bytes)
	packet[4] = 2           // LI = 2
	packet[5] = X224Data    // Code
	packet[6] = X224DataEOT // EOT = 0x80

	// MCS Send Data Indication header
	pos := TPKTHeaderLength + X224DataHeaderLen
	packet[pos] = byte(MCSSendDataIndication << 2)
	relativeUserID := userID - MCSUserIDBase
	binary.BigEndian.PutUint16(packet[pos+1:pos+3], relativeUserID)
	binary.BigEndian.PutUint16(packet[pos+3:pos+5], channelID)
	packet[pos+5] = 0x70 // High priority, begin+end segment

	// Length (PER encoded)
	pos += 6
	if len(data) < 128 {
		packet[pos] = byte(len(data))
		pos++
	} else {
		packet[pos] = byte(0x80 | (len(data) >> 8))
		packet[pos+1] = byte(len(data))
		pos += 2
	}

	// Data
	copy(packet[pos:], data)

	// Single write to socket
	_, err := w.Write(packet)
	if err != nil {
		return fmt.Errorf("tpkt: write: %w", err)
	}
	return nil
}
