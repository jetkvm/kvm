package rdp

import (
	"sync"
)

// scatterHeaderPool reuses header buffers for scatter-gather I/O.
// Max header size: TPKT(4) + X224(3) + MCS(8) + VC(8) = 23 bytes, round to 32.
var scatterHeaderPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 32)
		return &buf
	},
}

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
	vcPayloadLen := len(data)
	headerLen := mcsChannelHeaderLen(vcPayloadLen)

	// Use pooled buffer for zero-allocation hot path
	bufPtr := scatterHeaderPool.Get().(*[]byte)
	header := (*bufPtr)[:headerLen]
	defer scatterHeaderPool.Put(bufPtr)

	c.buildMCSChannelHeader(header, c.drdynvcID, vcPayloadLen)

	// Write using scatter-gather: [header, data]
	// The kernel will encrypt both buffers as a single TLS record without copying
	return c.writeWithDeadline(func() error {
		_, err := sg.WriteScatterGather(header, data)
		return err
	})
}

// ScatterGatherThreshold is the minimum payload size to use scatter-gather.
// Below this threshold, the overhead of scatter-gather isn't worth it.
const ScatterGatherThreshold = 4096 // 4KB
