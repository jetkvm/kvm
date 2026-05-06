package rfb

import (
	"errors"
	"fmt"
)

// ErrUnsupportedProtocol is returned when the client offers a protocol
// version we cannot speak.
var ErrUnsupportedProtocol = errors.New("rfb: client requested an unsupported protocol version")

// ErrNoCommonSecurity is returned when the client and server share no
// security types.
var ErrNoCommonSecurity = errors.New("rfb: no common security type")

// ServerInit holds the parameters sent to the client immediately after
// security negotiation completes.
type ServerInit struct {
	Width       uint16
	Height      uint16
	PixelFormat PixelFormat
	Name        string
}

// ClientInit holds the single byte the client sends after security
// negotiation, indicating whether it wants shared access.
type ClientInit struct {
	Shared bool
}

// HandshakeServerVersion exchanges the 12-byte ProtocolVersion lines
// and returns the version string the client offered. We only support
// "RFB 003.008".
func (c *Conn) HandshakeServerVersion() (string, error) {
	if err := c.writeRaw([]byte(ProtocolVersion38)); err != nil {
		return "", fmt.Errorf("rfb: write server version: %w", err)
	}
	if err := c.Flush(); err != nil {
		return "", fmt.Errorf("rfb: flush server version: %w", err)
	}

	var buf [12]byte
	if err := c.readFull(buf[:]); err != nil {
		return "", fmt.Errorf("rfb: read client version: %w", err)
	}
	v := string(buf[:])
	// Accept only 3.8; older versions are not supported.
	if v != ProtocolVersion38 {
		return v, ErrUnsupportedProtocol
	}
	return v, nil
}

// OfferSecurityTypes advertises the supplied security types and waits
// for the client to pick one. Returns the chosen type. The list must
// not be empty.
//
// If types contains SecInvalid, this method writes a connection-failed
// reason instead of a type list and returns ErrNoCommonSecurity. This
// is the standard way to politely reject a client.
func (c *Conn) OfferSecurityTypes(types []SecurityType) (SecurityType, error) {
	if len(types) == 0 {
		return SecInvalid, errors.New("rfb: OfferSecurityTypes called with empty list")
	}

	if err := c.writeByte(byte(len(types))); err != nil {
		return SecInvalid, err
	}
	for _, t := range types {
		if err := c.writeByte(byte(t)); err != nil {
			return SecInvalid, err
		}
	}
	if err := c.Flush(); err != nil {
		return SecInvalid, err
	}

	pick, err := c.readByte()
	if err != nil {
		return SecInvalid, fmt.Errorf("rfb: read security choice: %w", err)
	}
	chosen := SecurityType(pick)

	for _, t := range types {
		if t == chosen {
			return chosen, nil
		}
	}
	return chosen, ErrNoCommonSecurity
}

// SendSecurityFailure tells the client we cannot proceed. Used in
// place of OfferSecurityTypes when the client should be rejected
// outright (e.g. unsupported protocol version).
func (c *Conn) SendSecurityFailure(reason string) error {
	if err := c.writeByte(0); err != nil {
		return err
	}
	if err := c.writeU32(uint32(len(reason))); err != nil {
		return err
	}
	if err := c.writeRaw([]byte(reason)); err != nil {
		return err
	}
	return c.Flush()
}

// SendSecurityResultOK signals successful authentication.
func (c *Conn) SendSecurityResultOK() error {
	if err := c.writeU32(SecResultOK); err != nil {
		return err
	}
	return c.Flush()
}

// SendSecurityResultFailed signals failed authentication with an
// optional reason string. Per RFC 6143 §7.1.3, the reason is only sent
// for protocol version 3.8.
func (c *Conn) SendSecurityResultFailed(reason string) error {
	if err := c.writeU32(SecResultFailed); err != nil {
		return err
	}
	if err := c.writeU32(uint32(len(reason))); err != nil {
		return err
	}
	if err := c.writeRaw([]byte(reason)); err != nil {
		return err
	}
	return c.Flush()
}

// ReadClientInit reads the 1-byte ClientInit (shared-flag).
func (c *Conn) ReadClientInit() (ClientInit, error) {
	b, err := c.readByte()
	if err != nil {
		return ClientInit{}, fmt.Errorf("rfb: read ClientInit: %w", err)
	}
	return ClientInit{Shared: b != 0}, nil
}

// SendServerInit writes the ServerInit message (RFC 6143 §7.3.2).
func (c *Conn) SendServerInit(init ServerInit) error {
	if err := c.writeU16(init.Width); err != nil {
		return err
	}
	if err := c.writeU16(init.Height); err != nil {
		return err
	}
	var pf [16]byte
	MarshalPixelFormat(init.PixelFormat, pf[:])
	if err := c.writeRaw(pf[:]); err != nil {
		return err
	}
	if err := c.writeU32(uint32(len(init.Name))); err != nil {
		return err
	}
	if err := c.writeRaw([]byte(init.Name)); err != nil {
		return err
	}
	return c.Flush()
}
