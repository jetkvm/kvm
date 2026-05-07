package rfb

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"time"
)

// Conn wraps a net.Conn with framing buffers for the RFB protocol.
//
// The Conn assumes a single-writer model: at most one goroutine
// invokes write methods on the same Conn at a time. In typical use
// the handshake runs sequentially on one goroutine and is then
// followed by a per-connection dispatcher (also single goroutine)
// while a separate reader goroutine only calls read methods. Do not
// share writes between goroutines without external synchronization.
type Conn struct {
	nc net.Conn
	r  *bufio.Reader
	w  *bufio.Writer

	// extendedMouseButtons indicates that the Extended Mouse Buttons
	// extension (encoding -316) has been negotiated. The PointerEvent
	// reader uses it to pick the legacy (6-byte) or extended (7-byte)
	// wire format. Set by SetExtendedMouseButtons after the server
	// has sent the announce rectangle. Single-writer model also
	// applies to this flag.
	extendedMouseButtons bool
}

// SetExtendedMouseButtons enables decoding of the Extended Mouse
// Buttons extension on the next PointerEvent message. Call once
// after the announce rectangle has been written to the client.
func (c *Conn) SetExtendedMouseButtons(enabled bool) { c.extendedMouseButtons = enabled }

// NewConn wraps a net.Conn with read/write buffers.
func NewConn(nc net.Conn) *Conn {
	return &Conn{
		nc: nc,
		r:  bufio.NewReader(nc),
		w:  bufio.NewWriter(nc),
	}
}

// Close terminates the underlying connection.
func (c *Conn) Close() error { return c.nc.Close() }

// RemoteAddr returns the peer's network address.
func (c *Conn) RemoteAddr() net.Addr { return c.nc.RemoteAddr() }

// SetReadDeadline forwards to the underlying net.Conn.
func (c *Conn) SetReadDeadline(t time.Time) error { return c.nc.SetReadDeadline(t) }

// SetWriteDeadline forwards to the underlying net.Conn.
func (c *Conn) SetWriteDeadline(t time.Time) error { return c.nc.SetWriteDeadline(t) }

// Flush flushes the write buffer to the underlying connection.
func (c *Conn) Flush() error { return c.w.Flush() }

// readByte reads one byte.
func (c *Conn) readByte() (byte, error) { return c.r.ReadByte() }

// readFull fills buf entirely.
func (c *Conn) readFull(buf []byte) error {
	_, err := io.ReadFull(c.r, buf)
	return err
}

// readU16 reads a big-endian uint16.
func (c *Conn) readU16() (uint16, error) {
	var b [2]byte
	if err := c.readFull(b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b[:]), nil
}

// readU32 reads a big-endian uint32.
func (c *Conn) readU32() (uint32, error) {
	var b [4]byte
	if err := c.readFull(b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b[:]), nil
}

// readS32 reads a big-endian int32.
func (c *Conn) readS32() (int32, error) {
	v, err := c.readU32()
	return int32(v), err
}

// writeRaw writes buf verbatim to the buffered writer.
func (c *Conn) writeRaw(buf []byte) error {
	_, err := c.w.Write(buf)
	return err
}

// writeByte writes a single byte.
func (c *Conn) writeByte(b byte) error { return c.w.WriteByte(b) }

// writeU16 writes a big-endian uint16.
func (c *Conn) writeU16(v uint16) error {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	return c.writeRaw(b[:])
}

// writeU32 writes a big-endian uint32.
func (c *Conn) writeU32(v uint32) error {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return c.writeRaw(b[:])
}

// writeS32 writes a big-endian int32.
func (c *Conn) writeS32(v int32) error { return c.writeU32(uint32(v)) }
