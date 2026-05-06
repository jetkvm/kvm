package rfb

import (
	"errors"
	"fmt"
)

// ClientMessage is the union of all client-to-server messages this
// server understands. Callers type-switch on the concrete type.
type ClientMessage interface{ clientMessage() }

// SetPixelFormatMessage requests a different pixel layout for Raw
// rectangles (RFC 6143 §7.5.1).
type SetPixelFormatMessage struct {
	PixelFormat PixelFormat
}

func (SetPixelFormatMessage) clientMessage() {}

// SetEncodingsMessage advertises the encodings the client can decode
// in priority order (RFC 6143 §7.5.2). Pseudo-encoding numbers
// (negative) declare client capabilities such as DesktopSize support
// or H.264 (encoding 50).
type SetEncodingsMessage struct {
	Encodings []EncodingType
}

func (SetEncodingsMessage) clientMessage() {}

// FramebufferUpdateRequestMessage asks the server for an update of
// the named rectangle (RFC 6143 §7.5.3). If Incremental is false, the
// server should reply with the full content of the rectangle.
type FramebufferUpdateRequestMessage struct {
	Incremental bool
	X, Y, W, H  uint16
}

func (FramebufferUpdateRequestMessage) clientMessage() {}

// KeyEventMessage is a key press or release with an X11 keysym
// (RFC 6143 §7.5.4).
type KeyEventMessage struct {
	Down   bool
	Keysym uint32
}

func (KeyEventMessage) clientMessage() {}

// PointerEventMessage is a mouse position+button-mask update
// (RFC 6143 §7.5.5). Wheel events appear as transient presses of
// buttons 4/5/6/7.
type PointerEventMessage struct {
	ButtonMask uint8
	X, Y       uint16
}

func (PointerEventMessage) clientMessage() {}

// ClientCutTextMessage is a clipboard text update from the client
// (RFC 6143 §7.5.6). Per the spec, the text is Latin-1 encoded.
type ClientCutTextMessage struct {
	Text []byte
}

func (ClientCutTextMessage) clientMessage() {}

// ErrUnknownClientMessage is returned when an unrecognized message
// type is read from the connection.
var ErrUnknownClientMessage = errors.New("rfb: unknown client message type")

// ReadClientMessage parses the next client-to-server message.
// SetEncodings entries beyond the supplied limit are dropped to bound
// memory; ClientCutText longer than maxCutText is truncated.
const (
	maxEncodings = 256
	maxCutText   = 1 << 20 // 1 MiB
)

// ReadClientMessage reads and parses one client message from the
// connection. Blocks until a complete message is available or an
// error occurs.
func (c *Conn) ReadClientMessage() (ClientMessage, error) {
	mt, err := c.readByte()
	if err != nil {
		return nil, err
	}

	switch ClientMessageType(mt) {
	case ClientMsgSetPixelFormat:
		var pad [3]byte
		if err := c.readFull(pad[:]); err != nil {
			return nil, err
		}
		var pf [16]byte
		if err := c.readFull(pf[:]); err != nil {
			return nil, err
		}
		return SetPixelFormatMessage{PixelFormat: UnmarshalPixelFormat(pf[:])}, nil

	case ClientMsgSetEncodings:
		var pad [1]byte
		if err := c.readFull(pad[:]); err != nil {
			return nil, err
		}
		count, err := c.readU16()
		if err != nil {
			return nil, err
		}
		n := int(count)
		if n > maxEncodings {
			n = maxEncodings
		}
		// Allocate based on the capped count, not the wire value, so
		// a malicious client can't make us allocate up to 65535*4 bytes.
		// We still drain the full wire stream below to keep framing
		// aligned for the next message.
		encs := make([]EncodingType, n)
		for i := 0; i < int(count); i++ {
			v, err := c.readS32()
			if err != nil {
				return nil, err
			}
			if i < n {
				encs[i] = EncodingType(v)
			}
		}
		return SetEncodingsMessage{Encodings: encs}, nil

	case ClientMsgFramebufferUpdateRequest:
		incr, err := c.readByte()
		if err != nil {
			return nil, err
		}
		x, err := c.readU16()
		if err != nil {
			return nil, err
		}
		y, err := c.readU16()
		if err != nil {
			return nil, err
		}
		w, err := c.readU16()
		if err != nil {
			return nil, err
		}
		h, err := c.readU16()
		if err != nil {
			return nil, err
		}
		return FramebufferUpdateRequestMessage{
			Incremental: incr != 0,
			X:           x, Y: y, W: w, H: h,
		}, nil

	case ClientMsgKeyEvent:
		down, err := c.readByte()
		if err != nil {
			return nil, err
		}
		var pad [2]byte
		if err := c.readFull(pad[:]); err != nil {
			return nil, err
		}
		ks, err := c.readU32()
		if err != nil {
			return nil, err
		}
		return KeyEventMessage{Down: down != 0, Keysym: ks}, nil

	case ClientMsgPointerEvent:
		mask, err := c.readByte()
		if err != nil {
			return nil, err
		}
		x, err := c.readU16()
		if err != nil {
			return nil, err
		}
		y, err := c.readU16()
		if err != nil {
			return nil, err
		}
		return PointerEventMessage{ButtonMask: mask, X: x, Y: y}, nil

	case ClientMsgClientCutText:
		var pad [3]byte
		if err := c.readFull(pad[:]); err != nil {
			return nil, err
		}
		length, err := c.readU32()
		if err != nil {
			return nil, err
		}
		read := length
		if read > maxCutText {
			read = maxCutText
		}
		buf := make([]byte, read)
		if err := c.readFull(buf); err != nil {
			return nil, err
		}
		// Discard any excess bytes beyond our cap.
		if length > read {
			discard := length - read
			junk := make([]byte, 1024)
			for discard > 0 {
				n := uint32(len(junk))
				if discard < n {
					n = discard
				}
				if err := c.readFull(junk[:n]); err != nil {
					return nil, err
				}
				discard -= n
			}
		}
		return ClientCutTextMessage{Text: buf}, nil

	default:
		return nil, fmt.Errorf("%w: %d", ErrUnknownClientMessage, mt)
	}
}
