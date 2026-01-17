package vnc

import (
	"fmt"
	"net"
	"time"
)

// SendJPEGFrameDirect attempts to send a JPEG frame to the client.
// Returns true if frame was sent, false if not needed or failed.
// If sending fails due to connection error, closes the connection.
func (c *Connection) SendJPEGFrameDirect(frame []byte) bool {
	if c.closed.Load() {
		return false
	}

	if !c.needsJPEGEncoder.Load() {
		return false
	}

	if !c.frameRequested.CompareAndSwap(true, false) {
		// Frame dropped - client hasn't requested a new frame yet
		c.framesDropped.Add(1)
		c.intervalDropped.Add(1)
		return false
	}

	if err := c.sendFrameUpdate(frame); err != nil {
		c.server.deps.Logger.Debug().Err(err).Str("remote", c.conn.RemoteAddr().String()).Msg("failed to send JPEG frame, closing connection")
		c.Close() // Close dead connection to trigger cleanup
		return false
	}

	c.framesSent.Add(1)
	c.intervalSent.Add(1)

	// Log frame stats periodically - check frame count first to avoid time.Now() syscall
	// Only check time every ~60 frames (at 60fps = ~1 second between checks)
	totalSent := c.framesSent.Load()
	if totalSent%60 == 0 {
		// Use monotonic elapsed time since connection start (avoids Y2038 issue)
		elapsed := int32(time.Since(c.startTime).Seconds()) //nolint:gosec // Intentional int32 for 32-bit ARM
		lastLog := c.lastFrameLog.Load()
		if elapsed-lastLog >= int32(frameStatsIntervalSeconds) && c.lastFrameLog.CompareAndSwap(lastLog, elapsed) {
			// Only log if debug is enabled to avoid allocation
			if c.server.deps.Logger.Debug().Enabled() {
				// Cumulative stats
				totalDropped := c.framesDropped.Load()
				total := totalSent + totalDropped
				var dropRate float64
				if total > 0 {
					dropRate = float64(totalDropped) * 100 / float64(total)
				}

				// Per-interval stats (reset after reading for next interval)
				intervalSent := c.intervalSent.Swap(0)
				intervalDropped := c.intervalDropped.Swap(0)
				intervalDuration := elapsed - lastLog
				if intervalDuration < 1 {
					intervalDuration = 1
				}
				fps := float64(intervalSent) / float64(intervalDuration)

				c.server.deps.Logger.Debug().
					Float64("fps", fps).
					Int("intervalSent", int(intervalSent)).
					Int("intervalDropped", int(intervalDropped)).
					Int("totalSent", int(totalSent)).
					Int("totalDropped", int(totalDropped)).
					Float64("dropRate", dropRate).
					Str("remote", c.conn.RemoteAddr().String()).
					Msg("VNC frame stats")
			}
		}
	}

	return true
}

// sendFrameUpdate sends a single JPEG frame to the client.
func (c *Connection) sendFrameUpdate(jpegData []byte) error {
	// Validate JPEG data (must start with SOI marker)
	if len(jpegData) < 2 || jpegData[0] != jpegSOIByte0 || jpegData[1] != jpegSOIByte1 {
		c.server.deps.Logger.Warn().Int("len", len(jpegData)).Msg("invalid JPEG data from encoder - frame dropped")
		return nil
	}

	width, height := c.GetResolution()

	// Build header in pre-allocated buffer (zero allocations)
	// Format: 16 bytes RFB header + 1 byte tight ctrl + 1-3 bytes length
	header := &c.frameHeaderBuf
	header[0] = byte(msgFramebufferUpdate)
	header[1] = 0 // padding
	header[2] = 0 // num rectangles high
	header[3] = 1 // num rectangles low
	header[4] = 0 // x high
	header[5] = 0 // x low
	header[6] = 0 // y high
	header[7] = 0 // y low
	header[8] = byte(width >> 8)
	header[9] = byte(width)
	header[10] = byte(height >> 8)
	header[11] = byte(height)
	header[12] = 0 // encoding type high bytes
	header[13] = 0
	header[14] = 0
	header[15] = byte(encodingTight) // encoding type low byte
	header[16] = tightJPEG           // Tight JPEG compression control

	// Tight compact length encoding (inline for zero overhead)
	jpegLen := len(jpegData)
	var headerLen int
	if jpegLen < tightLen1Byte {
		header[17] = byte(jpegLen)
		headerLen = 18
	} else if jpegLen < tightLen2Byte {
		header[17] = byte(jpegLen&0x7F) | 0x80
		header[18] = byte(jpegLen >> 7)
		headerLen = 19
	} else {
		header[17] = byte(jpegLen&0x7F) | 0x80
		header[18] = byte((jpegLen>>7)&0x7F) | 0x80
		header[19] = byte(jpegLen >> 14)
		headerLen = 20
	}

	// Use writev (net.Buffers) for zero-copy send of header + JPEG data
	// This avoids copying ~100KB JPEG data into a buffer
	// Note: net.Buffers.WriteTo consumes the slice, so we create it locally
	// The small slice header allocation (24 bytes) is acceptable vs complexity of reuse
	bufs := net.Buffers{header[:headerLen], jpegData}

	// Set write deadline - no need to clear it afterwards since:
	// 1. The next frame send will update the deadline anyway (at 60fps = 16ms)
	// 2. Clearing requires an extra syscall (setsockopt) per frame
	// 3. Write and read deadlines are independent in Go's net package
	if err := c.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}
	_, err := bufs.WriteTo(c.conn)

	if err != nil {
		return fmt.Errorf("failed to send frame update: %w", err)
	}

	return nil
}
