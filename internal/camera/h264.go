package camera

import (
	"bytes"
	"sync"
)

// H.264 NAL unit types
const (
	NALTypeSlice  = 1  // Non-IDR slice
	NALTypeIDR    = 5  // IDR slice (keyframe)
	NALTypeSEI    = 6  // Supplemental enhancement information
	NALTypeSPS    = 7  // Sequence parameter set
	NALTypePPS    = 8  // Picture parameter set
	NALTypeAUD    = 9  // Access unit delimiter
	NALTypeFiller = 12 // Filler data
)

// Annex B start codes
var (
	startCode3 = []byte{0x00, 0x00, 0x01}
	startCode4 = []byte{0x00, 0x00, 0x00, 0x01}
)

// FrameInfo contains cached analysis results for an H.264 frame.
// Used to avoid multiple splitNALUnits calls per frame (reduces GC pressure).
type FrameInfo struct {
	HasIDR bool // Frame contains IDR NAL unit
	HasSPS bool // Frame contains SPS NAL unit
	HasPPS bool // Frame contains PPS NAL unit
}

// H264ParamCache caches SPS and PPS NAL units for H.264 stream initialization.
// UVC clients need SPS/PPS at stream start and periodically to decode video.
type H264ParamCache struct {
	mu  sync.RWMutex
	sps []byte // Cached SPS NAL unit (with start code)
	pps []byte // Cached PPS NAL unit (with start code)

	// Pre-allocated buffer for prepended frames to reduce GC pressure
	prependBuf []byte
}

// NewH264ParamCache creates a new parameter cache.
func NewH264ParamCache() *H264ParamCache {
	return &H264ParamCache{}
}

// AnalyzeAndUpdate analyzes the frame in a single pass: updates SPS/PPS cache
// and returns frame info. This is the optimized path that avoids multiple
// splitNALUnits calls (reduces GC pressure at 30+fps).
func (c *H264ParamCache) AnalyzeAndUpdate(frame []byte) FrameInfo {
	info := FrameInfo{}
	if len(frame) < 4 {
		return info
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Scan for NAL units in a single pass without allocating slice of slices
	scanNALUnits(frame, func(nalu []byte) {
		if len(nalu) < 1 {
			return
		}

		nalType := nalu[0] & 0x1F

		switch nalType {
		case NALTypeIDR:
			info.HasIDR = true

		case NALTypeSPS:
			info.HasSPS = true
			// Store SPS with 4-byte start code
			newSPS := make([]byte, 4+len(nalu))
			copy(newSPS, startCode4)
			copy(newSPS[4:], nalu)
			if !bytes.Equal(c.sps, newSPS) {
				c.sps = newSPS
			}

		case NALTypePPS:
			info.HasPPS = true
			// Store PPS with 3-byte start code
			newPPS := make([]byte, 3+len(nalu))
			copy(newPPS, startCode3)
			copy(newPPS[3:], nalu)
			if !bytes.Equal(c.pps, newPPS) {
				c.pps = newPPS
			}
		}
	})

	return info
}

// UpdateFromFrame extracts and caches SPS/PPS from an H.264 frame.
// Returns true if new SPS or PPS was found.
// Deprecated: Use AnalyzeAndUpdate for better performance.
func (c *H264ParamCache) UpdateFromFrame(frame []byte) bool {
	nalUnits := splitNALUnits(frame)
	updated := false

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, nalu := range nalUnits {
		if len(nalu) < 1 {
			continue
		}

		// Get NAL type from first byte (5 least significant bits)
		nalType := nalu[0] & 0x1F

		switch nalType {
		case NALTypeSPS:
			// Store SPS with 4-byte start code
			newSPS := make([]byte, 4+len(nalu))
			copy(newSPS, startCode4)
			copy(newSPS[4:], nalu)
			if !bytes.Equal(c.sps, newSPS) {
				c.sps = newSPS
				updated = true
			}

		case NALTypePPS:
			// Store PPS with 3-byte start code (subsequent NALUs use 3-byte)
			newPPS := make([]byte, 3+len(nalu))
			copy(newPPS, startCode3)
			copy(newPPS[3:], nalu)
			if !bytes.Equal(c.pps, newPPS) {
				c.pps = newPPS
				updated = true
			}
		}
	}

	return updated
}

// GetParameters returns cached SPS+PPS as a single buffer.
// Returns nil if SPS or PPS is not yet cached.
func (c *H264ParamCache) GetParameters() []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.sps) == 0 || len(c.pps) == 0 {
		return nil
	}

	// Combine SPS + PPS
	result := make([]byte, len(c.sps)+len(c.pps))
	copy(result, c.sps)
	copy(result[len(c.sps):], c.pps)
	return result
}

// HasParameters returns true if both SPS and PPS are cached.
func (c *H264ParamCache) HasParameters() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.sps) > 0 && len(c.pps) > 0
}

// Clear clears the cached parameters.
func (c *H264ParamCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sps = nil
	c.pps = nil
}

// IsIDRFrame returns true if the frame contains an IDR (keyframe) NAL unit.
func IsIDRFrame(frame []byte) bool {
	nalUnits := splitNALUnits(frame)
	for _, nalu := range nalUnits {
		if len(nalu) > 0 && (nalu[0]&0x1F) == NALTypeIDR {
			return true
		}
	}
	return false
}

// ContainsSPS returns true if the frame contains an SPS NAL unit.
func ContainsSPS(frame []byte) bool {
	nalUnits := splitNALUnits(frame)
	for _, nalu := range nalUnits {
		if len(nalu) > 0 && (nalu[0]&0x1F) == NALTypeSPS {
			return true
		}
	}
	return false
}

// PrependParameters prepends SPS+PPS to a frame if the frame is an IDR
// and doesn't already contain SPS. Returns the original frame if no
// prepending is needed.
// Deprecated: Use PrependParametersWithInfo for better performance.
func (c *H264ParamCache) PrependParameters(frame []byte) []byte {
	// Only prepend to IDR frames that don't already have SPS
	if !IsIDRFrame(frame) || ContainsSPS(frame) {
		return frame
	}

	params := c.GetParameters()
	if params == nil {
		return frame
	}

	// Prepend SPS+PPS to the frame
	result := make([]byte, len(params)+len(frame))
	copy(result, params)
	copy(result[len(params):], frame)
	return result
}

// PrependParametersWithInfo prepends SPS+PPS using pre-analyzed frame info.
// Uses pre-allocated buffer to reduce GC pressure.
// IMPORTANT: The returned slice is only valid until the next call - copy if needed.
func (c *H264ParamCache) PrependParametersWithInfo(frame []byte, info FrameInfo) []byte {
	// Only prepend to IDR frames that don't already have SPS
	if !info.HasIDR || info.HasSPS {
		return frame
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.sps) == 0 || len(c.pps) == 0 {
		return frame
	}

	// Calculate total size needed
	totalSize := len(c.sps) + len(c.pps) + len(frame)

	// Grow pre-allocated buffer if needed
	if cap(c.prependBuf) < totalSize {
		// Allocate with some headroom to avoid frequent resizing
		c.prependBuf = make([]byte, totalSize+4096)
	}

	// Use slice of pre-allocated buffer
	result := c.prependBuf[:totalSize]
	copy(result, c.sps)
	copy(result[len(c.sps):], c.pps)
	copy(result[len(c.sps)+len(c.pps):], frame)
	return result
}

// splitNALUnits splits an Annex B byte stream into individual NAL units.
// Each NAL unit is returned without its start code.
func splitNALUnits(data []byte) [][]byte {
	if len(data) < 4 {
		return nil
	}

	var nalUnits [][]byte
	start := -1

	for i := 0; i < len(data)-2; i++ {
		// Check for 3-byte start code
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			if start >= 0 {
				// Found next start code, extract previous NAL unit
				nalUnits = append(nalUnits, data[start:i])
			}
			// Skip start code (handle 4-byte case too)
			if i > 0 && data[i-1] == 0 {
				start = i + 3
			} else {
				start = i + 3
			}
			i += 2 // Skip past start code
			continue
		}

		// Check for 4-byte start code
		if i < len(data)-3 && data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			if start >= 0 {
				nalUnits = append(nalUnits, data[start:i])
			}
			start = i + 4
			i += 3
			continue
		}
	}

	// Add final NAL unit
	if start >= 0 && start < len(data) {
		nalUnits = append(nalUnits, data[start:])
	}

	return nalUnits
}

// scanNALUnits iterates through NAL units without allocating a slice.
// Uses callback pattern for zero-allocation frame analysis.
func scanNALUnits(data []byte, fn func(nalu []byte)) {
	if len(data) < 4 {
		return
	}

	start := -1

	for i := 0; i < len(data)-2; i++ {
		// Check for 3-byte start code
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			if start >= 0 {
				// Found next start code, process previous NAL unit
				fn(data[start:i])
			}
			// Skip start code (handle 4-byte case too)
			if i > 0 && data[i-1] == 0 {
				start = i + 3
			} else {
				start = i + 3
			}
			i += 2 // Skip past start code
			continue
		}

		// Check for 4-byte start code
		if i < len(data)-3 && data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			if start >= 0 {
				fn(data[start:i])
			}
			start = i + 4
			i += 3
			continue
		}
	}

	// Process final NAL unit
	if start >= 0 && start < len(data) {
		fn(data[start:])
	}
}

// GetFirstNALType returns the type of the first NAL unit in the frame.
// This is a fast O(1) operation that only looks at the first few bytes.
// Returns 0 if no valid NAL unit found.
func GetFirstNALType(data []byte) uint8 {
	if len(data) < 5 {
		return 0
	}

	// Check for 4-byte start code first (more common)
	if data[0] == 0 && data[1] == 0 && data[2] == 0 && data[3] == 1 {
		return data[4] & 0x1F
	}

	// Check for 3-byte start code
	if data[0] == 0 && data[1] == 0 && data[2] == 1 {
		return data[3] & 0x1F
	}

	return 0
}

// QuickFrameInfo quickly analyzes a frame without full NAL scan.
// Only scans first ~200 bytes to find frame type.
// Returns info about first NAL unit type - sufficient for most decisions.
func QuickFrameInfo(data []byte) FrameInfo {
	info := FrameInfo{}
	if len(data) < 5 {
		return info
	}

	// Only scan first 200 bytes - SPS/PPS/IDR NALs start at frame beginning
	scanLimit := len(data)
	if scanLimit > 200 {
		scanLimit = 200
	}

	// Find first NAL unit
	for i := 0; i < scanLimit-4; i++ {
		var nalType uint8
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 && i+4 < scanLimit {
			nalType = data[i+4] & 0x1F
		} else if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 && i+3 < scanLimit {
			nalType = data[i+3] & 0x1F
		} else {
			continue
		}

		switch nalType {
		case NALTypeIDR:
			info.HasIDR = true
		case NALTypeSPS:
			info.HasSPS = true
		case NALTypePPS:
			info.HasPPS = true
		}
	}

	return info
}
