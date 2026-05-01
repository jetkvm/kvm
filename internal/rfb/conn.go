package rfb

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"time"
)

// Conn wraps a net.Conn with framing buffers for the RFB protocol.
//
// Reads are not safe to issue from multiple goroutines, but writes are
// serialized internally. The typical pattern is one goroutine reading
// client messages and one writing server messages; the read side never
// writes (except via the dedicated handshake helpers, which run before
// either goroutine starts).
type Conn struct {
	nc net.Conn
	r  *bufio.Reader
	w  *bufio.Writer

	writeMu sync.Mutex
}

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

// LockWrite acquires the write mutex. Callers MUST call UnlockWrite —
// useful when emitting a multi-call composite message (e.g. a
// FramebufferUpdate header followed by per-rectangle data).
func (c *Conn) LockWrite() { c.writeMu.Lock() }

// UnlockWrite releases the write mutex.
func (c *Conn) UnlockWrite() { c.writeMu.Unlock() }

// Flush flushes the write buffer to the underlying connection. It
// acquires the write mutex internally.
func (c *Conn) Flush() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.w.Flush()
}

// flushLocked flushes assuming the write mutex is already held.
func (c *Conn) flushLocked() error { return c.w.Flush() }

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
