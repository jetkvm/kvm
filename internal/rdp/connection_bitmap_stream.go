package rdp

import (
	"fmt"
	"runtime/debug"
	"time"
)

// Bitmap streaming via MS-RDPBCGR (non-RDPGFX fallback for older clients).

// startBitmapStreaming starts streaming bitmap updates when RDPGFX is not available.
// This uses RGA hardware YUV→BGRX conversion for maximum performance.
// The caller must have stored the JPEG channel in c.jpegChan before calling.
func (c *Connection) startBitmapStreaming() {
	c.server.deps.Logger.Info().Msg("RDP: starting bitmap streaming")

	// Start video capture if not already started
	if c.server.deps.Video != nil {
		if err := c.server.deps.Video.StartVideo(); err != nil {
			c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to start video capture")
		}

		// Start raw frame encoder (outputs YUV422, Go converts to BGRX)
		if err := c.server.deps.Video.StartRGBEncoder(); err != nil {
			c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to start raw frame encoder, falling back to JPEG")
			// Fall back to JPEG if raw frames fail
			if err := c.server.deps.Video.StartJPEGEncoder(50); err != nil {
				c.server.deps.Logger.Warn().Err(err).Msg("RDP: failed to start JPEG encoder")
			} else {
				c.activeEncoder = bitmapEncoderJPEG
				c.server.deps.Logger.Info().Msg("RDP: using JPEG fallback for bitmap mode")
				c.startJPEGBitmapStreaming()
				return
			}
		} else {
			c.activeEncoder = bitmapEncoderRGB
			c.server.deps.Logger.Info().Msg("RDP: RGB encoder started for bitmap mode")
		}
	}

	// RGB path taken - unsubscribe unused JPEG channel to prevent resource leak
	if c.jpegChan != nil && c.server.deps.Video != nil {
		c.server.deps.Video.UnsubscribeJPEG(c.jpegChan)
		c.jpegChan = nil
	}

	// Subscribe to RGB frames and store for cleanup
	c.rgbChan = c.server.deps.Video.SubscribeRGB()
	if c.rgbChan == nil {
		c.server.deps.Logger.Warn().Msg("RDP: failed to subscribe to RGB frames, video streaming disabled")
		return
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.server.deps.Logger.Error().
					Interface("panic", r).
					Str("stack", string(debug.Stack())).
					Str("remote", c.RemoteAddr()).
					Msg("RDP: RGB bitmap streaming goroutine panicked")
			}
			// Cleanup subscription on exit
			if c.rgbChan != nil && c.server.deps.Video != nil {
				c.server.deps.Video.UnsubscribeRGB(c.rgbChan)
				c.rgbChan = nil
			}
		}()

		// Send frames as they arrive - no artificial rate limiting
		// Native code produces frames at up to 60fps
		frameCount := 0
		startTime := time.Now()

		for {
			select {
			case <-c.stopChan:
				elapsed := time.Since(startTime).Seconds()
				fps := float64(frameCount) / elapsed
				c.server.deps.Logger.Debug().Int("framesSent", frameCount).Float64("avgFps", fps).Msg("RDP: RGB bitmap streaming stopped")
				return
			case frame := <-c.rgbChan:
				if c.closed.Load() {
					continue
				}

				if frameCount == 0 {
					formatName := "YUV422"
					if frame.Format == RGBFrameFormatBGRX {
						formatName = "BGRX (RGA hardware)"
					}
					c.server.deps.Logger.Info().
						Int("frameSize", len(frame.Data)).
						Uint32("width", frame.Width).
						Uint32("height", frame.Height).
						Str("format", formatName).
						Msg("RDP: first bitmap frame received")
				}

				var bgrxData []byte
				var bgrxPoolPtr *[]byte // non-nil only for software-converted buffers

				if frame.Format == RGBFrameFormatBGRX {
					// RGA hardware already converted to BGRX - use directly
					bgrxData = frame.Data
				} else {
					// Software fallback: convert YUV422 YUYV to BGRX
					bgrxData, bgrxPoolPtr = convertYUV422ToBGRX(frame.Data, int(frame.Width), int(frame.Height))
				}

				if err := c.SendRGBBitmapUpdate(bgrxData, int(frame.Width), int(frame.Height)); err != nil {
					c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to send RGB bitmap update")
				} else {
					frameCount++
					if frameCount == 1 || frameCount%100 == 0 {
						elapsed := time.Since(startTime).Seconds()
						fps := float64(frameCount) / elapsed
						hwAccel := frame.Format == RGBFrameFormatBGRX
						c.server.deps.Logger.Debug().Int("frameCount", frameCount).Float64("avgFps", fps).Bool("hwAccel", hwAccel).Msg("RDP: RGB bitmap update sent")
					}
				}
				// Return buffer to pool only if we allocated it (software conversion)
				if bgrxPoolPtr != nil {
					releaseBGRXBuffer(bgrxPoolPtr)
				}
				// Release the native frame buffer back to the pool
				frame.Release()
			}
		}
	}()
}

// startJPEGBitmapStreaming is the fallback for when RGA is not available.
// Uses c.jpegChan which must be set before calling.
func (c *Connection) startJPEGBitmapStreaming() {
	if c.jpegChan == nil {
		c.server.deps.Logger.Warn().Msg("RDP: JPEG channel is nil, cannot start JPEG streaming")
		return
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.server.deps.Logger.Error().
					Interface("panic", r).
					Str("stack", string(debug.Stack())).
					Str("remote", c.RemoteAddr()).
					Msg("RDP: JPEG bitmap streaming goroutine panicked")
			}
			// Cleanup subscription on exit
			if c.jpegChan != nil && c.server.deps.Video != nil {
				c.server.deps.Video.UnsubscribeJPEG(c.jpegChan)
				c.jpegChan = nil
			}
		}()

		frameCount := 0
		startTime := time.Now()

		for {
			select {
			case <-c.stopChan:
				elapsed := time.Since(startTime).Seconds()
				fps := float64(frameCount) / elapsed
				c.server.deps.Logger.Debug().Int("framesSent", frameCount).Float64("avgFps", fps).Msg("RDP: JPEG bitmap streaming stopped")
				return
			case frame := <-c.jpegChan:
				if c.closed.Load() {
					continue
				}

				if err := c.SendBitmapUpdate(frame); err != nil {
					c.server.deps.Logger.Debug().Err(err).Msg("RDP: failed to send JPEG bitmap update")
				} else {
					frameCount++
					if frameCount%100 == 0 {
						elapsed := time.Since(startTime).Seconds()
						fps := float64(frameCount) / elapsed
						c.server.deps.Logger.Debug().Int("frameCount", frameCount).Float64("avgFps", fps).Msg("RDP: JPEG bitmap update sent")
					}
				}
			}
		}
	}()
}

// SendRGBBitmapUpdate sends a raw BGRX frame as RDP bitmap updates.
// This is the fast path - no JPEG decode needed, just tile and send.
// The data is expected in BGRX format (4 bytes per pixel, top-down).
//
// Memory optimization: sends each tile immediately using a pooled buffer,
// avoiding per-tile allocations that caused OOM on memory-constrained devices.
func (c *Connection) SendRGBBitmapUpdate(bgrxData []byte, width, height int) error {
	if c.closed.Load() {
		return nil
	}

	// Verify data size
	expectedSize := width * height * 4
	if len(bgrxData) < expectedSize {
		return fmt.Errorf("insufficient data: expected %d bytes, got %d", expectedSize, len(bgrxData))
	}

	// Calculate tile grid using smaller tiles for Fast-Path
	tilesX := (width + rgbTileSize - 1) / rgbTileSize
	tilesY := (height + rgbTileSize - 1) / rgbTileSize

	// Get pooled buffer for tile data (reused across all tiles)
	bufPtr := rgbTileBufferPool.Get().(*[]byte)
	tileBuffer := *bufPtr
	defer rgbTileBufferPool.Put(bufPtr)

	// Reusable tile struct to avoid allocations
	var tile tileRect

	for ty := 0; ty < tilesY; ty++ {
		for tx := 0; tx < tilesX; tx++ {
			left := tx * rgbTileSize
			top := ty * rgbTileSize
			right := left + rgbTileSize - 1
			bottom := top + rgbTileSize - 1

			// Clamp to image bounds
			if right >= width {
				right = width - 1
			}
			if bottom >= height {
				bottom = height - 1
			}

			tileW := right - left + 1
			tileH := bottom - top + 1
			tileSize := tileW * tileH * 4

			// Copy tile data with vertical flip (RDP expects bottom-up scanlines)
			for y := 0; y < tileH; y++ {
				srcY := top + y
				dstY := tileH - 1 - y
				srcOffset := (srcY*width + left) * 4
				dstOffset := dstY * tileW * 4
				copy(tileBuffer[dstOffset:dstOffset+tileW*4], bgrxData[srcOffset:srcOffset+tileW*4])
			}

			// Setup tile using pooled buffer directly (no allocation)
			tile.left = left
			tile.top = top
			tile.right = right
			tile.bottom = bottom
			tile.data = tileBuffer[:tileSize]

			// Send this tile immediately via Fast-Path
			if err := c.sendFastPathBitmapUpdate([]tileRect{tile}); err != nil {
				return err
			}
		}
	}

	return nil
}
