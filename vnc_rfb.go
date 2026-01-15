package kvm

import (
	"bytes"
	"crypto/des"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/jetkvm/kvm/internal/native"
	"github.com/jetkvm/kvm/internal/vnctls"
)

// RFB protocol constants
const (
	rfbProtocolVersion = "RFB 003.008\n"

	// Security types
	secTypeNone     = 1
	secTypeVNCAuth  = 2
	secTypeVeNCrypt = 19

	// VeNCrypt subtypes
	veNCryptPlain     = 256 // No encryption, plain auth
	veNCryptTLSNone   = 257 // TLS with no auth
	veNCryptTLSVnc    = 258 // TLS with VNC auth
	veNCryptTLSPlain  = 259 // TLS with plain username/password
	veNCryptX509None  = 260 // TLS with X509 cert, no auth
	veNCryptX509Vnc   = 261 // TLS with X509 cert + VNC auth
	veNCryptX509Plain = 262 // TLS with X509 cert + plain auth

	// Client message types
	msgSetPixelFormat           = 0
	msgSetEncodings             = 2
	msgFramebufferUpdateRequest = 3
	msgKeyEvent                 = 4
	msgPointerEvent             = 5
	msgClientCutText            = 6

	// Server message types
	msgFramebufferUpdate = 0
	msgSetColorMapEntry  = 1
	msgBell              = 2
	msgServerCutText     = 3

	// Encoding types
	encodingRaw         = 0
	encodingCopyRect    = 1
	encodingRRE         = 2
	encodingHextile     = 5
	encodingTight       = 7
	encodingZlib        = 6
	encodingZRLE        = 16
	encodingH264        = 50
	encodingCursor      = -239
	encodingDesktopSize = -223
	encodingTightPNG    = -260

	// Tight compression control
	// Note: 0x09 means JPEG in lower 4 bits, upper 4 bits can indicate stream reset
	// Some clients expect 0x90 (bit 7 set = no reset, lower nibble 0 = no filter)
	// For JPEG, the format varies by client. TightVNC uses 0x09, others use 0x90
	tightExplicitFilter = 0x04
	tightFill           = 0x08
	tightJPEG           = 0x90 // 0x90 = JPEG with no zlib stream reset
	tightMaxSubencoding = 0x09

	// H.264 encoding flags
	h264FlagResetContext    = 0x01 // Reset the specified rectangle's context
	h264FlagResetAllContext = 0x02 // Reset all rectangle contexts
)

// handshake performs the RFB protocol handshake
func (c *VNCConnection) handshake() error {
	vncLogger.Warn().Str("remote", c.conn.RemoteAddr().String()).Msg("starting VNC handshake")

	_ = c.conn.SetDeadline(time.Now().Add(30 * time.Second))
	defer func() { _ = c.conn.SetDeadline(time.Time{}) }()

	// Send protocol version
	n, err := c.conn.Write([]byte(rfbProtocolVersion))
	if err != nil {
		return fmt.Errorf("failed to write protocol version (%d bytes written): %w", n, err)
	}
	vncLogger.Warn().Str("remote", c.conn.RemoteAddr().String()).Int("bytes", n).Msg("sent protocol version")

	// Read client protocol version
	versionBuf := make([]byte, 12)
	if _, err := io.ReadFull(c.conn, versionBuf); err != nil {
		return fmt.Errorf("failed to read client protocol version: %w", err)
	}

	clientVersion := string(versionBuf)
	vncLogger.Info().Str("version", clientVersion).Str("remote", c.conn.RemoteAddr().String()).Msg("client protocol version received")

	// We support RFB 3.3, 3.7, and 3.8
	// For 3.8, we send security types list
	// For 3.3/3.7, we send a single security type

	if bytes.HasPrefix(versionBuf, []byte("RFB 003.008")) {
		// RFB 3.8 - send security types list
		vncLogger.Info().Str("remote", c.conn.RemoteAddr().String()).Msg("RFB 3.8 client - handshake complete")
		return nil
	} else if bytes.HasPrefix(versionBuf, []byte("RFB 003.007")) {
		// RFB 3.7 - also uses security types list
		vncLogger.Info().Str("remote", c.conn.RemoteAddr().String()).Msg("RFB 3.7 client - handshake complete")
		return nil
	} else if bytes.HasPrefix(versionBuf, []byte("RFB 003.003")) {
		// RFB 3.3 - different security negotiation
		vncLogger.Info().Str("remote", c.conn.RemoteAddr().String()).Msg("RFB 3.3 client - handshake complete")
		return nil
	}

	vncLogger.Warn().Str("version", clientVersion).Str("remote", c.conn.RemoteAddr().String()).Msg("unknown RFB version")
	return nil
}

// isTLSAvailable checks if TLS certificates are actually available
// This checks the same conditions as getCertificate() in web_tls.go
func isTLSAvailable() bool {
	timeSyncNeeded := isTimeSyncNeeded()
	timeSyncSuccess := timeSync != nil && timeSync.IsSyncSuccess()

	vncLogger.Warn().
		Str("tlsMode", config.TLSMode).
		Bool("timeSyncNeeded", timeSyncNeeded).
		Bool("timeSyncSuccess", timeSyncSuccess).
		Msg("checking TLS availability")

	switch config.TLSMode {
	case "self-signed":
		// For self-signed, we need time to be synced to generate/use certificates
		if timeSyncNeeded || !timeSyncSuccess {
			vncLogger.Warn().Msg("TLS not available: time is not synced")
			return false
		}
		return true
	case "custom":
		// Custom certificates should always be available if configured
		return true
	default:
		// TLS disabled or unknown mode
		return false
	}
}

// authenticate performs VNC authentication with VeNCrypt TLS support
func (c *VNCConnection) authenticate() error {
	_ = c.conn.SetDeadline(time.Now().Add(30 * time.Second))
	defer func() { _ = c.conn.SetDeadline(time.Time{}) }()

	// Enable TCP_NODELAY to disable Nagle's algorithm
	// This ensures each write is sent immediately without waiting for more data
	// Critical for VeNCrypt where acceptance byte must reach client before TLS handshake
	if tcpConn, ok := c.conn.(*net.TCPConn); ok {
		if err := tcpConn.SetNoDelay(true); err != nil {
			vncLogger.Warn().Err(err).Msg("failed to set TCP_NODELAY")
		} else {
			vncLogger.Info().Msg("TCP_NODELAY enabled for VNC connection")
		}
	}

	hasPassword := config.HashedPassword != ""
	// Check if TLS is both enabled AND actually available (cert can be obtained)
	tlsEnabled := c.server.tlsEnabled && config.TLSMode != "" && config.TLSMode != "disabled"
	tlsAvailable := tlsEnabled && isTLSAvailable()

	vncLogger.Warn().
		Bool("hasPassword", hasPassword).
		Bool("tlsEnabled", tlsEnabled).
		Bool("tlsAvailable", tlsAvailable).
		Str("remote", c.conn.RemoteAddr().String()).
		Msg("starting authentication")

	// Build security types list
	var secTypes []byte
	if tlsAvailable {
		// Offer VeNCrypt first (preferred), then fallback options
		if hasPassword {
			secTypes = []byte{2, secTypeVeNCrypt, secTypeVNCAuth}
		} else {
			secTypes = []byte{2, secTypeVeNCrypt, secTypeNone}
		}
	} else if tlsEnabled && !tlsAvailable {
		// TLS enabled but not available (e.g., time not synced) - warn and use plain auth
		vncLogger.Warn().Msg("TLS enabled but certificate not available, falling back to plain auth")
		if hasPassword {
			secTypes = []byte{1, secTypeVNCAuth}
		} else {
			secTypes = []byte{1, secTypeNone}
		}
	} else {
		// No TLS - offer traditional security types
		if hasPassword {
			secTypes = []byte{1, secTypeVNCAuth}
		} else {
			secTypes = []byte{1, secTypeNone}
		}
	}

	vncLogger.Warn().Hex("bytes", secTypes).Int("count", int(secTypes[0])).Str("remote", c.conn.RemoteAddr().String()).Msg("sending security types")
	n, err := c.conn.Write(secTypes)
	if err != nil {
		return fmt.Errorf("failed to send security types: %w", err)
	}
	vncLogger.Warn().Int("bytesWritten", n).Msg("security types sent")

	// Read selected security type
	secType := make([]byte, 1)
	if _, err := io.ReadFull(c.conn, secType); err != nil {
		return fmt.Errorf("failed to read security type: %w", err)
	}

	vncLogger.Warn().Hex("bytes", secType).Uint8("secType", secType[0]).Str("remote", c.conn.RemoteAddr().String()).Msg("client selected security type")

	switch secType[0] {
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

// authenticateVeNCrypt performs VeNCrypt authentication with TLS
func (c *VNCConnection) authenticateVeNCrypt(hasPassword bool) error {
	vncLogger.Warn().Str("remote", c.conn.RemoteAddr().String()).Msg("VeNCrypt authentication starting")

	// Send VeNCrypt version 0.2 (major=0, minor=2)
	// Per VeNCrypt spec: 2 bytes total (1 byte major, 1 byte minor)
	version := []byte{0, 2}
	vncLogger.Warn().Hex("bytes", version).Msg("VeNCrypt: sending server version")
	n, err := c.conn.Write(version)
	if err != nil {
		return fmt.Errorf("failed to send VeNCrypt version: %w", err)
	}
	vncLogger.Warn().Int("bytesWritten", n).Msg("VeNCrypt: server version sent")

	// Read client's VeNCrypt version
	clientVersion := make([]byte, 2)
	if _, err := io.ReadFull(c.conn, clientVersion); err != nil {
		return fmt.Errorf("failed to read client VeNCrypt version: %w", err)
	}

	vncLogger.Warn().
		Hex("bytes", clientVersion).
		Uint8("major", clientVersion[0]).
		Uint8("minor", clientVersion[1]).
		Str("remote", c.conn.RemoteAddr().String()).
		Msg("VeNCrypt: received client version")

	// Check version compatibility (we support 0.2)
	if clientVersion[0] != 0 || clientVersion[1] < 2 {
		// Send rejection (non-zero)
		vncLogger.Warn().Msg("VeNCrypt: rejecting client version (sending 0x01)")
		if _, err := c.conn.Write([]byte{1}); err != nil {
			return fmt.Errorf("failed to send version rejection: %w", err)
		}
		return fmt.Errorf("unsupported VeNCrypt version: %d.%d", clientVersion[0], clientVersion[1])
	}

	// Send acceptance (0)
	vncLogger.Warn().Msg("VeNCrypt: accepting client version (sending 0x00)")
	if _, err := c.conn.Write([]byte{0}); err != nil {
		return fmt.Errorf("failed to send version acceptance: %w", err)
	}

	// Build list of supported subtypes per libvncserver specification
	// We offer both TLS anonymous (257-259) and X509 (260-262) subtypes:
	// - TLS anonymous uses OpenSSL with ADH ciphers (for TigerVNC "TLS with anonymous certificates")
	// - X509 uses certificates (for clients that want certificate-based TLS)
	// Subtypes offered (in preference order):
	//   257: TLSNone - TLS anonymous, no additional auth
	//   258: TLSVnc - TLS anonymous + VNC challenge-response auth
	//   259: TLSPlain - TLS anonymous + username/password auth
	//   260: X509None - X509 cert TLS, no additional auth
	//   261: X509Vnc - X509 cert TLS + VNC challenge-response auth
	//   262: X509Plain - X509 cert TLS + username/password auth
	var subtypes []uint32
	if hasPassword {
		// Offer VNC auth variants first (compatible with local password)
		// Include both TLS anonymous (for Jump Desktop, TigerVNC) and X509 (for other clients)
		subtypes = []uint32{
			veNCryptTLSVnc,    // 258: TLS anonymous + VNC auth
			veNCryptX509Vnc,   // 261: X509 TLS + VNC auth
			veNCryptTLSPlain,  // 259: TLS anonymous + plain auth
			veNCryptX509Plain, // 262: X509 TLS + plain auth
		}
	} else {
		// No password - offer no-auth variants
		subtypes = []uint32{
			veNCryptTLSNone,  // 257: TLS anonymous, no auth
			veNCryptX509None, // 260: X509 TLS, no auth
		}
	}

	// Send number of subtypes (1 byte) and subtypes (4 bytes each)
	subtypeBuf := make([]byte, 1+len(subtypes)*4)
	subtypeBuf[0] = byte(len(subtypes))
	for i, st := range subtypes {
		binary.BigEndian.PutUint32(subtypeBuf[1+i*4:], st)
	}

	vncLogger.Warn().
		Hex("bytes", subtypeBuf).
		Interface("subtypes", subtypes).
		Int("totalBytes", len(subtypeBuf)).
		Str("remote", c.conn.RemoteAddr().String()).
		Msg("VeNCrypt: sending subtypes")

	nSubtypes, errSubtypes := c.conn.Write(subtypeBuf)
	if errSubtypes != nil {
		return fmt.Errorf("failed to send VeNCrypt subtypes: %w", errSubtypes)
	}
	vncLogger.Warn().Int("bytesWritten", nSubtypes).Msg("VeNCrypt: subtypes sent")

	// Read client's selected subtype
	selectedBuf := make([]byte, 4)
	if _, err := io.ReadFull(c.conn, selectedBuf); err != nil {
		return fmt.Errorf("failed to read selected subtype: %w", err)
	}
	selectedSubtype := binary.BigEndian.Uint32(selectedBuf)

	vncLogger.Warn().
		Hex("bytes", selectedBuf).
		Uint32("subtype", selectedSubtype).
		Str("remote", c.conn.RemoteAddr().String()).
		Msg("VeNCrypt: client selected subtype")

	// Validate selected subtype
	validSubtype := false
	for _, st := range subtypes {
		if st == selectedSubtype {
			validSubtype = true
			break
		}
	}
	if !validSubtype {
		// Send failure status
		vncLogger.Warn().Uint32("subtype", selectedSubtype).Msg("VeNCrypt: rejecting subtype (sending 0x01)")
		if _, err := c.conn.Write([]byte{1}); err != nil {
			return fmt.Errorf("failed to send subtype rejection: %w", err)
		}
		return fmt.Errorf("client selected unsupported subtype: %d", selectedSubtype)
	}

	// Send success status (1 byte)
	// NOTE: TigerVNC expects 1 for success, contrary to some VeNCrypt specs that say 0=success
	// From TigerVNC CSecurityTLS.cxx: if (is->readU8() == 0) throw error
	vncLogger.Warn().Uint32("subtype", selectedSubtype).Msg("VeNCrypt: accepting subtype (sending 0x01)")
	if _, err := c.conn.Write([]byte{1}); err != nil {
		return fmt.Errorf("failed to send subtype acceptance: %w", err)
	}

	vncLogger.Warn().
		Uint32("subtype", selectedSubtype).
		Str("remote", c.conn.RemoteAddr().String()).
		Msg("VeNCrypt: subtype acceptance sent, starting TLS handshake")

	// Perform TLS handshake for TLS subtypes
	switch selectedSubtype {
	case veNCryptTLSNone, veNCryptTLSVnc, veNCryptTLSPlain:
		// TLS anonymous - use OpenSSL with ADH ciphers
		vncLogger.Warn().Str("remote", c.conn.RemoteAddr().String()).Msg("calling upgradeTLSAnonymous")
		if err := c.upgradeTLSAnonymous(); err != nil {
			return fmt.Errorf("TLS handshake failed: %w", err)
		}
	case veNCryptX509None, veNCryptX509Vnc, veNCryptX509Plain:
		// X509 - use certificate-based TLS (Go's crypto/tls or OpenSSL)
		if err := c.upgradeTLSX509(); err != nil {
			return fmt.Errorf("TLS handshake failed: %w", err)
		}
	}

	// Perform additional authentication if required
	switch selectedSubtype {
	case veNCryptTLSVnc, veNCryptX509Vnc:
		// VNC auth over TLS (challenge-response)
		if err := c.vncAuth(); err != nil {
			_ = c.sendAuthResult(1, "Authentication failed")
			return err
		}
		if err := c.sendAuthResult(0, ""); err != nil {
			return err
		}
	case veNCryptTLSPlain, veNCryptX509Plain:
		// Plain auth over TLS (username/password)
		if err := c.plainAuth(); err != nil {
			_ = c.sendAuthResult(1, "Authentication failed")
			return err
		}
		if err := c.sendAuthResult(0, ""); err != nil {
			return err
		}
	case veNCryptTLSNone, veNCryptX509None:
		// No additional auth needed, but send SecurityResult for RFB 3.8
		if err := c.sendAuthResult(0, ""); err != nil {
			return err
		}
	}

	c.authenticated = true
	vncLogger.Info().Str("remote", c.conn.RemoteAddr().String()).Msg("VeNCrypt authentication complete")
	return nil
}

// upgradeTLSAnonymous upgrades the connection to TLS using anonymous DH (OpenSSL)
// This is used for VeNCrypt TLS anonymous subtypes (257-259)
func (c *VNCConnection) upgradeTLSAnonymous() error {
	vncLogger.Warn().Str("remote", c.conn.RemoteAddr().String()).Msg("upgrading connection to TLS (anonymous DH)")

	// Use OpenSSL for anonymous DH - no certificate needed
	tlsConn, err := vnctls.UpgradeToTLS(c.conn, false, "", "")
	if err != nil {
		return fmt.Errorf("OpenSSL TLS handshake failed: %w", err)
	}

	// Wrap in a net.Conn compatible interface
	c.conn = &opensslConnWrapper{tlsConn: tlsConn, underlying: c.conn}

	vncLogger.Info().
		Str("version", tlsConn.GetProtocolVersion()).
		Str("cipherSuite", tlsConn.GetCipherName()).
		Str("remote", c.conn.RemoteAddr().String()).
		Msg("TLS handshake complete (anonymous DH)")

	return nil
}

// upgradeTLSX509 upgrades the connection to TLS using X509 certificates
// This is used for VeNCrypt X509 subtypes (260-262)
func (c *VNCConnection) upgradeTLSX509() error {
	vncLogger.Warn().Str("remote", c.conn.RemoteAddr().String()).Msg("upgrading connection to TLS (X509)")

	tlsConfig := &tls.Config{
		GetCertificate: getCertificate,
		MinVersion:     tls.VersionTLS12,
		// For VeNCrypt compatibility, allow older cipher suites
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_128_CBC_SHA,
			tls.TLS_RSA_WITH_AES_256_CBC_SHA,
		},
	}

	// Wrap the existing connection with TLS
	tlsConn := tls.Server(c.conn, tlsConfig)

	// Set deadline for TLS handshake
	if err := tlsConn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return fmt.Errorf("failed to set TLS deadline: %w", err)
	}

	// Perform TLS handshake
	if err := tlsConn.Handshake(); err != nil {
		return fmt.Errorf("TLS handshake failed: %w", err)
	}

	// Clear deadline
	_ = tlsConn.SetDeadline(time.Time{})

	// Replace the connection with the TLS connection
	c.conn = tlsConn

	state := tlsConn.ConnectionState()
	vncLogger.Info().
		Str("version", tlsVersionString(state.Version)).
		Str("cipherSuite", tls.CipherSuiteName(state.CipherSuite)).
		Str("remote", c.conn.RemoteAddr().String()).
		Msg("TLS handshake complete (X509)")

	return nil
}

// opensslConnWrapper wraps an OpenSSL TLS connection to implement net.Conn
type opensslConnWrapper struct {
	tlsConn    *vnctls.TLSConn
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

// tlsVersionString returns a human-readable TLS version string
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

// authenticateVNCAuth performs traditional VNC authentication
func (c *VNCConnection) authenticateVNCAuth() error {
	vncLogger.Info().Str("remote", c.conn.RemoteAddr().String()).Msg("performing VNC challenge-response auth")
	if err := c.vncAuth(); err != nil {
		vncLogger.Warn().Err(err).Str("remote", c.conn.RemoteAddr().String()).Msg("VNC authentication failed")
		_ = c.sendAuthResult(1, "Authentication failed")
		return err
	}

	vncLogger.Info().Str("remote", c.conn.RemoteAddr().String()).Msg("VNC authentication successful")
	if err := c.sendAuthResult(0, ""); err != nil {
		return err
	}

	c.authenticated = true
	return nil
}

// authenticateNone performs None authentication (no password required)
func (c *VNCConnection) authenticateNone() error {
	vncLogger.Info().Str("remote", c.conn.RemoteAddr().String()).Msg("None authentication")

	// For RFB 3.8, send SecurityResult even for None auth
	if err := c.sendAuthResult(0, ""); err != nil {
		return err
	}

	c.authenticated = true
	return nil
}

// vncAuth performs VNC challenge-response authentication
func (c *VNCConnection) vncAuth() error {
	// Generate random 16-byte challenge
	challenge := make([]byte, 16)
	if _, err := rand.Read(challenge); err != nil {
		return fmt.Errorf("failed to generate challenge: %w", err)
	}

	// Send challenge
	if _, err := c.conn.Write(challenge); err != nil {
		return fmt.Errorf("failed to send challenge: %w", err)
	}

	// Read response
	response := make([]byte, 16)
	if _, err := io.ReadFull(c.conn, response); err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Get the plaintext password from config
	// Note: VNC auth uses plaintext password (max 8 chars)
	// Priority: LocalAuthPassword > VNCPassword
	var password string
	if config.LocalAuthPassword != "" {
		password = config.LocalAuthPassword
	} else if config.VNCPassword != "" {
		password = config.VNCPassword
	}
	if password == "" {
		return fmt.Errorf("no VNC password configured (set password in Access settings)")
	}

	// Compute expected response
	expected := computeVNCResponse(challenge, password)

	// Compare
	if !bytes.Equal(response, expected) {
		return fmt.Errorf("authentication failed")
	}

	return nil
}

// plainAuth performs Plain username/password authentication (VeNCrypt subtype 259, 262)
// Protocol: client sends username length (4 bytes), password length (4 bytes),
// then username and password strings
func (c *VNCConnection) plainAuth() error {
	// Read username and password lengths
	lenBuf := make([]byte, 8)
	if _, err := io.ReadFull(c.conn, lenBuf); err != nil {
		return fmt.Errorf("failed to read auth lengths: %w", err)
	}

	usernameLen := binary.BigEndian.Uint32(lenBuf[0:4])
	passwordLen := binary.BigEndian.Uint32(lenBuf[4:8])

	// Sanity check lengths
	if usernameLen > 256 || passwordLen > 256 {
		return fmt.Errorf("username or password too long")
	}

	// Read username
	username := make([]byte, usernameLen)
	if usernameLen > 0 {
		if _, err := io.ReadFull(c.conn, username); err != nil {
			return fmt.Errorf("failed to read username: %w", err)
		}
	}

	// Read password
	password := make([]byte, passwordLen)
	if passwordLen > 0 {
		if _, err := io.ReadFull(c.conn, password); err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}
	}

	vncLogger.Info().
		Str("username", string(username)).
		Str("remote", c.conn.RemoteAddr().String()).
		Msg("Plain authentication attempt")

	// Get the expected password from config
	// For JetKVM, we accept any username but validate password
	var expectedPassword string
	if config.LocalAuthPassword != "" {
		expectedPassword = config.LocalAuthPassword
	} else if config.VNCPassword != "" {
		expectedPassword = config.VNCPassword
	}
	if expectedPassword == "" {
		return fmt.Errorf("no password configured")
	}

	// Compare password
	if string(password) != expectedPassword {
		return fmt.Errorf("authentication failed")
	}

	return nil
}

// computeVNCResponse computes the VNC auth response for a challenge and password
func computeVNCResponse(challenge []byte, password string) []byte {
	// VNC uses DES with bit-reversed key bytes
	// Password is padded/truncated to 8 bytes
	key := make([]byte, 8)
	copy(key, []byte(password))

	// Reverse bits in each byte (VNC quirk)
	for i := range key {
		key[i] = reverseBits(key[i])
	}

	// Create DES cipher
	block, err := des.NewCipher(key)
	if err != nil {
		return nil
	}

	// Encrypt challenge (16 bytes = 2 blocks)
	response := make([]byte, 16)
	block.Encrypt(response[0:8], challenge[0:8])
	block.Encrypt(response[8:16], challenge[8:16])

	return response
}

// reverseBits reverses the bits in a byte
func reverseBits(b byte) byte {
	var result byte
	for i := 0; i < 8; i++ {
		result = (result << 1) | (b & 1)
		b >>= 1
	}
	return result
}

// sendAuthResult sends the authentication result
func (c *VNCConnection) sendAuthResult(status uint32, reason string) error {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, status)
	if _, err := c.conn.Write(buf); err != nil {
		return fmt.Errorf("failed to send auth result: %w", err)
	}

	// For RFB 3.8, send reason string if failed
	if status != 0 && reason != "" {
		reasonBytes := []byte(reason)
		lenBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(lenBuf, uint32(len(reasonBytes)))
		if _, err := c.conn.Write(lenBuf); err != nil {
			return err
		}
		if _, err := c.conn.Write(reasonBytes); err != nil {
			return err
		}
	}

	return nil
}

// clientInit handles the ClientInit message
func (c *VNCConnection) clientInit() error {
	// Read shared flag
	sharedBuf := make([]byte, 1)
	if _, err := io.ReadFull(c.conn, sharedBuf); err != nil {
		return fmt.Errorf("failed to read shared flag: %w", err)
	}

	shared := sharedBuf[0] != 0
	vncLogger.Debug().Bool("shared", shared).Msg("client init")

	// If not shared, we could disconnect other clients
	// For now, we always allow multiple connections

	return nil
}

// serverInit sends the ServerInit message
func (c *VNCConnection) serverInit() error {
	vncLogger.Info().Str("remote", c.conn.RemoteAddr().String()).Msg("sending ServerInit")
	c.mu.Lock()
	width := c.width
	height := c.height
	c.mu.Unlock()

	// Build ServerInit message
	buf := new(bytes.Buffer)

	// Framebuffer width and height (2 bytes each)
	_ = binary.Write(buf, binary.BigEndian, width)
	_ = binary.Write(buf, binary.BigEndian, height)

	// Pixel format (16 bytes)
	pf := c.pixelFormat
	_ = buf.WriteByte(pf.BitsPerPixel)
	_ = buf.WriteByte(pf.Depth)
	_ = buf.WriteByte(pf.BigEndian)
	_ = buf.WriteByte(pf.TrueColor)
	_ = binary.Write(buf, binary.BigEndian, pf.RedMax)
	_ = binary.Write(buf, binary.BigEndian, pf.GreenMax)
	_ = binary.Write(buf, binary.BigEndian, pf.BlueMax)
	_ = buf.WriteByte(pf.RedShift)
	_ = buf.WriteByte(pf.GreenShift)
	_ = buf.WriteByte(pf.BlueShift)
	_, _ = buf.Write([]byte{0, 0, 0}) // padding

	// Desktop name
	name := "JetKVM"
	_ = binary.Write(buf, binary.BigEndian, uint32(len(name)))
	_, _ = buf.WriteString(name)

	_, err := c.conn.Write(buf.Bytes())
	if err != nil {
		return fmt.Errorf("failed to send server init: %w", err)
	}

	vncLogger.Info().Uint16("width", width).Uint16("height", height).Msg("sent server init")
	return nil
}

// messageLoop handles client messages
func (c *VNCConnection) messageLoop() error {
	vncLogger.Warn().Str("remote", c.conn.RemoteAddr().String()).Msg("entering messageLoop")
	msgCount := 0
	for {
		select {
		case <-c.stopChan:
			vncLogger.Warn().Str("remote", c.conn.RemoteAddr().String()).Msg("messageLoop exiting (stopChan)")
			return nil
		default:
		}

		// Set read deadline
		_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		// Read message type
		msgType := make([]byte, 1)
		if _, err := io.ReadFull(c.conn, msgType); err != nil {
			if err == io.EOF {
				vncLogger.Warn().Str("remote", c.conn.RemoteAddr().String()).Msg("messageLoop exiting (EOF)")
				return nil
			}
			vncLogger.Warn().Err(err).Str("remote", c.conn.RemoteAddr().String()).Msg("messageLoop error reading message type")
			return fmt.Errorf("failed to read message type: %w", err)
		}

		msgCount++
		// Always log key and pointer events at WARN level, others are sampled
		if msgType[0] == msgKeyEvent || msgType[0] == msgPointerEvent {
			vncLogger.Warn().Uint8("type", msgType[0]).Int("msgCount", msgCount).Str("remote", c.conn.RemoteAddr().String()).Msg("received HID message")
		} else if msgCount <= 10 || msgCount%100 == 0 {
			vncLogger.Warn().Uint8("type", msgType[0]).Int("msgCount", msgCount).Str("remote", c.conn.RemoteAddr().String()).Msg("received message")
		}

		switch msgType[0] {
		case msgSetPixelFormat:
			if err := c.handleSetPixelFormat(); err != nil {
				return err
			}
		case msgSetEncodings:
			if err := c.handleSetEncodings(); err != nil {
				return err
			}
		case msgFramebufferUpdateRequest:
			if err := c.handleFramebufferUpdateRequest(); err != nil {
				return err
			}
		case msgKeyEvent:
			if err := c.handleKeyEvent(); err != nil {
				return err
			}
		case msgPointerEvent:
			if err := c.handlePointerEvent(); err != nil {
				return err
			}
		case msgClientCutText:
			if err := c.handleClientCutText(); err != nil {
				return err
			}
		default:
			vncLogger.Warn().Uint8("type", msgType[0]).Msg("unknown message type")
		}
	}
}

// handleSetPixelFormat handles SetPixelFormat message
func (c *VNCConnection) handleSetPixelFormat() error {
	// Read padding (3 bytes) + pixel format (16 bytes)
	buf := make([]byte, 19)
	if _, err := io.ReadFull(c.conn, buf); err != nil {
		return fmt.Errorf("failed to read pixel format: %w", err)
	}

	c.mu.Lock()
	c.pixelFormat = PixelFormat{
		BitsPerPixel: buf[3],
		Depth:        buf[4],
		BigEndian:    buf[5],
		TrueColor:    buf[6],
		RedMax:       binary.BigEndian.Uint16(buf[7:9]),
		GreenMax:     binary.BigEndian.Uint16(buf[9:11]),
		BlueMax:      binary.BigEndian.Uint16(buf[11:13]),
		RedShift:     buf[13],
		GreenShift:   buf[14],
		BlueShift:    buf[15],
	}
	c.mu.Unlock()

	vncLogger.Debug().Interface("pixelFormat", c.pixelFormat).Msg("set pixel format")
	return nil
}

// handleSetEncodings handles SetEncodings message
func (c *VNCConnection) handleSetEncodings() error {
	vncLogger.Info().Str("remote", c.conn.RemoteAddr().String()).Msg("handling SetEncodings message")
	// Read padding (1 byte) + number of encodings (2 bytes)
	header := make([]byte, 3)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return fmt.Errorf("failed to read encodings header: %w", err)
	}

	numEncodings := binary.BigEndian.Uint16(header[1:3])

	// Read encoding types
	encodings := make([]int32, numEncodings)
	for i := uint16(0); i < numEncodings; i++ {
		buf := make([]byte, 4)
		if _, err := io.ReadFull(c.conn, buf); err != nil {
			return fmt.Errorf("failed to read encoding: %w", err)
		}
		encodings[i] = int32(binary.BigEndian.Uint32(buf))
	}

	c.mu.Lock()
	c.encodings = encodings

	// Check for specific encodings
	c.hasTight = false
	c.hasH264 = false
	for _, enc := range encodings {
		switch enc {
		case encodingTight:
			c.hasTight = true
		case encodingH264:
			c.hasH264 = true
		}
	}

	// Determine if this client needs JPEG encoder
	// If client supports H.264, we prefer that (more efficient)
	// Otherwise, we need JPEG via TIGHT encoding
	needsJPEG := !c.hasH264 && c.hasTight
	previouslyNeededJPEG := c.needsJPEGEncoder
	c.needsJPEGEncoder = needsJPEG
	c.mu.Unlock()

	// Request or release JPEG encoder based on client capabilities
	if needsJPEG && !previouslyNeededJPEG {
		c.server.requestJPEGEncoder()
		vncLogger.Info().Msg("client needs JPEG encoder (no H.264 support)")
	} else if !needsJPEG && previouslyNeededJPEG {
		c.server.releaseJPEGEncoder()
		vncLogger.Info().Msg("client no longer needs JPEG encoder")
	}

	// Subscribe to H.264 frames if client supports it
	if c.hasH264 && c.h264Unsubscribe == nil {
		c.h264Unsubscribe = native.SubscribeH264Frames(c.h264FrameChan)
		vncLogger.Info().Msg("client supports H.264 encoding - subscribed to H.264 frames")
	}

	vncLogger.Warn().
		Interface("encodings", encodings).
		Bool("hasTight", c.hasTight).
		Bool("hasH264", c.hasH264).
		Bool("needsJPEG", needsJPEG).
		Str("remote", c.conn.RemoteAddr().String()).
		Msg("client set encodings")

	return nil
}

// handleFramebufferUpdateRequest handles FramebufferUpdateRequest message
func (c *VNCConnection) handleFramebufferUpdateRequest() error {
	// Read request: incremental (1) + x,y,w,h (8)
	buf := make([]byte, 9)
	if _, err := io.ReadFull(c.conn, buf); err != nil {
		return fmt.Errorf("failed to read fb update request: %w", err)
	}

	incremental := buf[0] != 0
	x := binary.BigEndian.Uint16(buf[1:3])
	y := binary.BigEndian.Uint16(buf[3:5])
	w := binary.BigEndian.Uint16(buf[5:7])
	h := binary.BigEndian.Uint16(buf[7:9])

	vncLogger.Debug().
		Bool("incremental", incremental).
		Uint16("x", x).Uint16("y", y).
		Uint16("w", w).Uint16("h", h).
		Str("remote", c.conn.RemoteAddr().String()).
		Msg("received FramebufferUpdateRequest")

	// Signal that client wants a frame - VNC is request-response protocol
	c.frameRequested.Store(true)

	return nil
}

// handleKeyEvent handles KeyEvent message
func (c *VNCConnection) handleKeyEvent() error {
	// Read key event: down (1) + padding (2) + key (4)
	buf := make([]byte, 7)
	if _, err := io.ReadFull(c.conn, buf); err != nil {
		return fmt.Errorf("failed to read key event: %w", err)
	}

	down := buf[0] != 0
	keysym := binary.BigEndian.Uint32(buf[3:7])

	// Convert X11 keysym to HID and send to USB
	c.handleVNCKey(keysym, down)

	return nil
}

// handlePointerEvent handles PointerEvent message
func (c *VNCConnection) handlePointerEvent() error {
	// Read pointer event: button-mask (1) + x (2) + y (2)
	buf := make([]byte, 5)
	if _, err := io.ReadFull(c.conn, buf); err != nil {
		return fmt.Errorf("failed to read pointer event: %w", err)
	}

	buttonMask := buf[0]
	x := binary.BigEndian.Uint16(buf[1:3])
	y := binary.BigEndian.Uint16(buf[3:5])

	// Send to USB HID
	c.handleVNCPointer(x, y, buttonMask)

	return nil
}

// handleClientCutText handles ClientCutText message
func (c *VNCConnection) handleClientCutText() error {
	// Read header: padding (3) + length (4)
	header := make([]byte, 7)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return fmt.Errorf("failed to read cut text header: %w", err)
	}

	length := binary.BigEndian.Uint32(header[3:7])

	// Read text (but we don't use it for KVM)
	if length > 0 {
		text := make([]byte, length)
		if _, err := io.ReadFull(c.conn, text); err != nil {
			return fmt.Errorf("failed to read cut text: %w", err)
		}
	}

	return nil
}

// sendFrameUpdate sends a framebuffer update with JPEG data
func (c *VNCConnection) sendFrameUpdate(jpegData []byte) error {
	c.mu.Lock()
	width := c.width
	height := c.height
	hasTight := c.hasTight
	c.mu.Unlock()

	if !hasTight {
		// Client doesn't support TIGHT - skip for now
		vncLogger.Warn().Msg("skipping frame (client doesn't support TIGHT encoding)")
		return nil
	}

	// Verify JPEG data starts with SOI marker (0xFF 0xD8)
	if len(jpegData) < 2 || jpegData[0] != 0xFF || jpegData[1] != 0xD8 {
		vncLogger.Warn().Int("len", len(jpegData)).Msg("invalid JPEG data (missing SOI marker)")
		if len(jpegData) >= 4 {
			vncLogger.Warn().Hex("header", jpegData[:4]).Msg("JPEG header bytes")
		}
		return nil
	}

	buf := new(bytes.Buffer)

	// FramebufferUpdate message header
	_ = buf.WriteByte(msgFramebufferUpdate)
	_ = buf.WriteByte(0) // padding

	// Number of rectangles
	_ = binary.Write(buf, binary.BigEndian, uint16(1))

	// Rectangle header
	_ = binary.Write(buf, binary.BigEndian, uint16(0))            // x
	_ = binary.Write(buf, binary.BigEndian, uint16(0))            // y
	_ = binary.Write(buf, binary.BigEndian, width)                // width
	_ = binary.Write(buf, binary.BigEndian, height)               // height
	_ = binary.Write(buf, binary.BigEndian, int32(encodingTight)) // encoding

	// TIGHT JPEG data
	// Compression control byte: JPEG (0x09)
	_ = buf.WriteByte(tightJPEG)

	// Write JPEG length using compact representation
	c.writeTightLength(buf, len(jpegData))

	// Write JPEG data
	_, _ = buf.Write(jpegData)

	// Log first frame's header for debugging
	data := buf.Bytes()
	if len(data) > 20 {
		vncLogger.Warn().
			Hex("header", data[:20]).
			Int("totalLen", len(data)).
			Uint16("width", width).
			Uint16("height", height).
			Int("jpegLen", len(jpegData)).
			Msg("sending TIGHT JPEG framebuffer update")
	}

	// Send to client
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := c.conn.Write(data)
	_ = c.conn.SetWriteDeadline(time.Time{})

	if err != nil {
		return fmt.Errorf("failed to send frame update: %w", err)
	}

	return nil
}

// writeTightLength writes a length using TIGHT's compact representation
func (c *VNCConnection) writeTightLength(buf *bytes.Buffer, length int) {
	// 1 byte for lengths < 128
	// 2 bytes for lengths < 16384
	// 3 bytes for larger lengths
	if length < 128 {
		_ = buf.WriteByte(byte(length))
	} else if length < 16384 {
		_ = buf.WriteByte(byte(length&0x7F) | 0x80)
		_ = buf.WriteByte(byte(length >> 7))
	} else {
		_ = buf.WriteByte(byte(length&0x7F) | 0x80)
		_ = buf.WriteByte(byte((length>>7)&0x7F) | 0x80)
		_ = buf.WriteByte(byte(length >> 14))
	}
}

// sendH264FrameUpdate sends a framebuffer update with H.264 data
func (c *VNCConnection) sendH264FrameUpdate(h264Data []byte, isKeyframe bool) error {
	c.mu.Lock()
	width := c.width
	height := c.height
	hasH264 := c.hasH264
	h264ContextInitialized := c.h264ContextInitialized
	c.mu.Unlock()

	if !hasH264 {
		// Client doesn't support H.264
		return nil
	}

	buf := new(bytes.Buffer)

	// FramebufferUpdate message header
	_ = buf.WriteByte(msgFramebufferUpdate)
	_ = buf.WriteByte(0) // padding

	// Number of rectangles
	_ = binary.Write(buf, binary.BigEndian, uint16(1))

	// Rectangle header
	_ = binary.Write(buf, binary.BigEndian, uint16(0))           // x
	_ = binary.Write(buf, binary.BigEndian, uint16(0))           // y
	_ = binary.Write(buf, binary.BigEndian, width)               // width
	_ = binary.Write(buf, binary.BigEndian, height)              // height
	_ = binary.Write(buf, binary.BigEndian, int32(encodingH264)) // encoding

	// H.264 encoding format:
	// U32 data length
	// U32 flags
	// H.264 stream data

	// Data length
	_ = binary.Write(buf, binary.BigEndian, uint32(len(h264Data)))

	// Flags
	var flags uint32
	if isKeyframe && !h264ContextInitialized {
		// First keyframe - reset context
		flags = h264FlagResetContext
		c.mu.Lock()
		c.h264ContextInitialized = true
		c.mu.Unlock()
	}
	_ = binary.Write(buf, binary.BigEndian, flags)

	// H.264 data
	_, _ = buf.Write(h264Data)

	// Send to client
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := c.conn.Write(buf.Bytes())
	_ = c.conn.SetWriteDeadline(time.Time{})

	if err != nil {
		return fmt.Errorf("failed to send H.264 frame update: %w", err)
	}

	return nil
}

// handleVNCKey converts X11 keysym to HID and sends to USB
func (c *VNCConnection) handleVNCKey(keysym uint32, down bool) {
	vncLogger.Warn().Uint32("keysym", keysym).Bool("down", down).Msg("VNC key event received")

	// Convert keysym to HID keycode
	hidKey, modifiers := keysymToHID(keysym)
	if hidKey == 0 && modifiers == 0 {
		vncLogger.Warn().Uint32("keysym", keysym).Msg("keysym not mapped to HID")
		return
	}

	vncLogger.Warn().Uint8("hidKey", hidKey).Uint8("modifiers", modifiers).Bool("down", down).Msg("sending HID key event")

	// Use keypress report for single key events
	if err := rpcKeypressReport(hidKey, down); err != nil {
		vncLogger.Warn().Err(err).Uint32("keysym", keysym).Msg("failed to send key event")
	}

	// Handle modifiers separately if present
	if modifiers != 0 {
		// Send modifier state via keyboard report
		var keys []byte
		if down && hidKey != 0 {
			keys = []byte{hidKey}
		}
		if err := rpcKeyboardReport(modifiers, keys); err != nil {
			vncLogger.Warn().Err(err).Msg("failed to send modifier event")
		}
	}
}

// handleVNCPointer converts VNC pointer events to USB HID
func (c *VNCConnection) handleVNCPointer(x, y uint16, buttonMask byte) {
	c.mu.Lock()
	width := c.width
	height := c.height
	c.mu.Unlock()

	// Convert to absolute coordinates (0-32767 range for USB HID)
	if width > 0 && height > 0 {
		absX := int(x) * 32767 / int(width)
		absY := int(y) * 32767 / int(height)

		// Convert button mask
		// VNC: bit 0 = left, bit 1 = middle, bit 2 = right
		// USB: bit 0 = left, bit 1 = right, bit 2 = middle
		// Need to swap bits 1 and 2
		vncLeft := (buttonMask & 0x01)        // bit 0 -> bit 0
		vncMiddle := (buttonMask & 0x02) >> 1 // bit 1 -> bit 2
		vncRight := (buttonMask & 0x04) >> 1  // bit 2 -> bit 1
		buttons := vncLeft | vncRight | (vncMiddle << 2)

		vncLogger.Warn().
			Uint16("vncX", x).Uint16("vncY", y).
			Int("absX", absX).Int("absY", absY).
			Uint8("buttons", buttons).
			Msg("VNC pointer event")

		// Send absolute mouse position
		if err := rpcAbsMouseReport(absX, absY, buttons); err != nil {
			vncLogger.Warn().Err(err).Msg("failed to send mouse event")
		}
	}
}

// keysymToHID converts X11 keysym to USB HID keycode and modifiers
func keysymToHID(keysym uint32) (uint8, uint8) {
	// Common keysym to HID mappings
	// Full table would be much larger, this covers basics

	// Modifiers
	switch keysym {
	case 0xFFE1, 0xFFE2: // Shift_L, Shift_R
		return 0, 0x02
	case 0xFFE3, 0xFFE4: // Control_L, Control_R
		return 0, 0x01
	case 0xFFE9, 0xFFEA: // Alt_L, Alt_R
		return 0, 0x04
	case 0xFFEB, 0xFFEC: // Super_L, Super_R (Win key)
		return 0, 0x08
	}

	// Function keys
	if keysym >= 0xFFBE && keysym <= 0xFFC9 {
		// F1-F12
		return uint8(0x3A + (keysym - 0xFFBE)), 0
	}

	// Special keys
	switch keysym {
	case 0xFF08: // BackSpace
		return 0x2A, 0
	case 0xFF09: // Tab
		return 0x2B, 0
	case 0xFF0D: // Return
		return 0x28, 0
	case 0xFF1B: // Escape
		return 0x29, 0
	case 0xFFFF: // Delete
		return 0x4C, 0
	case 0xFF50: // Home
		return 0x4A, 0
	case 0xFF51: // Left
		return 0x50, 0
	case 0xFF52: // Up
		return 0x52, 0
	case 0xFF53: // Right
		return 0x4F, 0
	case 0xFF54: // Down
		return 0x51, 0
	case 0xFF55: // Page_Up
		return 0x4B, 0
	case 0xFF56: // Page_Down
		return 0x4E, 0
	case 0xFF57: // End
		return 0x4D, 0
	case 0xFF63: // Insert
		return 0x49, 0
	case 0x0020: // Space
		return 0x2C, 0
	}

	// Letters (lowercase)
	if keysym >= 0x0061 && keysym <= 0x007A {
		return uint8(0x04 + (keysym - 0x0061)), 0
	}

	// Letters (uppercase)
	if keysym >= 0x0041 && keysym <= 0x005A {
		return uint8(0x04 + (keysym - 0x0041)), 0x02 // with shift
	}

	// Numbers
	if keysym >= 0x0030 && keysym <= 0x0039 {
		if keysym == 0x0030 {
			return 0x27, 0 // 0
		}
		return uint8(0x1E + (keysym - 0x0031)), 0 // 1-9
	}

	// Common punctuation
	switch keysym {
	case 0x002D: // minus
		return 0x2D, 0
	case 0x003D: // equal
		return 0x2E, 0
	case 0x005B: // bracketleft
		return 0x2F, 0
	case 0x005D: // bracketright
		return 0x30, 0
	case 0x005C: // backslash
		return 0x31, 0
	case 0x003B: // semicolon
		return 0x33, 0
	case 0x0027: // apostrophe
		return 0x34, 0
	case 0x0060: // grave
		return 0x35, 0
	case 0x002C: // comma
		return 0x36, 0
	case 0x002E: // period
		return 0x37, 0
	case 0x002F: // slash
		return 0x38, 0
	}

	return 0, 0
}
