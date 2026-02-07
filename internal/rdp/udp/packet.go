// Package udp implements the MS-RDPEUDP2 reliable UDP transport for RDP.
//
// The transport replaces the inner TCP connection with a purpose-built reliable
// UDP protocol, eliminating TCP-over-TCP meltdown when RDP runs over VPN tunnels.
//
// Protocol layers (bottom to top):
//
//	[RDPEUDP v1 SYN handshake] → [RDPEUDP2 v3 data transport] → [TLS] → [RDPEMT]
package udp

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// RDPEUDP v1 SYN flags (MS-RDPEUDP 2.2.1.1).
// The initial 3-way handshake uses v1 packet format even for RDPEUDP2.
const (
	RDPUDPFlagSYN   = 0x0001
	RDPUDPFlagFIN   = 0x0002
	RDPUDPFlagACK   = 0x0004
	RDPUDPFlagDATA  = 0x0008
	RDPUDPFlagSYNEX = 0x0200
)

// RDPEUDP2 v3 data header flags (MS-RDPEUDP2 2.2.1.1).
const (
	RDPEUDP2FlagACK   = 0x0001
	RDPEUDP2FlagDATA  = 0x0004
	RDPEUDP2FlagACKOF = 0x0008
)

// RDPEUDP version negotiation values.
const (
	RDPUDPVersion1 = 0x0001 // Original RDPEUDP
	RDPUDPVersion2 = 0x0002 // RDPEUDP2 (v2 negotiation)
	RDPUDPVersion3 = 0x0101 // RDPEUDP2 (v3 wire format)
)

// Wire format sizes.
const (
	// SYN packet minimum sizes
	synBaseHeaderLen = 16 // flags(2) + snInitial(4) + upstreamMTU(2) + downstreamMTU(2) + padding(6)
	synCookieLen     = 32 // SHA-256 hash
	synExHeaderLen   = 4  // flags(2) + version(2)

	// v3 data header
	dataHeaderLen = 2 // flags(2)

	// v3 ACK payload
	ackPayloadLen = 4 // seqNum(2) + rcvWindowSize(2)

	// v3 data payload header
	dataPayloadHeaderLen = 2 // seqNum(2)

	// v3 ACKOFA payload
	ackOfAPayloadLen = 4 // timestamp(4)

	// PacketPrefixByte position for RDPEUDP2 obfuscation
	packetPrefixPos = 7

	// Maximum MTU for data payloads
	MaxDataPayload = 1200
)

// Common errors.
var (
	ErrPacketTooShort  = errors.New("udp: packet too short")
	ErrInvalidSYN      = errors.New("udp: invalid SYN packet")
	ErrInvalidCookie   = errors.New("udp: invalid security cookie hash")
	ErrInvalidVersion  = errors.New("udp: unsupported version")
	ErrInvalidChecksum = errors.New("udp: invalid packet checksum")
)

// SynPacket represents a parsed RDPEUDP v1 SYN packet.
type SynPacket struct {
	Flags         uint16
	InitialSeqNum uint32
	UpstreamMTU   uint16
	DownstreamMTU uint16
	CookieHash    [32]byte // SHA-256 of security cookie
	Version       uint16   // From SYNEX extension
	SynExFlags    uint16
	HasSYNEX      bool
	HasCookieHash bool
}

// AckPayload represents a v3 ACK payload.
type AckPayload struct {
	SeqNum        uint64 // Decompressed 64-bit sequence number
	RcvWindowSize uint16
}

// DataPayload represents a v3 data payload.
type DataPayload struct {
	SeqNum uint64 // Decompressed 64-bit sequence number
	Data   []byte
}

// DataPacket represents a parsed RDPEUDP2 v3 data packet.
type DataPacket struct {
	Flags uint16
	Ack   *AckPayload
	Data  *DataPayload
	AckOf *uint32 // ACKOFA timestamp, nil if not present
}

// ParseSynPacket parses a RDPEUDP v1 SYN packet.
// The packet format is defined in MS-RDPEUDP 2.2.1.1.
func ParseSynPacket(data []byte) (*SynPacket, error) {
	if len(data) < synBaseHeaderLen {
		return nil, ErrPacketTooShort
	}

	pkt := &SynPacket{
		Flags:         binary.LittleEndian.Uint16(data[0:2]),
		InitialSeqNum: binary.LittleEndian.Uint32(data[4:8]),
		UpstreamMTU:   binary.LittleEndian.Uint16(data[8:10]),
		DownstreamMTU: binary.LittleEndian.Uint16(data[10:12]),
	}

	if pkt.Flags&RDPUDPFlagSYN == 0 {
		return nil, ErrInvalidSYN
	}

	pos := synBaseHeaderLen

	// Cookie hash follows the base header (MS-RDPEUDP 2.2.1.1)
	if len(data) >= pos+synCookieLen {
		copy(pkt.CookieHash[:], data[pos:pos+synCookieLen])
		pkt.HasCookieHash = true
		pos += synCookieLen
	}

	// SYNEX extension (MS-RDPEUDP 2.2.1.1)
	if pkt.Flags&RDPUDPFlagSYNEX != 0 && len(data) >= pos+synExHeaderLen {
		pkt.SynExFlags = binary.LittleEndian.Uint16(data[pos : pos+2])
		pkt.Version = binary.LittleEndian.Uint16(data[pos+2 : pos+4])
		pkt.HasSYNEX = true
	}

	return pkt, nil
}

// BuildSynAckPacket builds a RDPEUDP v1 SYN+ACK response.
func BuildSynAckPacket(serverSeqNum, clientSeqNum uint32, mtu uint16, cookie [16]byte) []byte {
	// SYN+ACK with SYNEX: base(16) + cookieHash(32) + synex(4)
	size := synBaseHeaderLen + synCookieLen + synExHeaderLen
	buf := make([]byte, size)

	// Flags: SYN + ACK + SYNEX
	flags := uint16(RDPUDPFlagSYN | RDPUDPFlagACK | RDPUDPFlagSYNEX)
	binary.LittleEndian.PutUint16(buf[0:2], flags)

	// snInitialSequenceNumber
	binary.LittleEndian.PutUint32(buf[4:8], serverSeqNum)

	// MTU values (upstream and downstream)
	binary.LittleEndian.PutUint16(buf[8:10], mtu)
	binary.LittleEndian.PutUint16(buf[10:12], mtu)

	pos := synBaseHeaderLen

	// Cookie hash: SHA-256 of the security cookie
	hash := sha256.Sum256(cookie[:])
	copy(buf[pos:pos+synCookieLen], hash[:])
	pos += synCookieLen

	// SYNEX: version = RDPEUDP2 (0x0101)
	binary.LittleEndian.PutUint16(buf[pos:pos+2], 0) // synExFlags
	binary.LittleEndian.PutUint16(buf[pos+2:pos+4], RDPUDPVersion3)

	return buf
}

// ValidateCookieHash checks if the SHA-256 hash in the SYN matches the cookie.
func ValidateCookieHash(cookie [16]byte, hash [32]byte) bool {
	expected := sha256.Sum256(cookie[:])
	// Constant-time comparison to prevent timing attacks
	match := true
	for i := range expected {
		if expected[i] != hash[i] {
			match = false
		}
	}
	return match
}

// ApplyPacketPrefix applies the RDPEUDP2 packet prefix obfuscation.
// Per MS-RDPEUDP2 2.2.1, byte[0] and byte[7] are swapped.
// This must be applied on both send and receive.
func ApplyPacketPrefix(data []byte) {
	if len(data) < packetPrefixPos+1 {
		return
	}
	data[0], data[packetPrefixPos] = data[packetPrefixPos], data[0]
}

// CompressSeqNum compresses a 64-bit sequence number to 16-bit wire format.
// Only the lower 16 bits are transmitted.
func CompressSeqNum(seqNum uint64) uint16 {
	return uint16(seqNum & 0xFFFF)
}

// DecompressSeqNum decompresses a 16-bit wire sequence number to 64-bit
// using wrap-around detection relative to a base sequence number.
func DecompressSeqNum(wire uint16, base uint64) uint64 {
	baseHigh := base & ^uint64(0xFFFF)
	baseLow := uint16(base & 0xFFFF)

	result := baseHigh | uint64(wire)

	// Handle wrap-around: if wire is much lower than base low bits,
	// it likely wrapped to the next 64K range
	if wire < baseLow && (baseLow-wire) > 0x8000 {
		result += 0x10000
	} else if wire > baseLow && (wire-baseLow) > 0x8000 {
		// wire is much higher, it's from the previous range
		if result >= 0x10000 {
			result -= 0x10000
		}
	}

	return result
}

// ParseDataPacket parses a RDPEUDP2 v3 data packet.
// The baseSeqNum is used for sequence number decompression.
func ParseDataPacket(data []byte, recvBaseSeqNum, sendBaseSeqNum uint64) (*DataPacket, error) {
	if len(data) < dataHeaderLen {
		return nil, ErrPacketTooShort
	}

	flags := binary.LittleEndian.Uint16(data[0:2])
	pkt := &DataPacket{Flags: flags}
	pos := dataHeaderLen

	// ACK payload (MS-RDPEUDP2 2.2.1.1.1)
	if flags&RDPEUDP2FlagACK != 0 {
		if len(data) < pos+ackPayloadLen {
			return nil, fmt.Errorf("udp: ACK payload too short at offset %d", pos)
		}
		wireSeq := binary.LittleEndian.Uint16(data[pos : pos+2])
		pkt.Ack = &AckPayload{
			SeqNum:        DecompressSeqNum(wireSeq, sendBaseSeqNum),
			RcvWindowSize: binary.LittleEndian.Uint16(data[pos+2 : pos+4]),
		}
		pos += ackPayloadLen
	}

	// Data payload (MS-RDPEUDP2 2.2.1.1.2)
	if flags&RDPEUDP2FlagDATA != 0 {
		if len(data) < pos+dataPayloadHeaderLen {
			return nil, fmt.Errorf("udp: DATA payload too short at offset %d", pos)
		}
		wireSeq := binary.LittleEndian.Uint16(data[pos : pos+2])
		pkt.Data = &DataPayload{
			SeqNum: DecompressSeqNum(wireSeq, recvBaseSeqNum),
			Data:   data[pos+dataPayloadHeaderLen:],
		}
		pos += dataPayloadHeaderLen + len(pkt.Data.Data)
	}

	// ACKOFA payload (MS-RDPEUDP2 2.2.1.1.3)
	if flags&RDPEUDP2FlagACKOF != 0 {
		if len(data) < pos+ackOfAPayloadLen {
			return nil, fmt.Errorf("udp: ACKOFA payload too short at offset %d", pos)
		}
		ts := binary.LittleEndian.Uint32(data[pos : pos+4])
		pkt.AckOf = &ts
	}

	return pkt, nil
}

// BuildDataPacket builds a RDPEUDP2 v3 data packet.
func BuildDataPacket(pkt *DataPacket) []byte {
	size := dataHeaderLen
	if pkt.Ack != nil {
		pkt.Flags |= RDPEUDP2FlagACK
		size += ackPayloadLen
	}
	if pkt.Data != nil {
		pkt.Flags |= RDPEUDP2FlagDATA
		size += dataPayloadHeaderLen + len(pkt.Data.Data)
	}
	if pkt.AckOf != nil {
		pkt.Flags |= RDPEUDP2FlagACKOF
		size += ackOfAPayloadLen
	}

	buf := make([]byte, size)
	binary.LittleEndian.PutUint16(buf[0:2], pkt.Flags)
	pos := dataHeaderLen

	if pkt.Ack != nil {
		binary.LittleEndian.PutUint16(buf[pos:pos+2], CompressSeqNum(pkt.Ack.SeqNum))
		binary.LittleEndian.PutUint16(buf[pos+2:pos+4], pkt.Ack.RcvWindowSize)
		pos += ackPayloadLen
	}

	if pkt.Data != nil {
		binary.LittleEndian.PutUint16(buf[pos:pos+2], CompressSeqNum(pkt.Data.SeqNum))
		copy(buf[pos+dataPayloadHeaderLen:], pkt.Data.Data)
		pos += dataPayloadHeaderLen + len(pkt.Data.Data)
	}

	if pkt.AckOf != nil {
		binary.LittleEndian.PutUint32(buf[pos:pos+4], *pkt.AckOf)
	}

	return buf
}

// BuildAckOnlyPacket is a convenience function for building an ACK-only v3 packet.
func BuildAckOnlyPacket(ackSeqNum uint64, rcvWindowSize uint16) []byte {
	return BuildDataPacket(&DataPacket{
		Ack: &AckPayload{
			SeqNum:        ackSeqNum,
			RcvWindowSize: rcvWindowSize,
		},
	})
}
