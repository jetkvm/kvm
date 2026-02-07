package rdpgw

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"
)

// maxData is the maximum RDP payload per MS-TSGU DATA packet.
// 16384 (client HLW buffer) - 8 (MS-TSGU header) - 2 (length prefix) = 16374.
const maxData = 16374

// tsguConn wraps a gateway Transport as a net.Conn so the RDP server can
// read/write directly through the gateway's WebSocket (or legacy HTTP)
// transport without an intermediate TCP connection or extra TLS layer.
type tsguConn struct {
	transport Transport
	readBuf   []byte // leftover data from a partial read
	closed    atomic.Bool
}

// newTSGUConn creates a net.Conn adapter over the given MS-TSGU transport.
func newTSGUConn(t Transport) *tsguConn {
	return &tsguConn{transport: t}
}

// Read implements net.Conn. It reads DATA packets from the transport, strips
// the 2-byte length prefix, and copies the RDP payload into p. Partial reads
// are buffered for subsequent calls.
func (c *tsguConn) Read(p []byte) (int, error) {
	// Serve from leftover buffer first
	if len(c.readBuf) > 0 {
		n := copy(p, c.readBuf)
		c.readBuf = c.readBuf[n:]
		return n, nil
	}

	for {
		pktType, payload, err := c.transport.ReadPacket()
		if err != nil {
			return 0, err
		}

		switch pktType {
		case pktTypeData:
			if len(payload) < 2 {
				continue
			}
			dataLen := int(binary.LittleEndian.Uint16(payload[0:2]))
			if 2+dataLen > len(payload) {
				dataLen = len(payload) - 2
			}
			data := payload[2 : 2+dataLen]

			n := copy(p, data)
			if n < len(data) {
				// Caller's buffer was too small — save the rest
				c.readBuf = make([]byte, len(data)-n)
				copy(c.readBuf, data[n:])
			}
			return n, nil

		case pktTypeKeepAlive:
			_ = c.transport.WritePacket(pktTypeKeepAlive, nil)
			continue

		case pktTypeCloseChannel:
			_ = c.transport.WritePacket(pktTypeCloseChannelResponse, buildCloseChannelResponse())
			return 0, io.EOF

		default:
			// Ignore unknown packet types during data phase
			continue
		}
	}
}

// Write implements net.Conn. It frames data with a 2-byte length prefix and
// sends it as one or more DATA packets. Writes larger than maxData are split.
func (c *tsguConn) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		chunk := p
		if len(chunk) > maxData {
			chunk = chunk[:maxData]
		}

		buf := make([]byte, 2+len(chunk))
		binary.LittleEndian.PutUint16(buf[0:2], uint16(len(chunk)))
		copy(buf[2:], chunk)

		if err := c.transport.WritePacket(pktTypeData, buf); err != nil {
			if total > 0 {
				return total, err
			}
			return 0, err
		}

		total += len(chunk)
		p = p[len(chunk):]
	}
	return total, nil
}

// Close implements net.Conn.
func (c *tsguConn) Close() error {
	if c.closed.Swap(true) {
		return nil
	}
	// Send CLOSE_CHANNEL with proper payload (errorCode + fieldsPresent + reserved = 8 bytes).
	// A nil payload causes BufferOverflowException in clients trying to read errorCode.
	close := make([]byte, 8)
	binary.LittleEndian.PutUint32(close[0:4], errorCodeSuccess)
	_ = c.transport.WritePacket(pktTypeCloseChannel, close)
	return c.transport.Close()
}

// LocalAddr implements net.Conn with a sentinel value (in-process connection).
func (c *tsguConn) LocalAddr() net.Addr {
	return tsguAddr("rdpgw-local")
}

// RemoteAddr implements net.Conn with a sentinel value.
func (c *tsguConn) RemoteAddr() net.Addr {
	return tsguAddr("rdpgw-remote")
}

// SetDeadline implements net.Conn (no-op for in-process connection).
func (c *tsguConn) SetDeadline(t time.Time) error { return nil }

// SetReadDeadline implements net.Conn (no-op).
func (c *tsguConn) SetReadDeadline(t time.Time) error { return nil }

// SetWriteDeadline implements net.Conn (no-op).
func (c *tsguConn) SetWriteDeadline(t time.Time) error { return nil }

// tsguAddr implements net.Addr for the in-process gateway connection.
type tsguAddr string

func (a tsguAddr) Network() string { return "tsgu" }
func (a tsguAddr) String() string  { return string(a) }

// Compile-time check that tsguConn implements net.Conn.
var _ net.Conn = (*tsguConn)(nil)

// Compile-time check that tsguAddr implements net.Addr.
var _ net.Addr = tsguAddr("")

// Verify the return type of newTSGUConn is usable as net.Conn.
var _ = func() net.Conn { return newTSGUConn(nil) }

func init() {
	// Verify maxData constant is correct.
	if maxData != 16374 {
		panic(fmt.Sprintf("rdpgw: maxData must be 16374, got %d", maxData))
	}
}
