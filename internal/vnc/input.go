package vnc

// X11 keysyms for special keys used in input handling.
const (
	keysymEscape       = 0xFF1B
	keysymInsert       = 0xFF63
	keysymShiftLeft    = 0xFFE1
	keysymShiftRight   = 0xFFE2
	keysymControlLeft  = 0xFFE3
	keysymControlRight = 0xFFE4
	keysymMetaLeft     = 0xFFE7
	keysymMetaRight    = 0xFFE8
	keysymSuperLeft    = 0xFFEB
	keysymSuperRight   = 0xFFEC
	keysymC            = 0x63 // lowercase 'c'
	keysymCUpper       = 0x43 // uppercase 'C'
	keysymX            = 0x78 // lowercase 'x'
	keysymXUpper       = 0x58 // uppercase 'X'
	keysymV            = 0x76 // lowercase 'v'
	keysymVUpper       = 0x56 // uppercase 'V'
)

// handleVNCKey processes a key event from the VNC client.
func (c *Connection) handleVNCKey(keysym uint32, down bool) {
	// Allow Escape key to cancel ongoing paste operations
	if keysym == keysymEscape && down && c.server.deps.HID.IsKeyboardMacroInProgress() {
		c.server.deps.Logger.Info().Msg("VNC: Escape pressed, canceling paste operation")
		c.server.deps.HID.CancelKeyboardMacro()
		return
	}

	// Block all other keyboard input while paste is in progress
	if c.server.deps.HID.IsKeyboardMacroInProgress() {
		if c.server.deps.Logger.Debug().Enabled() {
			c.server.deps.Logger.Debug().Uint32("keysym", keysym).Msg("VNC key event blocked: paste in progress")
		}
		return
	}

	// Track modifier key states for clipboard detection
	switch keysym {
	case keysymShiftLeft, keysymShiftRight:
		c.shiftDown = down
	case keysymControlLeft, keysymControlRight:
		c.ctrlDown = down
	case keysymMetaLeft, keysymMetaRight, keysymSuperLeft, keysymSuperRight:
		c.metaDown = down
	}

	// Detect copy/cut and paste key combinations (on key down)
	if down {
		// Detect copy/cut: Ctrl+C, Ctrl+X, Cmd+C, Cmd+X
		// When user copies on VNC-controlled machine, clear VNC clipboard
		// so next paste uses native clipboard instead of stale VNC client content
		isCopyCombo := (c.ctrlDown || c.metaDown) &&
			(keysym == keysymC || keysym == keysymCUpper ||
				keysym == keysymX || keysym == keysymXUpper)

		if isCopyCombo {
			c.clipboardMu.Lock()
			c.clipboardText = nil
			c.clipboardMu.Unlock()
			// Don't return - still forward the key for native copy/cut
		}

		isPasteCombo := false

		// Ctrl+V or Cmd+V (lowercase or uppercase V)
		if (c.ctrlDown || c.metaDown) && (keysym == keysymV || keysym == keysymVUpper) {
			isPasteCombo = true
		}

		// Shift+Insert
		if c.shiftDown && keysym == keysymInsert {
			isPasteCombo = true
		}

		if isPasteCombo {
			c.clipboardMu.Lock()

			// On first paste after connection: skip VNC clipboard and forward paste key
			// This allows copy-paste within the VNC-controlled machine to work
			// (e.g., user copies text on remote machine, then pastes it)
			// Note: we keep the VNC clipboard content for subsequent pastes
			if !c.firstPasteDone {
				c.firstPasteDone = true
				c.clipboardMu.Unlock()
				c.server.deps.Logger.Debug().Msg("VNC: first paste after connect, forwarding to allow native paste")
				// Fall through to forward the paste key
			} else {
				// Subsequent pastes: use VNC clipboard if available
				// Get and clear stored clipboard text atomically
				// This prevents the same content from being pasted multiple times
				text := c.clipboardText
				c.clipboardText = nil // Clear clipboard after use
				c.clipboardMu.Unlock()

				if len(text) > 0 {
					c.server.deps.Logger.Info().Int("bytes", len(text)).Msg("VNC: paste combo detected, typing clipboard")
					// Type clipboard text asynchronously (text is already copied out)
					go func() {
						if err := c.typeClipboardText(text); err != nil {
							c.server.deps.Logger.Warn().Err(err).Int("bytes", len(text)).Msg("VNC clipboard: failed to type text")
						}
					}()
					// Don't forward the paste key - we handled it with VNC clipboard
					return
				}
				// VNC clipboard is empty - fall through to forward the key
				// so the target machine's native paste works
			}
		}
	}

	hidKey := keysymToHID(keysym)
	if hidKey == 0 {
		// Check log level first to avoid allocations when debug is disabled
		if c.server.deps.Logger.Debug().Enabled() {
			c.server.deps.Logger.Debug().Uint32("keysym", keysym).Bool("down", down).Msg("VNC key event: unknown keysym, ignoring")
		}
		return
	}

	if err := c.server.deps.HID.KeypressReport(hidKey, down); err != nil {
		c.server.deps.Logger.Warn().Err(err).Uint32("keysym", keysym).Msg("failed to send key event")
	}
}

// handleVNCPointer processes a pointer (mouse) event from the VNC client.
func (c *Connection) handleVNCPointer(x, y uint16, buttonMask byte) {
	// Block mouse input while paste is in progress
	if c.server.deps.HID.IsKeyboardMacroInProgress() {
		return
	}

	width, height := c.GetResolution()

	if width == 0 || height == 0 {
		c.server.deps.Logger.Debug().Uint16("width", width).Uint16("height", height).Msg("VNC pointer: invalid resolution, ignoring")
		return
	}

	// Scale VNC coordinates to absolute HID coordinates (0-hidAbsoluteMax)
	absX := int(x) * hidAbsoluteMax / int(width)
	absY := int(y) * hidAbsoluteMax / int(height)

	// Map VNC buttons to HID button mask:
	// VNC: bit0=left, bit1=middle, bit2=right
	// HID: bit0=left, bit1=right, bit2=middle
	vncLeft := (buttonMask & 0x01)
	vncMiddle := (buttonMask & 0x02) >> 1
	vncRight := (buttonMask & 0x04) >> 1
	buttons := vncLeft | vncRight | (vncMiddle << 2)

	if err := c.server.deps.HID.AbsMouseReport(absX, absY, buttons); err != nil {
		c.server.deps.Logger.Warn().Err(err).Int("absX", absX).Int("absY", absY).Msg("failed to send mouse event")
	}

	// RFB protocol uses button mask bits 3-6 for scroll "buttons":
	// bit3 (0x08) = scroll up, bit4 (0x10) = scroll down
	// bit5 (0x20) = scroll left, bit6 (0x40) = scroll right
	var wheelY, wheelX int8
	if buttonMask&0x08 != 0 {
		wheelY = 1
	} else if buttonMask&0x10 != 0 {
		wheelY = -1
	}
	if buttonMask&0x20 != 0 {
		wheelX = -1 // Left scroll = negative
	} else if buttonMask&0x40 != 0 {
		wheelX = 1 // Right scroll = positive
	}

	if wheelY != 0 || wheelX != 0 {
		if err := c.server.deps.HID.WheelReport(wheelY, wheelX); err != nil {
			// Log at Debug level (not Trace) so scroll failures are visible in debug builds
			if c.server.deps.Logger.Debug().Enabled() {
				c.server.deps.Logger.Debug().Err(err).Int8("wheelY", wheelY).Int8("wheelX", wheelX).Msg("failed to send scroll event")
			}
		}
	}
}
