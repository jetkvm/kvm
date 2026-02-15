package udp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/rs/zerolog"
)

// RDPEMT action codes (MS-RDPEMT 2.2.1).
const (
	EMTActionCreateReq  = 0x00
	EMTActionCreateResp = 0x01
	EMTActionData       = 0x04
)

// EMT header size.
const emtHeaderLen = 4

// EMT Create Request payload size (16-byte cookie).
const emtCookieLen = 16

// Common EMT errors.
var (
	ErrEMTInvalidCookie = errors.New("emt: security cookie mismatch")
	ErrEMTClosed        = errors.New("emt: tunnel closed")
)

// Tunnel wraps a TLS connection running over RDPEUDP2 and provides
// RDPEMT framing (Create Request/Response and Data).
type Tunnel struct {
	tlsConn   net.Conn   // TLS-over-RDPEUDP2
	transport *Transport // Underlying RDPEUDP2 transport
	cookie    [16]byte   // Expected security cookie
	logger    *zerolog.Logger
}

// NewTunnel creates a new RDPEMT tunnel.
func NewTunnel(tlsConn net.Conn, transport *Transport, cookie [16]byte, logger *zerolog.Logger) *Tunnel {
	return &Tunnel{
		tlsConn:   tlsConn,
		transport: transport,
		cookie:    cookie,
		logger:    logger,
	}
}

// Handshake waits for the client's EMT Create Request, validates the cookie,
// and sends the Create Response.
func (t *Tunnel) Handshake() error {
	// Read EMT header
	hdr := make([]byte, emtHeaderLen)
	if _, err := io.ReadFull(t.tlsConn, hdr); err != nil {
		return fmt.Errorf("emt: read create request header: %w", err)
	}

	// Parse header: byte[0] = (Action << 4) | Flags
	action := (hdr[0] >> 4) & 0x0F
	payloadLen := binary.BigEndian.Uint16(hdr[1:3])

	if action != EMTActionCreateReq {
		return fmt.Errorf("emt: expected Create Request (0x%02X), got 0x%02X", EMTActionCreateReq, action)
	}

	// Read payload (contains security cookie)
	if payloadLen < emtCookieLen {
		return fmt.Errorf("emt: Create Request payload too short: %d", payloadLen)
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(t.tlsConn, payload); err != nil {
		return fmt.Errorf("emt: read create request payload: %w", err)
	}

	// Validate security cookie
	var clientCookie [16]byte
	copy(clientCookie[:], payload[:16])
	if clientCookie != t.cookie {
		return ErrEMTInvalidCookie
	}

	t.logger.Debug().Msg("EMT: Create Request validated, sending response")

	// Send Create Response
	resp := make([]byte, emtHeaderLen)
	resp[0] = EMTActionCreateResp << 4 // Action | Flags
	binary.BigEndian.PutUint16(resp[1:3], 0)
	resp[3] = emtHeaderLen // HeaderLength

	if _, err := t.tlsConn.Write(resp); err != nil {
		return fmt.Errorf("emt: write create response: %w", err)
	}

	t.logger.Debug().Msg("EMT: tunnel established")
	return nil
}

// ReadData reads one RDPEMT data payload, stripping the 4-byte EMT header.
func (t *Tunnel) ReadData() ([]byte, error) {
	hdr := make([]byte, emtHeaderLen)
	if _, err := io.ReadFull(t.tlsConn, hdr); err != nil {
		return nil, err
	}

	action := (hdr[0] >> 4) & 0x0F
	payloadLen := binary.BigEndian.Uint16(hdr[1:3])

	if action != EMTActionData {
		return nil, fmt.Errorf("emt: expected Data (0x%02X), got 0x%02X", EMTActionData, action)
	}

	if payloadLen == 0 {
		return nil, nil
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(t.tlsConn, payload); err != nil {
		return nil, err
	}

	return payload, nil
}

// WriteData writes data with an RDPEMT Data header.
func (t *Tunnel) WriteData(data []byte) error {
	// Build EMT header + data
	pkt := make([]byte, emtHeaderLen+len(data))
	pkt[0] = EMTActionData << 4 // Action | Flags
	binary.BigEndian.PutUint16(pkt[1:3], uint16(len(data)))
	pkt[3] = emtHeaderLen // HeaderLength
	copy(pkt[emtHeaderLen:], data)

	_, err := t.tlsConn.Write(pkt)
	return err
}

// Close closes the RDPEMT tunnel and its underlying transport.
func (t *Tunnel) Close() error {
	var firstErr error

	if err := t.tlsConn.Close(); err != nil {
		firstErr = err
	}
	if err := t.transport.Close(); err != nil && firstErr == nil {
		firstErr = err
	}

	return firstErr
}

// Transport returns the underlying RDPEUDP2 transport.
func (t *Tunnel) Transport() *Transport {
	return t.transport
}
