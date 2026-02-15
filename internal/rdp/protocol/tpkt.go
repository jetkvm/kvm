package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
)

// packetPool provides pre-allocated buffers for zero-allocation packet building.
// Max packet size: TPKT(4) + X.224(3) + MCS(8) + VC(8) + DVC(~1609) = ~1632 bytes.
// Rounded up to 2KB for safety margin.
var packetPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 2048)
		return &buf
	},
}

// ReadBuffer provides reusable buffers for zero-allocation read operations.
// The buffer must be Released after use to return it to the pool.
type ReadBuffer struct {
	Data []byte     // The actual data slice
	buf  *[]byte    // Pointer to the underlying buffer for pool return
	pool *sync.Pool // The pool to return to
}

// Release returns the buffer to the pool for reuse.
// The Data slice must not be used after calling Release.
// Safe to call multiple times (becomes a no-op after first call).
func (b *ReadBuffer) Release() {
	if b.pool != nil && b.buf != nil {
		b.pool.Put(b.buf)
		b.buf = nil
		b.pool = nil
		b.Data = nil
	}
}

// Size-tiered buffer pools to minimize memory waste.
// Small: <2KB (control messages, small DVC)
// Medium: <16KB (typical DVC, most GFX PDUs)
// Large: <64KB (max TPKT, large GFX frames)
var (
	readPoolSmall = sync.Pool{
		New: func() any {
			buf := make([]byte, 2048)
			return &buf
		},
	}
	readPoolMedium = sync.Pool{
		New: func() any {
			buf := make([]byte, 16384)
			return &buf
		},
	}
	readPoolLarge = sync.Pool{
		New: func() any {
			buf := make([]byte, 65536)
			return &buf
		},
	}
)

// getReadBuffer returns a buffer from the appropriate pool based on size.
func getReadBuffer(size int) (*[]byte, *sync.Pool) {
	if size <= 2048 {
		return readPoolSmall.Get().(*[]byte), &readPoolSmall
	}
	if size <= 16384 {
		return readPoolMedium.Get().(*[]byte), &readPoolMedium
	}
	return readPoolLarge.Get().(*[]byte), &readPoolLarge
}

// TPKT implements RFC 1006 Transport Protocol over TCP.
// It provides the framing layer for ISO transport classes over TCP.
//
// TPKT Header format (4 bytes):
//
//	+--------+--------+--------+--------+
//	| vrsn=3 |reserved| length (BE u16) |
//	+--------+--------+--------+--------+
type TPKT struct {
	Version  uint8
	Reserved uint8
	Length   uint16 // Total length including header
}

var (
	ErrTPKTInvalidVersion = errors.New("tpkt: invalid version (expected 3)")
	ErrTPKTLengthTooSmall = errors.New("tpkt: length less than header size")
	ErrTPKTLengthTooLarge = errors.New("tpkt: length exceeds maximum")
)

// ReadTPKT reads a TPKT header from the reader.
func ReadTPKT(r io.Reader) (*TPKT, error) {
	var header [TPKTHeaderLength]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, fmt.Errorf("tpkt: read header: %w", err)
	}

	t := &TPKT{
		Version:  header[0],
		Reserved: header[1],
		Length:   binary.BigEndian.Uint16(header[2:4]),
	}

	if t.Version != TPKTVersion {
		return nil, ErrTPKTInvalidVersion
	}

	if t.Length < TPKTHeaderLength {
		return nil, ErrTPKTLengthTooSmall
	}

	if t.Length > MaxTPKTLength {
		return nil, ErrTPKTLengthTooLarge
	}

	return t, nil
}

// PayloadLength returns the length of the payload (excluding header).
func (t *TPKT) PayloadLength() int {
	return int(t.Length) - TPKTHeaderLength
}

// ReadTPKTPayload reads a complete TPKT packet and returns the payload.
// NOTE: This allocates a new buffer for each call. For hot paths, use
// ReadTPKTPayloadPooled instead to reuse buffers.
func ReadTPKTPayload(r io.Reader) ([]byte, error) {
	header, err := ReadTPKT(r)
	if err != nil {
		return nil, err
	}

	payloadLen := header.PayloadLength()
	if payloadLen == 0 {
		return nil, nil
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("tpkt: read payload: %w", err)
	}

	return payload, nil
}

// ReadTPKTPayloadPooled reads a complete TPKT packet using a pooled buffer.
// Returns a ReadBuffer that MUST be Released after use to return the buffer to the pool.
// For zero-allocation hot paths.
//
// Usage:
//
//	buf, err := ReadTPKTPayloadPooled(reader)
//	if err != nil { return err }
//	defer buf.Release()
//	// Process buf.Data...
func ReadTPKTPayloadPooled(r io.Reader) (*ReadBuffer, error) {
	header, err := ReadTPKT(r)
	if err != nil {
		return nil, err
	}

	payloadLen := header.PayloadLength()
	if payloadLen == 0 {
		return &ReadBuffer{}, nil
	}

	// Get appropriately-sized buffer from pool
	bufPtr, pool := getReadBuffer(payloadLen)
	buf := *bufPtr

	// Ensure buffer is large enough (should always be true with our pools)
	if len(buf) < payloadLen {
		pool.Put(bufPtr)
		// Fallback: allocate new buffer (rare path for >64KB)
		buf = make([]byte, payloadLen)
		bufPtr = &buf
		pool = nil
	}

	if _, err := io.ReadFull(r, buf[:payloadLen]); err != nil {
		if pool != nil {
			pool.Put(bufPtr)
		}
		return nil, fmt.Errorf("tpkt: read payload: %w", err)
	}

	return &ReadBuffer{
		Data: buf[:payloadLen],
		buf:  bufPtr,
		pool: pool,
	}, nil
}

// WriteTPKT writes a TPKT packet to the writer.
// The packet is built completely in memory and written with a single Write() call
// to ensure atomicity when multiple goroutines write to the same connection.
// Go's TLS Write() is internally serialized, so a single Write() call is safe.
func WriteTPKT(w io.Writer, payload []byte) error {
	packet, err := BuildTPKT(payload)
	if err != nil {
		return err
	}

	if _, err := w.Write(packet); err != nil {
		return fmt.Errorf("tpkt: write: %w", err)
	}

	return nil
}

// BuildTPKT builds a complete TPKT packet with header and payload.
func BuildTPKT(payload []byte) ([]byte, error) {
	totalLen := TPKTHeaderLength + len(payload)
	if totalLen > MaxTPKTLength {
		return nil, ErrTPKTLengthTooLarge
	}

	packet := make([]byte, totalLen)
	packet[0] = TPKTVersion
	packet[1] = 0 // reserved
	binary.BigEndian.PutUint16(packet[2:4], uint16(totalLen))
	copy(packet[TPKTHeaderLength:], payload)

	return packet, nil
}
