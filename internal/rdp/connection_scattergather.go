package rdp

import (
	"encoding/binary"

	"github.com/jetkvm/kvm/internal/rdp/protocol"
)

// ScatterGatherWriter is implemented by connections that support scatter-gather I/O.
// When kTLS is enabled, this allows zero-copy writes of multiple buffers.
type ScatterGatherWriter interface {
	// WriteScatterGather writes multiple buffers as a single TLS record.
	WriteScatterGather(bufs ...[]byte) (int, error)
	// IsKTLSSendEnabled returns true if kTLS is enabled for sending.
	IsKTLSSendEnabled() bool
}

// supportsScatterGather checks if the connection supports scatter-gather writes.
// Returns the ScatterGatherWriter interface if supported, nil otherwise.
func (c *Connection) supportsScatterGather() ScatterGatherWriter {
	if sg, ok := c.conn.(ScatterGatherWriter); ok && sg.IsKTLSSendEnabled() {
		return sg
	}
	return nil
}

// sendDVCDataScatterGather sends DVC data using scatter-gather I/O when kTLS is available.
// This avoids copying the payload data into the RDP header buffer.
//
// HOT PATH: Used for large H.264 frames to minimize memory copies.
// Only beneficial when payload > ~1KB and kTLS is enabled.
func (c *Connection) sendDVCDataScatterGather(sg ScatterGatherWriter, data []byte) error {
	if c.drdynvcID == 0 {
		return nil
	}

	// Packet layout for scatter-gather:
	// Buffer 1: [TPKT header 4][X.224 header 3][MCS header 6-8][VC header 8]
	// Buffer 2: [DVC data payload]
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
	headerLen := tpktHeaderLen + x224HeaderLen + mcsHeaderLen + vcHeaderLen

	// Build header buffer (small, fixed-size allocation is acceptable)
	// For even better performance, this could use a sync.Pool
	header := make([]byte, headerLen)
	pos := 0

	// TPKT header (4 bytes, big-endian length)
	header[pos] = protocol.TPKTVersion
	header[pos+1] = 0
	binary.BigEndian.PutUint16(header[pos+2:pos+4], uint16(totalPacketLen))
	pos += tpktHeaderLen

	// X.224 Data TPDU header (3 bytes)
	header[pos] = 2                      // LI
	header[pos+1] = protocol.X224Data    // Code
	header[pos+2] = protocol.X224DataEOT // EOT
	pos += x224HeaderLen

	// MCS Send Data Indication header
	header[pos] = byte(protocol.MCSSendDataIndication << 2)
	relativeUserID := c.userID - protocol.MCSUserIDBase
	binary.BigEndian.PutUint16(header[pos+1:pos+3], relativeUserID)
	binary.BigEndian.PutUint16(header[pos+3:pos+5], c.drdynvcID)
	header[pos+5] = 0x70 // High priority, begin+end segment
	pos += 6

	// MCS length field (PER encoded)
	mcsDataLen := vcHeaderLen + vcPayloadLen
	if mcsDataLen < 128 {
		header[pos] = byte(mcsDataLen)
		pos++
	} else {
		header[pos] = byte(0x80 | (mcsDataLen >> 8))
		header[pos+1] = byte(mcsDataLen)
		pos += 2
	}

	// VC PDU header (8 bytes, little-endian)
	binary.LittleEndian.PutUint32(header[pos:pos+4], uint32(vcPayloadLen))
	binary.LittleEndian.PutUint32(header[pos+4:pos+8], channelFlagFirst|channelFlagLast)

	// Write using scatter-gather: [header, data]
	// The kernel will encrypt both buffers as a single TLS record without copying
	_, err := sg.WriteScatterGather(header, data)
	return err
}

// ScatterGatherThreshold is the minimum payload size to use scatter-gather.
// Below this threshold, the overhead of scatter-gather isn't worth it.
const ScatterGatherThreshold = 4096 // 4KB
