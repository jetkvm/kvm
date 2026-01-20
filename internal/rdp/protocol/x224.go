package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// X224 implements the ISO 8073 / ITU-T X.224 transport protocol.
// This is used for RDP connection establishment.

var (
	ErrX224InvalidTPDU       = errors.New("x224: invalid TPDU type")
	ErrX224InvalidLength     = errors.New("x224: invalid length indicator")
	ErrX224NegotiationFailed = errors.New("x224: negotiation failed")
)

// ConnectionRequest represents an X.224 Connection Request TPDU (CR).
//
// Format:
//
//	+--------+--------+--------+--------+--------+--------+--------+
//	|   LI   |  0xE0  |    DST-REF      |    SRC-REF      | Class  |
//	+--------+--------+--------+--------+--------+--------+--------+
//	|              Variable Part (cookie, RDP NEG REQ)             |
//	+--------------------------------------------------------------+
type ConnectionRequest struct {
	LengthIndicator uint8
	TypeCredit      uint8 // 0xE0 for CR
	DstRef          uint16
	SrcRef          uint16
	ClassOptions    uint8

	// Variable part
	Cookie string      // "Cookie: mstshash=..." or routing token
	NegReq *NegRequest // RDP Negotiation Request
}

// NegRequest is the RDP Negotiation Request structure.
// Sent in the variable part of X.224 Connection Request.
type NegRequest struct {
	Type           uint8  // TYPE_RDP_NEG_REQ (0x01)
	Flags          uint8  // Negotiation flags
	Length         uint16 // Always 8
	RequestedProto uint32 // Protocol bitmask
}

// ConnectionConfirm represents an X.224 Connection Confirm TPDU (CC).
type ConnectionConfirm struct {
	LengthIndicator uint8
	TypeCredit      uint8 // 0xD0 for CC
	DstRef          uint16
	SrcRef          uint16
	ClassOptions    uint8

	// Variable part
	NegRsp *NegResponse
	NegErr *NegFailure
}

// NegResponse is the RDP Negotiation Response structure.
type NegResponse struct {
	Type          uint8  // TYPE_RDP_NEG_RSP (0x02)
	Flags         uint8  // Response flags
	Length        uint16 // Always 8
	SelectedProto uint32 // Selected protocol
}

// NegFailure is the RDP Negotiation Failure structure.
type NegFailure struct {
	Type        uint8  // TYPE_RDP_NEG_FAILURE (0x03)
	Flags       uint8  // Always 0
	Length      uint16 // Always 8
	FailureCode uint32
}

// DataTPDU represents an X.224 Data TPDU (DT).
//
// Format:
//
//	+--------+--------+--------+
//	|   LI   |  0xF0  |  EOT   |
//	+--------+--------+--------+
//	|         User Data        |
//	+--------------------------+
type DataTPDU struct {
	LengthIndicator uint8
	TypeCredit      uint8 // 0xF0 for DT
	EOT             uint8 // 0x80 for end of transmission
	Data            []byte
}

// ParseConnectionRequest parses an X.224 Connection Request from raw TPDU data.
func ParseConnectionRequest(data []byte) (*ConnectionRequest, error) {
	if len(data) < X224ConnectionReqMinLen {
		return nil, ErrX224InvalidLength
	}

	cr := &ConnectionRequest{
		LengthIndicator: data[0],
		TypeCredit:      data[1],
		DstRef:          binary.BigEndian.Uint16(data[2:4]),
		SrcRef:          binary.BigEndian.Uint16(data[4:6]),
		ClassOptions:    data[6],
	}

	if cr.TypeCredit != X224ConnectionRequest {
		return nil, fmt.Errorf("%w: expected 0x%02X, got 0x%02X",
			ErrX224InvalidTPDU, X224ConnectionRequest, cr.TypeCredit)
	}

	// Parse variable part
	if len(data) > X224ConnectionReqMinLen {
		varPart := data[X224ConnectionReqMinLen:]
		if err := cr.parseVariablePart(varPart); err != nil {
			return nil, err
		}
	}

	return cr, nil
}

// parseVariablePart parses the variable part of a Connection Request.
func (cr *ConnectionRequest) parseVariablePart(data []byte) error {
	// Look for cookie (starts with "Cookie:" or is a routing token)
	cookieEnd := bytes.IndexByte(data, '\r')
	if cookieEnd > 0 && cookieEnd < len(data)-1 && data[cookieEnd+1] == '\n' {
		cr.Cookie = string(data[:cookieEnd])
		data = data[cookieEnd+2:]
	}

	// Parse RDP Negotiation Request if present
	if len(data) >= 8 && data[0] == NegTypeRequest {
		cr.NegReq = &NegRequest{
			Type:           data[0],
			Flags:          data[1],
			Length:         binary.LittleEndian.Uint16(data[2:4]),
			RequestedProto: binary.LittleEndian.Uint32(data[4:8]),
		}
	}

	return nil
}

// RequestsTLS returns true if the client requested TLS.
func (cr *ConnectionRequest) RequestsTLS() bool {
	if cr.NegReq == nil {
		return false
	}
	return cr.NegReq.RequestedProto&ProtocolTLS != 0
}

// RequestsCredSSP returns true if the client requested CredSSP (NLA).
func (cr *ConnectionRequest) RequestsCredSSP() bool {
	if cr.NegReq == nil {
		return false
	}
	return cr.NegReq.RequestedProto&ProtocolCredSSP != 0
}

// BuildConnectionConfirm builds an X.224 Connection Confirm TPDU.
// If clientSentNegReq is true, we MUST include RDP_NEG_RSP per MS-RDPBCGR.
func BuildConnectionConfirm(selectedProto uint32, flags uint8, clientSentNegReq bool) []byte {
	// Calculate length - per MS-RDPBCGR, if client sent RDP_NEG_REQ,
	// server MUST respond with RDP_NEG_RSP (even for PROTOCOL_RDP)
	totalLen := X224ConnectionConfirmLen
	if clientSentNegReq {
		totalLen += 8 // NegotiationResponse size
	}

	buf := make([]byte, totalLen)

	// Fixed part
	buf[0] = uint8(totalLen - 1) // LI excludes itself
	buf[1] = X224ConnectionConfirm
	binary.BigEndian.PutUint16(buf[2:4], 0) // DST-REF
	binary.BigEndian.PutUint16(buf[4:6], 0) // SRC-REF
	buf[6] = X224ConnectionReqClass

	// Variable part - RDP Negotiation Response
	if clientSentNegReq {
		buf[7] = NegTypeResponse
		buf[8] = flags
		binary.LittleEndian.PutUint16(buf[9:11], 8) // Length
		binary.LittleEndian.PutUint32(buf[11:15], selectedProto)
	}

	return buf
}

// BuildConnectionFailure builds an X.224 Connection Confirm with failure.
func BuildConnectionFailure(failureCode uint32) []byte {
	totalLen := X224ConnectionConfirmLen + 8 // Include NegotiationFailure

	buf := make([]byte, totalLen)

	// Fixed part
	buf[0] = uint8(totalLen - 1) // LI excludes itself
	buf[1] = X224ConnectionConfirm
	binary.BigEndian.PutUint16(buf[2:4], 0) // DST-REF
	binary.BigEndian.PutUint16(buf[4:6], 0) // SRC-REF
	buf[6] = X224ConnectionReqClass

	// Variable part - RDP Negotiation Failure
	buf[7] = NegTypeFailure
	buf[8] = 0                                  // Flags
	binary.LittleEndian.PutUint16(buf[9:11], 8) // Length
	binary.LittleEndian.PutUint32(buf[11:15], failureCode)

	return buf
}

// ParseDataTPDU parses an X.224 Data TPDU.
func ParseDataTPDU(data []byte) (*DataTPDU, error) {
	if len(data) < X224DataHeaderLen {
		return nil, ErrX224InvalidLength
	}

	dt := &DataTPDU{
		LengthIndicator: data[0],
		TypeCredit:      data[1],
		EOT:             data[2],
	}

	if dt.TypeCredit != X224Data {
		return nil, fmt.Errorf("%w: expected 0x%02X, got 0x%02X",
			ErrX224InvalidTPDU, X224Data, dt.TypeCredit)
	}

	if len(data) > X224DataHeaderLen {
		dt.Data = data[X224DataHeaderLen:]
	}

	return dt, nil
}

// BuildDataTPDU builds an X.224 Data TPDU.
func BuildDataTPDU(payload []byte) []byte {
	buf := make([]byte, X224DataHeaderLen+len(payload))
	buf[0] = 2           // LI = 2 (header is 3 bytes, LI excludes itself)
	buf[1] = X224Data    // Code
	buf[2] = X224DataEOT // EOT = 0x80
	copy(buf[X224DataHeaderLen:], payload)
	return buf
}

// WriteX224Data writes an X.224 Data TPDU wrapped in TPKT.
func WriteX224Data(w io.Writer, payload []byte) error {
	tpdu := BuildDataTPDU(payload)
	return WriteTPKT(w, tpdu)
}

// WriteX224DataPooled writes an X.224 Data TPDU wrapped in TPKT using a pooled buffer.
// HOT PATH: Zero allocations for packets that fit in the pool buffer (2KB).
func WriteX224DataPooled(w io.Writer, payload []byte) error {
	// Total packet size: TPKT(4) + X.224(3) + payload
	totalLen := TPKTHeaderLength + X224DataHeaderLen + len(payload)

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
		return WriteX224Data(w, payload)
	}

	// Return buffer to pool after use
	defer packetPool.Put(bufPtr)

	// Build packet in pooled buffer: [TPKT header][X.224 header][payload]
	packet := buf[:totalLen]

	// TPKT header (4 bytes)
	packet[0] = TPKTVersion
	packet[1] = 0 // reserved
	binary.BigEndian.PutUint16(packet[2:4], uint16(totalLen))

	// X.224 Data TPDU header (3 bytes)
	packet[4] = 2           // LI = 2
	packet[5] = X224Data    // Code
	packet[6] = X224DataEOT // EOT = 0x80

	// Payload
	copy(packet[TPKTHeaderLength+X224DataHeaderLen:], payload)

	// Single write to socket
	_, err := w.Write(packet)
	if err != nil {
		return fmt.Errorf("tpkt: write: %w", err)
	}
	return nil
}

// ReadX224Data reads an X.224 Data TPDU from a TPKT payload.
func ReadX224Data(r io.Reader) ([]byte, error) {
	payload, err := ReadTPKTPayload(r)
	if err != nil {
		return nil, err
	}

	dt, err := ParseDataTPDU(payload)
	if err != nil {
		return nil, err
	}

	return dt.Data, nil
}
