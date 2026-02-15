package channels

import (
	"encoding/binary"
	"errors"
	"time"
)

// H.264 frame encoding and sending for RDPGFX.
// This file contains the hot-path video frame delivery code.

// ErrGFXNoCodec is returned when no H.264 codec is available.
var ErrGFXNoCodec = errors.New("gfx: no H.264 codec available")

// checkBackpressure checks if the frame pending queue is full and handles self-healing.
// Returns nil if a frame can be sent, ErrGFXBackpressure if the caller should drop the frame.
//
// Self-healing: if pending stays at max for >10s with no acks, resets the counter.
// This recovers from edge cases where the client dropped some acks but is still alive.
//
// Thread-safety note: the atomic loads/stores here have a benign TOCTOU race.
// Two concurrent callers may both observe pending >= max and both enter the self-heal
// path. Both will reset to 0, which is safe — the counter just restarts from 0 twice.
// The alternative (adding a mutex) would add contention on the hot path for no benefit.
func (g *GFXChannel) checkBackpressure() error {
	pending := g.framesPending.Load()
	if pending >= GFXMaxFramesPending {
		now := time.Now().UnixMilli()
		since := g.backpressureSince.Load()
		if since == 0 {
			g.backpressureSince.Store(now)
		} else if now-since > 10000 {
			// Check that no acks arrived during the stuck period
			lastAck := g.lastAckTime.Load() * 1000 // Convert seconds to millis
			if lastAck < since {
				// No acks since backpressure started — reset to recover.
				// Also reset frameID and lastAckFrameID to keep them consistent;
				// otherwise updateFrameAckState would immediately recalculate
				// a high pending count from the diverged IDs.
				g.frameID.Store(0)
				g.lastAckFrameID.Store(0)
				g.framesPending.Store(0)
				g.backpressureSince.Store(0)
				g.log("RDPGFX: self-healed stuck pending counter (no acks for %dms)", now-since)
				return nil
			}
			// Acks are arriving but pending is still high — client is slow, not stuck
			g.backpressureSince.Store(now) // Reset timer
			return ErrGFXBackpressure
		}
		return ErrGFXBackpressure
	}
	// Not in backpressure — clear the timer
	g.backpressureSince.Store(0)
	return nil
}

// SendH264Frame sends an H.264 frame with zero-allocation optimization.
// The h264Data should contain raw H.264 NAL units.
// Returns ErrGFXBackpressure if too many frames are pending.
// Returns ErrGFXNoCodec if client doesn't support AVC420/AVC444.
//
// HOT PATH: This function is called for every video frame (~30-60 fps).
// It uses pre-allocated buffers to achieve zero heap allocations for small frames.
// Thread-safe: uses frameMu to protect shared buffers from concurrent access.
func (g *GFXChannel) SendH264Frame(h264Data []byte, isKeyframe bool) error {
	if !g.ready.Load() {
		return ErrGFXNotReady
	}

	// Check if client supports AVC420 codec
	if !g.avc420 {
		return ErrGFXNoCodec
	}

	// Flow control - check pending frames (lock-free atomic check)
	if err := g.checkBackpressure(); err != nil {
		return err
	}

	// Protect shared buffers (frameBuf, metaBuf) from concurrent access
	g.frameMu.Lock()
	defer g.frameMu.Unlock()

	// Get next frame ID (lock-free atomic increment)
	frameID := g.frameID.Add(1)
	// Use synthetic timestamp based on frame ID (~30fps timing)
	// Client expects consistent frame-rate-based timestamps, not wall-clock time
	timestamp := frameID * 33

	// Increment pending before sending (lock-free)
	g.framesPending.Add(1)

	// Calculate total size for GFX PDU (excluding ZGFX header)
	// START_FRAME + WIRETOSURFACE_1 + END_FRAME
	const metaSize = 14 // RFX_AVC420_METABLOCK size
	startSize := GFXHeaderSize + GFXStartFrameSize
	wireSize := GFXHeaderSize + GFXWireToSurface1Size + metaSize + len(h264Data)
	endSize := GFXHeaderSize + GFXEndFrameSize
	gfxPDUSize := startSize + wireSize + endSize

	// Check if we need multi-segment ZGFX format
	// ZGFX single-segment max data is 65535 bytes
	if gfxPDUSize > ZGFXMaxSegmentData {
		// Large frame - must use multi-segment format (allocates)
		// Note: Don't decrement pending here - sendH264FrameMultiSegment manages it
		return g.sendH264FrameMultiSegment(h264Data, isKeyframe, frameID, timestamp)
	}

	// ZERO-ALLOCATION PATH: Use pre-allocated frameBuf for small frames
	// Layout: [ZGFX header 2 bytes][GFX PDU]
	totalSize := 2 + gfxPDUSize

	// Check if frame fits in pre-allocated buffer (shouldn't happen since we check segment size first)
	if totalSize > len(g.frameBuf) {
		return g.sendH264FrameMultiSegment(h264Data, isKeyframe, frameID, timestamp)
	}

	// Build directly in pre-allocated buffer
	buf := g.frameBuf[:totalSize]

	// ZGFX single-segment header (2 bytes) - uncompressed passthrough
	buf[0] = 0xE0 // ZGFX_SEGMENTED_SINGLE
	buf[1] = 0x04 // ZGFX_PACKET_COMPR_TYPE_RDP8 (uncompressed)

	// GFX PDU starts at offset 2
	pos := 2

	// START_FRAME (16 bytes)
	binary.LittleEndian.PutUint16(buf[pos:], GFXCmdStartFrame)
	binary.LittleEndian.PutUint16(buf[pos+2:], 0)
	binary.LittleEndian.PutUint32(buf[pos+4:], uint32(startSize))
	binary.LittleEndian.PutUint32(buf[pos+8:], timestamp)
	binary.LittleEndian.PutUint32(buf[pos+12:], frameID)
	pos += startSize

	// WIRETOSURFACE_1 header (25 bytes) + metadata (14 bytes) + H.264 data
	binary.LittleEndian.PutUint16(buf[pos:], GFXCmdWireToSurface1)
	binary.LittleEndian.PutUint16(buf[pos+2:], 0)
	binary.LittleEndian.PutUint32(buf[pos+4:], uint32(wireSize))
	binary.LittleEndian.PutUint16(buf[pos+8:], g.surfaceID)
	binary.LittleEndian.PutUint16(buf[pos+10:], GFXCodecAVC420)
	buf[pos+12] = GFXPixelFormatXRGB
	binary.LittleEndian.PutUint16(buf[pos+13:], 0)        // left
	binary.LittleEndian.PutUint16(buf[pos+15:], 0)        // top
	binary.LittleEndian.PutUint16(buf[pos+17:], g.width)  // right
	binary.LittleEndian.PutUint16(buf[pos+19:], g.height) // bottom
	binary.LittleEndian.PutUint32(buf[pos+21:], uint32(metaSize+len(h264Data)))

	// Build AVC420 metadata inline (14 bytes) - avoids function call overhead
	metaPos := pos + 25
	binary.LittleEndian.PutUint32(buf[metaPos:], 1)           // numRegionRects
	binary.LittleEndian.PutUint16(buf[metaPos+4:], 0)         // left
	binary.LittleEndian.PutUint16(buf[metaPos+6:], 0)         // top
	binary.LittleEndian.PutUint16(buf[metaPos+8:], g.width)   // right
	binary.LittleEndian.PutUint16(buf[metaPos+10:], g.height) // bottom
	// qpVal with progressive flag + qualityVal
	if isKeyframe {
		buf[metaPos+12] = 18 | 0x80 // Lower QP for keyframes
	} else {
		buf[metaPos+12] = 22 | 0x80 // Default QP for P-frames
	}
	buf[metaPos+13] = 100 // Quality value

	// Copy H.264 data (this copy is unavoidable)
	copy(buf[metaPos+14:], h264Data)
	pos += wireSize

	// END_FRAME (12 bytes)
	binary.LittleEndian.PutUint16(buf[pos:], GFXCmdEndFrame)
	binary.LittleEndian.PutUint16(buf[pos+2:], 0)
	binary.LittleEndian.PutUint32(buf[pos+4:], uint32(endSize))
	binary.LittleEndian.PutUint32(buf[pos+8:], frameID)

	// Send ZGFX-wrapped GFX PDU directly to DVC channel
	// Note: buf is a slice of g.frameBuf, no allocation here
	if err := g.channel.SendData(buf); err != nil {
		g.framesPending.Add(-1)
		return err
	}
	return nil
}

// sendH264FrameMultiSegment is used when the GFX PDU exceeds ZGFX single-segment limit (65KB).
// This happens for large H.264 keyframes. Uses ZGFX multi-segment format.
// Builds ZGFX framing directly to eliminate the extra copy through sendGFXData.
// Uses pooled buffers to reduce GC pressure for keyframes.
// Note: framesPending was already incremented by caller.
func (g *GFXChannel) sendH264FrameMultiSegment(h264Data []byte, isKeyframe bool, frameID, timestamp uint32) error {
	meta := g.buildAVC420Metadata(isKeyframe)

	startSize := GFXHeaderSize + GFXStartFrameSize
	wireSize := GFXHeaderSize + GFXWireToSurface1Size + len(meta) + len(h264Data)
	endSize := GFXHeaderSize + GFXEndFrameSize
	gfxPDUSize := startSize + wireSize + endSize

	// Calculate ZGFX multi-segment size directly
	numSegments := (gfxPDUSize + ZGFXMaxSegmentData - 1) / ZGFXMaxSegmentData
	zgfxHeaderSize := 7 // descriptor(1) + segmentCount(2) + uncompressedSize(4)
	zgfxTotalSize := zgfxHeaderSize
	for i := range numSegments {
		segDataSize := ZGFXMaxSegmentData
		remaining := gfxPDUSize - i*ZGFXMaxSegmentData
		if remaining < segDataSize {
			segDataSize = remaining
		}
		zgfxTotalSize += 4 + 1 + segDataSize // segmentSize(4) + flags(1) + data
	}

	// Use single pooled buffer for the entire ZGFX-wrapped output
	poolBuf := gfxLargeBufferPool.Get().(*[]byte)
	if cap(*poolBuf) < zgfxTotalSize {
		*poolBuf = make([]byte, zgfxTotalSize)
	}
	out := (*poolBuf)[:zgfxTotalSize]

	// Build GFX PDU in frameBuf (we hold frameMu). For extremely large frames
	// that exceed frameBuf, fall back to a separate pool allocation.
	var gfxBuf []byte
	var gfxPoolBuf *[]byte
	if gfxPDUSize <= len(g.frameBuf) {
		gfxBuf = g.frameBuf[:gfxPDUSize]
	} else {
		gfxPoolBuf = gfxLargeBufferPool.Get().(*[]byte)
		if cap(*gfxPoolBuf) < gfxPDUSize {
			*gfxPoolBuf = make([]byte, gfxPDUSize)
		}
		gfxBuf = (*gfxPoolBuf)[:gfxPDUSize]
	}
	pos := 0

	// START_FRAME
	binary.LittleEndian.PutUint16(gfxBuf[pos:], GFXCmdStartFrame)
	binary.LittleEndian.PutUint16(gfxBuf[pos+2:], 0)
	binary.LittleEndian.PutUint32(gfxBuf[pos+4:], uint32(startSize))
	binary.LittleEndian.PutUint32(gfxBuf[pos+8:], timestamp)
	binary.LittleEndian.PutUint32(gfxBuf[pos+12:], frameID)
	pos += startSize

	// WIRETOSURFACE_1
	binary.LittleEndian.PutUint16(gfxBuf[pos:], GFXCmdWireToSurface1)
	binary.LittleEndian.PutUint16(gfxBuf[pos+2:], 0)
	binary.LittleEndian.PutUint32(gfxBuf[pos+4:], uint32(wireSize))
	binary.LittleEndian.PutUint16(gfxBuf[pos+8:], g.surfaceID)
	binary.LittleEndian.PutUint16(gfxBuf[pos+10:], GFXCodecAVC420)
	gfxBuf[pos+12] = GFXPixelFormatXRGB
	binary.LittleEndian.PutUint16(gfxBuf[pos+13:], 0)
	binary.LittleEndian.PutUint16(gfxBuf[pos+15:], 0)
	binary.LittleEndian.PutUint16(gfxBuf[pos+17:], g.width)
	binary.LittleEndian.PutUint16(gfxBuf[pos+19:], g.height)
	binary.LittleEndian.PutUint32(gfxBuf[pos+21:], uint32(len(meta)+len(h264Data)))
	copy(gfxBuf[pos+25:], meta)
	copy(gfxBuf[pos+25+len(meta):], h264Data)
	pos += wireSize

	// END_FRAME
	binary.LittleEndian.PutUint16(gfxBuf[pos:], GFXCmdEndFrame)
	binary.LittleEndian.PutUint16(gfxBuf[pos+2:], 0)
	binary.LittleEndian.PutUint32(gfxBuf[pos+4:], uint32(endSize))
	binary.LittleEndian.PutUint32(gfxBuf[pos+8:], frameID)

	// Build ZGFX multi-segment header directly in output buffer
	outPos := 0
	out[outPos] = ZGFXSegmentedMulti
	outPos++
	binary.LittleEndian.PutUint16(out[outPos:outPos+2], uint16(numSegments))
	outPos += 2
	binary.LittleEndian.PutUint32(out[outPos:outPos+4], uint32(gfxPDUSize))
	outPos += 4

	// Build segments from GFX PDU data
	srcPos := 0
	for range numSegments {
		segDataSize := ZGFXMaxSegmentData
		remaining := gfxPDUSize - srcPos
		if remaining < segDataSize {
			segDataSize = remaining
		}
		segTotalSize := 1 + segDataSize // flags(1) + data
		binary.LittleEndian.PutUint32(out[outPos:outPos+4], uint32(segTotalSize))
		outPos += 4
		out[outPos] = ZGFXPacketComprRDP8 // uncompressed
		outPos++
		copy(out[outPos:outPos+segDataSize], gfxBuf[srcPos:srcPos+segDataSize])
		outPos += segDataSize
		srcPos += segDataSize
	}

	// Send directly to DVC channel (no intermediate sendGFXData copy)
	err := g.channel.SendData(out)
	gfxLargeBufferPool.Put(poolBuf)
	if gfxPoolBuf != nil {
		gfxLargeBufferPool.Put(gfxPoolBuf)
	}

	if err != nil {
		g.framesPending.Add(-1)
		return err
	}
	return nil
}

// buildAVC420Metadata builds the RFX_AVC420_METABLOCK structure.
// Uses pre-allocated buffer to avoid allocations.
// See MS-RDPEGFX 2.2.4.4.1 for the specification.
func (g *GFXChannel) buildAVC420Metadata(isKeyframe bool) []byte {
	// RFX_AVC420_METABLOCK structure (MS-RDPEGFX 2.2.4.4.1):
	//   numRegionRects (4 bytes) - UINT32
	//   regionRects (8 bytes each) - RDPGFX_RECT16 array
	//   quantQualityVals (2 bytes each) - RDPGFX_AVC420_QUANT_QUALITY array
	// Total for 1 region: 4 + 8 + 2 = 14 bytes
	//
	// Note: The H.264 data follows immediately after the metadata with NO length prefix.
	// The bitmapDataLength in RDPGFX_WIRE_TO_SURFACE_PDU_1 specifies the total size.

	buf := g.metaBuf[:14]

	// Number of region rectangles = 1
	binary.LittleEndian.PutUint32(buf[0:4], 1)

	// RDPGFX_RECT16 (MS-RDPEGFX 2.2.1.2): left, top, right, bottom as UINT16
	binary.LittleEndian.PutUint16(buf[4:6], 0)          // left
	binary.LittleEndian.PutUint16(buf[6:8], 0)          // top
	binary.LittleEndian.PutUint16(buf[8:10], g.width)   // right
	binary.LittleEndian.PutUint16(buf[10:12], g.height) // bottom

	// RDPGFX_AVC420_QUANT_QUALITY (MS-RDPEGFX 2.2.4.5):
	//   qpVal (1 byte): bits[5:0]=QP, bit[6]=reserved, bit[7]=progressive flag
	//   qualityVal (1 byte): quality value 0-100
	qp := byte(22) // Default QP for P-frames
	if isKeyframe {
		qp = 18 // Lower QP (higher quality) for keyframes
	}
	buf[12] = qp | 0x80 // Set progressive flag (bit 7)
	buf[13] = 100       // Quality value (0-100)

	return buf
}

// SendH264FrameAVC444 sends an H.264 frame using AVC444 format (higher quality).
// luma contains the Y plane H.264 data, chroma contains UV plane data.
// Uses pooled buffers to reduce GC pressure for large frames.
// Thread-safe: uses frameMu to protect shared buffers from concurrent access.
func (g *GFXChannel) SendH264FrameAVC444(luma, chroma []byte, isKeyframe bool) error {
	if !g.ready.Load() || !g.avc444 {
		// Fall back to AVC420 (SendH264Frame handles its own locking)
		return g.SendH264Frame(luma, isKeyframe)
	}

	// Flow control - check pending frames with self-healing (lock-free atomic check)
	if err := g.checkBackpressure(); err != nil {
		return err
	}

	// Protect shared buffers (metaBuf) from concurrent access
	g.frameMu.Lock()
	defer g.frameMu.Unlock()

	frameID := g.frameID.Add(1)
	// Use synthetic timestamp based on frame ID (~30fps timing)
	timestamp := frameID * 33

	g.framesPending.Add(1)

	// Build AVC444 structure
	// cbAvc420EncodedBitstream1 with LC mode in high bits
	lcMode := uint32(0) // Both streams present
	if len(chroma) == 0 {
		lcMode = 1 // Luma only
	}

	// Build metadata for both streams (RFX_AVC420_METABLOCK, 14 bytes each for 1 region)
	lumaMeta := g.buildAVC420Metadata(isKeyframe)

	// Use pre-allocated chroma metadata buffer (14 bytes, built inline)
	var chromaMeta [14]byte
	chromaMetaLen := 0
	if lcMode == 0 {
		// Build chroma metadata inline
		binary.LittleEndian.PutUint32(chromaMeta[0:4], 1)          // numRegionRects
		binary.LittleEndian.PutUint16(chromaMeta[4:6], 0)          // left
		binary.LittleEndian.PutUint16(chromaMeta[6:8], 0)          // top
		binary.LittleEndian.PutUint16(chromaMeta[8:10], g.width)   // right
		binary.LittleEndian.PutUint16(chromaMeta[10:12], g.height) // bottom
		chromaMeta[12] = 22 | 0x80                                 // qpVal with progressive flag
		chromaMeta[13] = 100                                       // qualityVal
		chromaMetaLen = 14
	}

	// Calculate sizes
	avc444DataLen := 4 + len(lumaMeta) + len(luma) + chromaMetaLen + len(chroma)
	startSize := GFXHeaderSize + GFXStartFrameSize
	wireSize := GFXHeaderSize + GFXWireToSurface1Size + avc444DataLen
	endSize := GFXHeaderSize + GFXEndFrameSize
	totalSize := startSize + wireSize + endSize

	// Use pooled buffer for large AVC444 frames
	poolBuf := gfxLargeBufferPool.Get().(*[]byte)
	if cap(*poolBuf) < totalSize {
		*poolBuf = make([]byte, totalSize)
	}
	buf := (*poolBuf)[:totalSize]
	pos := 0

	// START_FRAME
	binary.LittleEndian.PutUint16(buf[pos:pos+2], GFXCmdStartFrame)
	binary.LittleEndian.PutUint16(buf[pos+2:pos+4], 0)
	binary.LittleEndian.PutUint32(buf[pos+4:pos+8], uint32(startSize))
	binary.LittleEndian.PutUint32(buf[pos+8:pos+12], timestamp)
	binary.LittleEndian.PutUint32(buf[pos+12:pos+16], frameID)
	pos += startSize

	// WIRETOSURFACE_1 with AVC444
	binary.LittleEndian.PutUint16(buf[pos:pos+2], GFXCmdWireToSurface1)
	binary.LittleEndian.PutUint16(buf[pos+2:pos+4], 0)
	binary.LittleEndian.PutUint32(buf[pos+4:pos+8], uint32(wireSize))
	binary.LittleEndian.PutUint16(buf[pos+8:pos+10], g.surfaceID)
	binary.LittleEndian.PutUint16(buf[pos+10:pos+12], GFXCodecAVC444)
	buf[pos+12] = GFXPixelFormatXRGB
	binary.LittleEndian.PutUint16(buf[pos+13:pos+15], 0)
	binary.LittleEndian.PutUint16(buf[pos+15:pos+17], 0)
	binary.LittleEndian.PutUint16(buf[pos+17:pos+19], g.width)
	binary.LittleEndian.PutUint16(buf[pos+19:pos+21], g.height)
	binary.LittleEndian.PutUint32(buf[pos+21:pos+25], uint32(avc444DataLen))

	// AVC444 data
	dataPos := pos + 25

	// cbAvc420EncodedBitstream1 with LC mode
	stream1Len := len(lumaMeta) + len(luma)
	binary.LittleEndian.PutUint32(buf[dataPos:dataPos+4], uint32(stream1Len)|(lcMode<<30))
	dataPos += 4

	// Luma stream
	copy(buf[dataPos:], lumaMeta)
	dataPos += len(lumaMeta)
	copy(buf[dataPos:], luma)
	dataPos += len(luma)

	// Chroma stream (if present)
	if lcMode == 0 {
		copy(buf[dataPos:], chromaMeta[:])
		dataPos += chromaMetaLen
		copy(buf[dataPos:], chroma)
	}

	pos = startSize + wireSize

	// END_FRAME
	binary.LittleEndian.PutUint16(buf[pos:pos+2], GFXCmdEndFrame)
	binary.LittleEndian.PutUint16(buf[pos+2:pos+4], 0)
	binary.LittleEndian.PutUint32(buf[pos+4:pos+8], uint32(endSize))
	binary.LittleEndian.PutUint32(buf[pos+8:pos+12], frameID)

	err := g.sendGFXData(buf)

	// Return buffer to pool after sendGFXData copies data
	gfxLargeBufferPool.Put(poolBuf)

	if err != nil {
		// Send failed - decrement pending count to avoid leak
		g.framesPending.Add(-1)
		return err
	}
	return nil
}

// IsReady returns true if the channel is ready to send frames.
// This requires both caps negotiation complete (ready) AND surface initialized.
// The initialized check prevents frames from being sent before the client has
// received and processed CreateSurface/MapSurfaceToOutput commands.
func (g *GFXChannel) IsReady() bool {
	return g.ready.Load() && g.initialized.Load()
}

// SupportsAVC420 returns true if AVC420 (H.264) is supported.
func (g *GFXChannel) SupportsAVC420() bool {
	return g.avc420
}

// SupportsAVC444 returns true if AVC444 (H.264 high quality) is supported.
func (g *GFXChannel) SupportsAVC444() bool {
	return g.avc444
}

// GetPendingFrames returns the number of frames awaiting acknowledgment.
func (g *GFXChannel) GetPendingFrames() int {
	return int(g.framesPending.Load())
}

// CanAcceptFrame returns true if the channel can accept another frame.
// This is a fast check that avoids work when backpressure would cause a drop anyway.
// HOT PATH: Single atomic load, always inlined.
func (g *GFXChannel) CanAcceptFrame() bool {
	return g.ready.Load() && g.avc420 && g.framesPending.Load() < GFXMaxFramesPending
}

// ShouldDropPFrame returns true if P-frames should be dropped due to backpressure.
// This provides adaptive rate limiting for WAN/high-latency connections.
// HOT PATH: Single atomic load.
func (g *GFXChannel) ShouldDropPFrame() bool {
	return g.framesPending.Load() >= GFXBackpressureDropPFrames
}

// ShouldRateLimitPFrame returns true if P-frames should be rate-limited (skip every other).
// This is a softer form of backpressure than ShouldDropPFrame.
// HOT PATH: Single atomic load.
func (g *GFXChannel) ShouldRateLimitPFrame() bool {
	return g.framesPending.Load() >= GFXBackpressureRateLimit
}

// IsConnectionStale returns true if we haven't received frame acks in too long.
// This indicates the client has likely disconnected or frozen.
func (g *GFXChannel) IsConnectionStale() bool {
	lastAck := g.lastAckTime.Load()
	if lastAck == 0 {
		// No acks received yet - not stale (still initializing)
		return false
	}

	elapsed := time.Now().Unix() - lastAck

	// If no ack for more than 30 seconds AND we have significant pending frames, connection is stale
	// High timeout needed for high-latency connections (e.g., Tailscale over internet)
	return elapsed > 30 && g.framesPending.Load() > 16
}

// Close closes the GFX channel.
func (g *GFXChannel) Close() error {
	g.ready.Store(false)
	if g.channel != nil {
		return g.channel.Close()
	}
	return nil
}
