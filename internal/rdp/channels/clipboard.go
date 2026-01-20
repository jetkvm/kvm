// Package channels implements RDP virtual channels.
package channels

import (
	"encoding/binary"
	"sync"
	"unicode/utf16"
)

// CLIPRDR is a STATIC virtual channel (like RDPSND), not a DVC.
// It implements the MS-RDPECLIP protocol for clipboard redirection.
// This implementation only supports client-to-server (unidirectional) clipboard.
const ClipboardChannelName = "cliprdr"

// CLIPRDR message types (MS-RDPECLIP 2.2.1).
const (
	CBMonitorReady     = 0x0001
	CBFormatList       = 0x0002
	CBFormatListResp   = 0x0003
	CBFormatDataReq    = 0x0004
	CBFormatDataResp   = 0x0005
	CBTempDirectory    = 0x0006
	CBClipCaps         = 0x0007
	CBFileContentsReq  = 0x0008
	CBFileContentsResp = 0x0009
	CBLockClipData     = 0x000A
	CBUnlockClipData   = 0x000B
)

// CLIPRDR message flags.
const (
	CBResponseOK   = 0x0001
	CBResponseFail = 0x0002
	CBASCIINames   = 0x0004
)

// Clipboard format IDs.
const (
	CFText        = 1  // CF_TEXT (ANSI)
	CFUnicodeText = 13 // CF_UNICODETEXT (UTF-16LE)
)

// General capability flags.
const (
	CBUseLongFormatNames    = 0x00000002
	CBStreamFileClipEnabled = 0x00000004
	CBFileClipNoFilePaths   = 0x00000008
	CBCanLockClipData       = 0x00000010
	CBHugeFileSupport       = 0x00000020
)

// Capability set type.
const (
	CBCapsTypeGeneral = 0x0001
)

// ClipboardChannel implements the CLIPRDR static virtual channel.
type ClipboardChannel struct {
	sendFunc func(data []byte) error
	logger   func(format string, args ...any)

	// Stored clipboard text (UTF-8)
	clipboardText []byte
	clipboardMu   sync.Mutex

	// Client capabilities
	useLongFormatNames bool
	clientVersion      uint32

	ready bool
}

// NewClipboardChannel creates a new CLIPRDR channel.
func NewClipboardChannel(sendFunc func([]byte) error) *ClipboardChannel {
	return &ClipboardChannel{
		sendFunc: sendFunc,
	}
}

// SetLogger sets the logging function.
func (c *ClipboardChannel) SetLogger(logger func(format string, args ...any)) {
	c.logger = logger
}

func (c *ClipboardChannel) log(format string, args ...any) {
	if c.logger != nil {
		c.logger(format, args...)
	}
}

// Start sends initial Capabilities and Monitor Ready PDUs.
func (c *ClipboardChannel) Start() error {
	// Send Clipboard Capabilities first
	if err := c.sendCapabilities(); err != nil {
		return err
	}
	// Send Monitor Ready to indicate server is ready
	return c.sendMonitorReady()
}

func (c *ClipboardChannel) sendCapabilities() error {
	// CLIPRDR_CAPS (MS-RDPECLIP 2.2.2.1)
	// Header: msgType(2) + msgFlags(2) + dataLen(4) = 8 bytes
	// Capabilities: cCapabilitiesSets(2) + pad(2) = 4 bytes
	// General cap: capType(2) + capLen(2) + version(4) + flags(4) = 12 bytes
	// Total: 8 + 4 + 12 = 24 bytes

	buf := make([]byte, 24)

	// Header
	binary.LittleEndian.PutUint16(buf[0:2], CBClipCaps)
	binary.LittleEndian.PutUint16(buf[2:4], 0) // msgFlags
	binary.LittleEndian.PutUint32(buf[4:8], 16) // dataLen (4 + 12)

	// Capabilities header
	binary.LittleEndian.PutUint16(buf[8:10], 1) // cCapabilitiesSets
	// pad[10:12] = 0

	// General capability set
	binary.LittleEndian.PutUint16(buf[12:14], CBCapsTypeGeneral) // capabilitySetType
	binary.LittleEndian.PutUint16(buf[14:16], 12)                // lengthCapability
	binary.LittleEndian.PutUint32(buf[16:20], 2)                 // version (CB_CAPS_VERSION_2)
	binary.LittleEndian.PutUint32(buf[20:24], CBUseLongFormatNames)

	c.log("CLIPRDR: sending capabilities")
	return c.sendFunc(buf)
}

func (c *ClipboardChannel) sendMonitorReady() error {
	// CLIPRDR_MONITOR_READY (MS-RDPECLIP 2.2.2.2)
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint16(buf[0:2], CBMonitorReady)
	binary.LittleEndian.PutUint16(buf[2:4], 0) // msgFlags
	binary.LittleEndian.PutUint32(buf[4:8], 0) // dataLen

	c.ready = true
	c.log("CLIPRDR: sending monitor ready")
	return c.sendFunc(buf)
}

// HandlePDU processes incoming clipboard PDUs.
func (c *ClipboardChannel) HandlePDU(data []byte) error {
	if len(data) < 8 {
		return nil
	}

	msgType := binary.LittleEndian.Uint16(data[0:2])
	msgFlags := binary.LittleEndian.Uint16(data[2:4])
	dataLen := binary.LittleEndian.Uint32(data[4:8])

	payload := data[8:]
	if uint32(len(payload)) < dataLen {
		c.log("CLIPRDR: payload too short: have %d, need %d", len(payload), dataLen)
		return nil
	}
	payload = payload[:dataLen]

	switch msgType {
	case CBClipCaps:
		return c.handleCapabilities(payload)
	case CBFormatList:
		return c.handleFormatList(payload, msgFlags)
	case CBFormatDataResp:
		return c.handleFormatDataResponse(payload, msgFlags)
	case CBLockClipData:
		// Client is locking clipboard - acknowledge but ignore
		c.log("CLIPRDR: received lock clipboard")
		return nil
	case CBUnlockClipData:
		// Client is unlocking clipboard - acknowledge but ignore
		c.log("CLIPRDR: received unlock clipboard")
		return nil
	default:
		c.log("CLIPRDR: unhandled message type 0x%04X", msgType)
		return nil
	}
}

func (c *ClipboardChannel) handleCapabilities(data []byte) error {
	if len(data) < 4 {
		return nil
	}

	numCapSets := binary.LittleEndian.Uint16(data[0:2])
	c.log("CLIPRDR: received capabilities, %d capability sets", numCapSets)

	// Parse capability sets
	pos := 4 // Skip cCapabilitiesSets(2) + pad(2)
	for i := uint16(0); i < numCapSets && pos+4 <= len(data); i++ {
		capType := binary.LittleEndian.Uint16(data[pos : pos+2])
		capLen := binary.LittleEndian.Uint16(data[pos+2 : pos+4])

		if capType == CBCapsTypeGeneral && capLen >= 12 && pos+int(capLen) <= len(data) {
			c.clientVersion = binary.LittleEndian.Uint32(data[pos+4 : pos+8])
			flags := binary.LittleEndian.Uint32(data[pos+8 : pos+12])
			c.useLongFormatNames = (flags & CBUseLongFormatNames) != 0
			c.log("CLIPRDR: client version=%d, longNames=%v", c.clientVersion, c.useLongFormatNames)
		}

		pos += int(capLen)
	}

	return nil
}

func (c *ClipboardChannel) handleFormatList(data []byte, msgFlags uint16) error {
	// Client announces clipboard formats available
	c.log("CLIPRDR: received format list (%d bytes)", len(data))

	// Parse format list to check if text is available
	hasText := false
	if c.useLongFormatNames || (msgFlags&CBASCIINames) == 0 {
		// Long format names (UTF-16LE null-terminated strings)
		hasText = c.parseFormatListLong(data)
	} else {
		// Short format names (32-byte fixed ASCII names)
		hasText = c.parseFormatListShort(data)
	}

	// Send Format List Response (acknowledge)
	resp := make([]byte, 8)
	binary.LittleEndian.PutUint16(resp[0:2], CBFormatListResp)
	binary.LittleEndian.PutUint16(resp[2:4], CBResponseOK)
	binary.LittleEndian.PutUint32(resp[4:8], 0)

	if err := c.sendFunc(resp); err != nil {
		return err
	}

	// Request Unicode text if available
	if hasText {
		c.log("CLIPRDR: requesting CF_UNICODETEXT")
		return c.requestFormatData(CFUnicodeText)
	}

	return nil
}

func (c *ClipboardChannel) parseFormatListLong(data []byte) bool {
	// Long format: formatId(4) + formatName(UTF-16LE, null-terminated)
	pos := 0
	for pos+4 <= len(data) {
		formatID := binary.LittleEndian.Uint32(data[pos : pos+4])
		pos += 4

		// Find null terminator (UTF-16LE)
		nameStart := pos
		for pos+2 <= len(data) {
			if data[pos] == 0 && data[pos+1] == 0 {
				pos += 2
				break
			}
			pos += 2
		}

		// Check for text formats
		if formatID == CFText || formatID == CFUnicodeText {
			return true
		}

		// Also check format name for "text" (case insensitive)
		if nameStart < pos-2 {
			name := c.utf16LEToUTF8(data[nameStart : pos-2])
			c.log("CLIPRDR: format %d: %s", formatID, string(name))
		}
	}
	return false
}

func (c *ClipboardChannel) parseFormatListShort(data []byte) bool {
	// Short format: formatId(4) + formatName(32 bytes ASCII, null-padded)
	for pos := 0; pos+36 <= len(data); pos += 36 {
		formatID := binary.LittleEndian.Uint32(data[pos : pos+4])
		if formatID == CFText || formatID == CFUnicodeText {
			return true
		}
	}
	return false
}

func (c *ClipboardChannel) requestFormatData(formatID uint32) error {
	// CLIPRDR_FORMAT_DATA_REQUEST (MS-RDPECLIP 2.2.5.1)
	buf := make([]byte, 12)
	binary.LittleEndian.PutUint16(buf[0:2], CBFormatDataReq)
	binary.LittleEndian.PutUint16(buf[2:4], 0)  // msgFlags
	binary.LittleEndian.PutUint32(buf[4:8], 4)  // dataLen
	binary.LittleEndian.PutUint32(buf[8:12], formatID)
	return c.sendFunc(buf)
}

func (c *ClipboardChannel) handleFormatDataResponse(data []byte, msgFlags uint16) error {
	// Check for failure
	if (msgFlags & CBResponseFail) != 0 {
		c.log("CLIPRDR: format data request failed")
		return nil
	}

	if len(data) == 0 {
		c.log("CLIPRDR: received empty clipboard data")
		return nil
	}

	// Data is UTF-16LE, convert to UTF-8
	text := c.utf16LEToUTF8(data)

	c.log("CLIPRDR: received clipboard text (%d bytes UTF-8)", len(text))

	// Store for later typing
	c.clipboardMu.Lock()
	c.clipboardText = text
	c.clipboardMu.Unlock()

	return nil
}

func (c *ClipboardChannel) utf16LEToUTF8(data []byte) []byte {
	// Handle odd length
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}

	// Remove null terminators
	for len(data) >= 2 && data[len(data)-1] == 0 && data[len(data)-2] == 0 {
		data = data[:len(data)-2]
	}

	if len(data) == 0 {
		return nil
	}

	// Convert UTF-16LE to UTF-8
	u16s := make([]uint16, len(data)/2)
	for i := range u16s {
		u16s[i] = binary.LittleEndian.Uint16(data[i*2:])
	}

	runes := utf16.Decode(u16s)
	return []byte(string(runes))
}

// GetClipboardText returns stored clipboard text for typing.
// Returns nil if no clipboard text is available.
func (c *ClipboardChannel) GetClipboardText() []byte {
	c.clipboardMu.Lock()
	defer c.clipboardMu.Unlock()

	if len(c.clipboardText) == 0 {
		return nil
	}

	// Return copy
	text := make([]byte, len(c.clipboardText))
	copy(text, c.clipboardText)
	return text
}

// ClearClipboardText clears the stored clipboard text.
func (c *ClipboardChannel) ClearClipboardText() {
	c.clipboardMu.Lock()
	c.clipboardText = nil
	c.clipboardMu.Unlock()
}

// IsReady returns true if the channel has completed initialization.
func (c *ClipboardChannel) IsReady() bool {
	return c.ready
}
