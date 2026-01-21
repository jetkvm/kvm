package channels

import (
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// RDPGFX implements the RDP Graphics Pipeline Extension.
// Optimized for zero-copy H.264 passthrough with minimal allocations.

// RDPGFX channel name.
const GFXChannelName = "Microsoft::Windows::RDS::Graphics"

// RDPGFX command IDs.
const (
	GFXCmdWireToSurface1     = 0x0001
	GFXCmdWireToSurface2     = 0x0002
	GFXCmdDeleteEncodingCtx  = 0x0003
	GFXCmdSolidFill          = 0x0004
	GFXCmdSurfaceToSurface   = 0x0005
	GFXCmdSurfaceToCache     = 0x0006
	GFXCmdCacheToSurface     = 0x0007
	GFXCmdEvictCacheEntry    = 0x0008
	GFXCmdCreateSurface      = 0x0009
	GFXCmdDeleteSurface      = 0x000A
	GFXCmdStartFrame         = 0x000B
	GFXCmdEndFrame           = 0x000C
	GFXCmdFrameAck           = 0x000D
	GFXCmdResetGraphics      = 0x000E
	GFXCmdMapSurfaceToOutput = 0x000F
	GFXCmdCacheImportOffer   = 0x0010
	GFXCmdCacheImportReply   = 0x0011
	GFXCmdCapsAdvertise      = 0x0012
	GFXCmdCapsConfirm        = 0x0013
	GFXCmdMapSurfaceToWindow = 0x0015
	GFXCmdQoEFrameAck        = 0x0016
	GFXCmdMapSurfaceToScaled = 0x0017
)

// RDPGFX codec IDs.
const (
	GFXCodecUncompressed = 0x0000
	GFXCodecRemoteFX     = 0x0003
	GFXCodecClearCodec   = 0x0008
	GFXCodecPlanar       = 0x000A
	GFXCodecAVC420       = 0x000B
	GFXCodecAlpha        = 0x000C
	GFXCodecAVC444       = 0x000E
	GFXCodecAVC444v2     = 0x000F
)

// RDPGFX pixel formats.
const (
	GFXPixelFormatXRGB = 0x20
	GFXPixelFormatARGB = 0x21
)

// RDPGFX capability versions.
const (
	GFXCapsVersion8   = 0x00080004
	GFXCapsVersion81  = 0x00080105
	GFXCapsVersion10  = 0x000A0002
	GFXCapsVersion101 = 0x000A0100
	GFXCapsVersion102 = 0x000A0200
	GFXCapsVersion103 = 0x000A0301
	GFXCapsVersion104 = 0x000A0400
	GFXCapsVersion105 = 0x000A0502
	GFXCapsVersion106 = 0x000A0601
	GFXCapsVersion107 = 0x000A0701
)

// RDPGFX capability flags.
const (
	GFXCapsFlagThinClient    = 0x00000001
	GFXCapsFlagSmallCache    = 0x00000002
	GFXCapsFlagAVC420Enabled = 0x00000010
	GFXCapsFlagAVCDisabled   = 0x00000020
	GFXCapsFlagAVCThinClient = 0x00000040
)

// GFX PDU header size.
const (
	GFXHeaderSize         = 8   // cmdId(2) + flags(2) + pduLength(4)
	GFXStartFrameSize     = 8   // timestamp(4) + frameId(4)
	GFXEndFrameSize       = 4   // frameId(4)
	GFXWireToSurface1Size = 17  // surfaceId(2) + codecId(2) + pixelFormat(1) + rect(8) + bitmapDataLen(4)
	GFXCreateSurfaceSize  = 7   // surfaceId(2) + width(2) + height(2) + pixelFormat(1)
	GFXDeleteSurfaceSize  = 2   // surfaceId(2)
	GFXMapSurfaceSize = 12 // surfaceId(2) + reserved(2) + outputOriginX(4) + outputOriginY(4)
	GFXFrameAckSize   = 12 // queueDepth(4) + frameId(4) + totalDecoded(4)
)

// Maximum values.
const (
	GFXMaxFramesPending = 8 // Maximum frames in flight before backpressure
	GFXDefaultSurfaceID = 1 // Default surface for main display
)

// ZGFX compression constants.
const (
	ZGFXSegmentedSingle  = 0xE0 // Single segment descriptor
	ZGFXSegmentedMulti   = 0xE1 // Multiple segment descriptor
	ZGFXPacketComprRDP8  = 0x04 // RDP 8.0 compression type (uncompressed passthrough)
	ZGFXSegmentedMaxSize = 65535
)

// Common errors.
var (
	ErrGFXNotReady     = errors.New("gfx: channel not ready")
	ErrGFXBackpressure = errors.New("gfx: too many frames pending")
)

// GFXReadyCallback is called when the GFX channel is ready to send frames.
type GFXReadyCallback func(g *GFXChannel)

// GFXChannel implements the RDPGFX channel with optimized H.264 passthrough.
type GFXChannel struct {
	channel *DVCChannel
	manager *DVCManager

	// Callback when channel becomes ready
	onReady GFXReadyCallback

	// Negotiated capabilities
	capsVersion uint32
	capsFlags   uint32
	avc420      bool
	avc444      bool

	// Surface state
	surfaceID uint16
	width     uint16
	height    uint16

	// Frame tracking for flow control
	frameID        atomic.Uint32
	framesPending  atomic.Int32
	lastAckFrameID atomic.Uint32
	totalDecoded   atomic.Uint32
	lastAckTime    atomic.Int32 // Unix timestamp (lower 32 bits) of last ack
	startTime      atomic.Int64 // UnixMilli timestamp when channel became ready

	// Pre-allocated buffers for surface management to avoid GC pressure
	createBuf [GFXHeaderSize + GFXCreateSurfaceSize]byte
	deleteBuf [GFXHeaderSize + GFXDeleteSurfaceSize]byte
	mapBuf    [GFXHeaderSize + GFXMapSurfaceSize]byte // 8+12=20 bytes

	// H.264 metadata buffer (reused for each frame)
	// RFX_AVC420_METABLOCK: numRects(4) + rect(8) + quantQuality(2) = 14 bytes for single region
	metaBuf [14]byte

	// ===== ZERO-ALLOCATION HOT PATH BUFFERS =====
	// Pre-allocated frame buffer for H.264 streaming (sized for 1080p keyframes)
	// Layout: [2-byte ZGFX header][GFX PDU: START_FRAME + WIRETOSURFACE_1 + H264 data + END_FRAME]
	// Max size: 2 + 16 + (25 + 14 + 300KB) + 12 ≈ 320KB for large keyframes
	frameBuf []byte

	// Mutex for send operations (only for surface creation, not frames)
	sendMu sync.Mutex

	ready atomic.Bool
}

// Frame buffer size for zero-allocation hot path.
// Sized for: ZGFX(2) + START_FRAME(16) + WIRETOSURFACE_1(25+14+data) + END_FRAME(12)
// For 1080p at high bitrate: ~300KB keyframes, so allocate 512KB to be safe.
const GFXFrameBufSize = 512 * 1024

// NewGFXChannel creates a new RDPGFX channel.
func NewGFXChannel(manager *DVCManager) *GFXChannel {
	return &GFXChannel{
		manager:   manager,
		surfaceID: GFXDefaultSurfaceID,
		frameBuf:  make([]byte, GFXFrameBufSize), // Pre-allocate for zero-alloc hot path
	}
}


// ZGFX segment size limits.
// FreeRDP's OutputBuffer is 65536 bytes, so uncompressed segment data must fit.
// For single segment: [0xE0][flags][data] -> data can be up to 65535 bytes
// For multi-segment: each segment can have up to 65535 bytes of data
const (
	ZGFXMaxSegmentData = 65535 // Max data bytes per segment (OutputBuffer size - 1 for flags)
)

// sendGFXData sends GFX PDU data wrapped with ZGFX header.
// FreeRDP expects ZGFX-wrapped data (runs zgfx_decompress on received data).
// ZGFX format for uncompressed single segment: [0xE0 descriptor][0x04 flags][raw data]
func (g *GFXChannel) sendGFXData(data []byte) error {
	var wrapped []byte

	if len(data) <= ZGFXMaxSegmentData {
		// Small data: use single-segment format
		// [0xE0 descriptor][0x04 flags][raw data]
		wrapped = make([]byte, 2+len(data))
		wrapped[0] = 0xE0 // ZGFX_SEGMENTED_SINGLE
		wrapped[1] = 0x04 // ZGFX_PACKET_COMPR_TYPE_RDP8 (uncompressed)
		copy(wrapped[2:], data)
	} else {
		// Large data: use multi-segment format
		// [0xE1 descriptor][segmentCount:2][uncompressedSize:4][segments...]
		// Each segment: [segmentSize:4][flags:1][data]
		numSegments := (len(data) + ZGFXMaxSegmentData - 1) / ZGFXMaxSegmentData

		// Calculate total size: header(7) + segments
		totalSize := 7 // descriptor(1) + segmentCount(2) + uncompressedSize(4)
		for i := range numSegments {
			segDataSize := ZGFXMaxSegmentData
			remaining := len(data) - i*ZGFXMaxSegmentData
			if remaining < segDataSize {
				segDataSize = remaining
			}
			totalSize += 4 + 1 + segDataSize // segmentSize(4) + flags(1) + data
		}

		wrapped = make([]byte, totalSize)
		pos := 0

		// Header
		wrapped[pos] = 0xE1 // ZGFX_SEGMENTED_MULTIPART
		pos++
		binary.LittleEndian.PutUint16(wrapped[pos:pos+2], uint16(numSegments))
		pos += 2
		binary.LittleEndian.PutUint32(wrapped[pos:pos+4], uint32(len(data)))
		pos += 4

		// Segments
		srcPos := 0
		for range numSegments {
			segDataSize := ZGFXMaxSegmentData
			remaining := len(data) - srcPos
			if remaining < segDataSize {
				segDataSize = remaining
			}
			segTotalSize := 1 + segDataSize // flags(1) + data

			binary.LittleEndian.PutUint32(wrapped[pos:pos+4], uint32(segTotalSize))
			pos += 4
			wrapped[pos] = 0x04 // ZGFX_PACKET_COMPR_TYPE_RDP8 (uncompressed)
			pos++
			copy(wrapped[pos:pos+segDataSize], data[srcPos:srcPos+segDataSize])
			pos += segDataSize
			srcPos += segDataSize
		}
	}

	return g.channel.SendData(wrapped)
}

// unwrapZGFX unwraps ZGFX-compressed data. Only handles uncompressed passthrough.
func (g *GFXChannel) unwrapZGFX(data []byte) ([]byte, error) {
	if len(data) < 1 {
		return data, nil // Return as-is if too short
	}

	descriptor := data[0]

	// Check if it's a ZGFX wrapped packet
	// Format: [0xE0 descriptor][0x00 flags][raw data]
	// FreeRDP's zgfx_decompress_segment expects the flags byte after descriptor
	if descriptor == ZGFXSegmentedSingle {
		// Single segment: [descriptor][flags][data]
		if len(data) < 2 {
			return nil, errors.New("zgfx: single segment too short")
		}
		flags := data[1]

		// If PACKET_COMPR_TYPE_RDP8 (0x04) is set, it's compressed
		if flags&0x04 != 0 {
			return nil, errors.New("zgfx: compressed data not supported")
		}

		// Return the raw data after descriptor and flags
		return data[2:], nil

	} else if descriptor == ZGFXSegmentedMulti {
		// Multipart segment: [descriptor] [segmentCount:2] [uncompressedSize:4] [segments...]
		if len(data) < 7 {
			return nil, errors.New("zgfx: multipart header too short")
		}

		segmentCount := binary.LittleEndian.Uint16(data[1:3])
		uncompressedSize := binary.LittleEndian.Uint32(data[3:7])

		result := make([]byte, 0, uncompressedSize)
		pos := 7

		for i := uint16(0); i < segmentCount && pos < len(data); i++ {
			if pos+4 > len(data) {
				return nil, errors.New("zgfx: multipart segment size missing")
			}

			segmentSize := binary.LittleEndian.Uint32(data[pos : pos+4])
			pos += 4

			if pos+int(segmentSize) > len(data) {
				return nil, errors.New("zgfx: multipart segment data truncated")
			}

			if segmentSize < 1 {
				continue
			}

			flags := data[pos]
			if flags&0x20 != 0 {
				return nil, errors.New("zgfx: compressed segment not supported")
			}

			// Append segment data (skip flags byte)
			result = append(result, data[pos+1:pos+int(segmentSize)]...)
			pos += int(segmentSize)
		}

		return result, nil
	}

	// Not ZGFX wrapped - could be raw GFX PDU (shouldn't happen but handle gracefully)
	return data, nil
}

// SetReadyCallback sets the callback to be called when the channel is ready.
// This should be called before Open().
func (g *GFXChannel) SetReadyCallback(cb GFXReadyCallback) {
	g.onReady = cb
}

// Open opens the RDPGFX channel.
func (g *GFXChannel) Open() error {
	ch, err := g.manager.CreateChannel(GFXChannelName, g)
	if err != nil {
		return err
	}
	g.channel = ch
	return nil
}

// OnData handles incoming RDPGFX data.
func (g *GFXChannel) OnData(data []byte) error {
	if len(data) < 2 {
		return nil
	}

	// First, unwrap ZGFX if present
	unwrapped, err := g.unwrapZGFX(data)
	if err != nil {
		return err
	}
	data = unwrapped

	if len(data) < GFXHeaderSize {
		return nil
	}

	cmdID := binary.LittleEndian.Uint16(data[0:2])
	_ = binary.LittleEndian.Uint16(data[2:4]) // flags - unused
	_ = binary.LittleEndian.Uint32(data[4:8]) // pduLen - unused

	switch cmdID {
	case GFXCmdCapsAdvertise:
		return g.handleCapsAdvertise(data[GFXHeaderSize:])
	case GFXCmdFrameAck:
		return g.handleFrameAck(data[GFXHeaderSize:])
	case GFXCmdQoEFrameAck:
		return g.handleQoEFrameAck(data[GFXHeaderSize:])
	case GFXCmdCacheImportOffer:
		// Client advertising cache, we don't use caching
		return nil
	default:
		// Unhandled command
	}

	return nil
}

// OnClose handles channel close.
func (g *GFXChannel) OnClose() {
	g.ready.Store(false)
}

// handleCapsAdvertise processes client capability advertisement.
func (g *GFXChannel) handleCapsAdvertise(data []byte) error {
	if len(data) < 2 {
		return nil
	}

	capsCount := binary.LittleEndian.Uint16(data[0:2])
	pos := 2

	// Find the best capability set (prefer AVC444 > AVC420)
	bestVersion := uint32(0)
	bestFlags := uint32(0)

	for i := uint16(0); i < capsCount && pos+8 <= len(data); i++ {
		version := binary.LittleEndian.Uint32(data[pos : pos+4])
		capsLen := binary.LittleEndian.Uint32(data[pos+4 : pos+8])
		pos += 8

		flags := uint32(0)
		if capsLen >= 4 && pos+4 <= len(data) {
			flags = binary.LittleEndian.Uint32(data[pos : pos+4])
		}
		pos += int(capsLen)

		// Check for H.264 support
		if version >= GFXCapsVersion10 && flags&GFXCapsFlagAVCDisabled == 0 {
			// AVC444 capable
			if version > bestVersion {
				bestVersion = version
				bestFlags = flags
			}
		} else if version >= GFXCapsVersion81 && flags&GFXCapsFlagAVC420Enabled != 0 {
			// AVC420 capable
			if version > bestVersion {
				bestVersion = version
				bestFlags = flags
			}
		} else if version > bestVersion {
			bestVersion = version
			bestFlags = flags
		}
	}

	g.capsVersion = bestVersion
	g.capsFlags = bestFlags

	// Determine codec support
	if bestVersion >= GFXCapsVersion10 && bestFlags&GFXCapsFlagAVCDisabled == 0 {
		g.avc444 = true
		g.avc420 = true
	} else if bestVersion >= GFXCapsVersion81 && bestFlags&GFXCapsFlagAVC420Enabled != 0 {
		g.avc420 = true
	}

	// Send capability confirm
	return g.sendCapsConfirm()
}

// sendCapsConfirm sends capability confirmation.
func (g *GFXChannel) sendCapsConfirm() error {
	// Build caps confirm PDU
	// Header(8) + version(4) + capsDataLen(4) + flags(4) = 20 bytes
	buf := make([]byte, 20)

	// Header
	binary.LittleEndian.PutUint16(buf[0:2], GFXCmdCapsConfirm)
	binary.LittleEndian.PutUint16(buf[2:4], 0)  // flags
	binary.LittleEndian.PutUint32(buf[4:8], 20) // pduLength

	// Capability set
	binary.LittleEndian.PutUint32(buf[8:12], g.capsVersion)
	binary.LittleEndian.PutUint32(buf[12:16], 4) // capsDataLen
	binary.LittleEndian.PutUint32(buf[16:20], g.capsFlags)

	if err := g.sendGFXData(buf); err != nil {
		return err
	}

	// Record start time for frame timestamps (wall-clock time in milliseconds)
	g.startTime.Store(time.Now().UnixMilli())
	g.ready.Store(true)

	// Notify that channel is ready
	if g.onReady != nil {
		g.onReady(g)
	}

	return nil
}

// handleFrameAck processes frame acknowledgment.
func (g *GFXChannel) handleFrameAck(data []byte) error {
	if len(data) < GFXFrameAckSize {
		return nil
	}

	queueDepth := binary.LittleEndian.Uint32(data[0:4])
	ackFrameID := binary.LittleEndian.Uint32(data[4:8])
	totalDecoded := binary.LittleEndian.Uint32(data[8:12])

	g.lastAckFrameID.Store(ackFrameID)
	g.totalDecoded.Store(totalDecoded)
	now := time.Now().Unix()
	g.lastAckTime.Store(int32(now)) // Record when we received this ack

	// Calculate pending frames based on frame IDs
	// pending = frames sent - frames acknowledged
	lastSent := g.frameID.Load()
	pending := int32(lastSent) - int32(ackFrameID)
	if pending < 0 {
		pending = 0
	}
	// Also consider client's queue depth if it's higher
	if int32(queueDepth) > pending {
		pending = int32(queueDepth)
	}
	g.framesPending.Store(pending)

	return nil
}

// handleQoEFrameAck processes QoE frame acknowledgment.
// QoE acks include extended timing data but must also update flow control.
func (g *GFXChannel) handleQoEFrameAck(data []byte) error {
	if len(data) >= 4 {
		ackFrameID := binary.LittleEndian.Uint32(data[0:4])
		g.lastAckFrameID.Store(ackFrameID)

		// Update ack time to prevent stale connection detection
		now := time.Now().Unix()
		g.lastAckTime.Store(int32(now))

		// Update pending frames based on frame IDs (same as regular ack)
		lastSent := g.frameID.Load()
		pending := int32(lastSent) - int32(ackFrameID)
		if pending < 0 {
			pending = 0
		}
		g.framesPending.Store(pending)
	}
	return nil
}

// Initialize creates the surface and maps it to output.
func (g *GFXChannel) Initialize(width, height uint16) error {
	if !g.ready.Load() {
		return ErrGFXNotReady
	}

	g.sendMu.Lock()
	defer g.sendMu.Unlock()

	g.width = width
	g.height = height

	// Send reset graphics
	if err := g.sendResetGraphics(width, height); err != nil {
		return err
	}

	// Create surface
	if err := g.sendCreateSurface(g.surfaceID, width, height); err != nil {
		return err
	}

	// Map surface to output
	if err := g.sendMapSurfaceToOutput(g.surfaceID, 0, 0); err != nil {
		return err
	}

	return nil
}

// sendResetGraphics sends a reset graphics command.
// NOTE: FreeRDP requires the total ResetGraphics PDU to be exactly 340 bytes.
// This is RDPGFX_RESET_GRAPHICS_PDU_SIZE in FreeRDP code.
func (g *GFXChannel) sendResetGraphics(width, height uint16) error {
	// ResetGraphics PDU format (MS-RDPEGFX 2.2.2.4):
	// - Header: cmdId(2) + flags(2) + pduLength(4) = 8 bytes
	// - Body: width(4) + height(4) + monitorCount(4) + monitors(20*n) + padding
	// - Total PDU must be exactly 340 bytes (FreeRDP RDPGFX_RESET_GRAPHICS_PDU_SIZE)
	//
	// pduLength in header = total PDU size = 340
	const totalPDUSize = 340                              // RDPGFX_RESET_GRAPHICS_PDU_SIZE
	const bodySize = totalPDUSize - GFXHeaderSize         // 340 - 8 = 332
	const pduLength = totalPDUSize                        // pduLength = total size including header

	buf := make([]byte, totalPDUSize)

	// Header - pduLength is the total PDU size (340)
	binary.LittleEndian.PutUint16(buf[0:2], GFXCmdResetGraphics)
	binary.LittleEndian.PutUint16(buf[2:4], 0)
	binary.LittleEndian.PutUint32(buf[4:8], pduLength) // 340

	// Width, height
	binary.LittleEndian.PutUint32(buf[8:12], uint32(width))
	binary.LittleEndian.PutUint32(buf[12:16], uint32(height))

	// Monitor count = 1
	binary.LittleEndian.PutUint32(buf[16:20], 1)

	// Monitor definition (primary monitor) - MONITOR_DEF structure (20 bytes)
	binary.LittleEndian.PutUint32(buf[20:24], 0)              // left
	binary.LittleEndian.PutUint32(buf[24:28], 0)              // top
	binary.LittleEndian.PutUint32(buf[28:32], uint32(width))  // right
	binary.LittleEndian.PutUint32(buf[32:36], uint32(height)) // bottom
	binary.LittleEndian.PutUint32(buf[36:40], 1)              // flags (primary)

	// Remaining bytes (buf[40:340]) are already zero from make()

	return g.sendGFXData(buf)
}

// sendCreateSurface sends a create surface command.
func (g *GFXChannel) sendCreateSurface(surfaceID, width, height uint16) error {
	buf := g.createBuf[:]

	// Header
	binary.LittleEndian.PutUint16(buf[0:2], GFXCmdCreateSurface)
	binary.LittleEndian.PutUint16(buf[2:4], 0)
	binary.LittleEndian.PutUint32(buf[4:8], GFXHeaderSize+GFXCreateSurfaceSize)

	// Surface definition
	binary.LittleEndian.PutUint16(buf[8:10], surfaceID)
	binary.LittleEndian.PutUint16(buf[10:12], width)
	binary.LittleEndian.PutUint16(buf[12:14], height)
	buf[14] = GFXPixelFormatXRGB

	return g.sendGFXData(buf)
}

// sendMapSurfaceToOutput sends a map surface to output command.
func (g *GFXChannel) sendMapSurfaceToOutput(surfaceID uint16, x, y uint32) error {
	buf := g.mapBuf[:]

	// Header
	binary.LittleEndian.PutUint16(buf[0:2], GFXCmdMapSurfaceToOutput)
	binary.LittleEndian.PutUint16(buf[2:4], 0)
	binary.LittleEndian.PutUint32(buf[4:8], GFXHeaderSize+GFXMapSurfaceSize)

	// Mapping - coordinates are 4 bytes each per MS-RDPEGFX
	binary.LittleEndian.PutUint16(buf[8:10], surfaceID)
	binary.LittleEndian.PutUint16(buf[10:12], 0)  // reserved
	binary.LittleEndian.PutUint32(buf[12:16], x)  // outputOriginX (4 bytes)
	binary.LittleEndian.PutUint32(buf[16:20], y)  // outputOriginY (4 bytes)

	return g.sendGFXData(buf)
}

// UpdateResolution handles resolution change.
func (g *GFXChannel) UpdateResolution(width, height uint16) error {
	if !g.ready.Load() {
		return ErrGFXNotReady
	}

	if width == g.width && height == g.height {
		return nil
	}

	g.sendMu.Lock()
	defer g.sendMu.Unlock()

	// Delete old surface
	if err := g.sendDeleteSurface(g.surfaceID); err != nil {
		return err
	}

	g.width = width
	g.height = height

	// Create new surface
	if err := g.sendCreateSurface(g.surfaceID, width, height); err != nil {
		return err
	}

	// Remap
	return g.sendMapSurfaceToOutput(g.surfaceID, 0, 0)
}

// sendDeleteSurface sends a delete surface command.
func (g *GFXChannel) sendDeleteSurface(surfaceID uint16) error {
	buf := g.deleteBuf[:]

	// Header
	binary.LittleEndian.PutUint16(buf[0:2], GFXCmdDeleteSurface)
	binary.LittleEndian.PutUint16(buf[2:4], 0)
	binary.LittleEndian.PutUint32(buf[4:8], GFXHeaderSize+GFXDeleteSurfaceSize)

	// Surface ID
	binary.LittleEndian.PutUint16(buf[8:10], surfaceID)

	return g.sendGFXData(buf)
}

// ErrGFXNoCodec is returned when no H.264 codec is available.
var ErrGFXNoCodec = errors.New("gfx: no H.264 codec available")

// SendH264Frame sends an H.264 frame with zero-allocation optimization.
// The h264Data should contain raw H.264 NAL units.
// Returns ErrGFXBackpressure if too many frames are pending.
// Returns ErrGFXNoCodec if client doesn't support AVC420/AVC444.
//
// HOT PATH: This function is called for every video frame (~30-60 fps).
// It uses pre-allocated buffers to achieve zero heap allocations for small frames.
func (g *GFXChannel) SendH264Frame(h264Data []byte, isKeyframe bool) error {
	if !g.ready.Load() {
		return ErrGFXNotReady
	}

	// Check if client supports AVC420 codec
	if !g.avc420 {
		return ErrGFXNoCodec
	}

	// Flow control - check pending frames (lock-free atomic check)
	pending := g.framesPending.Load()
	if pending >= GFXMaxFramesPending {
		return ErrGFXBackpressure
	}

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
	binary.LittleEndian.PutUint32(buf[metaPos:], 1)          // numRegionRects
	binary.LittleEndian.PutUint16(buf[metaPos+4:], 0)        // left
	binary.LittleEndian.PutUint16(buf[metaPos+6:], 0)        // top
	binary.LittleEndian.PutUint16(buf[metaPos+8:], g.width)  // right
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
// Note: framesPending was already incremented by caller.
func (g *GFXChannel) sendH264FrameMultiSegment(h264Data []byte, isKeyframe bool, frameID, timestamp uint32) error {
	meta := g.buildAVC420Metadata(isKeyframe)

	startSize := GFXHeaderSize + GFXStartFrameSize
	wireSize := GFXHeaderSize + GFXWireToSurface1Size + len(meta) + len(h264Data)
	endSize := GFXHeaderSize + GFXEndFrameSize
	gfxPDUSize := startSize + wireSize + endSize

	// Build GFX PDU
	gfxBuf := make([]byte, gfxPDUSize)
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

	// Wrap with ZGFX multi-segment and send
	if err := g.sendGFXData(gfxBuf); err != nil {
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
func (g *GFXChannel) SendH264FrameAVC444(luma, chroma []byte, isKeyframe bool) error {
	if !g.ready.Load() || !g.avc444 {
		// Fall back to AVC420
		return g.SendH264Frame(luma, isKeyframe)
	}

	if g.framesPending.Load() >= GFXMaxFramesPending {
		return ErrGFXBackpressure
	}

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

	var chromaMeta []byte
	if lcMode == 0 {
		// Build chroma metadata inline (same structure as luma but always 14 bytes)
		chromaMeta = make([]byte, 14)
		binary.LittleEndian.PutUint32(chromaMeta[0:4], 1)    // numRegionRects
		binary.LittleEndian.PutUint16(chromaMeta[4:6], 0)    // left
		binary.LittleEndian.PutUint16(chromaMeta[6:8], 0)    // top
		binary.LittleEndian.PutUint16(chromaMeta[8:10], g.width)  // right
		binary.LittleEndian.PutUint16(chromaMeta[10:12], g.height) // bottom
		chromaMeta[12] = 22 | 0x80 // qpVal with progressive flag
		chromaMeta[13] = 100       // qualityVal
	}

	// Calculate sizes
	avc444DataLen := 4 + len(lumaMeta) + len(luma) + len(chromaMeta) + len(chroma)
	startSize := GFXHeaderSize + GFXStartFrameSize
	wireSize := GFXHeaderSize + GFXWireToSurface1Size + avc444DataLen
	endSize := GFXHeaderSize + GFXEndFrameSize
	totalSize := startSize + wireSize + endSize

	buf := make([]byte, totalSize)
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
		copy(buf[dataPos:], chromaMeta)
		dataPos += len(chromaMeta)
		copy(buf[dataPos:], chroma)
	}

	pos = startSize + wireSize

	// END_FRAME
	binary.LittleEndian.PutUint16(buf[pos:pos+2], GFXCmdEndFrame)
	binary.LittleEndian.PutUint16(buf[pos+2:pos+4], 0)
	binary.LittleEndian.PutUint32(buf[pos+4:pos+8], uint32(endSize))
	binary.LittleEndian.PutUint32(buf[pos+8:pos+12], frameID)

	if err := g.sendGFXData(buf); err != nil {
		// Send failed - decrement pending count to avoid leak
		g.framesPending.Add(-1)
		return err
	}
	return nil
}

// IsReady returns true if the channel is ready to send frames.
func (g *GFXChannel) IsReady() bool {
	return g.ready.Load()
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

// IsConnectionStale returns true if we haven't received frame acks in too long.
// This indicates the client has likely disconnected or frozen.
func (g *GFXChannel) IsConnectionStale() bool {
	lastAck := g.lastAckTime.Load()
	if lastAck == 0 {
		// No acks received yet - not stale (still initializing)
		return false
	}

	now := int32(time.Now().Unix())
	elapsed := now - lastAck

	// If no ack for more than 5 seconds AND we have pending frames, connection is stale
	return elapsed > 5 && g.framesPending.Load() > 2
}

// Close closes the GFX channel.
func (g *GFXChannel) Close() error {
	g.ready.Store(false)
	if g.channel != nil {
		return g.channel.Close()
	}
	return nil
}
