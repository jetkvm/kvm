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

// H264ParamCache caches SPS and PPS NAL units for H.264 stream initialization.
// UVC clients need SPS/PPS at stream start and periodically to decode video.
type H264ParamCache struct {
	mu  sync.RWMutex
	sps []byte // Cached SPS NAL unit (with start code)
	pps []byte // Cached PPS NAL unit (with start code)
}

// NewH264ParamCache creates a new parameter cache.
func NewH264ParamCache() *H264ParamCache {
	return &H264ParamCache{}
}

// UpdateFromFrame extracts and caches SPS/PPS from an H.264 frame.
// Returns true if new SPS or PPS was found.
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
