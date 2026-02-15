package rdp

import (
	"encoding/binary"

	"github.com/jetkvm/kvm/internal/rdp/protocol"
)

// Capability building functions for RDP Demand Active PDU.
// These build the server capability sets sent during the capability exchange phase.

// buildCapabilitySets builds the server capability sets.
// Note: FreeRDP explicitly rejects certain capabilities from servers (Brush, GlyphCache, etc.)
// Only send capabilities that are expected from servers per MS-RDPBCGR.
func (c *Connection) buildCapabilitySets(width, height uint16) []byte {
	buf := make([]byte, 0, 512)

	// General Capability Set (required per MS-RDPBCGR 2.2.7)
	buf = append(buf, buildGeneralCapability()...)

	// Bitmap Capability Set (required per MS-RDPBCGR 2.2.7)
	buf = append(buf, buildBitmapCapability(width, height)...)

	// Order Capability Set (required per MS-RDPBCGR 2.2.7, even if no order support)
	buf = append(buf, buildOrderCapability()...)

	// Pointer Capability Set (required per MS-RDPBCGR 2.2.7)
	buf = append(buf, buildPointerCapability()...)

	// Input Capability Set (may be required by older clients)
	buf = append(buf, buildInputCapability()...)

	// Multifragment Update Capability Set - required for large updates (> 16KB)
	// Windows Desktop client specifically checks for this capability
	buf = append(buf, buildMultifragUpdateCapability()...)

	// Large Pointer Capability Set - for large cursor support
	buf = append(buf, buildLargePointerCapability()...)

	return buf
}

// countCapabilitySets counts the number of capability sets in a buffer.
func countCapabilitySets(caps []byte) int {
	count := 0
	pos := 0
	for pos+4 <= len(caps) {
		length := int(caps[pos+2]) | int(caps[pos+3])<<8
		if length < 4 || pos+length > len(caps) {
			break
		}
		count++
		pos += length
	}
	return count
}

// Individual capability set builders

func buildGeneralCapability() []byte {
	buf := make([]byte, 24)
	// Type
	buf[0] = byte(protocol.CapabilityGeneral)
	buf[1] = byte(protocol.CapabilityGeneral >> 8)
	// Length
	buf[2] = 24
	buf[3] = 0
	// OS major/minor type (Windows)
	buf[4] = 1
	buf[6] = 3
	// Protocol version
	buf[8] = 0x00
	buf[9] = 0x02
	// Compression types
	buf[12] = 0x00
	// Extra flags - enable FASTPATH_OUTPUT_SUPPORTED for larger bitmap updates
	// FASTPATH was added in RDP 5.1; modern clients like Windows Desktop support it
	extraFlags := protocol.ExtraFlagsFastPathOutputSupported | protocol.ExtraFlagsNoBitmapCompressionHdr
	buf[14] = byte(extraFlags)
	buf[15] = byte(extraFlags >> 8)
	// Update capability
	buf[16] = 0x00
	// Remote unshare
	buf[18] = 0x00
	// Compression level
	buf[20] = 0x00
	// Refresh rect support
	buf[22] = 0x01
	// Suppress output support
	buf[23] = 0x01
	return buf
}

func buildBitmapCapability(width, height uint16) []byte {
	buf := make([]byte, 28)
	// Type
	buf[0] = byte(protocol.CapabilityBitmap)
	buf[1] = byte(protocol.CapabilityBitmap >> 8)
	// Length
	buf[2] = 28
	buf[3] = 0
	// Preferred BPP
	buf[4] = 32
	buf[5] = 0
	// Receive 1-bit palettes
	buf[6] = 1
	// Receive 4-bit palettes
	buf[8] = 1
	// Receive 8-bit palettes
	buf[10] = 1
	// Desktop width
	buf[12] = byte(width)
	buf[13] = byte(width >> 8)
	// Desktop height
	buf[14] = byte(height)
	buf[15] = byte(height >> 8)
	// Pad
	buf[16] = 0
	buf[17] = 0
	// Desktop resize (2 bytes at offset 18-19)
	buf[18] = 1
	buf[19] = 0
	// Bitmap compression (2 bytes at offset 20-21)
	buf[20] = 1
	buf[21] = 0
	// High color flags (1 byte at offset 22) - 0 = no specific color depth preference
	buf[22] = 0
	// Drawing flags (1 byte at offset 23)
	buf[23] = 0x01 // DRAW_ALLOW_DYNAMIC_COLOR_FIDELITY
	// Multiple rectangle support (2 bytes at offset 24-25)
	buf[24] = 1
	buf[25] = 0
	// Pad
	buf[26] = 0
	buf[27] = 0
	return buf
}

func buildOrderCapability() []byte {
	buf := make([]byte, 88)
	// Type
	buf[0] = byte(protocol.CapabilityOrder)
	buf[1] = byte(protocol.CapabilityOrder >> 8)
	// Length
	buf[2] = 88
	buf[3] = 0
	// Terminal descriptor (16 bytes) - all zeros
	// Pad (4 bytes)
	// Desktop save X granularity
	buf[24] = 1
	buf[25] = 0
	// Desktop save Y granularity
	buf[26] = 20
	buf[27] = 0
	// Pad
	// Maximum order level
	buf[30] = 1
	buf[31] = 0
	// Number of fonts
	buf[32] = 0
	buf[33] = 0
	// Order flags
	buf[34] = 0x22 // NEGOTIATEORDERSUPPORT | COLORINDEXSUPPORT
	buf[35] = 0
	// Order support (32 bytes) - minimal support
	// Order support extra flags
	buf[68] = 0
	buf[69] = 0
	// Pad
	// Desktop save size
	buf[76] = 0
	buf[77] = 0
	buf[78] = 0x04 // 480KB
	buf[79] = 0
	// Pad
	// Text ANSI code page
	buf[84] = 0xE4 // 1252
	buf[85] = 0x04
	// Pad
	return buf
}

func buildPointerCapability() []byte {
	// Per MS-RDPBCGR 2.2.7.1.5, the Pointer Capability Set sent in a Demand Active PDU
	// MUST include the pointerCacheSize field, making it 10 bytes minimum.
	buf := make([]byte, 10)
	// Type
	buf[0] = byte(protocol.CapabilityPointer)
	buf[1] = byte(protocol.CapabilityPointer >> 8)
	// Length (10 bytes including pointerCacheSize)
	buf[2] = 10
	buf[3] = 0
	// Color pointer flag (1 = supported)
	buf[4] = 1
	buf[5] = 0
	// Color pointer cache size (25 is the default)
	buf[6] = 25
	buf[7] = 0
	// Pointer cache size (25 is the default) - REQUIRED in server Demand Active PDU
	buf[8] = 25
	buf[9] = 0
	return buf
}

func buildInputCapability() []byte {
	buf := make([]byte, 88)
	// Type
	buf[0] = byte(protocol.CapabilityInput)
	buf[1] = byte(protocol.CapabilityInput >> 8)
	// Length
	buf[2] = 88
	buf[3] = 0
	// Input flags (TS_INPUT_CAPABILITYSET inputFlags, little-endian):
	// 0x0001 = INPUT_FLAG_SCANCODES
	// 0x0004 = INPUT_FLAG_MOUSEX
	// 0x0020 = INPUT_FLAG_FASTPATH_INPUT2
	// 0x0100 = TS_INPUT_FLAG_MOUSE_HWHEEL (required for client to send horizontal wheel events)
	buf[4] = 0x25
	buf[5] = 0x01
	// Pad
	// Keyboard layout (US)
	buf[8] = 0x09
	buf[9] = 0x04
	// Keyboard type (IBM enhanced 101/102)
	buf[12] = 4
	buf[13] = 0
	buf[14] = 0
	buf[15] = 0
	// Keyboard subtype
	buf[16] = 0
	// Keyboard function keys
	buf[20] = 12
	// IME filename (64 bytes) - zeros
	return buf
}

// buildMultifragUpdateCapability builds the Multifragment Update capability set.
// Per MS-RDPBCGR 2.2.7.2.6 - Required for updates larger than 16KB.
func buildMultifragUpdateCapability() []byte {
	buf := make([]byte, 8)
	// Type
	buf[0] = byte(protocol.CapabilityMultifragUpdate)
	buf[1] = byte(protocol.CapabilityMultifragUpdate >> 8)
	// Length
	buf[2] = 8
	buf[3] = 0
	// MaxRequestSize - maximum size of a single update in bytes
	// 0x3E8000 = 4,096,000 bytes (roughly 4MB) - sufficient for 1080p frames
	binary.LittleEndian.PutUint32(buf[4:8], 0x3E8000)
	return buf
}

// buildLargePointerCapability builds the Large Pointer capability set.
// Per MS-RDPBCGR 2.2.7.2.7 - For large cursor support (> 32x32).
func buildLargePointerCapability() []byte {
	buf := make([]byte, 6)
	// Type
	buf[0] = byte(protocol.CapabilityLargePointer)
	buf[1] = byte(protocol.CapabilityLargePointer >> 8)
	// Length
	buf[2] = 6
	buf[3] = 0
	// LargePointerSupportFlags
	// 0x0001 = LARGE_POINTER_FLAG_96x96 - support 96x96 pointers
	buf[4] = 0x01
	buf[5] = 0x00
	return buf
}
