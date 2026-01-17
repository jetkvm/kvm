package vnc

import (
	"crypto/des"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

// net.Conn is used for TLS connections

// TLSAvailabilityChecker is called to determine if TLS is currently available.
// This is set by the kvm package during initialization.
var TLSAvailabilityChecker func() bool

// GetCertificateFunc is called to get the TLS certificate.
// This is set by the kvm package during initialization.
var GetCertificateFunc func(*tls.ClientHelloInfo) (*tls.Certificate, error)

// TLSConnUpgrader upgrades a connection to TLS using OpenSSL.
// This is set by the kvm package during initialization.
var TLSConnUpgrader func(conn net.Conn, useX509 bool, certFile, keyFile string) (TLSConnection, error)

// TLSConnection represents an upgraded TLS connection.
type TLSConnection interface {
	net.Conn
	GetProtocolVersion() string
	GetCipherName() string
}

// IsHardwareCryptoEnabledFunc returns true if hardware crypto is enabled.
var IsHardwareCryptoEnabledFunc func() bool

// GetHardwareCryptoEngineFunc returns the hardware crypto engine name.
var GetHardwareCryptoEngineFunc func() string

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
	tlsEnabled := c.server.tlsEnabled && tlsMode != "" && tlsMode != "disabled"
	tlsAvailable := tlsEnabled && c.isTLSAvailable()

	// Build security types list based on TLS and password configuration
	var secTypes []byte
	fallbackSecType := byte(secTypeNone)
	if hasPassword {
		fallbackSecType = byte(secTypeVNCAuth)
	}

	if tlsEnabled {
		// Offer VeNCrypt first with fallback for clients that don't support our cipher suites
		secTypes = []byte{2, byte(secTypeVeNCrypt), fallbackSecType}
		if tlsAvailable {
			c.server.deps.Logger.Debug().Str("remote", c.conn.RemoteAddr().String()).
				Msg("VNC TLS available, offering VeNCrypt with fallback")
		} else {
			c.server.deps.Logger.Warn().Str("remote", c.conn.RemoteAddr().String()).
				Msg("VNC TLS enabled but not available (certificate/time issue) - offering both secure and insecure options")
		}
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

	// Offer VeNCrypt subtypes with X509 FIRST for broader client compatibility
	// Both X509 and anonymous TLS now use OpenSSL for hardware crypto acceleration
	// X509 provides server certificate authentication (prevents MITM)
	// Anonymous TLS (TLSVnc) is fallback for clients that don't support X509
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

	validSubtype := false
	for _, st := range subtypes {
		if st == selectedSubtype {
			validSubtype = true
			break
		}
	}
	if !validSubtype {
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

	switch selectedSubtype {
	case veNCryptTLSNone, veNCryptTLSVnc, veNCryptTLSPlain:
		if err := c.upgradeTLSAnonymous(); err != nil {
			return fmt.Errorf("TLS handshake failed: %w", err)
		}
	case veNCryptX509None, veNCryptX509Vnc, veNCryptX509Plain:
		if err := c.upgradeTLSX509(); err != nil {
			return fmt.Errorf("TLS handshake failed: %w", err)
		}
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

// upgradeTLSAnonymous upgrades the connection to anonymous TLS.
func (c *Connection) upgradeTLSAnonymous() error {
	if TLSConnUpgrader == nil {
		return fmt.Errorf("TLS upgrader not configured")
	}

	tlsConn, err := TLSConnUpgrader(c.conn, false, "", "")
	if err != nil {
		return fmt.Errorf("OpenSSL TLS handshake failed: %w", err)
	}

	c.conn = &opensslConnWrapper{tlsConn: tlsConn, underlying: c.conn}

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
		Msg("TLS handshake complete (anonymous DH)")

	return nil
}

// upgradeTLSX509 upgrades the connection to X509 TLS.
func (c *Connection) upgradeTLSX509() error {
	if GetCertificateFunc == nil {
		return fmt.Errorf("certificate function not configured")
	}

	// Use Go's crypto/tls which has NEON assembly for ARM - faster than non-optimized OpenSSL
	tlsConfig := &tls.Config{
		GetCertificate: GetCertificateFunc,
		MinVersion:     tls.VersionTLS12,
		// Prefer ECDSA cipher suites (self-signed certs use ECDSA P256)
		// Go's crypto/aes and crypto/gcm have NEON assembly on ARM
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, // Fastest - 128-bit with NEON
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		},
	}

	tlsConn := tls.Server(c.conn, tlsConfig)

	if err := tlsConn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return fmt.Errorf("failed to set TLS deadline: %w", err)
	}

	if err := tlsConn.Handshake(); err != nil {
		return fmt.Errorf("TLS handshake failed: %w", err)
	}

	if err := tlsConn.SetDeadline(time.Time{}); err != nil {
		c.server.deps.Logger.Debug().Err(err).Msg("failed to clear TLS deadline")
	}
	c.conn = tlsConn

	state := tlsConn.ConnectionState()
	c.server.deps.Logger.Info().
		Str("version", tlsVersionString(state.Version)).
		Str("cipherSuite", tls.CipherSuiteName(state.CipherSuite)).
		Str("remote", c.conn.RemoteAddr().String()).
		Msg("TLS handshake complete (X509 with Go NEON crypto)")

	return nil
}

// opensslConnWrapper wraps an OpenSSL TLS connection to implement net.Conn.
type opensslConnWrapper struct {
	tlsConn    TLSConnection
	underlying net.Conn
}

func (w *opensslConnWrapper) Read(b []byte) (int, error)  { return w.tlsConn.Read(b) }
func (w *opensslConnWrapper) Write(b []byte) (int, error) { return w.tlsConn.Write(b) }
func (w *opensslConnWrapper) Close() error                { return w.tlsConn.Close() }
func (w *opensslConnWrapper) LocalAddr() net.Addr         { return w.tlsConn.LocalAddr() }
func (w *opensslConnWrapper) RemoteAddr() net.Addr        { return w.tlsConn.RemoteAddr() }
func (w *opensslConnWrapper) SetDeadline(t time.Time) error {
	return w.underlying.SetDeadline(t)
}
func (w *opensslConnWrapper) SetReadDeadline(t time.Time) error {
	return w.underlying.SetReadDeadline(t)
}
func (w *opensslConnWrapper) SetWriteDeadline(t time.Time) error {
	return w.underlying.SetWriteDeadline(t)
}

// tlsVersionString returns a human-readable TLS version string.
func tlsVersionString(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("0x%04x", version)
	}
}

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
	for i := 0; i < 8; i++ {
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
