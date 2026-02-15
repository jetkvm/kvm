package rdp

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Input handling for RDP connections.
// This file contains all keyboard and mouse input processing functions.

// Scancode constants for clipboard key combinations.
const (
	scancodeCtrl = 0x1D
	scancodeC    = 0x2E
	scancodeX    = 0x2D
	scancodeV    = 0x2F
)

// Slow-path input event types (MS-RDPBCGR 2.2.8.1.1.3.1.1).
const (
	inputEventSync     = 0x0000 // INPUT_EVENT_SYNC
	inputEventScancode = 0x0004 // INPUT_EVENT_SCANCODE
	inputEventUnicode  = 0x0005 // INPUT_EVENT_UNICODE
	inputEventMouse    = 0x8001 // INPUT_EVENT_MOUSE
	inputEventMouseX   = 0x8002 // INPUT_EVENT_MOUSEX
)

// Mouse pointer flags (MS-RDPBCGR 2.2.8.1.1.3.1.1.3).
const (
	ptrflagsWheelNegative  = 0x0100 // PTRFLAGS_WHEEL_NEGATIVE
	ptrflagsWheel          = 0x0200 // PTRFLAGS_WHEEL
	ptrflagsHWheel         = 0x0400 // PTRFLAGS_HWHEEL
	ptrflagsMove           = 0x0800 // PTRFLAGS_MOVE
	ptrflagsButton1        = 0x1000 // PTRFLAGS_BUTTON1 (left)
	ptrflagsButton2        = 0x2000 // PTRFLAGS_BUTTON2 (right)
	ptrflagsButton3        = 0x4000 // PTRFLAGS_BUTTON3 (middle)
	ptrflagsDown           = 0x8000 // PTRFLAGS_DOWN
	ptrflagsButtonMask     = ptrflagsButton1 | ptrflagsButton2 | ptrflagsButton3
	ptrflagsWheelDeltaMask = 0x00FF // Lower 8 bits = wheel rotation units
)

// Keyboard flags (MS-RDPBCGR 2.2.8.1.1.3.1.1.6).
const (
	kbdflagsExtended = 0x0100 // KBDFLAGS_EXTENDED
	kbdflagsRelease  = 0x8000 // KBDFLAGS_RELEASE
)

// RDP wheel delta unit: one "notch" of the wheel.
const wheelDelta = 120

// scaleWheelDelta converts RDP wheel delta to HID wheel units.
// RDP uses WHEEL_DELTA (120) per notch; HID uses small values (±1 to ±3).
func scaleWheelDelta(delta int) int8 {
	scaled := int8(delta / wheelDelta)
	if scaled == 0 && delta != 0 {
		// Preserve direction for small movements
		if delta > 0 {
			return 1
		}
		return -1
	}
	// Clamp to reasonable range
	if scaled > 3 {
		return 3
	}
	if scaled < -3 {
		return -3
	}
	return scaled
}

// handleInputPDU handles slow-path input PDUs containing multiple input events.
func (c *Connection) handleInputPDU(data []byte) {
	if len(data) < 4 {
		return
	}

	// Input PDU contains multiple input events
	numEvents := int(data[0]) | int(data[1])<<8
	pos := 4 // Skip numEvents and pad

	for i := 0; i < numEvents && pos+6 <= len(data); i++ {
		// Each event: 4 bytes time (unused), 2 bytes type, variable data
		eventType := binary.LittleEndian.Uint16(data[pos+4 : pos+6])
		pos += 6

		switch eventType {
		case inputEventSync:
			if pos+6 > len(data) {
				break
			}
			// Synchronize event (Caps Lock, Num Lock, etc.) — skip
			pos += 6
		case inputEventMouse:
			if pos+6 > len(data) {
				break
			}
			c.handleMouseEvent(data[pos : pos+6])
			pos += 6
		case inputEventScancode:
			if pos+6 > len(data) {
				break
			}
			c.handleScancodeEvent(data[pos : pos+6])
			pos += 6
		case inputEventUnicode:
			if pos+6 > len(data) {
				break
			}
			c.handleUnicodeEvent(data[pos : pos+6])
			pos += 6
		case inputEventMouseX:
			if pos+6 > len(data) {
				break
			}
			c.handleMouseXEvent(data[pos : pos+6])
			pos += 6
		default:
			c.server.deps.Logger.Debug().
				Uint16("eventType", eventType).
				Msg("RDP: unhandled input event type")
			return // Unknown event, can't determine size to continue
		}
	}
}

// handleMouseEvent handles basic mouse input.
// RDP mouse flags (MS-RDPBCGR 2.2.8.1.1.3.1.1.3):
//   - PTRFLAGS_HWHEEL (0x0400): Horizontal wheel event
//   - PTRFLAGS_WHEEL (0x0200): Vertical wheel event
//   - PTRFLAGS_WHEEL_NEGATIVE (0x0100): Wheel rotation is negative
//   - PTRFLAGS_MOVE (0x0800): Mouse moved
//   - PTRFLAGS_DOWN (0x8000): Button is being pressed (vs released)
//   - PTRFLAGS_BUTTON1 (0x1000): Left button
//   - PTRFLAGS_BUTTON2 (0x2000): Right button
//   - PTRFLAGS_BUTTON3 (0x4000): Middle button
func (c *Connection) handleMouseEvent(data []byte) {
	if len(data) < 6 {
		return
	}

	pointerFlags := binary.LittleEndian.Uint16(data[0:2])
	xPos := binary.LittleEndian.Uint16(data[2:4])
	yPos := binary.LittleEndian.Uint16(data[4:6])

	// Convert to absolute HID coordinates
	w, h := c.GetResolution()
	if w == 0 || h == 0 {
		return
	}

	hasMove := (pointerFlags & ptrflagsMove) != 0

	// Update last known position when the client indicates movement.
	// Wheel-only events may carry stale or zero coordinates — using those
	// would jump the cursor to the top-left (0,0), which on GNOME triggers
	// the hot corner / top-bar workspace switch on scroll.
	if hasMove {
		c.lastMouseX = int(xPos) * 32767 / int(w)
		c.lastMouseY = int(yPos) * 32767 / int(h)
	}

	// Track button state based on RDP events
	// RDP sends button flags with PTRFLAGS_DOWN on press, without on release
	// Button flags are only set during actual button events, not during moves
	hasButtonEvent := (pointerFlags & ptrflagsButtonMask) != 0
	if hasButtonEvent {
		isDown := (pointerFlags & ptrflagsDown) != 0
		if isDown {
			// Button press - set bits
			if pointerFlags&ptrflagsButton1 != 0 {
				c.mouseButtons |= 0x01 // Left
			}
			if pointerFlags&ptrflagsButton2 != 0 {
				c.mouseButtons |= 0x02 // Right
			}
			if pointerFlags&ptrflagsButton3 != 0 {
				c.mouseButtons |= 0x04 // Middle
			}
		} else {
			// Button release - clear bits
			if pointerFlags&ptrflagsButton1 != 0 {
				c.mouseButtons &^= 0x01 // Left
			}
			if pointerFlags&ptrflagsButton2 != 0 {
				c.mouseButtons &^= 0x02 // Right
			}
			if pointerFlags&ptrflagsButton3 != 0 {
				c.mouseButtons &^= 0x04 // Middle
			}
		}
	}

	// Only send HID position report (Report ID 1) when there's a move or
	// button state change.  Wheel-only events should NOT send a position
	// report — the cursor is already at the right place from the last move.
	// Sending a redundant position report with stale/zero coords would move
	// the cursor, and on Ubuntu GNOME scrolling on the top bar switches workspaces.
	if hasMove || hasButtonEvent {
		if err := c.server.deps.HID.AbsMouseReport(c.lastMouseX, c.lastMouseY, c.mouseButtons); err != nil {
			c.server.deps.Logger.Debug().Err(err).Msg("RDP: mouse report failed")
		}
	}

	// Handle vertical wheel
	if pointerFlags&ptrflagsWheel != 0 {
		delta := int(pointerFlags & ptrflagsWheelDeltaMask)
		if pointerFlags&ptrflagsWheelNegative != 0 {
			delta = -delta
		}
		if err := c.server.deps.HID.WheelReport(scaleWheelDelta(delta), 0); err != nil {
			c.server.deps.Logger.Debug().Err(err).Msg("RDP: vertical wheel report failed")
		}
	}

	// Handle horizontal wheel
	if pointerFlags&ptrflagsHWheel != 0 {
		delta := int(pointerFlags & ptrflagsWheelDeltaMask)
		if pointerFlags&ptrflagsWheelNegative != 0 {
			delta = -delta
		}
		if err := c.server.deps.HID.WheelReport(0, scaleWheelDelta(delta)); err != nil {
			c.server.deps.Logger.Debug().Err(err).Msg("RDP: horizontal wheel report failed")
		}
	}
}

// handleMouseXEvent handles extended mouse input (INPUT_EVENT_MOUSEX).
// Extended buttons (4/5) are not yet mapped; delegates to handleMouseEvent for standard buttons.
func (c *Connection) handleMouseXEvent(data []byte) {
	c.handleMouseEvent(data)
}

// handleClipboardKeys handles clipboard-related key combinations (Ctrl+C, Ctrl+X, Ctrl+V).
// Returns true if the key event should be suppressed (not forwarded to HID).
func (c *Connection) handleClipboardKeys(scancode uint16, pressed bool) bool {
	// Track Ctrl key state for paste/copy detection
	if scancode == scancodeCtrl {
		c.ctrlPressed.Store(pressed)
		return false
	}

	// Check clipboard handling conditions
	clipboardEnabled := c.clipboardChannel != nil && c.server.deps.Config.GetRDPClipboardEnabled()

	// Handle clipboard-related key combinations (only on key down)
	if pressed && c.ctrlPressed.Load() && clipboardEnabled {
		// macOS uses Cmd+C/V for copy/paste, not Ctrl+C/V.
		// Forward Ctrl keys natively so they reach the target unmodified.
		if c.getTargetOS() == TargetOSMacOS {
			return false
		}

		switch scancode {
		case scancodeC, scancodeX: // Copy or Cut
			c.clipboardChannel.ClearClipboardText()
			c.clearPendingFiles()
			c.targetCopied.Store(true)
			c.server.deps.Logger.Debug().
				Uint16("scancode", scancode).
				Msg("RDP: copy/cut detected, cleared clipboard")
			// Don't suppress - still forward the key for native copy/cut

		case scancodeV: // Paste
			// If user last copied on the target machine, forward V natively
			// so the target OS handles paste from its own clipboard.
			if c.targetCopied.Load() {
				return false
			}

			// Check for pending files first (file paste takes priority)
			if c.HasPendingFiles() {
				c.server.deps.Logger.Debug().Msg("RDP: pasting clipboard files")
				c.pasteInProgress.Store(true)
				c.handleFilePaste()
				return true // Suppress the V key down
			}

			// If file transfer is in progress, don't paste stale text - wait for transfer
			if c.clipboardChannel.IsFileTransferInProgress() {
				c.server.deps.Logger.Debug().Msg("RDP: file transfer in progress, ignoring paste")
				return true // Suppress the V key - file will be pasted when transfer completes
			}

			// Then check for text
			text := c.clipboardChannel.GetClipboardText()
			if text != nil {
				c.server.deps.Logger.Debug().
					Int("textLen", len(text)).
					Msg("RDP: pasting clipboard text via keyboard")

				c.pasteInProgress.Store(true)

				if err := c.server.deps.HID.KeyboardMacro(string(text)); err != nil {
					c.server.deps.Logger.Debug().Err(err).Msg("RDP: clipboard paste failed")
				}
				return true // Suppress the V key down
			}
		}
	}

	// Handle V key release after paste - suppress to avoid orphan key-up
	if scancode == scancodeV && !pressed && c.pasteInProgress.Load() {
		c.pasteInProgress.Store(false)
		return true // Suppress the V key up
	}

	return false
}

// handleScancodeEvent handles keyboard scancode input.
func (c *Connection) handleScancodeEvent(data []byte) {
	if len(data) < 6 {
		return
	}

	keyboardFlags := binary.LittleEndian.Uint16(data[0:2])
	scancode := binary.LittleEndian.Uint16(data[2:4])

	// Key up or down
	pressed := keyboardFlags&kbdflagsRelease == 0

	// Handle clipboard keys - may suppress the event
	if c.handleClipboardKeys(scancode, pressed) {
		return
	}

	// Convert scancode to HID code
	hidCode := scancodeToHID(scancode, keyboardFlags)
	if hidCode == 0 {
		return
	}

	if err := c.server.deps.HID.KeypressReport(hidCode, pressed); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: key report failed")
	}
}

// handleUnicodeEvent handles Unicode keyboard input.
func (c *Connection) handleUnicodeEvent(data []byte) {
	if len(data) < 6 {
		return
	}

	keyboardFlags := binary.LittleEndian.Uint16(data[0:2])
	unicodeCode := binary.LittleEndian.Uint16(data[2:4])

	// Only handle key down for Unicode
	if keyboardFlags&kbdflagsRelease != 0 {
		return
	}

	// Use keyboard macro for Unicode input
	if err := c.server.deps.HID.KeyboardMacro(string(rune(unicodeCode))); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: unicode input failed")
	}
}

// handleFastPathInput reads and handles a Fast-Path Input PDU.
// Fast-Path format: header(1) + length(1-2) + events(variable)
// MS-RDPBCGR 2.2.8.1.2
func (c *Connection) handleFastPathInput() error {
	// Read header byte
	header, err := c.reader.ReadByte()
	if err != nil {
		return fmt.Errorf("read fast-path header: %w", err)
	}

	// Header bits 2-3: number of events (0 means count in payload)
	numEvents := int((header >> 2) & 0x03)

	// Read length (1 or 2 bytes)
	length1, err := c.reader.ReadByte()
	if err != nil {
		return fmt.Errorf("read fast-path length: %w", err)
	}

	var totalLength int
	if length1&0x80 != 0 {
		// Two-byte length
		length2, err := c.reader.ReadByte()
		if err != nil {
			return fmt.Errorf("read fast-path length2: %w", err)
		}
		totalLength = int(length1&0x7F)<<8 | int(length2)
	} else {
		totalLength = int(length1)
	}

	// Calculate payload size (total - header - length bytes)
	headerSize := 2 // header + length1
	if length1&0x80 != 0 {
		headerSize = 3 // header + length1 + length2
	}
	payloadSize := totalLength - headerSize
	if payloadSize < 0 {
		return fmt.Errorf("invalid fast-path length: %d", totalLength)
	}

	if payloadSize == 0 {
		return nil
	}

	// Read payload using pool for zero-allocation hot path
	var payload []byte
	var poolBuf *[]byte
	if payloadSize <= 256 {
		// Use pool for typical small payloads
		poolBuf = inputPayloadPool.Get().(*[]byte)
		payload = (*poolBuf)[:payloadSize]
	} else {
		// Rare: large payload, allocate (shouldn't happen in practice)
		payload = make([]byte, payloadSize)
	}

	if _, err := io.ReadFull(c.reader, payload); err != nil {
		if poolBuf != nil {
			inputPayloadPool.Put(poolBuf)
		}
		return fmt.Errorf("read fast-path payload: %w", err)
	}

	// Process events, then return buffer to pool
	defer func() {
		if poolBuf != nil {
			inputPayloadPool.Put(poolBuf)
		}
	}()

	// If numEvents is 0, first byte of payload is the event count
	pos := 0
	if numEvents == 0 {
		if len(payload) < 1 {
			return nil
		}
		numEvents = int(payload[0])
		pos = 1
	}

	// Process each event
	for i := 0; i < numEvents && pos < len(payload); i++ {
		if pos >= len(payload) {
			break
		}

		eventHeader := payload[pos]
		pos++

		// Bits 0-4: event flags
		// Bits 5-7: event code
		eventCode := (eventHeader >> 5) & 0x07
		eventFlags := eventHeader & 0x1F

		switch eventCode {
		case 0: // FASTPATH_INPUT_EVENT_SCANCODE
			if pos >= len(payload) {
				break
			}
			scancode := payload[pos]
			pos++
			c.handleFastPathScancode(scancode, eventFlags)

		case 1: // FASTPATH_INPUT_EVENT_MOUSE
			if pos+6 > len(payload) {
				break
			}
			c.handleFastPathMouse(payload[pos : pos+6])
			pos += 6

		case 2: // FASTPATH_INPUT_EVENT_MOUSEX
			if pos+6 > len(payload) {
				break
			}
			c.handleFastPathMouse(payload[pos : pos+6])
			pos += 6

		case 3: // FASTPATH_INPUT_EVENT_SYNC
			// Synchronize event (caps lock, num lock, etc.)
			// Just skip it for now

		case 4: // FASTPATH_INPUT_EVENT_UNICODE
			if pos+2 > len(payload) {
				break
			}
			unicodeCode := binary.LittleEndian.Uint16(payload[pos : pos+2])
			pos += 2
			c.handleFastPathUnicode(unicodeCode, eventFlags)

		case 5: // FASTPATH_INPUT_EVENT_QOE_TIMESTAMP
			if pos+4 > len(payload) {
				break
			}
			pos += 4 // Skip timestamp

		default:
			// Unknown event type, cannot continue safely
			return nil
		}
	}

	return nil
}

// handleFastPathScancode processes a fast-path keyboard event.
func (c *Connection) handleFastPathScancode(scancode byte, flags byte) {
	// Flags: bit 0 = release, bit 1 = extended
	released := flags&0x01 != 0
	extended := flags&0x02 != 0
	pressed := !released

	// Handle clipboard keys - may suppress the event
	if c.handleClipboardKeys(uint16(scancode), pressed) {
		return
	}

	// Build keyboard flags for HID conversion
	var kbdFlags uint16
	if released {
		kbdFlags |= kbdflagsRelease
	}
	if extended {
		kbdFlags |= kbdflagsExtended
	}

	hidCode := scancodeToHID(uint16(scancode), kbdFlags)
	if hidCode == 0 {
		return
	}

	if err := c.server.deps.HID.KeypressReport(hidCode, pressed); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: fast-path key report failed")
	}
}

// handleFastPathMouse processes a fast-path mouse event.
func (c *Connection) handleFastPathMouse(data []byte) {
	// Same format as slow-path mouse: flags(2) + x(2) + y(2)
	c.handleMouseEvent(data)
}

// handleFastPathUnicode processes a fast-path unicode event.
func (c *Connection) handleFastPathUnicode(unicodeCode uint16, flags byte) {
	released := flags&0x01 != 0
	if released {
		return // Only handle key down
	}

	if err := c.server.deps.HID.KeyboardMacro(string(rune(unicodeCode))); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("RDP: fast-path unicode input failed")
	}
}
