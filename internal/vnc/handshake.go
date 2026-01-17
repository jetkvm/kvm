package vnc

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

// handshake performs the RFB protocol version handshake.
func (c *Connection) handshake() error {
	if err := c.conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return fmt.Errorf("failed to set handshake deadline: %w", err)
	}
	defer func() {
		if err := c.conn.SetDeadline(time.Time{}); err != nil {
			c.server.deps.Logger.Debug().Err(err).Msg("failed to clear handshake deadline")
		}
	}()

	if _, err := c.conn.Write([]byte(rfbProtocolVersion)); err != nil {
		return fmt.Errorf("failed to write protocol version: %w", err)
	}

	var versionBuf [12]byte
	if _, err := io.ReadFull(c.conn, versionBuf[:]); err != nil {
		return fmt.Errorf("failed to read client protocol version: %w", err)
	}

	if !bytes.HasPrefix(versionBuf[:], []byte("RFB 003.00")) {
		c.server.deps.Logger.Debug().Str("version", string(versionBuf[:])).Msg("client using non-standard RFB version")
	}

	return nil
}

// clientInit reads the client's shared flag.
func (c *Connection) clientInit() error {
	var sharedBuf [1]byte
	if _, err := io.ReadFull(c.conn, sharedBuf[:]); err != nil {
		return fmt.Errorf("failed to read shared flag: %w", err)
	}
	return nil
}

// serverInit sends the server initialization message.
func (c *Connection) serverInit() error {
	width, height := c.GetResolution()

	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufferPool.Put(buf)

	// All writes to bytes.Buffer are infallible for in-memory operations
	_ = binary.Write(buf, binary.BigEndian, width)
	_ = binary.Write(buf, binary.BigEndian, height)

	c.pixelFormatMu.Lock()
	pf := c.pixelFormat
	c.pixelFormatMu.Unlock()

	buf.WriteByte(pf.BitsPerPixel)
	buf.WriteByte(pf.Depth)
	buf.WriteByte(pf.BigEndian)
	buf.WriteByte(pf.TrueColor)
	_ = binary.Write(buf, binary.BigEndian, pf.RedMax)
	_ = binary.Write(buf, binary.BigEndian, pf.GreenMax)
	_ = binary.Write(buf, binary.BigEndian, pf.BlueMax)
	buf.WriteByte(pf.RedShift)
	buf.WriteByte(pf.GreenShift)
	buf.WriteByte(pf.BlueShift)
	buf.Write([]byte{0, 0, 0}) // Padding

	name := "JetKVM"
	_ = binary.Write(buf, binary.BigEndian, uint32(len(name)))
	buf.WriteString(name)

	if _, err := c.conn.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("failed to send server init: %w", err)
	}

	return nil
}
