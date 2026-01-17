// RFB Protocol Implementation for VNC
//
// This file implements the Remote Framebuffer (RFB) protocol as specified in:
//   - RFC 6143: The Remote Framebuffer Protocol
//     https://datatracker.ietf.org/doc/html/rfc6143
//
// Extensions supported:
//   - VeNCrypt (security type 19): TLS encryption with X509 or anonymous DH
//   - Tight encoding (type 7): JPEG compression for efficient video streaming
//
// Authentication methods:
//   - VNC Authentication (type 2): DES challenge-response
//   - VeNCrypt TLS subtypes: X509Vnc, X509Plain, X509None, TLSVnc, TLSPlain, TLSNone
//
// See vnc_server.go for server lifecycle management.

package kvm

import (
	"bytes"
	"crypto/des"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/hidrpc"
	"github.com/jetkvm/kvm/internal/vnctls"
)

var bufferPool = sync.Pool{
	New: func() interface{} {
		// serverInitBufSize is sufficient for ServerInit message (~30 bytes + name)
		return bytes.NewBuffer(make([]byte, 0, serverInitBufSize))
	},
}

// RFB Security Types - used during VNC authentication handshake
type rfbSecurityType uint8

const (
	secTypeNone     rfbSecurityType = 1  // No authentication
	secTypeVNCAuth  rfbSecurityType = 2  // VNC challenge-response (DES)
	secTypeVeNCrypt rfbSecurityType = 19 // VeNCrypt extension (TLS wrapper)
)

func (s rfbSecurityType) String() string {
	switch s {
	case secTypeNone:
		return "None"
	case secTypeVNCAuth:
		return "VNCAuth"
	case secTypeVeNCrypt:
		return "VeNCrypt"
	default:
		return fmt.Sprintf("Unknown(%d)", s)
	}
}

// VeNCrypt Security Subtypes - extended security after VeNCrypt negotiation
type veNCryptSubtype uint32

const (
	veNCryptTLSNone   veNCryptSubtype = 257 // TLS + No auth
	veNCryptTLSVnc    veNCryptSubtype = 258 // TLS + VNC challenge-response auth
	veNCryptTLSPlain  veNCryptSubtype = 259 // TLS + plaintext username/password
	veNCryptX509None  veNCryptSubtype = 260 // X509 + No auth
	veNCryptX509Vnc   veNCryptSubtype = 261 // X509 + VNC challenge-response auth
	veNCryptX509Plain veNCryptSubtype = 262 // X509 + plaintext username/password
)

func (s veNCryptSubtype) String() string {
	switch s {
	case veNCryptTLSNone:
		return "TLSNone"
	case veNCryptTLSVnc:
		return "TLSVnc"
	case veNCryptTLSPlain:
		return "TLSPlain"
	case veNCryptX509None:
		return "X509None"
	case veNCryptX509Vnc:
		return "X509Vnc"
	case veNCryptX509Plain:
		return "X509Plain"
	default:
		return fmt.Sprintf("Unknown(%d)", s)
	}
}

// RFB Client Message Types - messages sent by VNC client
type rfbClientMsgType uint8

const (
	msgSetPixelFormat           rfbClientMsgType = 0
	msgSetEncodings             rfbClientMsgType = 2
	msgFramebufferUpdateRequest rfbClientMsgType = 3
	msgKeyEvent                 rfbClientMsgType = 4
	msgPointerEvent             rfbClientMsgType = 5
	msgClientCutText            rfbClientMsgType = 6
)

func (m rfbClientMsgType) String() string {
	switch m {
	case msgSetPixelFormat:
		return "SetPixelFormat"
	case msgSetEncodings:
		return "SetEncodings"
	case msgFramebufferUpdateRequest:
		return "FramebufferUpdateRequest"
	case msgKeyEvent:
		return "KeyEvent"
	case msgPointerEvent:
		return "PointerEvent"
	case msgClientCutText:
		return "ClientCutText"
	default:
		return fmt.Sprintf("Unknown(%d)", m)
	}
}

// RFB Server Message Types - messages sent by VNC server
type rfbServerMsgType uint8

const (
	msgFramebufferUpdate rfbServerMsgType = 0
)

func (m rfbServerMsgType) String() string {
	switch m {
	case msgFramebufferUpdate:
		return "FramebufferUpdate"
	default:
		return fmt.Sprintf("Unknown(%d)", m)
	}
}

// RFB Encoding Types - pixel data encoding methods
type rfbEncodingType int32

const (
	encodingRaw   rfbEncodingType = 0 // Raw pixels (fallback)
	encodingTight rfbEncodingType = 7 // Tight encoding with JPEG compression
)

func (e rfbEncodingType) String() string {
	switch e {
	case encodingRaw:
		return "Raw"
	case encodingTight:
		return "Tight"
	default:
		return fmt.Sprintf("Unknown(%d)", e)
	}
}

const (
	// RFB Protocol version 3.8 - required for VeNCrypt security type support
	rfbProtocolVersion = "RFB 003.008\n"

	// Tight encoding compression control byte:
	// Bits 7-4: compression type (0x9 = JPEG compression)
	// Bits 3-0: stream reset flags (0 = no stream resets)
	tightJPEG = 0x90

	// Authentication failure delay to prevent brute-force attacks
	authFailureDelay = 2 * time.Second

	// Maximum auth failures before disconnecting (prevents brute-force within a connection)
	maxAuthFailures = 3

	// Timeouts for various operations
	handshakeTimeout = 30 * time.Second
	readTimeout      = 60 * time.Second
	writeTimeout     = 5 * time.Second

	// VNC auth challenge/response size (DES block size * 2)
	vncChallengeSize = 16

	// Maximum plaintext auth credential length
	maxCredentialLength = 256

	// Maximum clipboard text size (1MB to prevent OOM on embedded device)
	maxCutTextLength = 1024 * 1024

	// Tight encoding compact length thresholds (RFB protocol)
	// Length < 128: 1 byte (7 bits), < 16384: 2 bytes (14 bits), else: 3 bytes (21 bits)
	tightLen1Byte = 128
	tightLen2Byte = 16384

	// Buffer pool capacity for ServerInit message assembly
	serverInitBufSize = 256

	// JPEG Start of Image marker bytes (validates JPEG data)
	jpegSOIByte0 = 0xFF
	jpegSOIByte1 = 0xD8

	// Pre-allocated buffer sizes for RFB protocol messages
	// These match the exact sizes required by the VNC/RFB protocol
	rfbMsgBufSize         = 1  // Message type byte
	rfbKeyBufSize         = 7  // Key event: 1 (down) + 2 (padding) + 4 (keysym)
	rfbPointerBufSize     = 5  // Pointer event: 1 (buttons) + 2 (x) + 2 (y)
	rfbFBReqBufSize       = 9  // FB update request: 1 (incremental) + 2 (x) + 2 (y) + 2 (w) + 2 (h)
	rfbPixelFmtBufSize    = 19 // SetPixelFormat: 3 (padding) + 16 (pixel format)
	rfbEncHeaderBufSize   = 3  // SetEncodings header: 1 (padding) + 2 (num encodings)
	rfbEncBufSize         = 4  // Single encoding value (int32)
	rfbFrameHeaderBufSize = 20 // Frame header: 16 (RFB) + 1 (tight ctrl) + 3 (max length)

	// USB HID absolute positioning maximum (per USB HID spec)
	hidAbsoluteMax = 32767
)

func (c *VNCConnection) handshake() error {
	if err := c.conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return fmt.Errorf("failed to set handshake deadline: %w", err)
	}
	defer func() {
		if err := c.conn.SetDeadline(time.Time{}); err != nil {
			vncLogger.Debug().Err(err).Msg("failed to clear handshake deadline")
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
		vncLogger.Debug().Str("version", string(versionBuf[:])).Msg("client using non-standard RFB version")
	}

	return nil
}

func isTLSAvailable() bool {
	switch config.TLSMode {
	case "self-signed":
		return !isTimeSyncNeeded() && timeSync != nil && timeSync.IsSyncSuccess()
	case "custom":
		return true
	default:
		return false
	}
}

func (c *VNCConnection) authenticate() error {
	if err := c.conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return fmt.Errorf("failed to set auth deadline: %w", err)
	}
	defer func() {
		if err := c.conn.SetDeadline(time.Time{}); err != nil {
			vncLogger.Debug().Err(err).Msg("failed to clear auth deadline")
		}
	}()

	if tcpConn, ok := c.conn.(*net.TCPConn); ok {
		if err := tcpConn.SetNoDelay(true); err != nil {
			vncLogger.Warn().Err(err).Msg("failed to set TCP_NODELAY - VNC input may feel laggy")
		}
	}

	// Check for raw password - VNC auth requires the actual password, not just a hash
	// VNC uses DES encryption with the raw password as key
	hasPassword := config.LocalAuthPassword != "" || config.VNCPassword != ""

	// VeNCrypt provides TLS encryption for VNC connections
	// When VNCUseTLS is enabled and TLS is available, ONLY offer VeNCrypt (no insecure fallback)
	// When TLS is not available, fall back to VNCAuth with warning (needed for initial setup)
	tlsEnabled := c.server.tlsEnabled && config.TLSMode != "" && config.TLSMode != "disabled"
	tlsAvailable := tlsEnabled && isTLSAvailable()

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
			vncLogger.Debug().Str("remote", c.conn.RemoteAddr().String()).
				Msg("VNC TLS available, offering VeNCrypt with fallback")
		} else {
			vncLogger.Warn().Str("remote", c.conn.RemoteAddr().String()).
				Msg("VNC TLS enabled but not available (certificate/time issue) - offering both secure and insecure options")
		}
	} else {
		// TLS not enabled - use insecure modes with warning
		secTypes = []byte{1, fallbackSecType}
		vncLogger.Warn().Str("remote", c.conn.RemoteAddr().String()).
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

func (c *VNCConnection) authenticateVeNCrypt(hasPassword bool) error {
	if _, err := c.conn.Write([]byte{0, 2}); err != nil {
		return fmt.Errorf("failed to send VeNCrypt version: %w", err)
	}

	var clientVersion [2]byte
	if _, err := io.ReadFull(c.conn, clientVersion[:]); err != nil {
		return fmt.Errorf("failed to read client VeNCrypt version: %w", err)
	}

	if clientVersion[0] != 0 || clientVersion[1] < 2 {
		if _, err := c.conn.Write([]byte{1}); err != nil {
			vncLogger.Debug().Err(err).Msg("failed to send VeNCrypt version rejection")
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
			vncLogger.Debug().Err(err).Msg("failed to send subtype rejection")
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
			vncLogger.Debug().Err(sendErr).Str("remote", c.conn.RemoteAddr().String()).Msg("failed to send auth failure result")
		}
		if c.authFailures >= maxAuthFailures {
			return fmt.Errorf("too many auth failures (%d), disconnecting", c.authFailures)
		}
		return authErr
	}

	if err := c.sendAuthResult(0, ""); err != nil {
		return err
	}

	vncLogger.Info().Str("remote", c.conn.RemoteAddr().String()).Msg("VeNCrypt authentication complete")
	return nil
}

func (c *VNCConnection) upgradeTLSAnonymous() error {
	tlsConn, err := vnctls.UpgradeToTLS(c.conn, false, "", "")
	if err != nil {
		return fmt.Errorf("OpenSSL TLS handshake failed: %w", err)
	}

	c.conn = &opensslConnWrapper{tlsConn: tlsConn, underlying: c.conn}

	vncLogger.Info().
		Str("version", tlsConn.GetProtocolVersion()).
		Str("cipherSuite", tlsConn.GetCipherName()).
		Bool("hwCrypto", vnctls.IsHardwareCryptoEnabled()).
		Str("hwEngine", vnctls.GetHardwareCryptoEngine()).
		Str("remote", c.conn.RemoteAddr().String()).
		Msg("TLS handshake complete (anonymous DH)")

	return nil
}

func (c *VNCConnection) upgradeTLSX509() error {
	// Use Go's crypto/tls which has NEON assembly for ARM - faster than non-optimized OpenSSL
	tlsConfig := &tls.Config{
		GetCertificate: getCertificate,
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
		vncLogger.Debug().Err(err).Msg("failed to clear TLS deadline")
	}
	c.conn = tlsConn

	state := tlsConn.ConnectionState()
	vncLogger.Info().
		Str("version", tlsVersionString(state.Version)).
		Str("cipherSuite", tls.CipherSuiteName(state.CipherSuite)).
		Str("remote", c.conn.RemoteAddr().String()).
		Msg("TLS handshake complete (X509 with Go NEON crypto)")

	return nil
}

type opensslConnWrapper struct {
	tlsConn    *vnctls.TLSConn
	underlying net.Conn
}

func (w *opensslConnWrapper) Read(b []byte) (int, error)    { return w.tlsConn.Read(b) }
func (w *opensslConnWrapper) Write(b []byte) (int, error)   { return w.tlsConn.Write(b) }
func (w *opensslConnWrapper) Close() error                  { return w.tlsConn.Close() }
func (w *opensslConnWrapper) LocalAddr() net.Addr           { return w.tlsConn.LocalAddr() }
func (w *opensslConnWrapper) RemoteAddr() net.Addr          { return w.tlsConn.RemoteAddr() }
func (w *opensslConnWrapper) SetDeadline(t time.Time) error { return w.underlying.SetDeadline(t) }
func (w *opensslConnWrapper) SetReadDeadline(t time.Time) error {
	return w.underlying.SetReadDeadline(t)
}
func (w *opensslConnWrapper) SetWriteDeadline(t time.Time) error {
	return w.underlying.SetWriteDeadline(t)
}

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

func (c *VNCConnection) authenticateVNCAuth() error {
	if err := c.vncAuth(); err != nil {
		c.authFailures++
		time.Sleep(authFailureDelay) // Delay to prevent brute-force
		if sendErr := c.sendAuthResult(1, "Authentication failed"); sendErr != nil {
			vncLogger.Debug().Err(sendErr).Str("remote", c.conn.RemoteAddr().String()).Msg("failed to send auth failure result")
		}
		if c.authFailures >= maxAuthFailures {
			return fmt.Errorf("too many auth failures (%d), disconnecting", c.authFailures)
		}
		return err
	}
	return c.sendAuthResult(0, "")
}

func (c *VNCConnection) authenticateNone() error {
	return c.sendAuthResult(0, "")
}

func (c *VNCConnection) vncAuth() error {
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

	password := getVNCPassword()
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

func (c *VNCConnection) plainAuth() error {
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

	expectedPassword := getVNCPassword()
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

func reverseBits(b byte) byte {
	var result byte
	for i := 0; i < 8; i++ {
		result = (result << 1) | (b & 1)
		b >>= 1
	}
	return result
}

// getVNCPassword returns the configured VNC password.
// Note: Config reads are not synchronized with RPC writes. In practice this is safe
// because Go strings are read atomically (pointer + length), and password changes
// during an active authentication are extremely unlikely on single-user KVM devices.
func getVNCPassword() string {
	if config.LocalAuthPassword != "" {
		return config.LocalAuthPassword
	}
	return config.VNCPassword
}

func (c *VNCConnection) sendAuthResult(status uint32, reason string) error {
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

func (c *VNCConnection) clientInit() error {
	var sharedBuf [1]byte
	if _, err := io.ReadFull(c.conn, sharedBuf[:]); err != nil {
		return fmt.Errorf("failed to read shared flag: %w", err)
	}
	return nil
}

func (c *VNCConnection) serverInit() error {
	width, height := c.getResolution()

	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufferPool.Put(buf)

	// All writes to bytes.Buffer are infallible for in-memory operations
	_ = binary.Write(buf, binary.BigEndian, width)
	_ = binary.Write(buf, binary.BigEndian, height)

	pf := c.pixelFormat
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

func (c *VNCConnection) messageLoop() error {
	for {
		select {
		case <-c.stopChan:
			return nil
		default:
		}

		if err := c.conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			return fmt.Errorf("failed to set read deadline: %w", err)
		}

		if _, err := io.ReadFull(c.conn, c.msgBuf[:]); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("failed to read message type: %w", err)
		}

		switch rfbClientMsgType(c.msgBuf[0]) {
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
			vncLogger.Warn().Uint8("msgType", c.msgBuf[0]).Msg("unknown VNC message type")
			return fmt.Errorf("unknown message type: %d", c.msgBuf[0])
		}
	}
}

func (c *VNCConnection) handleSetPixelFormat() error {
	if _, err := io.ReadFull(c.conn, c.pixelFmtBuf[:]); err != nil {
		return fmt.Errorf("failed to read pixel format: %w", err)
	}

	pf := PixelFormat{
		BitsPerPixel: c.pixelFmtBuf[3],
		Depth:        c.pixelFmtBuf[4],
		BigEndian:    c.pixelFmtBuf[5],
		TrueColor:    c.pixelFmtBuf[6],
		RedMax:       binary.BigEndian.Uint16(c.pixelFmtBuf[7:9]),
		GreenMax:     binary.BigEndian.Uint16(c.pixelFmtBuf[9:11]),
		BlueMax:      binary.BigEndian.Uint16(c.pixelFmtBuf[11:13]),
		RedShift:     c.pixelFmtBuf[13],
		GreenShift:   c.pixelFmtBuf[14],
		BlueShift:    c.pixelFmtBuf[15],
	}

	if err := pf.Validate(); err != nil {
		vncLogger.Warn().Err(err).Msg("client sent invalid pixel format, keeping server default")
		return nil
	}

	c.pixelFormatMu.Lock()
	c.pixelFormat = pf
	c.pixelFormatMu.Unlock()

	return nil
}

func (c *VNCConnection) handleSetEncodings() error {
	if _, err := io.ReadFull(c.conn, c.encHeaderBuf[:]); err != nil {
		return fmt.Errorf("failed to read encodings header: %w", err)
	}

	numEncodings := binary.BigEndian.Uint16(c.encHeaderBuf[1:3])

	// Limit encodings to prevent DoS from malicious clients
	const maxEncodings = 256
	if numEncodings > maxEncodings {
		return fmt.Errorf("too many encodings: %d (max %d)", numEncodings, maxEncodings)
	}

	foundTight := false
	for i := uint16(0); i < numEncodings; i++ {
		if _, err := io.ReadFull(c.conn, c.encBuf[:]); err != nil {
			return fmt.Errorf("failed to read encoding: %w", err)
		}
		enc := rfbEncodingType(binary.BigEndian.Uint32(c.encBuf[:]))
		if enc == encodingTight {
			foundTight = true
		}
	}

	c.hasTight.Store(foundTight)
	previouslyNeededJPEG := c.needsJPEGEncoder.Swap(foundTight)

	if foundTight && !previouslyNeededJPEG {
		if err := c.server.requestJPEGEncoder(); err != nil {
			vncLogger.Error().
				Err(err).
				Str("remote", c.conn.RemoteAddr().String()).
				Str("errorId", "VNC_JPEG_ENCODER_FAILED").
				Msg("JPEG encoder failed to start - VNC client will not receive video")
			c.needsJPEGEncoder.Store(false)
		}
	} else if !foundTight && previouslyNeededJPEG {
		c.server.releaseJPEGEncoder()
	}

	if !foundTight {
		vncLogger.Warn().
			Str("remote", c.conn.RemoteAddr().String()).
			Msg("VNC client does not support Tight encoding - video streaming unavailable")
	}

	return nil
}

func (c *VNCConnection) handleFramebufferUpdateRequest() error {
	if _, err := io.ReadFull(c.conn, c.fbReqBuf[:]); err != nil {
		return fmt.Errorf("failed to read fb update request: %w", err)
	}

	c.frameRequested.Store(true)

	return nil
}

func (c *VNCConnection) handleKeyEvent() error {
	if _, err := io.ReadFull(c.conn, c.keyBuf[:]); err != nil {
		return fmt.Errorf("failed to read key event: %w", err)
	}

	down := c.keyBuf[0] != 0
	keysym := binary.BigEndian.Uint32(c.keyBuf[3:7])

	c.handleVNCKey(keysym, down)

	return nil
}

func (c *VNCConnection) handlePointerEvent() error {
	if _, err := io.ReadFull(c.conn, c.pointerBuf[:]); err != nil {
		return fmt.Errorf("failed to read pointer event: %w", err)
	}

	buttonMask := c.pointerBuf[0]
	x := binary.BigEndian.Uint16(c.pointerBuf[1:3])
	y := binary.BigEndian.Uint16(c.pointerBuf[3:5])

	// Rate-limited logging (safe: single-goroutine message loop per connection)
	c.pointerEventCount++
	if c.pointerEventCount <= 3 || c.pointerEventCount%100 == 0 || time.Since(c.lastPointerLogTime) > 5*time.Second {
		vncLogger.Debug().Uint16("x", x).Uint16("y", y).Uint8("buttons", buttonMask).Int32("count", c.pointerEventCount).Msg("VNC pointer event")
		c.lastPointerLogTime = time.Now()
	}

	c.handleVNCPointer(x, y, buttonMask)

	return nil
}

func (c *VNCConnection) handleClientCutText() error {
	var header [7]byte
	if _, err := io.ReadFull(c.conn, header[:]); err != nil {
		return fmt.Errorf("failed to read cut text header: %w", err)
	}

	length := binary.BigEndian.Uint32(header[3:7])

	if length > maxCutTextLength {
		return fmt.Errorf("cut text too large: %d bytes (max %d)", length, maxCutTextLength)
	}

	if length > 0 {
		// Reuse pre-allocated buffer to avoid allocation per clipboard event
		if cap(c.cutTextBuf) < int(length) {
			c.cutTextBuf = make([]byte, length)
		} else {
			c.cutTextBuf = c.cutTextBuf[:length]
		}
		if _, err := io.ReadFull(c.conn, c.cutTextBuf); err != nil {
			return fmt.Errorf("failed to read cut text: %w", err)
		}

		// Store clipboard text for paste-on-demand (when user presses Ctrl+V, Cmd+V, etc.)
		// This prevents auto-pasting when a VNC client connects with clipboard content.
		c.clipboardMu.Lock()
		c.clipboardText = make([]byte, length)
		copy(c.clipboardText, c.cutTextBuf)
		c.clipboardMu.Unlock()

		if vncLogger.Debug().Enabled() {
			vncLogger.Debug().Int("bytes", int(length)).Msg("VNC clipboard: stored text (will type on paste)")
		}
	}

	return nil
}

func (c *VNCConnection) sendFrameUpdate(jpegData []byte) error {
	// Lock-free checks on hot path
	if !c.hasTight.Load() {
		return nil
	}

	// Validate JPEG data (must start with SOI marker)
	if len(jpegData) < 2 || jpegData[0] != jpegSOIByte0 || jpegData[1] != jpegSOIByte1 {
		vncLogger.Warn().Int("len", len(jpegData)).Msg("invalid JPEG data from encoder - frame dropped")
		return nil
	}

	width, height := c.getResolution()

	// Build header in pre-allocated buffer (zero allocations)
	// Format: 16 bytes RFB header + 1 byte tight ctrl + 1-3 bytes length
	header := &c.frameHeaderBuf
	header[0] = byte(msgFramebufferUpdate)
	header[1] = 0 // padding
	header[2] = 0 // num rectangles high
	header[3] = 1 // num rectangles low
	header[4] = 0 // x high
	header[5] = 0 // x low
	header[6] = 0 // y high
	header[7] = 0 // y low
	header[8] = byte(width >> 8)
	header[9] = byte(width)
	header[10] = byte(height >> 8)
	header[11] = byte(height)
	header[12] = 0 // encoding type high bytes
	header[13] = 0
	header[14] = 0
	header[15] = byte(encodingTight) // encoding type low byte
	header[16] = tightJPEG           // Tight JPEG compression control

	// Tight compact length encoding (inline for zero overhead)
	jpegLen := len(jpegData)
	var headerLen int
	if jpegLen < tightLen1Byte {
		header[17] = byte(jpegLen)
		headerLen = 18
	} else if jpegLen < tightLen2Byte {
		header[17] = byte(jpegLen&0x7F) | 0x80
		header[18] = byte(jpegLen >> 7)
		headerLen = 19
	} else {
		header[17] = byte(jpegLen&0x7F) | 0x80
		header[18] = byte((jpegLen>>7)&0x7F) | 0x80
		header[19] = byte(jpegLen >> 14)
		headerLen = 20
	}

	// Use writev (net.Buffers) for zero-copy send of header + JPEG data
	// This avoids copying ~100KB JPEG data into a buffer
	// Note: net.Buffers.WriteTo consumes the slice, so we create it locally
	// The small slice header allocation (24 bytes) is acceptable vs complexity of reuse
	bufs := net.Buffers{header[:headerLen], jpegData}

	// Set write deadline - no need to clear it afterwards since:
	// 1. The next frame send will update the deadline anyway (at 60fps = 16ms)
	// 2. Clearing requires an extra syscall (setsockopt) per frame
	// 3. Write and read deadlines are independent in Go's net package
	if err := c.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}
	_, err := bufs.WriteTo(c.conn)

	if err != nil {
		return fmt.Errorf("failed to send frame update: %w", err)
	}

	return nil
}

// X11 keysyms for special keys
const (
	keysymEscape       = 0xFF1B
	keysymInsert       = 0xFF63
	keysymShiftLeft    = 0xFFE1
	keysymShiftRight   = 0xFFE2
	keysymControlLeft  = 0xFFE3
	keysymControlRight = 0xFFE4
	keysymMetaLeft     = 0xFFE7
	keysymMetaRight    = 0xFFE8
	keysymSuperLeft    = 0xFFEB
	keysymSuperRight   = 0xFFEC
	keysymV            = 0x76 // lowercase 'v'
	keysymVUpper       = 0x56 // uppercase 'V'
)

func (c *VNCConnection) handleVNCKey(keysym uint32, down bool) {
	// Allow Escape key to cancel ongoing paste operations
	if keysym == keysymEscape && down && isKeyboardMacroInProgress() {
		vncLogger.Info().Msg("VNC: Escape pressed, canceling paste operation")
		cancelKeyboardMacro()
		return
	}

	// Block all other keyboard input while paste is in progress
	if isKeyboardMacroInProgress() {
		if vncLogger.Debug().Enabled() {
			vncLogger.Debug().Uint32("keysym", keysym).Msg("VNC key event blocked: paste in progress")
		}
		return
	}

	// Track modifier key states for paste detection
	switch keysym {
	case keysymShiftLeft, keysymShiftRight:
		c.shiftDown = down
	case keysymControlLeft, keysymControlRight:
		c.ctrlDown = down
	case keysymMetaLeft, keysymMetaRight, keysymSuperLeft, keysymSuperRight:
		c.metaDown = down
	}

	// Detect paste key combinations (on key down)
	if down {
		isPasteCombo := false

		// Ctrl+V or Cmd+V (lowercase or uppercase V)
		if (c.ctrlDown || c.metaDown) && (keysym == keysymV || keysym == keysymVUpper) {
			isPasteCombo = true
		}

		// Shift+Insert
		if c.shiftDown && keysym == keysymInsert {
			isPasteCombo = true
		}

		if isPasteCombo {
			// Get stored clipboard text
			c.clipboardMu.Lock()
			text := c.clipboardText
			c.clipboardMu.Unlock()

			if len(text) > 0 {
				vncLogger.Info().Int("bytes", len(text)).Msg("VNC: paste combo detected, typing clipboard")
				// Type clipboard text asynchronously
				textCopy := make([]byte, len(text))
				copy(textCopy, text)
				go func() {
					if err := typeClipboardText(textCopy); err != nil {
						vncLogger.Warn().Err(err).Int("bytes", len(textCopy)).Msg("VNC clipboard: failed to type text")
					}
				}()
			} else {
				if vncLogger.Debug().Enabled() {
					vncLogger.Debug().Msg("VNC: paste combo detected but clipboard is empty")
				}
			}
			// Don't forward the paste key to the target - we handled it
			return
		}
	}

	hidKey := keysymToHID(keysym)
	if hidKey == 0 {
		// Check log level first to avoid allocations when debug is disabled
		if vncLogger.Debug().Enabled() {
			vncLogger.Debug().Uint32("keysym", keysym).Bool("down", down).Msg("VNC key event: unknown keysym, ignoring")
		}
		return
	}

	if err := rpcKeypressReport(hidKey, down); err != nil {
		vncLogger.Warn().Err(err).Uint32("keysym", keysym).Msg("failed to send key event")
	}
}

func (c *VNCConnection) handleVNCPointer(x, y uint16, buttonMask byte) {
	// Block mouse input while paste is in progress
	if isKeyboardMacroInProgress() {
		return
	}

	width, height := c.getResolution()

	if width == 0 || height == 0 {
		vncLogger.Debug().Uint16("width", width).Uint16("height", height).Msg("VNC pointer: invalid resolution, ignoring")
		return
	}

	// Scale VNC coordinates to absolute HID coordinates (0-hidAbsoluteMax)
	absX := int(x) * hidAbsoluteMax / int(width)
	absY := int(y) * hidAbsoluteMax / int(height)

	// Map VNC buttons to HID button mask:
	// VNC: bit0=left, bit1=middle, bit2=right
	// HID: bit0=left, bit1=right, bit2=middle
	vncLeft := (buttonMask & 0x01)
	vncMiddle := (buttonMask & 0x02) >> 1
	vncRight := (buttonMask & 0x04) >> 1
	buttons := vncLeft | vncRight | (vncMiddle << 2)

	if err := rpcAbsMouseReport(absX, absY, buttons); err != nil {
		vncLogger.Warn().Err(err).Int("absX", absX).Int("absY", absY).Msg("failed to send mouse event")
	}

	// RFB protocol uses button mask bits 3-6 for scroll "buttons":
	// bit3 (0x08) = scroll up, bit4 (0x10) = scroll down
	// bit5 (0x20) = scroll left, bit6 (0x40) = scroll right
	var wheelY, wheelX int8
	if buttonMask&0x08 != 0 {
		wheelY = 1
	} else if buttonMask&0x10 != 0 {
		wheelY = -1
	}
	if buttonMask&0x20 != 0 {
		wheelX = -1 // Left scroll = negative
	} else if buttonMask&0x40 != 0 {
		wheelX = 1 // Right scroll = positive
	}

	if wheelY != 0 || wheelX != 0 {
		if err := rpcWheelReport(wheelY, wheelX); err != nil {
			// Log at Debug level (not Trace) so scroll failures are visible in debug builds
			if vncLogger.Debug().Enabled() {
				vncLogger.Debug().Err(err).Int8("wheelY", wheelY).Int8("wheelX", wheelX).Msg("failed to send scroll event")
			}
		}
	}
}

// getClipboardDelays returns press and release delays based on config.VNCPasteDelayMs.
// Returns (pressDelayMs, releaseDelayMs) - each is half the configured delay.
func getClipboardDelays() (int, int) {
	delay := config.VNCPasteDelayMs
	if delay < 0 {
		delay = 0
	}
	// Split delay evenly between press and release
	// For odd values, give extra ms to release for better compatibility
	pressDelay := delay / 2
	releaseDelay := delay - pressDelay
	return pressDelay, releaseDelay
}

// HID modifier constants (matches USB HID spec)
const (
	hidModShiftLeft = 0x02
	hidModAltRight  = 0x40 // AltGr on international keyboards
)

// domKeyToHID maps DOM key codes to USB HID key codes.
// This mapping is layout-independent - it represents the physical key positions.
var domKeyToHID = map[string]uint8{
	// Letter keys
	"KeyA": 0x04, "KeyB": 0x05, "KeyC": 0x06, "KeyD": 0x07,
	"KeyE": 0x08, "KeyF": 0x09, "KeyG": 0x0A, "KeyH": 0x0B,
	"KeyI": 0x0C, "KeyJ": 0x0D, "KeyK": 0x0E, "KeyL": 0x0F,
	"KeyM": 0x10, "KeyN": 0x11, "KeyO": 0x12, "KeyP": 0x13,
	"KeyQ": 0x14, "KeyR": 0x15, "KeyS": 0x16, "KeyT": 0x17,
	"KeyU": 0x18, "KeyV": 0x19, "KeyW": 0x1A, "KeyX": 0x1B,
	"KeyY": 0x1C, "KeyZ": 0x1D,
	// Digit keys
	"Digit1": 0x1E, "Digit2": 0x1F, "Digit3": 0x20, "Digit4": 0x21,
	"Digit5": 0x22, "Digit6": 0x23, "Digit7": 0x24, "Digit8": 0x25,
	"Digit9": 0x26, "Digit0": 0x27,
	// Punctuation and special keys
	"Minus":         0x2D, // - _
	"Equal":         0x2E, // = +
	"BracketLeft":   0x2F, // [ {
	"BracketRight":  0x30, // ] }
	"Backslash":     0x31, // \ |
	"Semicolon":     0x33, // ; :
	"Quote":         0x34, // ' "
	"Backquote":     0x35, // ` ~
	"Comma":         0x36, // , <
	"Period":        0x37, // . >
	"Slash":         0x38, // / ?
	"IntlBackslash": 0x64, // Non-US \ | (ISO keyboards)
	// Whitespace and control
	"Space": 0x2C,
	"Tab":   0x2B,
	"Enter": 0x28,
}

// keyCombo represents a character's key combination (matches TypeScript KeyCombo)
type keyCombo struct {
	key      string // DOM key code (e.g., "KeyA", "Digit1")
	shift    bool   // Requires Shift modifier
	altRight bool   // Requires AltGr (Right Alt) modifier
}

// keyboardLayout maps characters to their key combinations for a specific layout
type keyboardLayout map[rune]keyCombo

// Keyboard layout definitions
// Each layout maps Unicode characters to their key combinations

var layoutEnUS = keyboardLayout{
	// Letters (same physical keys, shift for uppercase)
	'a': {"KeyA", false, false}, 'b': {"KeyB", false, false}, 'c': {"KeyC", false, false}, 'd': {"KeyD", false, false},
	'e': {"KeyE", false, false}, 'f': {"KeyF", false, false}, 'g': {"KeyG", false, false}, 'h': {"KeyH", false, false},
	'i': {"KeyI", false, false}, 'j': {"KeyJ", false, false}, 'k': {"KeyK", false, false}, 'l': {"KeyL", false, false},
	'm': {"KeyM", false, false}, 'n': {"KeyN", false, false}, 'o': {"KeyO", false, false}, 'p': {"KeyP", false, false},
	'q': {"KeyQ", false, false}, 'r': {"KeyR", false, false}, 's': {"KeyS", false, false}, 't': {"KeyT", false, false},
	'u': {"KeyU", false, false}, 'v': {"KeyV", false, false}, 'w': {"KeyW", false, false}, 'x': {"KeyX", false, false},
	'y': {"KeyY", false, false}, 'z': {"KeyZ", false, false},
	'A': {"KeyA", true, false}, 'B': {"KeyB", true, false}, 'C': {"KeyC", true, false}, 'D': {"KeyD", true, false},
	'E': {"KeyE", true, false}, 'F': {"KeyF", true, false}, 'G': {"KeyG", true, false}, 'H': {"KeyH", true, false},
	'I': {"KeyI", true, false}, 'J': {"KeyJ", true, false}, 'K': {"KeyK", true, false}, 'L': {"KeyL", true, false},
	'M': {"KeyM", true, false}, 'N': {"KeyN", true, false}, 'O': {"KeyO", true, false}, 'P': {"KeyP", true, false},
	'Q': {"KeyQ", true, false}, 'R': {"KeyR", true, false}, 'S': {"KeyS", true, false}, 'T': {"KeyT", true, false},
	'U': {"KeyU", true, false}, 'V': {"KeyV", true, false}, 'W': {"KeyW", true, false}, 'X': {"KeyX", true, false},
	'Y': {"KeyY", true, false}, 'Z': {"KeyZ", true, false},
	// Digits
	'1': {"Digit1", false, false}, '2': {"Digit2", false, false}, '3': {"Digit3", false, false}, '4': {"Digit4", false, false},
	'5': {"Digit5", false, false}, '6': {"Digit6", false, false}, '7': {"Digit7", false, false}, '8': {"Digit8", false, false},
	'9': {"Digit9", false, false}, '0': {"Digit0", false, false},
	// Shifted digits (symbols)
	'!': {"Digit1", true, false}, '@': {"Digit2", true, false}, '#': {"Digit3", true, false}, '$': {"Digit4", true, false},
	'%': {"Digit5", true, false}, '^': {"Digit6", true, false}, '&': {"Digit7", true, false}, '*': {"Digit8", true, false},
	'(': {"Digit9", true, false}, ')': {"Digit0", true, false},
	// Punctuation
	'-': {"Minus", false, false}, '_': {"Minus", true, false},
	'=': {"Equal", false, false}, '+': {"Equal", true, false},
	'[': {"BracketLeft", false, false}, '{': {"BracketLeft", true, false},
	']': {"BracketRight", false, false}, '}': {"BracketRight", true, false},
	'\\': {"Backslash", false, false}, '|': {"Backslash", true, false},
	';': {"Semicolon", false, false}, ':': {"Semicolon", true, false},
	'\'': {"Quote", false, false}, '"': {"Quote", true, false},
	'`': {"Backquote", false, false}, '~': {"Backquote", true, false},
	',': {"Comma", false, false}, '<': {"Comma", true, false},
	'.': {"Period", false, false}, '>': {"Period", true, false},
	'/': {"Slash", false, false}, '?': {"Slash", true, false},
	// Whitespace
	' ': {"Space", false, false}, '\t': {"Tab", false, false}, '\n': {"Enter", false, false}, '\r': {"Enter", false, false},
}

var layoutEnUK = keyboardLayout{
	// Letters (same as US)
	'a': {"KeyA", false, false}, 'b': {"KeyB", false, false}, 'c': {"KeyC", false, false}, 'd': {"KeyD", false, false},
	'e': {"KeyE", false, false}, 'f': {"KeyF", false, false}, 'g': {"KeyG", false, false}, 'h': {"KeyH", false, false},
	'i': {"KeyI", false, false}, 'j': {"KeyJ", false, false}, 'k': {"KeyK", false, false}, 'l': {"KeyL", false, false},
	'm': {"KeyM", false, false}, 'n': {"KeyN", false, false}, 'o': {"KeyO", false, false}, 'p': {"KeyP", false, false},
	'q': {"KeyQ", false, false}, 'r': {"KeyR", false, false}, 's': {"KeyS", false, false}, 't': {"KeyT", false, false},
	'u': {"KeyU", false, false}, 'v': {"KeyV", false, false}, 'w': {"KeyW", false, false}, 'x': {"KeyX", false, false},
	'y': {"KeyY", false, false}, 'z': {"KeyZ", false, false},
	'A': {"KeyA", true, false}, 'B': {"KeyB", true, false}, 'C': {"KeyC", true, false}, 'D': {"KeyD", true, false},
	'E': {"KeyE", true, false}, 'F': {"KeyF", true, false}, 'G': {"KeyG", true, false}, 'H': {"KeyH", true, false},
	'I': {"KeyI", true, false}, 'J': {"KeyJ", true, false}, 'K': {"KeyK", true, false}, 'L': {"KeyL", true, false},
	'M': {"KeyM", true, false}, 'N': {"KeyN", true, false}, 'O': {"KeyO", true, false}, 'P': {"KeyP", true, false},
	'Q': {"KeyQ", true, false}, 'R': {"KeyR", true, false}, 'S': {"KeyS", true, false}, 'T': {"KeyT", true, false},
	'U': {"KeyU", true, false}, 'V': {"KeyV", true, false}, 'W': {"KeyW", true, false}, 'X': {"KeyX", true, false},
	'Y': {"KeyY", true, false}, 'Z': {"KeyZ", true, false},
	// Digits
	'1': {"Digit1", false, false}, '2': {"Digit2", false, false}, '3': {"Digit3", false, false}, '4': {"Digit4", false, false},
	'5': {"Digit5", false, false}, '6': {"Digit6", false, false}, '7': {"Digit7", false, false}, '8': {"Digit8", false, false},
	'9': {"Digit9", false, false}, '0': {"Digit0", false, false},
	// UK-specific shifted digits
	'!': {"Digit1", true, false}, '"': {"Digit2", true, false}, '£': {"Digit3", true, false}, '$': {"Digit4", true, false},
	'%': {"Digit5", true, false}, '^': {"Digit6", true, false}, '&': {"Digit7", true, false}, '*': {"Digit8", true, false},
	'(': {"Digit9", true, false}, ')': {"Digit0", true, false},
	// UK-specific symbols
	'€': {"Digit4", false, true}, // AltGr+4
	'@': {"Quote", true, false},  // Shift+' (different from US!)
	'\'': {"Quote", false, false},
	'#': {"Backslash", false, false}, '~': {"Backslash", true, false},
	'\\': {"IntlBackslash", false, false}, '|': {"IntlBackslash", true, false},
	// Common punctuation (same as US)
	'-': {"Minus", false, false}, '_': {"Minus", true, false},
	'=': {"Equal", false, false}, '+': {"Equal", true, false},
	'[': {"BracketLeft", false, false}, '{': {"BracketLeft", true, false},
	']': {"BracketRight", false, false}, '}': {"BracketRight", true, false},
	';': {"Semicolon", false, false}, ':': {"Semicolon", true, false},
	'`': {"Backquote", false, false},
	',': {"Comma", false, false}, '<': {"Comma", true, false},
	'.': {"Period", false, false}, '>': {"Period", true, false},
	'/': {"Slash", false, false}, '?': {"Slash", true, false},
	// Whitespace
	' ': {"Space", false, false}, '\t': {"Tab", false, false}, '\n': {"Enter", false, false}, '\r': {"Enter", false, false},
}

var layoutDeDE = keyboardLayout{
	// QWERTZ layout - Y and Z are swapped
	'a': {"KeyA", false, false}, 'b': {"KeyB", false, false}, 'c': {"KeyC", false, false}, 'd': {"KeyD", false, false},
	'e': {"KeyE", false, false}, 'f': {"KeyF", false, false}, 'g': {"KeyG", false, false}, 'h': {"KeyH", false, false},
	'i': {"KeyI", false, false}, 'j': {"KeyJ", false, false}, 'k': {"KeyK", false, false}, 'l': {"KeyL", false, false},
	'm': {"KeyM", false, false}, 'n': {"KeyN", false, false}, 'o': {"KeyO", false, false}, 'p': {"KeyP", false, false},
	'q': {"KeyQ", false, false}, 'r': {"KeyR", false, false}, 's': {"KeyS", false, false}, 't': {"KeyT", false, false},
	'u': {"KeyU", false, false}, 'v': {"KeyV", false, false}, 'w': {"KeyW", false, false}, 'x': {"KeyX", false, false},
	'y': {"KeyZ", false, false}, 'z': {"KeyY", false, false}, // SWAPPED!
	'A': {"KeyA", true, false}, 'B': {"KeyB", true, false}, 'C': {"KeyC", true, false}, 'D': {"KeyD", true, false},
	'E': {"KeyE", true, false}, 'F': {"KeyF", true, false}, 'G': {"KeyG", true, false}, 'H': {"KeyH", true, false},
	'I': {"KeyI", true, false}, 'J': {"KeyJ", true, false}, 'K': {"KeyK", true, false}, 'L': {"KeyL", true, false},
	'M': {"KeyM", true, false}, 'N': {"KeyN", true, false}, 'O': {"KeyO", true, false}, 'P': {"KeyP", true, false},
	'Q': {"KeyQ", true, false}, 'R': {"KeyR", true, false}, 'S': {"KeyS", true, false}, 'T': {"KeyT", true, false},
	'U': {"KeyU", true, false}, 'V': {"KeyV", true, false}, 'W': {"KeyW", true, false}, 'X': {"KeyX", true, false},
	'Y': {"KeyZ", true, false}, 'Z': {"KeyY", true, false}, // SWAPPED!
	// Digits
	'1': {"Digit1", false, false}, '2': {"Digit2", false, false}, '3': {"Digit3", false, false}, '4': {"Digit4", false, false},
	'5': {"Digit5", false, false}, '6': {"Digit6", false, false}, '7': {"Digit7", false, false}, '8': {"Digit8", false, false},
	'9': {"Digit9", false, false}, '0': {"Digit0", false, false},
	// German-specific shifted digits
	'!': {"Digit1", true, false}, '"': {"Digit2", true, false}, '§': {"Digit3", true, false}, '$': {"Digit4", true, false},
	'%': {"Digit5", true, false}, '&': {"Digit6", true, false}, '/': {"Digit7", true, false}, '(': {"Digit8", true, false},
	')': {"Digit9", true, false}, '=': {"Digit0", true, false},
	// German-specific AltGr symbols
	'@': {"KeyQ", false, true}, '€': {"KeyE", false, true}, 'µ': {"KeyM", false, true},
	'²': {"Digit2", false, true}, '³': {"Digit3", false, true}, '{': {"Digit7", false, true},
	'[': {"Digit8", false, true}, ']': {"Digit9", false, true}, '}': {"Digit0", false, true},
	'\\': {"Minus", false, true}, '~': {"BracketRight", false, true}, '|': {"IntlBackslash", false, true},
	// German punctuation
	'ß': {"Minus", false, false}, '?': {"Minus", true, false},
	'+': {"BracketRight", false, false}, '*': {"BracketRight", true, false},
	'#': {"Backslash", false, false}, '\'': {"Backslash", true, false},
	'-': {"Slash", false, false}, '_': {"Slash", true, false},
	'.': {"Period", false, false}, ':': {"Period", true, false},
	',': {"Comma", false, false}, ';': {"Comma", true, false},
	'<': {"IntlBackslash", false, false}, '>': {"IntlBackslash", true, false},
	// German umlauts (on dedicated keys)
	'ü': {"BracketLeft", false, false}, 'Ü': {"BracketLeft", true, false},
	'ö': {"Semicolon", false, false}, 'Ö': {"Semicolon", true, false},
	'ä': {"Quote", false, false}, 'Ä': {"Quote", true, false},
	// Whitespace
	' ': {"Space", false, false}, '\t': {"Tab", false, false}, '\n': {"Enter", false, false}, '\r': {"Enter", false, false},
}

var layoutFrFR = keyboardLayout{
	// AZERTY layout - completely different arrangement
	'a': {"KeyQ", false, false}, 'b': {"KeyB", false, false}, 'c': {"KeyC", false, false}, 'd': {"KeyD", false, false},
	'e': {"KeyE", false, false}, 'f': {"KeyF", false, false}, 'g': {"KeyG", false, false}, 'h': {"KeyH", false, false},
	'i': {"KeyI", false, false}, 'j': {"KeyJ", false, false}, 'k': {"KeyK", false, false}, 'l': {"KeyL", false, false},
	'm': {"Semicolon", false, false}, 'n': {"KeyN", false, false}, 'o': {"KeyO", false, false}, 'p': {"KeyP", false, false},
	'q': {"KeyA", false, false}, 'r': {"KeyR", false, false}, 's': {"KeyS", false, false}, 't': {"KeyT", false, false},
	'u': {"KeyU", false, false}, 'v': {"KeyV", false, false}, 'w': {"KeyZ", false, false}, 'x': {"KeyX", false, false},
	'y': {"KeyY", false, false}, 'z': {"KeyW", false, false},
	'A': {"KeyQ", true, false}, 'B': {"KeyB", true, false}, 'C': {"KeyC", true, false}, 'D': {"KeyD", true, false},
	'E': {"KeyE", true, false}, 'F': {"KeyF", true, false}, 'G': {"KeyG", true, false}, 'H': {"KeyH", true, false},
	'I': {"KeyI", true, false}, 'J': {"KeyJ", true, false}, 'K': {"KeyK", true, false}, 'L': {"KeyL", true, false},
	'M': {"Semicolon", true, false}, 'N': {"KeyN", true, false}, 'O': {"KeyO", true, false}, 'P': {"KeyP", true, false},
	'Q': {"KeyA", true, false}, 'R': {"KeyR", true, false}, 'S': {"KeyS", true, false}, 'T': {"KeyT", true, false},
	'U': {"KeyU", true, false}, 'V': {"KeyV", true, false}, 'W': {"KeyZ", true, false}, 'X': {"KeyX", true, false},
	'Y': {"KeyY", true, false}, 'Z': {"KeyW", true, false},
	// French number row (unshifted = symbols, shifted = digits!)
	'&': {"Digit1", false, false}, '1': {"Digit1", true, false},
	'é': {"Digit2", false, false}, '2': {"Digit2", true, false},
	'"': {"Digit3", false, false}, '3': {"Digit3", true, false},
	'\'': {"Digit4", false, false}, '4': {"Digit4", true, false},
	'(': {"Digit5", false, false}, '5': {"Digit5", true, false},
	'-': {"Digit6", false, false}, '6': {"Digit6", true, false},
	'è': {"Digit7", false, false}, '7': {"Digit7", true, false},
	'_': {"Digit8", false, false}, '8': {"Digit8", true, false},
	'ç': {"Digit9", false, false}, '9': {"Digit9", true, false},
	'à': {"Digit0", false, false}, '0': {"Digit0", true, false},
	// French AltGr symbols
	'~': {"Digit2", false, true}, '#': {"Digit3", false, true}, '{': {"Digit4", false, true},
	'[': {"Digit5", false, true}, '|': {"Digit6", false, true}, '`': {"Digit7", false, true},
	'\\': {"Digit8", false, true}, '^': {"Digit9", false, true}, '@': {"Digit0", false, true},
	']': {"Minus", false, true}, '}': {"Equal", false, true}, '€': {"KeyE", false, true},
	// French punctuation
	')': {"Minus", false, false}, '°': {"Minus", true, false},
	'=': {"Equal", false, false}, '+': {"Equal", true, false},
	'$': {"BracketRight", false, false}, '£': {"BracketRight", true, false},
	'ù': {"Quote", false, false}, '%': {"Quote", true, false},
	'*': {"Backslash", false, false}, 'µ': {"Backslash", true, false},
	',': {"KeyM", false, false}, '?': {"KeyM", true, false},
	';': {"Comma", false, false}, '.': {"Comma", true, false},
	':': {"Period", false, false}, '/': {"Period", true, false},
	'!': {"Slash", false, false}, '§': {"Slash", true, false},
	'<': {"IntlBackslash", false, false}, '>': {"IntlBackslash", true, false},
	// Whitespace
	' ': {"Space", false, false}, '\t': {"Tab", false, false}, '\n': {"Enter", false, false}, '\r': {"Enter", false, false},
}

// layoutRegistry maps layout codes to their character mappings
var layoutRegistry = map[string]keyboardLayout{
	"en-US": layoutEnUS,
	"en-UK": layoutEnUK,
	"de-DE": layoutDeDE,
	"fr-FR": layoutFrFR,
}

// getKeyboardLayout returns the layout for the given code, falling back to en-US
func getKeyboardLayout(layoutCode string) keyboardLayout {
	if layout, ok := layoutRegistry[layoutCode]; ok {
		return layout
	}
	return layoutEnUS
}

// textToMacroStepsWithDelays converts text to keyboard macro steps for typing via USB HID.
// Uses the keyboard layout from config.KeyboardLayout.
// Characters not in the layout mapping are skipped.
// pressDelayMs and releaseDelayMs control typing speed.
// Returns the macro steps and count of skipped characters.
func textToMacroStepsWithDelays(text []byte, layoutCode string, pressDelayMs, releaseDelayMs int) ([]hidrpc.KeyboardMacroStep, int) {
	layout := getKeyboardLayout(layoutCode)

	// Pre-allocate: each character needs 2 steps (press + release)
	steps := make([]hidrpc.KeyboardMacroStep, 0, len(text)*2)
	skipped := 0

	// Convert bytes to string for proper UTF-8 rune iteration
	textStr := string(text)

	for _, char := range textStr {
		combo, ok := layout[char]
		if !ok {
			skipped++
			continue
		}

		hidKey, ok := domKeyToHID[combo.key]
		if !ok {
			skipped++
			continue
		}

		// Build modifier byte
		var modifier uint8
		if combo.shift {
			modifier |= hidModShiftLeft
		}
		if combo.altRight {
			modifier |= hidModAltRight
		}

		// Key press step
		keys := make([]byte, hidrpc.HidKeyBufferSize)
		keys[0] = hidKey
		steps = append(steps, hidrpc.KeyboardMacroStep{
			Modifier: modifier,
			Keys:     keys,
			Delay:    uint16(pressDelayMs),
		})

		// Key release step (all zeros)
		releaseKeys := make([]byte, hidrpc.HidKeyBufferSize)
		steps = append(steps, hidrpc.KeyboardMacroStep{
			Modifier: 0,
			Keys:     releaseKeys,
			Delay:    uint16(releaseDelayMs),
		})
	}

	return steps, skipped
}

// Maximum clipboard text size for typing (100KB - matches typical use cases, larger content takes too long)
const maxClipboardTypeSize = 100 * 1024

// typeClipboardText types the given text via keyboard macro.
// Uses config.KeyboardLayout to determine the keyboard mapping.
// This implements VNC clipboard-as-keystrokes functionality.
// Respects config.VNCClipboardEnabled setting.
// Rejects text larger than 100KB to prevent accidental paste of large content.
func typeClipboardText(text []byte) error {
	if !config.VNCClipboardEnabled {
		vncLogger.Debug().Int("bytes", len(text)).Msg("VNC clipboard: typing disabled, ignoring")
		return nil
	}

	if len(text) == 0 {
		return nil
	}

	if len(text) > maxClipboardTypeSize {
		vncLogger.Info().Int("bytes", len(text)).Int("maxBytes", maxClipboardTypeSize).Msg("VNC clipboard: text too large, ignoring (likely not intended for typing)")
		return nil
	}

	layoutCode := config.KeyboardLayout
	if layoutCode == "" {
		layoutCode = "en-US"
	}

	pressDelay, releaseDelay := getClipboardDelays()
	steps, skipped := textToMacroStepsWithDelays(text, layoutCode, pressDelay, releaseDelay)
	if len(steps) == 0 {
		vncLogger.Info().Int("skipped", skipped).Str("layout", layoutCode).Int("textLen", len(text)).Msg("VNC clipboard: no typeable characters in text")
		return nil
	}

	if skipped > 0 {
		vncLogger.Info().Int("skipped", skipped).Int("typed", len(steps)/2).Str("layout", layoutCode).Msg("VNC clipboard: some characters not in layout")
	}

	vncLogger.Info().Int("chars", len(steps)/2).Str("layout", layoutCode).Msg("VNC clipboard: typing text via keyboard")

	err := rpcExecuteKeyboardMacro(steps)
	if err != nil {
		vncLogger.Error().Err(err).Msg("VNC clipboard: keyboard macro failed")
	} else {
		vncLogger.Info().Int("chars", len(steps)/2).Msg("VNC clipboard: typing completed")
	}
	return err
}

// keysymToHID converts X11 keysym codes (used by VNC/RFB protocol) to USB HID usage codes.
//
// References:
//   - X11 Keysyms: https://www.x.org/releases/current/doc/xproto/x11protocol.html#keysym_encoding
//     Source header: /usr/include/X11/keysymdef.h
//   - USB HID Usage Tables: https://usb.org/document-library/hid-usage-tables-15
//     Section 10: Keyboard/Keypad Page (0x07)
//
// Supported keyboard layouts:
//   - US ANSI (104-key)
//   - UK/British ISO (105-key with § ± keys)
//   - German QWERTZ (ä ö ü ß)
//   - French AZERTY (é è ç à ù)
//   - Nordic (Swedish, Norwegian, Danish, Finnish - å ø æ)
//   - Spanish (ñ ¿ ¡)
//   - Portuguese (ã õ)
//   - Italian (ì ò)
//   - Japanese JIS (変換, 無変換, ひらがな)
//   - Korean (한/영, 한자)
//   - Mac extended (F13-F24, Command/Meta keys)
//
// Also supports:
//   - Dead keys for composing accented characters
//   - Numpad with and without NumLock
//   - Currency symbols (€ £ ¥ ¢)
//   - Power key (Mac)
//
// NOT supported (requires USB Consumer HID page, not keyboard page):
//   - Media keys (volume, play/pause, etc.)
//   - Browser keys (back, forward, refresh, etc.)
//
// Returns 0 for unsupported keysyms.
func keysymToHID(keysym uint32) uint8 {
	// Modifier keys
	switch keysym {
	case 0xFFE1:
		return 0xE1 // Left Shift
	case 0xFFE2:
		return 0xE5 // Right Shift
	case 0xFFE3:
		return 0xE0 // Left Control
	case 0xFFE4:
		return 0xE4 // Right Control
	case 0xFFE7:
		return 0xE3 // Left Meta (macOS Command key)
	case 0xFFE8:
		return 0xE7 // Right Meta (macOS Command key)
	case 0xFFE9:
		return 0xE2 // Left Alt
	case 0xFFEA:
		return 0xE6 // Right Alt
	case 0xFFEB:
		return 0xE3 // Left Super (Windows/Linux GUI key)
	case 0xFFEC:
		return 0xE7 // Right Super (Windows/Linux GUI key)
	case 0xFFED:
		return 0xE3 // Left Hyper -> map to GUI
	case 0xFFEE:
		return 0xE7 // Right Hyper -> map to GUI
	}

	// Function keys F1-F12 (0xFFBE-0xFFC9)
	if keysym >= 0xFFBE && keysym <= 0xFFC9 {
		return uint8(0x3A + (keysym - 0xFFBE))
	}

	// Function keys F13-F24 (0xFFCA-0xFFD5) - Mac extended keyboards
	if keysym >= 0xFFCA && keysym <= 0xFFD5 {
		return uint8(0x68 + (keysym - 0xFFCA)) // F13=0x68, F14=0x69, ... F24=0x73
	}

	// Navigation and special keys
	switch keysym {
	case 0xFF08:
		return 0x2A // Backspace
	case 0xFF09:
		return 0x2B // Tab
	case 0xFF0D:
		return 0x28 // Enter/Return
	case 0xFF8D:
		return 0x58 // KP_Enter
	case 0xFF0A:
		return 0x28 // Linefeed -> Enter
	case 0xFF1B:
		return 0x29 // Escape
	case 0xFFFF:
		return 0x4C // Delete (forward)
	case 0xFF50:
		return 0x4A // Home
	case 0xFF51:
		return 0x50 // Left Arrow
	case 0xFF52:
		return 0x52 // Up Arrow
	case 0xFF53:
		return 0x4F // Right Arrow
	case 0xFF54:
		return 0x51 // Down Arrow
	case 0xFF55:
		return 0x4B // Page Up
	case 0xFF56:
		return 0x4E // Page Down
	case 0xFF57:
		return 0x4D // End
	case 0xFF58:
		return 0x4A // Begin -> Home
	case 0xFF63:
		return 0x49 // Insert
	case 0xFF14:
		return 0x47 // Scroll Lock
	case 0xFF7F:
		return 0x53 // Num Lock
	case 0xFF13:
		return 0x48 // Pause
	case 0xFF6B:
		return 0x48 // Break -> Pause
	case 0xFF61:
		return 0x46 // Print Screen
	case 0xFF15:
		return 0x46 // Sys_Req -> Print Screen
	case 0xFF20:
		return 0x65 // Multi_key -> Menu/Application
	case 0xFF67:
		return 0x65 // Menu
	case 0xFFE5:
		return 0x39 // Caps Lock
	case 0xFFE6:
		return 0x39 // Shift_Lock -> Caps Lock
	case 0x0020:
		return 0x2C // Space
	case 0xFF80:
		return 0x2C // KP_Space -> Space
	}

	// Lowercase letters a-z (0x0061-0x007A)
	if keysym >= 0x0061 && keysym <= 0x007A {
		return uint8(0x04 + (keysym - 0x0061))
	}

	// Uppercase letters A-Z (0x0041-0x005A) - same HID codes as lowercase
	if keysym >= 0x0041 && keysym <= 0x005A {
		return uint8(0x04 + (keysym - 0x0041))
	}

	// Numbers 0-9 (0x0030-0x0039)
	if keysym >= 0x0030 && keysym <= 0x0039 {
		if keysym == 0x0030 {
			return 0x27 // 0
		}
		return uint8(0x1E + (keysym - 0x0031)) // 1-9
	}

	// Basic punctuation and symbols
	switch keysym {
	case 0x002D:
		return 0x2D // - (minus/hyphen)
	case 0x003D:
		return 0x2E // =
	case 0x005B:
		return 0x2F // [
	case 0x005D:
		return 0x30 // ]
	case 0x005C:
		return 0x31 // \ (backslash)
	case 0x003B:
		return 0x33 // ;
	case 0x0027:
		return 0x34 // ' (apostrophe)
	case 0x0060:
		return 0x35 // ` (grave/backtick)
	case 0x002C:
		return 0x36 // ,
	case 0x002E:
		return 0x37 // .
	case 0x002F:
		return 0x38 // /
	}

	// Shifted symbols - map to base key (shift state handled separately by VNC client)
	switch keysym {
	case 0x0021:
		return 0x1E // ! -> 1
	case 0x0040:
		return 0x1F // @ -> 2
	case 0x0023:
		return 0x20 // # -> 3
	case 0x0024:
		return 0x21 // $ -> 4
	case 0x0025:
		return 0x22 // % -> 5
	case 0x005E:
		return 0x23 // ^ -> 6
	case 0x0026:
		return 0x24 // & -> 7
	case 0x002A:
		return 0x25 // * -> 8
	case 0x0028:
		return 0x26 // ( -> 9
	case 0x0029:
		return 0x27 // ) -> 0
	case 0x005F:
		return 0x2D // _ -> -
	case 0x002B:
		return 0x2E // + -> =
	case 0x007B:
		return 0x2F // { -> [
	case 0x007D:
		return 0x30 // } -> ]
	case 0x007C:
		return 0x31 // | -> \
	case 0x003A:
		return 0x33 // : -> ;
	case 0x0022:
		return 0x34 // " -> '
	case 0x007E:
		return 0x35 // ~ -> `
	case 0x003C:
		return 0x36 // < -> ,
	case 0x003E:
		return 0x37 // > -> .
	case 0x003F:
		return 0x38 // ? -> /
	}

	// Numpad keys
	switch keysym {
	case 0xFFAA:
		return 0x55 // KP_Multiply
	case 0xFFAB:
		return 0x57 // KP_Add
	case 0xFFAC:
		return 0x85 // KP_Separator (comma on some layouts)
	case 0xFFAD:
		return 0x56 // KP_Subtract
	case 0xFFAE:
		return 0x63 // KP_Decimal
	case 0xFFAF:
		return 0x54 // KP_Divide
	case 0xFFB0:
		return 0x62 // KP_0
	case 0xFFB1:
		return 0x59 // KP_1
	case 0xFFB2:
		return 0x5A // KP_2
	case 0xFFB3:
		return 0x5B // KP_3
	case 0xFFB4:
		return 0x5C // KP_4
	case 0xFFB5:
		return 0x5D // KP_5
	case 0xFFB6:
		return 0x5E // KP_6
	case 0xFFB7:
		return 0x5F // KP_7
	case 0xFFB8:
		return 0x60 // KP_8
	case 0xFFB9:
		return 0x61 // KP_9
	case 0xFFBD:
		return 0x67 // KP_Equal (Mac keyboards)
	// Numpad navigation (when NumLock is off)
	case 0xFF95:
		return 0x5C // KP_Home -> KP_7
	case 0xFF96:
		return 0x50 // KP_Left -> Left
	case 0xFF97:
		return 0x52 // KP_Up -> Up
	case 0xFF98:
		return 0x4F // KP_Right -> Right
	case 0xFF99:
		return 0x51 // KP_Down -> Down
	case 0xFF9A:
		return 0x60 // KP_Page_Up -> KP_9
	case 0xFF9B:
		return 0x5A // KP_Page_Down -> KP_3
	case 0xFF9C:
		return 0x62 // KP_End -> KP_1
	case 0xFF9D:
		return 0x5D // KP_Begin -> KP_5
	case 0xFF9E:
		return 0x62 // KP_Insert -> KP_0
	case 0xFF9F:
		return 0x63 // KP_Delete -> KP_Decimal
	}

	// ISO keyboard: extra key between left shift and Z (non-US backslash)
	// This key exists on UK, German, and other ISO layouts
	switch keysym {
	case 0x00A6:
		return 0x64 // ¦ (broken bar) - ISO key
	case 0x00A7:
		return 0x35 // § (section) - UK/German, left of 1
	case 0x00B0:
		return 0x35 // ° (degree) - German shift+^
	case 0x00B1:
		return 0x35 // ± (plus-minus) - UK shift+§
	case 0x00B2:
		return 0x1F // ² (superscript 2) - German
	case 0x00B3:
		return 0x20 // ³ (superscript 3) - German
	case 0x00B4:
		return 0x34 // ´ (acute accent) - dead key result
	case 0x00B5:
		return 0x10 // µ (micro) - AltGr+M on some layouts
	case 0x00AC:
		return 0x35 // ¬ (not sign) - UK
	}

	// German keyboard specific
	switch keysym {
	case 0x00E4:
		return 0x34 // ä -> ' key position (German layout)
	case 0x00C4:
		return 0x34 // Ä -> ' key position
	case 0x00F6:
		return 0x33 // ö -> ; key position
	case 0x00D6:
		return 0x33 // Ö -> ; key position
	case 0x00FC:
		return 0x2F // ü -> [ key position
	case 0x00DC:
		return 0x2F // Ü -> [ key position
	case 0x00DF:
		return 0x2D // ß -> - key position
	case 0x1E9E:
		return 0x2D // ẞ (capital ß) -> - key position
	}

	// French keyboard specific (AZERTY)
	switch keysym {
	case 0x00E0:
		return 0x27 // à -> 0 key position
	case 0x00C0:
		return 0x27 // À
	case 0x00E7:
		return 0x26 // ç -> 9 key position
	case 0x00C7:
		return 0x26 // Ç
	case 0x00E8:
		return 0x24 // è -> 7 key position
	case 0x00C8:
		return 0x24 // È
	case 0x00E9:
		return 0x1F // é -> 2 key position
	case 0x00C9:
		return 0x1F // É
	case 0x00F9:
		return 0x34 // ù -> ' key position
	case 0x00D9:
		return 0x34 // Ù
	}

	// Nordic keyboard specific (Swedish, Norwegian, Danish, Finnish)
	switch keysym {
	case 0x00E5:
		return 0x2F // å -> [ key position
	case 0x00C5:
		return 0x2F // Å
	case 0x00E6:
		return 0x34 // æ -> ' key position
	case 0x00C6:
		return 0x34 // Æ
	case 0x00F8:
		return 0x33 // ø -> ; key position
	case 0x00D8:
		return 0x33 // Ø
	}

	// Spanish keyboard specific
	switch keysym {
	case 0x00F1:
		return 0x33 // ñ -> ; key position
	case 0x00D1:
		return 0x33 // Ñ
	case 0x00BF:
		return 0x2E // ¿ -> = key position
	case 0x00A1:
		return 0x2E // ¡ -> = key position
	}

	// Portuguese keyboard specific
	switch keysym {
	case 0x00E3:
		return 0x34 // ã -> ' key position
	case 0x00C3:
		return 0x34 // Ã
	case 0x00F5:
		return 0x33 // õ -> ; key position
	case 0x00D5:
		return 0x33 // Õ
	}

	// Italian keyboard specific
	switch keysym {
	case 0x00EC:
		return 0x2E // ì -> = key position
	case 0x00CC:
		return 0x2E // Ì
	case 0x00F2:
		return 0x33 // ò -> ; key position
	case 0x00D2:
		return 0x33 // Ò
	}

	// Common European characters (Latin-1 Supplement: 0x00C0-0x00FF)
	switch keysym {
	case 0x00E2:
		return 0x04 // â -> a
	case 0x00C2:
		return 0x04 // Â
	case 0x00EA:
		return 0x08 // ê -> e
	case 0x00CA:
		return 0x08 // Ê
	case 0x00EB:
		return 0x08 // ë -> e
	case 0x00CB:
		return 0x08 // Ë
	case 0x00EE:
		return 0x0C // î -> i
	case 0x00CE:
		return 0x0C // Î
	case 0x00EF:
		return 0x0C // ï -> i
	case 0x00CF:
		return 0x0C // Ï
	case 0x00F4:
		return 0x12 // ô -> o
	case 0x00D4:
		return 0x12 // Ô
	case 0x00FB:
		return 0x18 // û -> u
	case 0x00DB:
		return 0x18 // Û
	case 0x00FF:
		return 0x1C // ÿ -> y
	case 0x0178:
		return 0x1C // Ÿ
	}

	// Romanian specific (Latin Extended-A and -B)
	switch keysym {
	case 0x0103, 0x01E3: // ă (a-breve) - both Unicode and legacy keysym
		return 0x04 // -> a
	case 0x0102, 0x01C3: // Ă
		return 0x04
	case 0x0219, 0x015F: // ș (s-comma-below) and ş (s-cedilla, older)
		return 0x16 // -> s
	case 0x0218, 0x015E: // Ș and Ş
		return 0x16
	case 0x021B, 0x0163: // ț (t-comma-below) and ţ (t-cedilla, older)
		return 0x17 // -> t
	case 0x021A, 0x0162: // Ț and Ţ
		return 0x17
	}

	// Turkish specific
	switch keysym {
	case 0x011F: // ğ (g-breve)
		return 0x0A // -> g
	case 0x011E: // Ğ
		return 0x0A
	case 0x0131: // ı (dotless i)
		return 0x0C // -> i
	case 0x0130: // İ (I with dot)
		return 0x0C
	case 0x015F: // ş (s-cedilla) - also used in Turkish
		return 0x16 // -> s
	case 0x015E: // Ş
		return 0x16
	}

	// Polish specific
	switch keysym {
	case 0x0105: // ą (a-ogonek)
		return 0x04 // -> a
	case 0x0104: // Ą
		return 0x04
	case 0x0107: // ć (c-acute)
		return 0x06 // -> c
	case 0x0106: // Ć
		return 0x06
	case 0x0119: // ę (e-ogonek)
		return 0x08 // -> e
	case 0x0118: // Ę
		return 0x08
	case 0x0142: // ł (l-stroke)
		return 0x0F // -> l
	case 0x0141: // Ł
		return 0x0F
	case 0x0144: // ń (n-acute)
		return 0x11 // -> n
	case 0x0143: // Ń
		return 0x11
	case 0x00F3: // ó (o-acute) - also Latin-1
		return 0x12 // -> o
	case 0x00D3: // Ó
		return 0x12
	case 0x015B: // ś (s-acute)
		return 0x16 // -> s
	case 0x015A: // Ś
		return 0x16
	case 0x017A: // ź (z-acute)
		return 0x1D // -> z
	case 0x0179: // Ź
		return 0x1D
	case 0x017C: // ż (z-dot-above)
		return 0x1D // -> z
	case 0x017B: // Ż
		return 0x1D
	}

	// Czech/Slovak specific
	switch keysym {
	case 0x010D: // č (c-caron)
		return 0x06 // -> c
	case 0x010C: // Č
		return 0x06
	case 0x010F: // ď (d-caron)
		return 0x07 // -> d
	case 0x010E: // Ď
		return 0x07
	case 0x011B: // ě (e-caron)
		return 0x08 // -> e
	case 0x011A: // Ě
		return 0x08
	case 0x0148: // ň (n-caron)
		return 0x11 // -> n
	case 0x0147: // Ň
		return 0x11
	case 0x0159: // ř (r-caron)
		return 0x15 // -> r
	case 0x0158: // Ř
		return 0x15
	case 0x0161: // š (s-caron)
		return 0x16 // -> s
	case 0x0160: // Š
		return 0x16
	case 0x0165: // ť (t-caron)
		return 0x17 // -> t
	case 0x0164: // Ť
		return 0x17
	case 0x016F: // ů (u-ring)
		return 0x18 // -> u
	case 0x016E: // Ů
		return 0x18
	case 0x017E: // ž (z-caron)
		return 0x1D // -> z
	case 0x017D: // Ž
		return 0x1D
	}

	// Hungarian specific
	switch keysym {
	case 0x0151: // ő (o-double-acute)
		return 0x12 // -> o
	case 0x0150: // Ő
		return 0x12
	case 0x0171: // ű (u-double-acute)
		return 0x18 // -> u
	case 0x0170: // Ű
		return 0x18
	}

	// Croatian/Slovenian specific
	switch keysym {
	case 0x0111: // đ (d-stroke)
		return 0x07 // -> d
	case 0x0110: // Đ
		return 0x07
	}

	// Icelandic specific
	switch keysym {
	case 0x00FE: // þ (thorn)
		return 0x17 // -> t (closest approximation)
	case 0x00DE: // Þ
		return 0x17
	case 0x00F0: // ð (eth)
		return 0x07 // -> d
	case 0x00D0: // Ð
		return 0x07
	}

	// Latvian/Lithuanian specific
	switch keysym {
	case 0x0100, 0x0101: // Ā/ā (A macron)
		return 0x04 // -> a
	case 0x010A, 0x010B: // Ċ/ċ (C dot above)
		return 0x06 // -> c
	case 0x0112, 0x0113: // Ē/ē (E macron)
		return 0x08 // -> e
	case 0x0116, 0x0117: // Ė/ė (E dot above)
		return 0x08 // -> e
	case 0x0122, 0x0123: // Ģ/ģ (G cedilla)
		return 0x0A // -> g
	case 0x012A, 0x012B: // Ī/ī (I macron)
		return 0x0C // -> i
	case 0x012E, 0x012F: // Į/į (I ogonek)
		return 0x0C // -> i
	case 0x0136, 0x0137: // Ķ/ķ (K cedilla)
		return 0x0E // -> k
	case 0x013B, 0x013C: // Ļ/ļ (L cedilla)
		return 0x0F // -> l
	case 0x0145, 0x0146: // Ņ/ņ (N cedilla)
		return 0x11 // -> n
	case 0x014C, 0x014D: // Ō/ō (O macron)
		return 0x12 // -> o
	case 0x0156, 0x0157: // Ŗ/ŗ (R cedilla)
		return 0x15 // -> r
	case 0x016A, 0x016B: // Ū/ū (U macron)
		return 0x18 // -> u
	case 0x0172, 0x0173: // Ų/ų (U ogonek)
		return 0x18 // -> u
	case 0x0174, 0x0175: // Ŵ/ŵ (W circumflex)
		return 0x1A // -> w
	case 0x0176, 0x0177: // Ŷ/ŷ (Y circumflex)
		return 0x1C // -> y
	}

	// Vietnamese diacritics (Latin Extended Additional: 0x1E00-0x1EFF)
	// Map to base letters - host OS handles composition
	switch {
	// A with various diacritics
	case keysym >= 0x1EA0 && keysym <= 0x1EB7:
		return 0x04 // -> a (ạ ả ấ ầ ẩ ẫ ậ ắ ằ ẳ ẵ ặ)
	// E with various diacritics
	case keysym >= 0x1EB8 && keysym <= 0x1EC7:
		return 0x08 // -> e (ẹ ẻ ẽ ế ề ể ễ ệ)
	// I with various diacritics
	case keysym >= 0x1EC8 && keysym <= 0x1ECB:
		return 0x0C // -> i (ỉ ị)
	// O with various diacritics
	case keysym >= 0x1ECC && keysym <= 0x1EE3:
		return 0x12 // -> o (ọ ỏ ố ồ ổ ỗ ộ ớ ờ ở ỡ ợ)
	// U with various diacritics
	case keysym >= 0x1EE4 && keysym <= 0x1EF1:
		return 0x18 // -> u (ụ ủ ứ ừ ử ữ ự)
	// Y with various diacritics
	case keysym >= 0x1EF2 && keysym <= 0x1EF9:
		return 0x1C // -> y (ỳ ỵ ỷ ỹ)
	}

	// Additional Latin-1 Supplement characters (0x00C0-0x00FF)
	// These are commonly sent by iOS keyboards
	switch keysym {
	case 0x00E1, 0x00C1: // á/Á (a acute)
		return 0x04 // -> a
	case 0x00ED, 0x00CD: // í/Í (i acute)
		return 0x0C // -> i
	case 0x00FA, 0x00DA: // ú/Ú (u acute)
		return 0x18 // -> u
	case 0x00FD, 0x00DD: // ý/Ý (y acute)
		return 0x1C // -> y
	}

	// Maltese specific
	switch keysym {
	case 0x0120, 0x0121: // Ġ/ġ (G dot above)
		return 0x0A // -> g
	case 0x0126, 0x0127: // Ħ/ħ (H stroke)
		return 0x0B // -> h
	}

	// Welsh specific
	switch keysym {
	case 0x1E80, 0x1E81: // Ẁ/ẁ (W grave)
		return 0x1A // -> w
	case 0x1E82, 0x1E83: // Ẃ/ẃ (W acute)
		return 0x1A // -> w
	case 0x1E84, 0x1E85: // Ẅ/ẅ (W diaeresis)
		return 0x1A // -> w
	case 0x1EF2, 0x1EF3: // Ỳ/ỳ (Y grave)
		return 0x1C // -> y
	}

	// Esperanto specific
	switch keysym {
	case 0x0108, 0x0109: // Ĉ/ĉ (C circumflex)
		return 0x06 // -> c
	case 0x011C, 0x011D: // Ĝ/ĝ (G circumflex)
		return 0x0A // -> g
	case 0x0124, 0x0125: // Ĥ/ĥ (H circumflex)
		return 0x0B // -> h
	case 0x0134, 0x0135: // Ĵ/ĵ (J circumflex)
		return 0x0D // -> j
	case 0x015C, 0x015D: // Ŝ/ŝ (S circumflex)
		return 0x16 // -> s
	case 0x016C, 0x016D: // Ŭ/ŭ (U breve)
		return 0x18 // -> u
	}

	// Catalan specific
	switch keysym {
	case 0x00B7: // · (middle dot/interpunct) - Catalan L·L
		return 0x37 // -> . (period)
	}

	// Pinyin/Chinese romanization diacritics
	switch keysym {
	case 0x01D5, 0x01D6: // Ǖ/ǖ (U diaeresis macron)
		return 0x18 // -> u
	case 0x01D7, 0x01D8: // Ǘ/ǘ (U diaeresis acute)
		return 0x18 // -> u
	case 0x01D9, 0x01DA: // Ǚ/ǚ (U diaeresis caron)
		return 0x18 // -> u
	case 0x01DB, 0x01DC: // Ǜ/ǜ (U diaeresis grave)
		return 0x18 // -> u
	}

	// Additional commonly typed diacritics from iOS
	// These cover characters that might be accessed via iOS keyboard long-press
	switch keysym {
	case 0x00AA: // ª (feminine ordinal)
		return 0x04 // -> a
	case 0x00BA: // º (masculine ordinal)
		return 0x12 // -> o
	}

	// Currency symbols
	switch keysym {
	case 0x00A3:
		return 0x20 // £ (pound) - UK Shift+3
	case 0x20AC:
		return 0x08 // € (euro) - AltGr+E on many layouts
	case 0x00A5:
		return 0x89 // ¥ (yen) - JIS International3 key
	case 0x00A2:
		return 0x06 // ¢ (cent) -> c key (AltGr+c on some layouts)
	}

	// Japanese keyboard specific (JIS layout)
	// HID International key codes: 0x87=Int1(Ro), 0x88=Int2(Kana), 0x89=Int3(Yen),
	//                              0x8A=Int4(Henkan), 0x8B=Int5(Muhenkan)
	switch keysym {
	case 0xFF22:
		return 0x8B // Muhenkan (無変換) -> International5
	case 0xFF23:
		return 0x8A // Henkan (変換) -> International4
	case 0xFF27:
		return 0x88 // Hiragana_Katakana -> International2
	case 0xFF24:
		return 0x88 // Romaji -> International2
	case 0xFF26:
		return 0x88 // Eisu_toggle -> International2
	case 0xFF2D:
		return 0x87 // Zenkaku_Hankaku -> International1 (Ro key position)
	}

	// Korean keyboard specific
	switch keysym {
	case 0xFF31:
		return 0x90 // Hangul
	case 0xFF34:
		return 0x91 // Hangul_Hanja
	}

	// Dead keys (for composing accented characters)
	switch keysym {
	case 0xFE50:
		return 0x35 // dead_grave
	case 0xFE51:
		return 0x34 // dead_acute
	case 0xFE52:
		return 0x23 // dead_circumflex -> 6 key
	case 0xFE53:
		return 0x35 // dead_tilde
	case 0xFE54:
		return 0x34 // dead_macron
	case 0xFE55:
		return 0x34 // dead_breve
	case 0xFE56:
		return 0x37 // dead_abovedot
	case 0xFE57:
		return 0x34 // dead_diaeresis (umlaut)
	case 0xFE58:
		return 0x34 // dead_abovering
	case 0xFE59:
		return 0x34 // dead_doubleacute
	case 0xFE5A:
		return 0x36 // dead_caron (háček)
	case 0xFE5B:
		return 0x36 // dead_cedilla
	case 0xFE5C:
		return 0x34 // dead_ogonek
	case 0xFE5D:
		return 0x34 // dead_iota
	case 0xFE5E:
		return 0x34 // dead_voiced_sound
	case 0xFE5F:
		return 0x34 // dead_semivoiced_sound
	case 0xFE60:
		return 0x34 // dead_belowdot
	case 0xFE61:
		return 0x34 // dead_hook
	case 0xFE62:
		return 0x34 // dead_horn
	case 0xFE63:
		return 0x34 // dead_stroke
	case 0xFE64:
		return 0x34 // dead_abovecomma
	case 0xFE65:
		return 0x34 // dead_abovereversedcomma
	case 0xFE66:
		return 0x34 // dead_doublegrave
	case 0xFE67:
		return 0x34 // dead_belowring
	case 0xFE68:
		return 0x34 // dead_belowmacron
	case 0xFE69:
		return 0x34 // dead_belowcircumflex
	case 0xFE6A:
		return 0x34 // dead_belowtilde
	case 0xFE6B:
		return 0x34 // dead_belowbreve
	case 0xFE6C:
		return 0x34 // dead_belowdiaeresis
	case 0xFE6D:
		return 0x34 // dead_invertedbreve
	case 0xFE6E:
		return 0x34 // dead_belowcomma
	case 0xFE6F:
		return 0x34 // dead_currency
	case 0xFE90:
		return 0x35 // dead_lowline
	case 0xFE91:
		return 0x34 // dead_aboveverticalline
	case 0xFE92:
		return 0x34 // dead_belowverticalline
	case 0xFE93:
		return 0x34 // dead_longsolidusoverlay
	}

	// Note: Media keys (XF86Audio*, XF86Browser*) are NOT supported because:
	// 1. They require USB HID Consumer Page (0x0C), not Keyboard Page (0x07)
	// 2. The USB gadget is configured as a standard keyboard only
	// 3. Using invalid codes could conflict with modifier keys (0xE0-0xE7)
	// Media key keysyms (0x1008FF*) intentionally return 0 (unsupported)

	// Power key - 0x66 is valid HID code for Power
	if keysym == 0xFF7E {
		return 0x66 // Power (Mac power key)
	}

	return 0 // Unknown keysym
}
