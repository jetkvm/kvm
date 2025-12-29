package camera

import (
	"bytes"
	"sync"
	"sync/atomic"
)

const (
	NALTypeIDR = 5
	NALTypeSPS = 7
	NALTypePPS = 8
)

var (
	startCode3 = []byte{0x00, 0x00, 0x01}
	startCode4 = []byte{0x00, 0x00, 0x00, 0x01}
)

type FrameInfo struct {
	HasIDR bool
	HasSPS bool
	HasPPS bool
}

type H264ParamCache struct {
	mu         sync.Mutex // Protects sps, pps, prependBuf writes
	sps        []byte
	pps        []byte
	prependBuf []byte
	hasParams  atomic.Bool // Lock-free hot path check for HasParameters()
}

func NewH264ParamCache() *H264ParamCache { return &H264ParamCache{} }

func (c *H264ParamCache) AnalyzeAndUpdate(frame []byte) FrameInfo {
	info := FrameInfo{}
	if len(frame) < 4 {
		return info
	}

	c.mu.Lock()

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
			spsLen := 4 + len(nalu)
			if len(c.sps) == spsLen &&
				c.sps[0] == 0 && c.sps[1] == 0 && c.sps[2] == 0 && c.sps[3] == 1 &&
				bytes.Equal(c.sps[4:], nalu) {
				break
			}
			newSPS := make([]byte, spsLen)
			copy(newSPS, startCode4)
			copy(newSPS[4:], nalu)
			c.sps = newSPS

		case NALTypePPS:
			info.HasPPS = true
			ppsLen := 3 + len(nalu)
			if len(c.pps) == ppsLen &&
				c.pps[0] == 0 && c.pps[1] == 0 && c.pps[2] == 1 &&
				bytes.Equal(c.pps[3:], nalu) {
				break
			}
			newPPS := make([]byte, ppsLen)
			copy(newPPS, startCode3)
			copy(newPPS[3:], nalu)
			c.pps = newPPS
		}
	})

	// Update atomic flag for lock-free hot path check
	c.hasParams.Store(len(c.sps) > 0 && len(c.pps) > 0)
	c.mu.Unlock()

	return info
}

// HasParameters returns true if both SPS and PPS are cached (lock-free hot path).
func (c *H264ParamCache) HasParameters() bool {
	return c.hasParams.Load()
}

func (c *H264ParamCache) Clear() {
	c.mu.Lock()
	c.sps = nil
	c.pps = nil
	c.hasParams.Store(false)
	c.mu.Unlock()
}

// PrependParametersWithInfo prepends SPS/PPS to IDR frames lacking them.
// Returns a slice aliasing an internal buffer - only valid until next call.
func (c *H264ParamCache) PrependParametersWithInfo(frame []byte, info FrameInfo) []byte {
	if !info.HasIDR || info.HasSPS {
		return frame
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.sps) == 0 || len(c.pps) == 0 {
		return frame
	}

	totalSize := len(c.sps) + len(c.pps) + len(frame)

	if cap(c.prependBuf) < totalSize {
		c.prependBuf = make([]byte, totalSize+4096)
	}

	result := c.prependBuf[:totalSize]
	copy(result, c.sps)
	copy(result[len(c.sps):], c.pps)
	copy(result[len(c.sps)+len(c.pps):], frame)
	return result
}

func scanNALUnits(data []byte, fn func(nalu []byte)) {
	if len(data) < 4 {
		return
	}

	start := -1

	for i := 0; i < len(data)-2; i++ {
		if i < len(data)-3 && data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			if start >= 0 {
				fn(data[start:i])
			}
			start = i + 4
			i += 3
			continue
		}

		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			if start >= 0 {
				fn(data[start:i])
			}
			start = i + 3
			i += 2
			continue
		}
	}

	if start >= 0 && start < len(data) {
		fn(data[start:])
	}
}

func GetFirstNALType(data []byte) uint8 {
	if len(data) < 5 {
		return 0
	}

	if data[0] == 0 && data[1] == 0 && data[2] == 0 && data[3] == 1 {
		return data[4] & 0x1F
	}

	if data[0] == 0 && data[1] == 0 && data[2] == 1 {
		return data[3] & 0x1F
	}

	return 0
}

func QuickFrameInfo(data []byte) FrameInfo {
	info := FrameInfo{}
	if len(data) < 5 {
		return info
	}

	const maxScanForNAL = 32
	scanLimit := len(data)
	if scanLimit > maxScanForNAL {
		scanLimit = maxScanForNAL
	}

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
