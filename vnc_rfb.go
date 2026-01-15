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

	"github.com/jetkvm/kvm/internal/native"
	"github.com/jetkvm/kvm/internal/vnctls"
)

var bufferPool = sync.Pool{
	New: func() interface{} {
		return bytes.NewBuffer(make([]byte, 0, 4096))
	},
}

const (
	rfbProtocolVersion = "RFB 003.008\n"

	secTypeNone     = 1
	secTypeVNCAuth  = 2
	secTypeVeNCrypt = 19

	veNCryptTLSNone   = 257
	veNCryptTLSVnc    = 258
	veNCryptTLSPlain  = 259
	veNCryptX509None  = 260
	veNCryptX509Vnc   = 261
	veNCryptX509Plain = 262

	msgSetPixelFormat           = 0
	msgSetEncodings             = 2
	msgFramebufferUpdateRequest = 3
	msgKeyEvent                 = 4
	msgPointerEvent             = 5
	msgClientCutText            = 6

	msgFramebufferUpdate = 0

	encodingTight = 7
	encodingH264  = 50

	tightJPEG = 0x90

	h264FlagResetContext = 0x01
)

func (c *VNCConnection) handshake() error {
	_ = c.conn.SetDeadline(time.Now().Add(30 * time.Second))
	defer func() { _ = c.conn.SetDeadline(time.Time{}) }()

	if _, err := c.conn.Write([]byte(rfbProtocolVersion)); err != nil {
		return fmt.Errorf("failed to write protocol version: %w", err)
	}

	versionBuf := make([]byte, 12)
	if _, err := io.ReadFull(c.conn, versionBuf); err != nil {
		return fmt.Errorf("failed to read client protocol version: %w", err)
	}

	if !bytes.HasPrefix(versionBuf, []byte("RFB 003.00")) {
		vncLogger.Warn().Str("version", string(versionBuf)).Msg("unknown RFB version")
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
	_ = c.conn.SetDeadline(time.Now().Add(30 * time.Second))
	defer func() { _ = c.conn.SetDeadline(time.Time{}) }()

	if tcpConn, ok := c.conn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
	}

	// Check for raw password - VNC auth requires the actual password, not just a hash
	// VNC uses DES encryption with the raw password as key
	hasPassword := config.LocalAuthPassword != "" || config.VNCPassword != ""

	// VeNCrypt is not widely supported by iOS clients (Jump Desktop, etc.)
	// Only enable it when explicitly configured AND TLS is properly available
	tlsEnabled := c.server.tlsEnabled && config.TLSMode != "" && config.TLSMode != "disabled"
	tlsAvailable := tlsEnabled && isTLSAvailable()

	var secTypes []byte
	if tlsAvailable {
		if hasPassword {
			secTypes = []byte{2, secTypeVNCAuth, secTypeVeNCrypt}
		} else {
			secTypes = []byte{2, secTypeNone, secTypeVeNCrypt}
		}
	} else {
		if hasPassword {
			secTypes = []byte{1, secTypeVNCAuth}
		} else {
			secTypes = []byte{1, secTypeNone}
		}
	}

	if _, err := c.conn.Write(secTypes); err != nil {
		return fmt.Errorf("failed to send security types: %w", err)
	}

	secType := make([]byte, 1)
	if _, err := io.ReadFull(c.conn, secType); err != nil {
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

	clientVersion := make([]byte, 2)
	if _, err := io.ReadFull(c.conn, clientVersion); err != nil {
		return fmt.Errorf("failed to read client VeNCrypt version: %w", err)
	}

	if clientVersion[0] != 0 || clientVersion[1] < 2 {
		_, _ = c.conn.Write([]byte{1})
		return fmt.Errorf("unsupported VeNCrypt version: %d.%d", clientVersion[0], clientVersion[1])
	}

	if _, err := c.conn.Write([]byte{0}); err != nil {
		return fmt.Errorf("failed to send version acceptance: %w", err)
	}

	var subtypes []uint32
	if hasPassword {
		subtypes = []uint32{veNCryptTLSVnc, veNCryptX509Vnc, veNCryptTLSPlain, veNCryptX509Plain}
	} else {
		subtypes = []uint32{veNCryptTLSNone, veNCryptX509None}
	}

	subtypeBuf := make([]byte, 1+len(subtypes)*4)
	subtypeBuf[0] = byte(len(subtypes))
	for i, st := range subtypes {
		binary.BigEndian.PutUint32(subtypeBuf[1+i*4:], st)
	}

	if _, err := c.conn.Write(subtypeBuf); err != nil {
		return fmt.Errorf("failed to send VeNCrypt subtypes: %w", err)
	}

	selectedBuf := make([]byte, 4)
	if _, err := io.ReadFull(c.conn, selectedBuf); err != nil {
		return fmt.Errorf("failed to read selected subtype: %w", err)
	}
	selectedSubtype := binary.BigEndian.Uint32(selectedBuf)

	validSubtype := false
	for _, st := range subtypes {
		if st == selectedSubtype {
			validSubtype = true
			break
		}
	}
	if !validSubtype {
		_, _ = c.conn.Write([]byte{1})
		return fmt.Errorf("client selected unsupported subtype: %d", selectedSubtype)
	}

	// TigerVNC expects 1 for success
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
			_ = c.sendAuthResult(1, "Authentication failed")
			return err
		}
		if err := c.sendAuthResult(0, ""); err != nil {
			return err
		}
	case veNCryptTLSPlain, veNCryptX509Plain:
		if err := c.plainAuth(); err != nil {
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

	c.authenticated = true
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
		Str("remote", c.conn.RemoteAddr().String()).
		Msg("TLS handshake complete (anonymous DH)")

	return nil
}

func (c *VNCConnection) upgradeTLSX509() error {
	tlsConfig := &tls.Config{
		GetCertificate: getCertificate,
		MinVersion:     tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		},
	}

	tlsConn := tls.Server(c.conn, tlsConfig)

	if err := tlsConn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return fmt.Errorf("failed to set TLS deadline: %w", err)
	}

	if err := tlsConn.Handshake(); err != nil {
		return fmt.Errorf("TLS handshake failed: %w", err)
	}

	_ = tlsConn.SetDeadline(time.Time{})
	c.conn = tlsConn

	state := tlsConn.ConnectionState()
	vncLogger.Info().
		Str("version", tlsVersionString(state.Version)).
		Str("cipherSuite", tls.CipherSuiteName(state.CipherSuite)).
		Str("remote", c.conn.RemoteAddr().String()).
		Msg("TLS handshake complete (X509)")

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
		_ = c.sendAuthResult(1, "Authentication failed")
		return err
	}

	if err := c.sendAuthResult(0, ""); err != nil {
		return err
	}

	c.authenticated = true
	return nil
}

func (c *VNCConnection) authenticateNone() error {
	if err := c.sendAuthResult(0, ""); err != nil {
		return err
	}

	c.authenticated = true
	return nil
}

func (c *VNCConnection) vncAuth() error {
	challenge := make([]byte, 16)
	if _, err := rand.Read(challenge); err != nil {
		return fmt.Errorf("failed to generate challenge: %w", err)
	}

	if _, err := c.conn.Write(challenge); err != nil {
		return fmt.Errorf("failed to send challenge: %w", err)
	}

	response := make([]byte, 16)
	if _, err := io.ReadFull(c.conn, response); err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	password := getVNCPassword()
	if password == "" {
		return fmt.Errorf("no VNC password configured")
	}

	expected, err := computeVNCResponse(challenge, password)
	if err != nil {
		return fmt.Errorf("failed to compute response: %w", err)
	}

	if subtle.ConstantTimeCompare(response, expected) != 1 {
		return fmt.Errorf("authentication failed")
	}

	return nil
}

func (c *VNCConnection) plainAuth() error {
	lenBuf := make([]byte, 8)
	if _, err := io.ReadFull(c.conn, lenBuf); err != nil {
		return fmt.Errorf("failed to read auth lengths: %w", err)
	}

	usernameLen := binary.BigEndian.Uint32(lenBuf[0:4])
	passwordLen := binary.BigEndian.Uint32(lenBuf[4:8])

	if usernameLen > 256 || passwordLen > 256 {
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

func getVNCPassword() string {
	if config.LocalAuthPassword != "" {
		return config.LocalAuthPassword
	}
	return config.VNCPassword
}

func (c *VNCConnection) sendAuthResult(status uint32, reason string) error {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, status)
	if _, err := c.conn.Write(buf); err != nil {
		return fmt.Errorf("failed to send auth result: %w", err)
	}

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

func (c *VNCConnection) clientInit() error {
	sharedBuf := make([]byte, 1)
	if _, err := io.ReadFull(c.conn, sharedBuf); err != nil {
		return fmt.Errorf("failed to read shared flag: %w", err)
	}
	return nil
}

func (c *VNCConnection) serverInit() error {
	c.mu.Lock()
	width := c.width
	height := c.height
	c.mu.Unlock()

	buf := new(bytes.Buffer)

	_ = binary.Write(buf, binary.BigEndian, width)
	_ = binary.Write(buf, binary.BigEndian, height)

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
	_, _ = buf.Write([]byte{0, 0, 0})

	name := "JetKVM"
	_ = binary.Write(buf, binary.BigEndian, uint32(len(name)))
	_, _ = buf.WriteString(name)

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

		_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))

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
		}
	}
}

func (c *VNCConnection) handleSetPixelFormat() error {
	if _, err := io.ReadFull(c.conn, c.pixelFmtBuf[:]); err != nil {
		return fmt.Errorf("failed to read pixel format: %w", err)
	}

	c.mu.Lock()
	c.pixelFormat = PixelFormat{
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
	c.mu.Unlock()

	return nil
}

func (c *VNCConnection) handleSetEncodings() error {
	if _, err := io.ReadFull(c.conn, c.encHeaderBuf[:]); err != nil {
		return fmt.Errorf("failed to read encodings header: %w", err)
	}

	numEncodings := binary.BigEndian.Uint16(c.encHeaderBuf[1:3])

	encodings := make([]int32, numEncodings)
	for i := uint16(0); i < numEncodings; i++ {
		if _, err := io.ReadFull(c.conn, c.encBuf[:]); err != nil {
			return fmt.Errorf("failed to read encoding: %w", err)
		}
		encodings[i] = int32(binary.BigEndian.Uint32(c.encBuf[:]))
	}

	c.mu.Lock()
	c.encodings = encodings

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

	needsJPEG := !c.hasH264 && c.hasTight
	previouslyNeededJPEG := c.needsJPEGEncoder.Swap(needsJPEG)
	c.mu.Unlock()

	if needsJPEG && !previouslyNeededJPEG {
		c.server.requestJPEGEncoder()
	} else if !needsJPEG && previouslyNeededJPEG {
		c.server.releaseJPEGEncoder()
	}

	if c.hasH264 && c.h264Unsubscribe == nil {
		c.h264Unsubscribe = native.SubscribeH264Frames(c.h264FrameChan)
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

	c.handleVNCPointer(x, y, buttonMask)

	return nil
}

func (c *VNCConnection) handleClientCutText() error {
	header := make([]byte, 7)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return fmt.Errorf("failed to read cut text header: %w", err)
	}

	length := binary.BigEndian.Uint32(header[3:7])

	// Limit to 1MB to prevent OOM on resource-limited device
	const maxCutTextLen = 1024 * 1024
	if length > maxCutTextLen {
		return fmt.Errorf("cut text too large: %d bytes", length)
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
	c.mu.Lock()
	width := c.width
	height := c.height
	hasTight := c.hasTight
	c.mu.Unlock()

	if !hasTight {
		return nil
	}

	if len(jpegData) < 2 || jpegData[0] != 0xFF || jpegData[1] != 0xD8 {
		return nil
	}

	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufferPool.Put(buf)

	header := [16]byte{
		msgFramebufferUpdate, 0,
		0, 1,
		0, 0, 0, 0,
		byte(width >> 8), byte(width),
		byte(height >> 8), byte(height),
		0, 0, 0, encodingTight,
	}
	_, _ = buf.Write(header[:])

	_ = buf.WriteByte(tightJPEG)
	c.writeTightLength(buf, len(jpegData))
	_, _ = buf.Write(jpegData)

	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := c.conn.Write(buf.Bytes())
	_ = c.conn.SetWriteDeadline(time.Time{})

	if err != nil {
		return fmt.Errorf("failed to send frame update: %w", err)
	}

	return nil
}

func (c *VNCConnection) writeTightLength(buf *bytes.Buffer, length int) {
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

func (c *VNCConnection) sendH264FrameUpdate(h264Data []byte, isKeyframe bool) error {
	c.mu.Lock()
	width := c.width
	height := c.height
	hasH264 := c.hasH264
	h264ContextInitialized := c.h264ContextInitialized
	c.mu.Unlock()

	if !hasH264 {
		return nil
	}

	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufferPool.Put(buf)

	dataLen := uint32(len(h264Data))
	var flags uint32
	if isKeyframe && !h264ContextInitialized {
		flags = h264FlagResetContext
		c.mu.Lock()
		c.h264ContextInitialized = true
		c.mu.Unlock()
	}

	header := [24]byte{
		msgFramebufferUpdate, 0,
		0, 1,
		0, 0, 0, 0,
		byte(width >> 8), byte(width),
		byte(height >> 8), byte(height),
		0, 0, 0, encodingH264,
		byte(dataLen >> 24), byte(dataLen >> 16), byte(dataLen >> 8), byte(dataLen),
		byte(flags >> 24), byte(flags >> 16), byte(flags >> 8), byte(flags),
	}
	_, _ = buf.Write(header[:])
	_, _ = buf.Write(h264Data)

	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := c.conn.Write(buf.Bytes())
	_ = c.conn.SetWriteDeadline(time.Time{})

	if err != nil {
		return fmt.Errorf("failed to send H.264 frame update: %w", err)
	}

	return nil
}

func (c *VNCConnection) handleVNCKey(keysym uint32, down bool) {
	hidKey, modifiers := keysymToHID(keysym)
	if hidKey == 0 && modifiers == 0 {
		return
	}

	if err := rpcKeypressReport(hidKey, down); err != nil {
		vncLogger.Debug().Err(err).Uint32("keysym", keysym).Msg("failed to send key event")
	}

	if modifiers != 0 {
		var keys []byte
		if down && hidKey != 0 {
			keys = []byte{hidKey}
		}
		if err := rpcKeyboardReport(modifiers, keys); err != nil {
			vncLogger.Debug().Err(err).Msg("failed to send modifier event")
		}
	}
}

func (c *VNCConnection) handleVNCPointer(x, y uint16, buttonMask byte) {
	c.mu.Lock()
	width := c.width
	height := c.height
	c.mu.Unlock()

	if width > 0 && height > 0 {
		absX := int(x) * 32767 / int(width)
		absY := int(y) * 32767 / int(height)

		vncLeft := (buttonMask & 0x01)
		vncMiddle := (buttonMask & 0x02) >> 1
		vncRight := (buttonMask & 0x04) >> 1
		buttons := vncLeft | vncRight | (vncMiddle << 2)

		if err := rpcAbsMouseReport(absX, absY, buttons); err != nil {
			vncLogger.Debug().Err(err).Msg("failed to send mouse event")
		}
	}
}

func keysymToHID(keysym uint32) (uint8, uint8) {
	switch keysym {
	case 0xFFE1, 0xFFE2:
		return 0, 0x02
	case 0xFFE3, 0xFFE4:
		return 0, 0x01
	case 0xFFE9, 0xFFEA:
		return 0, 0x04
	case 0xFFEB, 0xFFEC:
		return 0, 0x08
	}

	if keysym >= 0xFFBE && keysym <= 0xFFC9 {
		return uint8(0x3A + (keysym - 0xFFBE)), 0
	}

	switch keysym {
	case 0xFF08:
		return 0x2A, 0
	case 0xFF09:
		return 0x2B, 0
	case 0xFF0D:
		return 0x28, 0
	case 0xFF1B:
		return 0x29, 0
	case 0xFFFF:
		return 0x4C, 0
	case 0xFF50:
		return 0x4A, 0
	case 0xFF51:
		return 0x50, 0
	case 0xFF52:
		return 0x52, 0
	case 0xFF53:
		return 0x4F, 0
	case 0xFF54:
		return 0x51, 0
	case 0xFF55:
		return 0x4B, 0
	case 0xFF56:
		return 0x4E, 0
	case 0xFF57:
		return 0x4D, 0
	case 0xFF63:
		return 0x49, 0
	case 0x0020:
		return 0x2C, 0
	}

	if keysym >= 0x0061 && keysym <= 0x007A {
		return uint8(0x04 + (keysym - 0x0061)), 0
	}

	if keysym >= 0x0041 && keysym <= 0x005A {
		return uint8(0x04 + (keysym - 0x0041)), 0x02
	}

	if keysym >= 0x0030 && keysym <= 0x0039 {
		if keysym == 0x0030 {
			return 0x27, 0
		}
		return uint8(0x1E + (keysym - 0x0031)), 0
	}

	switch keysym {
	case 0x002D:
		return 0x2D, 0
	case 0x003D:
		return 0x2E, 0
	case 0x005B:
		return 0x2F, 0
	case 0x005D:
		return 0x30, 0
	case 0x005C:
		return 0x31, 0
	case 0x003B:
		return 0x33, 0
	case 0x0027:
		return 0x34, 0
	case 0x0060:
		return 0x35, 0
	case 0x002C:
		return 0x36, 0
	case 0x002E:
		return 0x37, 0
	case 0x002F:
		return 0x38, 0
	}

	return 0, 0
}
