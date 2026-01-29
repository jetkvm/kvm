package vnc

import (
	"crypto/des"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"slices"
	"time"
)

// TLS Hook Functions
//
// These hooks are set by the kvm package during initialization, BEFORE calling Server.Start().
// They enable TLS features without creating import cycles between vnc and kvm packages.
//
// IMPORTANT: All hooks MUST be set before Server.Start() is called if TLS is enabled.
// The server validates this requirement at startup when TLS is configured.
// These are safe to read concurrently after initialization (write-once pattern).
var (
	// TLSAvailabilityChecker returns true if TLS certificates are ready and valid.
	TLSAvailabilityChecker func() bool

	// TLSConnUpgrader upgrades a plain connection to TLS using the server's certificate.
	// Used for all VeNCrypt subtypes (both "anonymous" TLS and X509). Modern clients
	// don't support true Anonymous DH (ADH ciphers), so we always present the server
	// certificate — the distinction between TLS* and X509* subtypes is only whether
	// the client verifies it.
	TLSConnUpgrader func(conn net.Conn) (TLSConnection, error)

	// IsHardwareCryptoEnabledFunc returns true if hardware crypto acceleration is enabled.
	IsHardwareCryptoEnabledFunc func() bool

	// GetHardwareCryptoEngineFunc returns the hardware crypto engine name (e.g., "devcrypto").
	GetHardwareCryptoEngineFunc func() string
)

// TLSConnection represents an upgraded TLS connection.
type TLSConnection interface {
	net.Conn
	GetProtocolVersion() string
	GetCipherName() string
}

// isTLSAvailable checks if TLS is currently available for VNC connections.
func (c *Connection) isTLSAvailable() bool {
	if TLSAvailabilityChecker != nil {
		return TLSAvailabilityChecker()
	}
	// Fallback: check if TLS provider indicates availability
	if c.server.deps.TLS != nil {
		return c.server.deps.TLS.IsTLSAvailable()
	}
	return false
}

// authenticate performs VNC authentication handshake.
func (c *Connection) authenticate() error {
	if err := c.conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return fmt.Errorf("failed to set auth deadline: %w", err)
	}
	defer func() {
		if err := c.conn.SetDeadline(time.Time{}); err != nil {
			c.server.deps.Logger.Debug().Err(err).Msg("failed to clear auth deadline")
		}
	}()

	if tcpConn, ok := c.conn.(*net.TCPConn); ok {
		if err := tcpConn.SetNoDelay(true); err != nil {
			c.server.deps.Logger.Warn().Err(err).Msg("failed to set TCP_NODELAY - VNC input may feel laggy")
		}
	}

	// Check for raw password - VNC auth requires the actual password, not just a hash
	// VNC uses DES encryption with the raw password as key
	hasPassword := c.server.deps.Config.GetLocalAuthPassword() != ""

	// VeNCrypt provides TLS encryption for VNC connections
	// When TLS is enabled and available, ONLY offer VeNCrypt (no insecure fallback)
	// When TLS is not available, fall back to VNCAuth with warning (needed for initial setup)
	tlsMode := c.server.deps.Config.GetTLSMode()
	tlsEnabled := c.server.IsTLSEnabled() && tlsMode != "" && tlsMode != "disabled"
	tlsAvailable := tlsEnabled && c.isTLSAvailable()

	// Build security types list based on TLS and password configuration
	var secTypes []byte
	fallbackSecType := byte(secTypeNone)
	if hasPassword {
		fallbackSecType = byte(secTypeVNCAuth)
	}

	if tlsEnabled && tlsAvailable {
		// TLS ready: only offer VeNCrypt, no insecure fallback
		secTypes = []byte{1, byte(secTypeVeNCrypt)}
		c.server.deps.Logger.Debug().Str("remote", c.conn.RemoteAddr().String()).
			Msg("VNC TLS available, offering VeNCrypt only")
	} else if tlsEnabled {
		// TLS enabled but not yet available (cert/time issue): offer both for initial setup
		secTypes = []byte{2, byte(secTypeVeNCrypt), fallbackSecType}
		c.server.deps.Logger.Warn().Str("remote", c.conn.RemoteAddr().String()).
			Msg("VNC TLS enabled but not available (certificate/time issue) - offering both secure and insecure options")
	} else {
		// TLS not enabled - use insecure modes with warning
		secTypes = []byte{1, fallbackSecType}
		c.server.deps.Logger.Warn().Str("remote", c.conn.RemoteAddr().String()).
			Msg("SECURITY WARNING: VNC connection without TLS encryption - enable TLS in settings")
	}

	if _, err := c.conn.Write(secTypes); err != nil {
		return fmt.Errorf("failed to send security types: %w", err)
	}

	var secType [1]byte
	if _, err := io.ReadFull(c.conn, secType[:]); err != nil {
		return fmt.Errorf("failed to read security type: %w", err)
	}

	switch rfbSecurityType(secType[0]) {
	case secTypeVeNCrypt:
		return c.authenticateVeNCrypt(hasPassword)
	case secTypeVNCAuth:
		return c.authenticateVNCAuth()
	case secTypeNone:
		return c.authenticateNone()
	default:
		return fmt.Errorf("unsupported security type: %d", secType[0])
	}
}

// authenticateVeNCrypt handles VeNCrypt authentication with TLS.
func (c *Connection) authenticateVeNCrypt(hasPassword bool) error {
	if _, err := c.conn.Write([]byte{0, 2}); err != nil {
		return fmt.Errorf("failed to send VeNCrypt version: %w", err)
	}

	var clientVersion [2]byte
	if _, err := io.ReadFull(c.conn, clientVersion[:]); err != nil {
		return fmt.Errorf("failed to read client VeNCrypt version: %w", err)
	}

	if clientVersion[0] != 0 || clientVersion[1] < 2 {
		if _, err := c.conn.Write([]byte{1}); err != nil {
			c.server.deps.Logger.Debug().Err(err).Msg("failed to send VeNCrypt version rejection")
		}
		return fmt.Errorf("unsupported VeNCrypt version: %d.%d", clientVersion[0], clientVersion[1])
	}

	if _, err := c.conn.Write([]byte{0}); err != nil {
		return fmt.Errorf("failed to send version acceptance: %w", err)
	}

	// Offer both X509 and anonymous TLS subtypes. X509 is preferred (listed first)
	// because it provides MITM protection via certificate verification.
	// Anonymous TLS subtypes are included for clients that don't support X509
	// (e.g., Jump Desktop only supports TLSVnc/TLSPlain/TLSNone).
	var subtypes []veNCryptSubtype
	if hasPassword {
		subtypes = []veNCryptSubtype{veNCryptX509Vnc, veNCryptX509Plain, veNCryptTLSVnc, veNCryptTLSPlain}
	} else {
		subtypes = []veNCryptSubtype{veNCryptX509None, veNCryptTLSNone}
	}

	subtypeBuf := make([]byte, 1+len(subtypes)*4)
	subtypeBuf[0] = byte(len(subtypes))
	for i, st := range subtypes {
		binary.BigEndian.PutUint32(subtypeBuf[1+i*4:], uint32(st))
	}

	if _, err := c.conn.Write(subtypeBuf); err != nil {
		return fmt.Errorf("failed to send VeNCrypt subtypes: %w", err)
	}

	var selectedBuf [4]byte
	if _, err := io.ReadFull(c.conn, selectedBuf[:]); err != nil {
		return fmt.Errorf("failed to read selected subtype: %w", err)
	}
	selectedSubtype := veNCryptSubtype(binary.BigEndian.Uint32(selectedBuf[:]))

	if !slices.Contains(subtypes, selectedSubtype) {
		// VeNCrypt spec: non-zero = rejection
		if _, err := c.conn.Write([]byte{1}); err != nil {
			c.server.deps.Logger.Debug().Err(err).Msg("failed to send subtype rejection")
		}
		return fmt.Errorf("client selected unsupported subtype: %d", selectedSubtype)
	}

	// VeNCrypt subtype ACK (TigerVNC-compatible: 1 = success)
	if _, err := c.conn.Write([]byte{1}); err != nil {
		return fmt.Errorf("failed to send subtype acceptance: %w", err)
	}

	// All VeNCrypt subtypes use the same certificate-based TLS upgrade.
	// Modern clients don't support true ADH (Anonymous DH) cipher suites,
	// so we always present the server certificate. The distinction between
	// TLS* and X509* subtypes is only whether the client verifies the cert.
	if err := c.upgradeTLS(); err != nil {
		return fmt.Errorf("TLS handshake failed: %w", err)
	}

	// Perform authentication based on selected subtype
	var authErr error
	switch selectedSubtype {
	case veNCryptTLSVnc, veNCryptX509Vnc:
		authErr = c.vncAuth()
	case veNCryptTLSPlain, veNCryptX509Plain:
		authErr = c.plainAuth()
	}

	if authErr != nil {
		c.authFailures++
		time.Sleep(authFailureDelay) // Delay to prevent brute-force
		if sendErr := c.sendAuthResult(1, "Authentication failed"); sendErr != nil {
			c.server.deps.Logger.Debug().Err(sendErr).Str("remote", c.conn.RemoteAddr().String()).Msg("failed to send auth failure result")
		}
		if c.authFailures >= maxAuthFailures {
			return fmt.Errorf("too many auth failures (%d), disconnecting", c.authFailures)
		}
		return authErr
	}

	if err := c.sendAuthResult(0, ""); err != nil {
		return err
	}

	c.server.deps.Logger.Info().Str("remote", c.conn.RemoteAddr().String()).Msg("VeNCrypt authentication complete")
	return nil
}

// upgradeTLS upgrades the connection to TLS using the server's certificate.
// Used for all VeNCrypt subtypes. The server always presents its certificate;
// modern clients don't support true ADH (Anonymous DH) cipher suites, so
// we use standard X509 cipher suites for all subtypes. The only difference
// between TLS* and X509* subtypes is the authentication method (VNC auth,
// plain auth, or none) — the TLS layer is identical.
func (c *Connection) upgradeTLS() error {
	if TLSConnUpgrader == nil {
		return fmt.Errorf("TLS upgrader not configured")
	}

	tlsConn, err := TLSConnUpgrader(c.conn)
	if err != nil {
		return fmt.Errorf("TLS handshake failed: %w", err)
	}

	c.conn = &tlsConnWrapper{tlsConn: tlsConn}

	hwCrypto := false
	hwEngine := ""
	if IsHardwareCryptoEnabledFunc != nil {
		hwCrypto = IsHardwareCryptoEnabledFunc()
	}
	if GetHardwareCryptoEngineFunc != nil {
		hwEngine = GetHardwareCryptoEngineFunc()
	}

	c.server.deps.Logger.Info().
		Str("version", tlsConn.GetProtocolVersion()).
		Str("cipherSuite", tlsConn.GetCipherName()).
		Bool("hwCrypto", hwCrypto).
		Str("hwEngine", hwEngine).
		Str("remote", c.conn.RemoteAddr().String()).
		Msg("VeNCrypt TLS handshake complete")

	return nil
}

// tlsConnWrapper wraps a TLSConnection to implement net.Conn.
// On ARM Linux, the TLSConnection is backed by OpenSSL with hardware crypto;
// deadlines use SO_SNDTIMEO/SO_RCVTIMEO kernel socket timeouts because OpenSSL
// sets the fd to blocking mode (Go's epoll-based deadlines don't apply).
type tlsConnWrapper struct {
	tlsConn TLSConnection
}

func (w *tlsConnWrapper) Read(b []byte) (int, error)         { return w.tlsConn.Read(b) }
func (w *tlsConnWrapper) Write(b []byte) (int, error)        { return w.tlsConn.Write(b) }
func (w *tlsConnWrapper) Close() error                       { return w.tlsConn.Close() }
func (w *tlsConnWrapper) LocalAddr() net.Addr                { return w.tlsConn.LocalAddr() }
func (w *tlsConnWrapper) RemoteAddr() net.Addr               { return w.tlsConn.RemoteAddr() }
func (w *tlsConnWrapper) SetDeadline(t time.Time) error      { return w.tlsConn.SetDeadline(t) }
func (w *tlsConnWrapper) SetReadDeadline(t time.Time) error  { return w.tlsConn.SetReadDeadline(t) }
func (w *tlsConnWrapper) SetWriteDeadline(t time.Time) error { return w.tlsConn.SetWriteDeadline(t) }

// authenticateVNCAuth handles VNC challenge-response authentication.
func (c *Connection) authenticateVNCAuth() error {
	if err := c.vncAuth(); err != nil {
		c.authFailures++
		time.Sleep(authFailureDelay) // Delay to prevent brute-force
		if sendErr := c.sendAuthResult(1, "Authentication failed"); sendErr != nil {
			c.server.deps.Logger.Debug().Err(sendErr).Str("remote", c.conn.RemoteAddr().String()).Msg("failed to send auth failure result")
		}
		if c.authFailures >= maxAuthFailures {
			return fmt.Errorf("too many auth failures (%d), disconnecting", c.authFailures)
		}
		return err
	}
	return c.sendAuthResult(0, "")
}

// authenticateNone handles no-authentication mode.
func (c *Connection) authenticateNone() error {
	return c.sendAuthResult(0, "")
}

// vncAuth performs VNC DES challenge-response authentication.
func (c *Connection) vncAuth() error {
	var challenge [vncChallengeSize]byte
	if _, err := rand.Read(challenge[:]); err != nil {
		return fmt.Errorf("failed to generate challenge: %w", err)
	}

	if _, err := c.conn.Write(challenge[:]); err != nil {
		return fmt.Errorf("failed to send challenge: %w", err)
	}

	var response [vncChallengeSize]byte
	if _, err := io.ReadFull(c.conn, response[:]); err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	password := c.server.deps.Config.GetLocalAuthPassword()
	if password == "" {
		return fmt.Errorf("no VNC password configured")
	}

	expected, err := computeVNCResponse(challenge[:], password)
	if err != nil {
		return fmt.Errorf("failed to compute response: %w", err)
	}

	if subtle.ConstantTimeCompare(response[:], expected) != 1 {
		return fmt.Errorf("authentication failed")
	}

	return nil
}

// plainAuth performs plaintext username/password authentication.
func (c *Connection) plainAuth() error {
	var lenBuf [8]byte
	if _, err := io.ReadFull(c.conn, lenBuf[:]); err != nil {
		return fmt.Errorf("failed to read auth lengths: %w", err)
	}

	usernameLen := binary.BigEndian.Uint32(lenBuf[0:4])
	passwordLen := binary.BigEndian.Uint32(lenBuf[4:8])

	if usernameLen > maxCredentialLength || passwordLen > maxCredentialLength {
		return fmt.Errorf("username or password too long")
	}

	username := make([]byte, usernameLen)
	if usernameLen > 0 {
		if _, err := io.ReadFull(c.conn, username); err != nil {
			return fmt.Errorf("failed to read username: %w", err)
		}
	}

	password := make([]byte, passwordLen)
	if passwordLen > 0 {
		if _, err := io.ReadFull(c.conn, password); err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}
	}

	expectedPassword := c.server.deps.Config.GetLocalAuthPassword()
	if expectedPassword == "" {
		return fmt.Errorf("no password configured")
	}

	if subtle.ConstantTimeCompare(password, []byte(expectedPassword)) != 1 {
		return fmt.Errorf("authentication failed")
	}

	return nil
}

// computeVNCResponse implements VNC challenge-response authentication.
// VNC uses DES encryption with a historical quirk: each byte of the
// password key must have its bits reversed before use. This originated
// from the original AT&T VNC implementation's byte order handling.
// The password is truncated/padded to exactly 8 bytes.
func computeVNCResponse(challenge []byte, password string) ([]byte, error) {
	key := make([]byte, 8)
	copy(key, []byte(password))

	for i := range key {
		key[i] = reverseBits(key[i])
	}

	block, err := des.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create DES cipher: %w", err)
	}

	response := make([]byte, vncChallengeSize)
	block.Encrypt(response[0:8], challenge[0:8])
	block.Encrypt(response[8:vncChallengeSize], challenge[8:vncChallengeSize])

	return response, nil
}

// reverseBits reverses the bits in a byte (VNC DES quirk).
func reverseBits(b byte) byte {
	var result byte
	for range 8 {
		result = (result << 1) | (b & 1)
		b >>= 1
	}
	return result
}

// sendAuthResult sends the authentication result to the client.
func (c *Connection) sendAuthResult(status uint32, reason string) error {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], status)
	if _, err := c.conn.Write(buf[:]); err != nil {
		return fmt.Errorf("failed to send auth result: %w", err)
	}

	if status != 0 && reason != "" {
		reasonBytes := []byte(reason)
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(reasonBytes)))
		if _, err := c.conn.Write(lenBuf[:]); err != nil {
			return err
		}
		if _, err := c.conn.Write(reasonBytes); err != nil {
			return err
		}
	}

	return nil
}
