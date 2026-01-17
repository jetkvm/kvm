package vnc

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

// messageLoop reads and processes client messages.
func (c *Connection) messageLoop() error {
	for {
		select {
		case <-c.stopChan:
			return nil
		default:
		}

		if err := c.conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			return fmt.Errorf("failed to set read deadline: %w", err)
		}

		if _, err := io.ReadFull(c.conn, c.msgBuf[:]); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("failed to read message type: %w", err)
		}

		switch rfbClientMsgType(c.msgBuf[0]) {
		case msgSetPixelFormat:
			if err := c.handleSetPixelFormat(); err != nil {
				return err
			}
		case msgSetEncodings:
			if err := c.handleSetEncodings(); err != nil {
				return err
			}
		case msgFramebufferUpdateRequest:
			if err := c.handleFramebufferUpdateRequest(); err != nil {
				return err
			}
		case msgKeyEvent:
			if err := c.handleKeyEvent(); err != nil {
				return err
			}
		case msgPointerEvent:
			if err := c.handlePointerEvent(); err != nil {
				return err
			}
		case msgClientCutText:
			if err := c.handleClientCutText(); err != nil {
				return err
			}
		default:
			c.server.deps.Logger.Warn().Uint8("msgType", c.msgBuf[0]).Msg("unknown VNC message type")
			return fmt.Errorf("unknown message type: %d", c.msgBuf[0])
		}
	}
}

// handleSetPixelFormat processes a SetPixelFormat message.
func (c *Connection) handleSetPixelFormat() error {
	if _, err := io.ReadFull(c.conn, c.pixelFmtBuf[:]); err != nil {
		return fmt.Errorf("failed to read pixel format: %w", err)
	}

	pf := PixelFormat{
		BitsPerPixel: c.pixelFmtBuf[3],
		Depth:        c.pixelFmtBuf[4],
		BigEndian:    c.pixelFmtBuf[5],
		TrueColor:    c.pixelFmtBuf[6],
		RedMax:       binary.BigEndian.Uint16(c.pixelFmtBuf[7:9]),
		GreenMax:     binary.BigEndian.Uint16(c.pixelFmtBuf[9:11]),
		BlueMax:      binary.BigEndian.Uint16(c.pixelFmtBuf[11:13]),
		RedShift:     c.pixelFmtBuf[13],
		GreenShift:   c.pixelFmtBuf[14],
		BlueShift:    c.pixelFmtBuf[15],
	}

	if err := pf.Validate(); err != nil {
		c.server.deps.Logger.Warn().Err(err).Msg("client sent invalid pixel format, keeping server default")
		return nil
	}

	c.pixelFormatMu.Lock()
	c.pixelFormat = pf
	c.pixelFormatMu.Unlock()

	return nil
}

// handleSetEncodings processes a SetEncodings message.
func (c *Connection) handleSetEncodings() error {
	if _, err := io.ReadFull(c.conn, c.encHeaderBuf[:]); err != nil {
		return fmt.Errorf("failed to read encodings header: %w", err)
	}

	numEncodings := binary.BigEndian.Uint16(c.encHeaderBuf[1:3])

	// Limit encodings to prevent DoS from malicious clients
	const maxEncodings = 256
	if numEncodings > maxEncodings {
		return fmt.Errorf("too many encodings: %d (max %d)", numEncodings, maxEncodings)
	}

	foundTight := false
	for i := uint16(0); i < numEncodings; i++ {
		if _, err := io.ReadFull(c.conn, c.encBuf[:]); err != nil {
			return fmt.Errorf("failed to read encoding: %w", err)
		}
		enc := rfbEncodingType(binary.BigEndian.Uint32(c.encBuf[:]))
		if enc == encodingTight {
			foundTight = true
		}
	}

	c.hasTight.Store(foundTight)
	previouslyNeededJPEG := c.needsJPEGEncoder.Swap(foundTight)

	if foundTight && !previouslyNeededJPEG {
		if err := c.server.requestJPEGEncoder(); err != nil {
			c.server.deps.Logger.Error().
				Err(err).
				Str("remote", c.conn.RemoteAddr().String()).
				Str("errorId", "VNC_JPEG_ENCODER_FAILED").
				Msg("JPEG encoder failed to start - VNC client will not receive video")
			c.needsJPEGEncoder.Store(false)
		}
	} else if !foundTight && previouslyNeededJPEG {
		c.server.releaseJPEGEncoder()
	}

	if !foundTight {
		c.server.deps.Logger.Warn().
			Str("remote", c.conn.RemoteAddr().String()).
			Msg("VNC client does not support Tight encoding - video streaming unavailable")
	}

	return nil
}

// handleFramebufferUpdateRequest processes a framebuffer update request.
func (c *Connection) handleFramebufferUpdateRequest() error {
	if _, err := io.ReadFull(c.conn, c.fbReqBuf[:]); err != nil {
		return fmt.Errorf("failed to read fb update request: %w", err)
	}

	c.frameRequested.Store(true)

	return nil
}

// handleKeyEvent processes a keyboard event.
func (c *Connection) handleKeyEvent() error {
	if _, err := io.ReadFull(c.conn, c.keyBuf[:]); err != nil {
		return fmt.Errorf("failed to read key event: %w", err)
	}

	down := c.keyBuf[0] != 0
	keysym := binary.BigEndian.Uint32(c.keyBuf[3:7])

	c.handleVNCKey(keysym, down)

	return nil
}

// handlePointerEvent processes a mouse/pointer event.
func (c *Connection) handlePointerEvent() error {
	if _, err := io.ReadFull(c.conn, c.pointerBuf[:]); err != nil {
		return fmt.Errorf("failed to read pointer event: %w", err)
	}

	buttonMask := c.pointerBuf[0]
	x := binary.BigEndian.Uint16(c.pointerBuf[1:3])
	y := binary.BigEndian.Uint16(c.pointerBuf[3:5])

	// Rate-limited logging (safe: single-goroutine message loop per connection)
	c.pointerEventCount++
	if c.pointerEventCount <= 3 || c.pointerEventCount%100 == 0 || time.Since(c.lastPointerLogTime) > 5*time.Second {
		c.server.deps.Logger.Debug().Uint16("x", x).Uint16("y", y).Uint8("buttons", buttonMask).Int32("count", c.pointerEventCount).Msg("VNC pointer event")
		c.lastPointerLogTime = time.Now()
	}

	c.handleVNCPointer(x, y, buttonMask)

	return nil
}

// handleClientCutText processes clipboard text from the client.
func (c *Connection) handleClientCutText() error {
	var header [7]byte
	if _, err := io.ReadFull(c.conn, header[:]); err != nil {
		return fmt.Errorf("failed to read cut text header: %w", err)
	}

	length := binary.BigEndian.Uint32(header[3:7])

	if length > maxCutTextLength {
		return fmt.Errorf("cut text too large: %d bytes (max %d)", length, maxCutTextLength)
	}

	if length > 0 {
		// Reuse pre-allocated buffer to avoid allocation per clipboard event
		if cap(c.cutTextBuf) < int(length) {
			c.cutTextBuf = make([]byte, length)
		} else {
			c.cutTextBuf = c.cutTextBuf[:length]
		}
		if _, err := io.ReadFull(c.conn, c.cutTextBuf); err != nil {
			return fmt.Errorf("failed to read cut text: %w", err)
		}

		// Store clipboard text for paste-on-demand (when user presses Ctrl+V, Cmd+V, etc.)
		// This prevents auto-pasting when a VNC client connects with clipboard content.
		// Reuse buffer to avoid allocation per clipboard event.
		c.clipboardMu.Lock()
		if cap(c.clipboardText) < int(length) {
			c.clipboardText = make([]byte, length)
		} else {
			c.clipboardText = c.clipboardText[:length]
		}
		copy(c.clipboardText, c.cutTextBuf)
		c.clipboardMu.Unlock()

		if c.server.deps.Logger.Debug().Enabled() {
			c.server.deps.Logger.Debug().Int("bytes", int(length)).Msg("VNC clipboard: stored text (will type on paste)")
		}
	}

	return nil
}
