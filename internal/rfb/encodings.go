package rfb

import "fmt"

// Rect describes a rectangle that will be sent inside a
// FramebufferUpdate message header. The encoding-specific payload
// follows separately.
type Rect struct {
	X, Y, W, H uint16
	Encoding   EncodingType
}

// BeginFramebufferUpdate writes the FramebufferUpdate message header
// (RFC 6143 §7.6.1) for `n` rectangles. The caller must follow up
// with `n` WriteRectHeader+payload pairs and a final Conn.Flush.
// The Conn type assumes a single-writer model — see Conn's doc.
func (c *Conn) BeginFramebufferUpdate(n uint16) error {
	if err := c.writeByte(byte(ServerMsgFramebufferUpdate)); err != nil {
		return err
	}
	if err := c.writeByte(0); err != nil { // padding
		return err
	}
	return c.writeU16(n)
}

// WriteRectHeader writes a 12-byte rectangle header (x, y, w, h,
// encoding). The caller is responsible for writing the
// encoding-specific payload immediately after.
func (c *Conn) WriteRectHeader(r Rect) error {
	if err := c.writeU16(r.X); err != nil {
		return err
	}
	if err := c.writeU16(r.Y); err != nil {
		return err
	}
	if err := c.writeU16(r.W); err != nil {
		return err
	}
	if err := c.writeU16(r.H); err != nil {
		return err
	}
	return c.writeS32(int32(r.Encoding))
}

// WriteRawRect writes a Raw-encoded rectangle. `pixels` must be
// width*height*(bitsPerPixel/8) bytes laid out in row-major order
// using the format previously requested by the client (or the
// server's default if SetPixelFormat was never sent).
func (c *Conn) WriteRawRect(r Rect, pixels []byte) error {
	if r.Encoding != EncodingRaw {
		return fmt.Errorf("rfb: WriteRawRect called with encoding %d", r.Encoding)
	}
	if err := c.WriteRectHeader(r); err != nil {
		return err
	}
	return c.writeRaw(pixels)
}

// WriteOpenH264Rect writes an "Open H.264 Encoding" rectangle
// (pseudo-encoding 50). The H.264 payload must be a valid Annex-B
// bytestream — typically one or more NAL units concatenated with
// 00 00 00 01 (or 00 00 01) start codes.
//
// `flags` is the U32 flags field; commonly OpenH264FlagResetContext
// when the rectangle should reset the client's decoder state (e.g.
// on first frame, after a resolution change, or after any non-H.264
// fallback rectangle).
func (c *Conn) WriteOpenH264Rect(r Rect, flags uint32, h264 []byte) error {
	if r.Encoding != EncodingOpenH264 {
		return fmt.Errorf("rfb: WriteOpenH264Rect called with encoding %d", r.Encoding)
	}
	if err := c.WriteRectHeader(r); err != nil {
		return err
	}
	if err := c.writeU32(uint32(len(h264))); err != nil {
		return err
	}
	if err := c.writeU32(flags); err != nil {
		return err
	}
	return c.writeRaw(h264)
}

// WriteDesktopSizeRect emits a DesktopSize pseudo-encoding rectangle
// (-223). The framebuffer's new dimensions are encoded in the rect's
// W/H fields; X/Y are zero. There is no payload.
func (c *Conn) WriteDesktopSizeRect(width, height uint16) error {
	r := Rect{X: 0, Y: 0, W: width, H: height, Encoding: EncodingDesktopSize}
	return c.WriteRectHeader(r)
}
