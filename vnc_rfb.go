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

	"github.com/jetkvm/kvm/internal/vnctls"
)

var bufferPool = sync.Pool{
	New: func() interface{} {
		// 256 bytes is sufficient for ServerInit message (~30 bytes + name)
		return bytes.NewBuffer(make([]byte, 0, 256))
	},
}

const (
	// RFB Protocol version 3.8 - required for VeNCrypt security type support
	rfbProtocolVersion = "RFB 003.008\n"

	secTypeNone     = 1
	secTypeVNCAuth  = 2
	secTypeVeNCrypt = 19

	// VeNCrypt subtypes
	veNCryptTLSNone   = 257 // TLS + No auth
	veNCryptTLSVnc    = 258 // TLS + VNC challenge-response auth
	veNCryptTLSPlain  = 259 // TLS + plaintext username/password
	veNCryptX509None  = 260 // X509 + No auth
	veNCryptX509Vnc   = 261 // X509 + VNC challenge-response auth
	veNCryptX509Plain = 262 // X509 + plaintext username/password

	msgSetPixelFormat           = 0
	msgSetEncodings             = 2
	msgFramebufferUpdateRequest = 3
	msgKeyEvent                 = 4
	msgPointerEvent             = 5
	msgClientCutText            = 6

	msgFramebufferUpdate = 0

	encodingTight = 7

	// Tight encoding compression control byte:
	// Bits 7-4: compression type (0x9 = JPEG compression)
	// Bits 3-0: stream ID (0 = reset all streams)
	tightJPEG = 0x90

	// Authentication failure delay to prevent brute-force attacks
	authFailureDelay = 2 * time.Second

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
	timeSyncNeeded := isTimeSyncNeeded()
	timeSyncSuccess := timeSync != nil && timeSync.IsSyncSuccess()

	switch config.TLSMode {
	case "self-signed":
		if timeSyncNeeded || !timeSyncSuccess {
			return false
		}
		return true
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
			vncLogger.Info().Err(err).Msg("failed to set TCP_NODELAY, latency may be higher")
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

	var secTypes []byte
	if tlsAvailable {
		// TLS enabled and available: offer VeNCrypt first, with VNCAuth fallback
		// Some clients (e.g., Jump Desktop) may not support our VeNCrypt cipher suites
		// VeNCrypt-capable clients will use TLS, others fall back to VNCAuth
		if hasPassword {
			secTypes = []byte{2, secTypeVeNCrypt, secTypeVNCAuth}
		} else {
			secTypes = []byte{2, secTypeVeNCrypt, secTypeNone}
		}
		vncLogger.Debug().Str("remote", c.conn.RemoteAddr().String()).
			Msg("VNC TLS available, offering VeNCrypt with VNCAuth fallback")
	} else if tlsEnabled {
		// TLS enabled but not yet available (e.g., time not synced, cert not ready)
		// Offer both VeNCrypt and fallback - VeNCrypt first so compliant clients try TLS
		vncLogger.Warn().Str("remote", c.conn.RemoteAddr().String()).
			Msg("VNC TLS enabled but not available (certificate/time issue) - offering both secure and insecure options")
		if hasPassword {
			secTypes = []byte{2, secTypeVeNCrypt, secTypeVNCAuth}
		} else {
			secTypes = []byte{2, secTypeVeNCrypt, secTypeNone}
		}
	} else {
		// TLS not enabled - use insecure modes with warning
		vncLogger.Warn().Str("remote", c.conn.RemoteAddr().String()).
			Msg("SECURITY WARNING: VNC connection without TLS encryption - enable TLS in settings")
		if hasPassword {
			secTypes = []byte{1, secTypeVNCAuth}
		} else {
			secTypes = []byte{1, secTypeNone}
		}
	}

	if _, err := c.conn.Write(secTypes); err != nil {
		return fmt.Errorf("failed to send security types: %w", err)
	}

	var secType [1]byte
	if _, err := io.ReadFull(c.conn, secType[:]); err != nil {
		return fmt.Errorf("failed to read security type: %w", err)
	}

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
	var subtypes []uint32
	if hasPassword {
		subtypes = []uint32{veNCryptX509Vnc, veNCryptX509Plain, veNCryptTLSVnc, veNCryptTLSPlain}
	} else {
		subtypes = []uint32{veNCryptX509None, veNCryptTLSNone}
	}

	subtypeBuf := make([]byte, 1+len(subtypes)*4)
	subtypeBuf[0] = byte(len(subtypes))
	for i, st := range subtypes {
		binary.BigEndian.PutUint32(subtypeBuf[1+i*4:], st)
	}

	if _, err := c.conn.Write(subtypeBuf); err != nil {
		return fmt.Errorf("failed to send VeNCrypt subtypes: %w", err)
	}

	var selectedBuf [4]byte
	if _, err := io.ReadFull(c.conn, selectedBuf[:]); err != nil {
		return fmt.Errorf("failed to read selected subtype: %w", err)
	}
	selectedSubtype := binary.BigEndian.Uint32(selectedBuf[:])

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

	// VeNCrypt subtype ACK: 1 = accepted (TigerVNC compatible)
	// Note: VeNCrypt spec is ambiguous, but TigerVNC uses 1 for success
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

	switch selectedSubtype {
	case veNCryptTLSVnc, veNCryptX509Vnc:
		if err := c.vncAuth(); err != nil {
			time.Sleep(authFailureDelay) // Delay to prevent brute-force
			_ = c.sendAuthResult(1, "Authentication failed")
			return err
		}
		if err := c.sendAuthResult(0, ""); err != nil {
			return err
		}
	case veNCryptTLSPlain, veNCryptX509Plain:
		if err := c.plainAuth(); err != nil {
			time.Sleep(authFailureDelay) // Delay to prevent brute-force
			_ = c.sendAuthResult(1, "Authentication failed")
			return err
		}
		if err := c.sendAuthResult(0, ""); err != nil {
			return err
		}
	case veNCryptTLSNone, veNCryptX509None:
		if err := c.sendAuthResult(0, ""); err != nil {
			return err
		}
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
		time.Sleep(authFailureDelay) // Delay to prevent brute-force
		_ = c.sendAuthResult(1, "Authentication failed")
		return err
	}
	return c.sendAuthResult(0, "")
}

func (c *VNCConnection) authenticateNone() error {
	return c.sendAuthResult(0, "")
}

func (c *VNCConnection) vncAuth() error {
	var challenge [16]byte
	if _, err := rand.Read(challenge[:]); err != nil {
		return fmt.Errorf("failed to generate challenge: %w", err)
	}

	if _, err := c.conn.Write(challenge[:]); err != nil {
		return fmt.Errorf("failed to send challenge: %w", err)
	}

	var response [16]byte
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

	response := make([]byte, 16)
	block.Encrypt(response[0:8], challenge[0:8])
	block.Encrypt(response[8:16], challenge[8:16])

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
			vncLogger.Debug().Err(err).Msg("failed to set read deadline")
		}

		if _, err := io.ReadFull(c.conn, c.msgBuf[:]); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("failed to read message type: %w", err)
		}

		switch c.msgBuf[0] {
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

	c.mu.Lock()
	c.pixelFormat = pf
	c.mu.Unlock()

	return nil
}

func (c *VNCConnection) handleSetEncodings() error {
	if _, err := io.ReadFull(c.conn, c.encHeaderBuf[:]); err != nil {
		return fmt.Errorf("failed to read encodings header: %w", err)
	}

	numEncodings := binary.BigEndian.Uint16(c.encHeaderBuf[1:3])

	foundTight := false
	for i := uint16(0); i < numEncodings; i++ {
		if _, err := io.ReadFull(c.conn, c.encBuf[:]); err != nil {
			return fmt.Errorf("failed to read encoding: %w", err)
		}
		enc := int32(binary.BigEndian.Uint32(c.encBuf[:]))
		if enc == encodingTight {
			foundTight = true
		}
	}

	// Update hasTight atomically
	c.hasTight.Store(foundTight)
	previouslyNeededJPEG := c.needsJPEGEncoder.Swap(foundTight)

	if foundTight && !previouslyNeededJPEG {
		if err := c.server.requestJPEGEncoder(); err != nil {
			// JPEG encoder failed to start - client won't get video
			// Log but don't disconnect - they may still be able to use keyboard/mouse
			vncLogger.Warn().Err(err).Str("remote", c.conn.RemoteAddr().String()).
				Msg("JPEG encoder failed to start, video may be unavailable")
			c.needsJPEGEncoder.Store(false)
		}
	} else if !foundTight && previouslyNeededJPEG {
		c.server.releaseJPEGEncoder()
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
		text := make([]byte, length)
		if _, err := io.ReadFull(c.conn, text); err != nil {
			return fmt.Errorf("failed to read cut text: %w", err)
		}
	}

	return nil
}

func (c *VNCConnection) sendFrameUpdate(jpegData []byte) error {
	// Lock-free checks on hot path
	if !c.hasTight.Load() {
		return nil
	}

	// Validate JPEG data (must start with SOI marker 0xFFD8)
	if len(jpegData) < 2 || jpegData[0] != 0xFF || jpegData[1] != 0xD8 {
		vncLogger.Debug().Int("len", len(jpegData)).Msg("invalid JPEG data, skipping frame")
		return nil
	}

	width, height := c.getResolution()

	// Build header in pre-allocated buffer (zero allocations)
	// Format: 16 bytes RFB header + 1 byte tight ctrl + 1-3 bytes length
	header := &c.frameHeaderBuf
	header[0] = msgFramebufferUpdate
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
	header[15] = encodingTight // encoding type low byte
	header[16] = tightJPEG     // Tight JPEG compression control

	// Tight compact length encoding (inline for zero overhead)
	jpegLen := len(jpegData)
	var headerLen int
	if jpegLen < 128 {
		header[17] = byte(jpegLen)
		headerLen = 18
	} else if jpegLen < 16384 {
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
	bufs := net.Buffers{header[:headerLen], jpegData}

	if err := c.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		vncLogger.Debug().Err(err).Msg("failed to set write deadline")
	}
	_, err := bufs.WriteTo(c.conn)
	if clearErr := c.conn.SetWriteDeadline(time.Time{}); clearErr != nil {
		vncLogger.Debug().Err(clearErr).Msg("failed to clear write deadline")
	}

	if err != nil {
		return fmt.Errorf("failed to send frame update: %w", err)
	}

	return nil
}

func (c *VNCConnection) handleVNCKey(keysym uint32, down bool) {
	hidKey := keysymToHID(keysym)
	if hidKey == 0 {
		vncLogger.Debug().Uint32("keysym", keysym).Bool("down", down).Msg("VNC key event: unknown keysym, ignoring")
		return
	}

	if err := rpcKeypressReport(hidKey, down); err != nil {
		vncLogger.Warn().Err(err).Uint32("keysym", keysym).Msg("failed to send key event")
	}
}

func (c *VNCConnection) handleVNCPointer(x, y uint16, buttonMask byte) {
	width, height := c.getResolution()

	if width == 0 || height == 0 {
		vncLogger.Debug().Uint16("width", width).Uint16("height", height).Msg("VNC pointer: invalid resolution, ignoring")
		return
	}

	// Scale VNC coordinates to absolute HID coordinates (0-32767)
	absX := int(x) * 32767 / int(width)
	absY := int(y) * 32767 / int(height)

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
			vncLogger.Trace().Err(err).Msg("failed to send scroll event")
		}
	}
}

// keysymToHID converts X11 keysym codes (used by VNC/RFB protocol) to USB HID usage codes.
// Keysyms are defined in X11/keysymdef.h. HID codes follow USB HID Usage Tables specification.
// Returns 0 for unsupported keysyms.
func keysymToHID(keysym uint32) uint8 {
	switch keysym {
	case 0xFFE1:
		return 0xE1 // Left Shift
	case 0xFFE2:
		return 0xE5 // Right Shift
	case 0xFFE3:
		return 0xE0 // Left Control
	case 0xFFE4:
		return 0xE4 // Right Control
	case 0xFFE9:
		return 0xE2 // Left Alt
	case 0xFFEA:
		return 0xE6 // Right Alt
	case 0xFFEB:
		return 0xE3 // Left Super/Meta
	case 0xFFEC:
		return 0xE7 // Right Super/Meta
	}

	// Function keys F1-F12
	if keysym >= 0xFFBE && keysym <= 0xFFC9 {
		return uint8(0x3A + (keysym - 0xFFBE))
	}

	// Navigation and special keys
	switch keysym {
	case 0xFF08:
		return 0x2A // Backspace
	case 0xFF09:
		return 0x2B // Tab
	case 0xFF0D:
		return 0x28 // Enter
	case 0xFF1B:
		return 0x29 // Escape
	case 0xFFFF:
		return 0x4C // Delete
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
	case 0xFF63:
		return 0x49 // Insert
	case 0xFF14:
		return 0x47 // Scroll Lock
	case 0xFF7F:
		return 0x53 // Num Lock
	case 0xFF13:
		return 0x48 // Pause
	case 0xFF61:
		return 0x46 // Print Screen
	case 0xFF20:
		return 0x65 // Menu/Application
	case 0xFFE5:
		return 0x39 // Caps Lock
	case 0x0020:
		return 0x2C // Space
	}

	// Lowercase letters a-z
	if keysym >= 0x0061 && keysym <= 0x007A {
		return uint8(0x04 + (keysym - 0x0061))
	}

	// Uppercase letters A-Z (same HID codes as lowercase)
	if keysym >= 0x0041 && keysym <= 0x005A {
		return uint8(0x04 + (keysym - 0x0041))
	}

	// Numbers 0-9
	if keysym >= 0x0030 && keysym <= 0x0039 {
		if keysym == 0x0030 {
			return 0x27 // 0
		}
		return uint8(0x1E + (keysym - 0x0031)) // 1-9
	}

	// Punctuation and symbols (unshifted)
	switch keysym {
	case 0x002D:
		return 0x2D // -
	case 0x003D:
		return 0x2E // =
	case 0x005B:
		return 0x2F // [
	case 0x005D:
		return 0x30 // ]
	case 0x005C:
		return 0x31 // backslash
	case 0x003B:
		return 0x33 // ;
	case 0x0027:
		return 0x34 // '
	case 0x0060:
		return 0x35 // `
	case 0x002C:
		return 0x36 // ,
	case 0x002E:
		return 0x37 // .
	case 0x002F:
		return 0x38 // /
	}

	// Shifted symbols - map to base key (shift state handled separately)
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
		return 0x31 // | -> backslash
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
	case 0xFF8D:
		return 0x58 // KP_Enter
	}

	return 0 // Unknown keysym
}
