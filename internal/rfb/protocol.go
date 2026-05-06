// Package rfb implements the server side of the RFB protocol (RFC 6143)
// used by VNC clients. It is intentionally JetKVM-agnostic and can be
// used as a standalone library; the Conn type wraps a net.Conn and
// exposes typed messages for handshake, security negotiation, and
// per-frame exchange.
package rfb

// ProtocolVersion38 is the RFB version string we advertise. It matches
// the 12-byte frame "RFB 003.008\n" specified in RFC 6143 §7.1.1.
const ProtocolVersion38 = "RFB 003.008\n"

// SecurityType identifies an RFB security type as advertised in the
// protocol handshake (RFC 6143 §7.1.2).
type SecurityType uint8

const (
	SecInvalid SecurityType = 0
	SecNone    SecurityType = 1
	SecVNCAuth SecurityType = 2
)

// SecurityResult values returned to the client after the security
// handshake (RFC 6143 §7.1.3).
const (
	SecResultOK     uint32 = 0
	SecResultFailed uint32 = 1
)

// ClientMessageType identifies a client-to-server RFB message
// (RFC 6143 §7.5).
type ClientMessageType uint8

const (
	ClientMsgSetPixelFormat           ClientMessageType = 0
	ClientMsgSetEncodings             ClientMessageType = 2
	ClientMsgFramebufferUpdateRequest ClientMessageType = 3
	ClientMsgKeyEvent                 ClientMessageType = 4
	ClientMsgPointerEvent             ClientMessageType = 5
	ClientMsgClientCutText            ClientMessageType = 6
)

// ServerMessageType identifies a server-to-client RFB message
// (RFC 6143 §7.6).
type ServerMessageType uint8

const (
	ServerMsgFramebufferUpdate   ServerMessageType = 0
	ServerMsgSetColourMapEntries ServerMessageType = 1
	ServerMsgBell                ServerMessageType = 2
	ServerMsgServerCutText       ServerMessageType = 3
)

// EncodingType identifies an RFB encoding or pseudo-encoding. Standard
// encodings have non-negative numbers; pseudo-encodings (capability
// advertisements from the client) have negative numbers.
type EncodingType int32

const (
	EncodingRaw      EncodingType = 0
	EncodingCopyRect EncodingType = 1

	// EncodingDesktopSize is a pseudo-encoding the client advertises to
	// indicate it can handle resolution changes. The server signals a
	// resize by emitting a rectangle with this encoding and the new
	// width/height.
	EncodingDesktopSize EncodingType = -223

	// EncodingOpenH264 is the "Open H.264 Encoding" registered as
	// pseudo-encoding 50 by TigerVNC. Payload is an Annex-B H.264
	// bytestream prefixed with U32 length and U32 flags. See
	// https://github.com/rfbproto/rfbproto/pull/39 and
	// https://github.com/TigerVNC/tigervnc/pull/1194 (TigerVNC ≥ 1.13.0,
	// requires FFmpeg at build time).
	EncodingOpenH264 EncodingType = 50
)

// OpenH264 flag bits in the rectangle's U32 flags field. Bit 0 resets
// this rectangle's decoder context; bit 1 resets all rectangles.
const (
	OpenH264FlagResetContext  uint32 = 0x1
	OpenH264FlagResetAllRects uint32 = 0x2
)

// PointerButton bits in the PointerEvent button-mask. RFB historically
// expresses scroll-wheel events as transient presses of buttons 4
// (up), 5 (down), 6 (left) and 7 (right).
const (
	PointerButtonLeft   uint8 = 1 << 0
	PointerButtonMiddle uint8 = 1 << 1
	PointerButtonRight  uint8 = 1 << 2
	PointerButtonUp     uint8 = 1 << 3
	PointerButtonDown   uint8 = 1 << 4
	PointerButtonLeftWh uint8 = 1 << 5
	PointerButtonRightW uint8 = 1 << 6
)

// PointerMaskToHIDButtons converts an RFB PointerEvent button-mask
// (RFC 6143 §7.5.5) to a USB HID boot-mouse button byte.
//
// RFB layout:  bit 0 = left, bit 1 = MIDDLE, bit 2 = RIGHT
// USB HID:     bit 0 = left, bit 1 = RIGHT,  bit 2 = MIDDLE
//
// Bits 3..6 (RFB wheel pseudo-buttons) are dropped — callers should
// translate them into wheel reports separately.
func PointerMaskToHIDButtons(m uint8) uint8 {
	left := m & PointerButtonLeft   // bit 0, same in both
	mid := m & PointerButtonMiddle  // RFB bit 1 -> HID bit 2
	right := m & PointerButtonRight // RFB bit 2 -> HID bit 1
	return left | (mid << 1) | (right >> 1)
}
