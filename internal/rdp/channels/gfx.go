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
	GFXHeaderSize         = 8  // cmdId(2) + flags(2) + pduLength(4)
	GFXStartFrameSize     = 8  // timestamp(4) + frameId(4)
	GFXEndFrameSize       = 4  // frameId(4)
	GFXWireToSurface1Size = 17 // surfaceId(2) + codecId(2) + pixelFormat(1) + rect(8) + bitmapDataLen(4)
	GFXCreateSurfaceSize  = 7  // surfaceId(2) + width(2) + height(2) + pixelFormat(1)
	GFXDeleteSurfaceSize  = 2  // surfaceId(2)
	GFXMapSurfaceSize     = 12 // surfaceId(2) + reserved(2) + outputOriginX(4) + outputOriginY(4)
	GFXFrameAckSize       = 12 // queueDepth(4) + frameId(4) + totalDecoded(4)
)

// Maximum values.
const (
	GFXMaxFramesPending = 16 // Maximum frames in flight before backpressure (~266ms at 60fps)
	GFXDefaultSurfaceID = 1  // Default surface for main display
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

// gfxLargeBufferPool reduces GC pressure for large keyframe allocations.
// Used when frames exceed the pre-allocated frameBuf (>65KB, needing multi-segment ZGFX).
// Sized to accommodate typical 1080p keyframes with ZGFX overhead.
var gfxLargeBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, GFXFrameBufSize)
		return &buf
	},
}

// GFXReadyCallback is called when the GFX channel is ready to send frames.
type GFXReadyCallback func(g *GFXChannel)

// GFXLogFunc is a logging callback for GFX channel events.
type GFXLogFunc func(msg string, args ...interface{})

// GFXChannel implements the RDPGFX channel with optimized H.264 passthrough.
type GFXChannel struct {
	channel *DVCChannel
	manager *DVCManager

	// Callback when channel becomes ready
	onReady GFXReadyCallback

	// Logger callback
	logger GFXLogFunc

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

	// Mutex for surface operations (creation, deletion, resolution changes)
	sendMu sync.Mutex

	// Mutex for frame operations (protects frameBuf and metaBuf from concurrent access)
	frameMu sync.Mutex

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

// SetLogger sets the logging callback for debugging.
func (g *GFXChannel) SetLogger(logger GFXLogFunc) {
	g.logger = logger
}

// log writes a log message if logger is set.
func (g *GFXChannel) log(msg string, args ...interface{}) {
	if g.logger != nil {
		g.logger(msg, args...)
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
// Uses pooled buffers for large keyframes to reduce GC pressure.
func (g *GFXChannel) sendGFXData(data []byte) error {
	var wrapped []byte
	var poolBuf *[]byte // Track if we're using a pooled buffer

	if len(data) <= ZGFXMaxSegmentData {
		// Small data: use single-segment format
		// [0xE0 descriptor][0x04 flags][raw data]
		wrapped = make([]byte, 2+len(data))
		wrapped[0] = 0xE0 // ZGFX_SEGMENTED_SINGLE
		wrapped[1] = 0x04 // ZGFX_PACKET_COMPR_TYPE_RDP8 (uncompressed)
		copy(wrapped[2:], data)
	} else {
		// Large data: use multi-segment format with pooled buffer
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

		// Use pooled buffer for large ZGFX multi-segment wrapping
		poolBuf = gfxLargeBufferPool.Get().(*[]byte)
		if cap(*poolBuf) < totalSize {
			// Pool buffer too small, allocate a larger one
			*poolBuf = make([]byte, totalSize)
		}
		wrapped = (*poolBuf)[:totalSize]
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

	err := g.channel.SendData(wrapped)

	// Return buffer to pool if we used one (after SendData copies data)
	if poolBuf != nil {
		gfxLargeBufferPool.Put(poolBuf)
	}

	return err
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

// gfxVersionNames maps GFX capability versions to human-readable names.
var gfxVersionNames = map[uint32]string{
	GFXCapsVersion8:   "8.0",
	GFXCapsVersion81:  "8.1",
	GFXCapsVersion10:  "10.0",
	GFXCapsVersion101: "10.1",
	GFXCapsVersion102: "10.2",
	GFXCapsVersion103: "10.3",
	GFXCapsVersion104: "10.4",
	GFXCapsVersion105: "10.5",
	GFXCapsVersion106: "10.6",
	GFXCapsVersion107: "10.7",
}

// gfxVersionString returns the human-readable name for a GFX capability version.
func gfxVersionString(version uint32) string {
	if name, ok := gfxVersionNames[version]; ok {
		return name
	}
	return "unknown"
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

	// Log all capabilities for debugging
	g.log("RDPGFX: CapsAdvertise received, capsCount=%d", capsCount)

	for i := uint16(0); i < capsCount && pos+8 <= len(data); i++ {
		version := binary.LittleEndian.Uint32(data[pos : pos+4])
		capsLen := binary.LittleEndian.Uint32(data[pos+4 : pos+8])
		pos += 8

		flags := uint32(0)
		if capsLen >= 4 && pos+4 <= len(data) {
			flags = binary.LittleEndian.Uint32(data[pos : pos+4])
		}
		pos += int(capsLen)

		g.log("RDPGFX: Cap[%d] version=0x%08X (%s) flags=0x%08X capsLen=%d", i, version, gfxVersionString(version), flags, capsLen)
		g.log("RDPGFX: Cap[%d] ThinClient=%v SmallCache=%v AVC420Enabled=%v AVCDisabled=%v",
			i, flags&GFXCapsFlagThinClient != 0, flags&GFXCapsFlagSmallCache != 0,
			flags&GFXCapsFlagAVC420Enabled != 0, flags&GFXCapsFlagAVCDisabled != 0)

		// Check for H.264 support
		if version >= GFXCapsVersion10 && flags&GFXCapsFlagAVCDisabled == 0 {
			// AVC444 capable
			g.log("RDPGFX: Cap[%d] -> AVC444 capable (v10+ without AVC_DISABLED)", i)
			if version > bestVersion {
				bestVersion = version
				bestFlags = flags
			}
		} else if version >= GFXCapsVersion81 && flags&GFXCapsFlagAVC420Enabled != 0 {
			// AVC420 capable
			g.log("RDPGFX: Cap[%d] -> AVC420 capable (v8.1+ with AVC420_ENABLED)", i)
			if version > bestVersion {
				bestVersion = version
				bestFlags = flags
			}
		} else if version > bestVersion {
			g.log("RDPGFX: Cap[%d] -> No AVC support (version too old or AVC disabled)", i)
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

	g.log("RDPGFX: Selected version=0x%08X flags=0x%08X AVC420=%v AVC444=%v", bestVersion, bestFlags, g.avc420, g.avc444)

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
