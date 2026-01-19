package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

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
