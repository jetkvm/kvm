package rdpgw

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Transport abstracts the packet I/O layer for MS-TSGU.
// WebSocket (Windows App) and legacy HTTP (mstsc.exe) use different framing.
type Transport interface {
	ReadPacket() (pktType uint16, payload []byte, err error)
	WritePacket(pktType uint16, payload []byte) error
	Close() error
}

// --- WebSocket transport ---
//
// A single WebSocket message can contain multiple MS-TSGU packets (multiplexing),
// or a packet can span multiple messages (fragmentation). We buffer incoming
// message data and parse packets using the totalSize header field, matching
// the reference rdpgw implementation's defragmentation logic.

type wsTransport struct {
	ws     *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex // serializes writes
	buf    []byte     // buffered data from WebSocket messages
}

func NewWSTransport(ctx context.Context, ws *websocket.Conn) *wsTransport {
	ctx, cancel := context.WithCancel(ctx)
	return &wsTransport{ws: ws, ctx: ctx, cancel: cancel}
}

func (t *wsTransport) ReadPacket() (uint16, []byte, error) {
	// Ensure we have at least the header
	for len(t.buf) < packetHeaderSize {
		if err := t.readMore(); err != nil {
			return 0, nil, err
		}
	}

	// Parse totalSize from header to know when the full packet is available
	totalSize := int(binary.LittleEndian.Uint32(t.buf[4:8]))
	if totalSize < packetHeaderSize || totalSize > maxPacketSize {
		return 0, nil, fmt.Errorf("invalid packet size %d", totalSize)
	}

	// Read more WebSocket messages until we have the complete packet
	for len(t.buf) < totalSize {
		if err := t.readMore(); err != nil {
			return 0, nil, err
		}
	}

	// Extract this packet and advance the buffer
	pktType := binary.LittleEndian.Uint16(t.buf[0:2])
	payload := make([]byte, totalSize-packetHeaderSize)
	copy(payload, t.buf[packetHeaderSize:totalSize])
	t.buf = t.buf[totalSize:]

	return pktType, payload, nil
}

// readMore reads one WebSocket message and appends it to the buffer.
func (t *wsTransport) readMore() error {
	_, data, err := t.ws.Read(t.ctx)
	if err != nil {
		return fmt.Errorf("ws read: %w", err)
	}
	t.buf = append(t.buf, data...)
	return nil
}

func (t *wsTransport) WritePacket(pktType uint16, payload []byte) error {
	totalSize := packetHeaderSize + len(payload)
	buf := make([]byte, totalSize)
	binary.LittleEndian.PutUint16(buf[0:2], pktType)
	// buf[2:4] reserved
	binary.LittleEndian.PutUint32(buf[4:8], uint32(totalSize))
	copy(buf[packetHeaderSize:], payload)

	t.mu.Lock()
	defer t.mu.Unlock()

	return t.ws.Write(t.ctx, websocket.MessageBinary, buf)
}

func (t *wsTransport) Close() error {
	t.cancel()
	return t.ws.CloseNow()
}

// --- Legacy HTTP transport ---
//
// The MS-TSGU legacy transport (mstsc.exe, Windows App fallback) uses two
// HTTP connections paired by Rdg-Connection-Id:
//   - RDG_OUT_DATA: server → client (response body streamed as raw bytes)
//   - RDG_IN_DATA: client → server (request body in HTTP chunked encoding)
//
// Per MS-TSGU spec section 3.2.5.5.1:
//   - The OUT response includes 10 random seed bytes (no Content-Length)
//   - The IN response has Content-Length: 0
//   - The IN request body uses HTTP chunked transfer encoding
//
// legacyTransport wraps the paired connections into a single Transport.

type legacyTransport struct {
	reader  io.Reader  // chunked reader for IN (client→server)
	outConn net.Conn   // raw OUT connection (server→client writes)
	inConn  net.Conn   // raw IN connection (for close)
	mu      sync.Mutex // serializes writes
}

// NewLegacyTransport creates a transport from a paired IN/OUT legacy connection.
// The reader should be an httputil.NewChunkedReader wrapping the IN connection's
// bufio.Reader (to decode HTTP chunked transfer encoding from the client).
// The outConn is the raw OUT connection for writing raw bytes to the client.
func NewLegacyTransport(reader io.Reader, inConn, outConn net.Conn) *legacyTransport {
	return &legacyTransport{
		reader:  reader,
		outConn: outConn,
		inConn:  inConn,
	}
}

func (t *legacyTransport) ReadPacket() (uint16, []byte, error) {
	return readPacket(t.reader)
}

func (t *legacyTransport) WritePacket(pktType uint16, payload []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Write raw bytes directly to the OUT connection (no chunked encoding).
	// The OUT channel streams raw MS-TSGU packets in the HTTP response body.
	totalSize := packetHeaderSize + len(payload)
	buf := make([]byte, totalSize)
	binary.LittleEndian.PutUint16(buf[0:2], pktType)
	// buf[2:4] reserved
	binary.LittleEndian.PutUint32(buf[4:8], uint32(totalSize))
	copy(buf[packetHeaderSize:], payload)

	_, err := t.outConn.Write(buf)
	return err
}

func (t *legacyTransport) Close() error {
	t.inConn.Close()
	t.outConn.Close()
	return nil
}

// --- Legacy session pairing ---
//
// The client sends two HTTP requests with the same Rdg-Connection-Id header.
// The OUT connection arrives first, then the IN connection.
// We cache the OUT connection and pair it when the IN connection arrives.

// PendingLegacy holds the hijacked RDG_OUT_DATA connection waiting for its
// RDG_IN_DATA pair.
type PendingLegacy struct {
	OutConn   net.Conn
	createdAt time.Time
}

var (
	legacyPending   = make(map[string]*PendingLegacy)
	legacyPendingMu sync.Mutex
)

const legacyPairingTimeout = 30 * time.Second

// RegisterLegacyOut registers the RDG_OUT_DATA connection for pairing.
func RegisterLegacyOut(connID string, conn net.Conn) {
	legacyPendingMu.Lock()
	legacyPending[connID] = &PendingLegacy{
		OutConn:   conn,
		createdAt: time.Now(),
	}
	legacyPendingMu.Unlock()
}

// ClaimLegacyOut pairs an RDG_IN_DATA connection with the cached RDG_OUT_DATA.
// Returns nil if no matching OUT connection is found.
func ClaimLegacyOut(connID string) *PendingLegacy {
	legacyPendingMu.Lock()
	defer legacyPendingMu.Unlock()

	p, ok := legacyPending[connID]
	if !ok {
		return nil
	}
	delete(legacyPending, connID)

	if time.Since(p.createdAt) > legacyPairingTimeout {
		p.OutConn.Close()
		return nil
	}

	return p
}

// HijackRaw extracts the raw net.Conn and bufio.ReadWriter from an HTTP
// ResponseWriter. Unlike a simple Hijack, this preserves the bufio.ReadWriter
// so callers can use the buffered reader (for chunked decoding) and buffered
// writer (for sending the HTTP response).
func HijackRaw(w http.ResponseWriter) (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, nil, fmt.Errorf("hijack: %w", err)
	}
	return conn, rw, nil
}

// SendLegacyAccept writes the HTTP 200 response on a hijacked legacy connection.
//
// Per MS-TSGU spec: the OUT response includes 10 random seed bytes to trigger
// proxy passthrough. The IN response has Content-Length: 0 (empty body).
func SendLegacyAccept(rw *bufio.ReadWriter, isOut bool) error {
	rw.WriteString("HTTP/1.1 200 OK\r\n")
	rw.WriteString("Date: " + time.Now().Format(time.RFC1123) + "\r\n")
	if !isOut {
		rw.WriteString("Content-Length: 0\r\n")
	}
	rw.WriteString("\r\n")

	if isOut {
		seed := make([]byte, 10)
		rand.Read(seed)
		rw.Write(seed)
	}
	return rw.Flush()
}

// NewChunkedReader creates an HTTP chunked reader from the hijacked
// connection's bufio.Reader. The MS-TSGU legacy IN channel uses HTTP
// chunked transfer encoding for the request body.
func NewChunkedReader(rw *bufio.ReadWriter) io.Reader {
	return httputil.NewChunkedReader(rw.Reader)
}

// DrainLegacy reads and discards initial data from the raw connection.
// Called after SendLegacyAccept on the IN channel before starting the
// MS-TSGU protocol.
func DrainLegacy(conn net.Conn) {
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	p := make([]byte, 32767)
	conn.Read(p)
	conn.SetReadDeadline(time.Time{}) // clear deadline
}
