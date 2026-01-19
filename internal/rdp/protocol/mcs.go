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
func DefaultDomainParameters() DomainParameters {
	return DomainParameters{
		MaxChannelIDs:   65535,
		MaxUserIDs:      65535,
		MaxTokenIDs:     65535,
		NumPriorities:   1,
		MinThroughput:   0,
		MaxHeight:       1,
		MaxMCSPDUSize:   65535,
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

	// Debug: print first 32 bytes of MCS data
	debugLen := 32
	if len(data) < debugLen {
		debugLen = len(data)
	}
	fmt.Printf("DEBUG MCS Connect-Initial first %d bytes: % 02X\n", debugLen, data[:debugLen])

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

	result_bytes := w.Bytes()

	// Debug: dump the MCS Connect-Response
	fmt.Printf("DEBUG MCS Connect-Response total len=%d\n", len(result_bytes))
	debugLen := 64
	if len(result_bytes) < debugLen {
		debugLen = len(result_bytes)
	}
	fmt.Printf("DEBUG MCS Connect-Response first %d bytes: % 02X\n", debugLen, result_bytes[:debugLen])
	fmt.Printf("DEBUG MCS Connect-Response GCC userData len=%d\n", len(userData))
	if len(userData) > 0 {
		gccDebugLen := 48
		if len(userData) < gccDebugLen {
			gccDebugLen = len(userData)
		}
		fmt.Printf("DEBUG GCC userData first %d bytes: % 02X\n", gccDebugLen, userData[:gccDebugLen])
	}

	return result_bytes
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
func BuildAttachUserConfirm(result int, userID uint16) []byte {
	// PER encoding:
	// - Type (1 byte): AttachUserConfirm << 2 = 11 << 2 = 44 = 0x2C
	// - Result (1 byte): enumerated value
	// - User ID present flag + User ID (optional based on result)
	if result == MCSResultSuccessful {
		// Include user ID
		buf := make([]byte, 4)
		buf[0] = byte(MCSAttachUserConfirm << 2)
		buf[1] = byte(result)
		// User ID with "present" flag in high bit
		buf[2] = byte(userID >> 8) // High byte of user ID
		buf[3] = byte(userID)
		return buf
	}

	// No user ID on failure
	buf := make([]byte, 2)
	buf[0] = byte(MCSAttachUserConfirm << 2)
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
	// - User ID (2 bytes)
	// - Channel ID (2 bytes)
	return &ChannelJoinRequest{
		UserID:    binary.BigEndian.Uint16(data[1:3]),
		ChannelID: binary.BigEndian.Uint16(data[3:5]),
	}, nil
}

// BuildChannelJoinConfirm builds a Channel Join Confirm PDU.
func BuildChannelJoinConfirm(result int, userID, channelID uint16) []byte {
	// PER encoding:
	// - Type (1 byte): ChannelJoinConfirm << 2 = 15 << 2 = 60 = 0x3C
	// - Result (1 byte)
	// - User ID (2 bytes)
	// - Channel ID (2 bytes) - optional on failure
	if result == MCSResultSuccessful {
		buf := make([]byte, 8)
		buf[0] = byte(MCSChannelJoinConfirm << 2)
		buf[1] = byte(result)
		binary.BigEndian.PutUint16(buf[2:4], userID)
		binary.BigEndian.PutUint16(buf[4:6], channelID)
		// Requested channel ID (repeated)
		binary.BigEndian.PutUint16(buf[6:8], channelID)
		return buf
	}

	buf := make([]byte, 4)
	buf[0] = byte(MCSChannelJoinConfirm << 2)
	buf[1] = byte(result)
	binary.BigEndian.PutUint16(buf[2:4], userID)
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
	// - User ID (2 bytes)
	// - Channel ID (2 bytes)
	// - Priority + Segmentation (1 byte)
	// - Length (1-2 bytes, PER encoded)
	// - User Data

	sdr := &SendDataRequest{
		UserID:       binary.BigEndian.Uint16(data[1:3]),
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

	// User ID
	binary.BigEndian.PutUint16(buf[1:3], userID)

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

	// Build the full TPKT+X.224+MCS packet for debug
	x224Data := BuildDataTPDU(mcsData)
	totalLen := TPKTHeaderLength + len(x224Data)
	fullPacket := make([]byte, totalLen)
	fullPacket[0] = TPKTVersion
	fullPacket[1] = 0
	binary.BigEndian.PutUint16(fullPacket[2:4], uint16(totalLen))
	copy(fullPacket[4:], x224Data)
	fmt.Printf("DEBUG Full TPKT+X.224+MCS packet len=%d, first 64 bytes: % 02X\n", len(fullPacket), fullPacket[:min(64, len(fullPacket))])
	if len(fullPacket) > 64 {
		fmt.Printf("DEBUG Full packet bytes 64-end (%d more): % 02X\n", len(fullPacket)-64, fullPacket[64:])
	}

	return WriteX224Data(w, mcsData)
}

// WriteMCSPDU writes an MCS PDU wrapped in X.224 and TPKT.
func WriteMCSPDU(w io.Writer, pdu []byte) error {
	return WriteX224Data(w, pdu)
}
