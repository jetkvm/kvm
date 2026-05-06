package kvm

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/jetkvm/kvm/internal/rfb"
	"github.com/jetkvm/kvm/internal/sync"
)

// vncServer is the global VNC server instance, or nil when the
// server is disabled. Accessed from native.go's frame callback.
var (
	vncServer   *VNCServer
	vncServerMu sync.Mutex
)

// VNCConfig is the JSON-RPC view of the VNC configuration.
type VNCConfig struct {
	Enabled  bool   `json:"enabled"`
	Port     int    `json:"port"`
	Password string `json:"password"`
}

// rpcGetVNCConfig returns the current VNC server configuration. The
// password field is masked so the plaintext never round-trips back
// to the frontend.
func rpcGetVNCConfig() VNCConfig {
	masked := ""
	if config.VncPassword != "" {
		masked = "********"
	}
	return VNCConfig{
		Enabled:  config.VncEnabled,
		Port:     config.VncPort,
		Password: masked,
	}
}

// rpcSetVNCConfig persists the supplied configuration and restarts
// the listener. A password value of "********" is treated as "leave
// unchanged".
func rpcSetVNCConfig(cfg VNCConfig) error {
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return errors.New("vnc port must be in 1..65535")
	}

	prev := struct {
		enabled  bool
		port     int
		password string
	}{
		enabled: config.VncEnabled, port: config.VncPort,
		password: config.VncPassword,
	}

	config.VncEnabled = cfg.Enabled
	config.VncPort = cfg.Port
	if cfg.Password != "********" {
		config.VncPassword = cfg.Password
	}

	if err := SaveConfig(); err != nil {
		config.VncEnabled, config.VncPort = prev.enabled, prev.port
		config.VncPassword = prev.password
		return fmt.Errorf("failed to save vnc config: %w", err)
	}

	StopVNCServer()
	if config.VncEnabled {
		if err := StartVNCServer(); err != nil {
			vncLogger.Error().Err(err).Msg("failed to start VNC server after config change")
			return err
		}
	}
	return nil
}

// StartVNCServer starts the VNC TCP listener if VNC is enabled.
// Idempotent: a second call while running is a no-op.
func StartVNCServer() error {
	vncServerMu.Lock()
	defer vncServerMu.Unlock()

	if vncServer != nil {
		return nil
	}
	if !config.VncEnabled {
		vncLogger.Debug().Msg("VNC server disabled in config")
		return nil
	}

	addr, err := vncBindAddress(config.VncPort)
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("vnc: listen %s: %w", addr, err)
	}

	srv := &VNCServer{
		ln:      ln,
		clients: make(map[*vncConn]struct{}),
		quit:    make(chan struct{}),
	}
	vncServer = srv
	vncLogger.Info().Str("address", addr).Msg("VNC server listening")

	go srv.acceptLoop()
	return nil
}

// StopVNCServer shuts down the VNC server if it is running.
func StopVNCServer() {
	vncServerMu.Lock()
	srv := vncServer
	vncServer = nil
	vncServerMu.Unlock()

	if srv == nil {
		return
	}
	vncLogger.Info().Msg("stopping VNC server")
	close(srv.quit)
	_ = srv.ln.Close()
	srv.clientsMu.Lock()
	for c := range srv.clients {
		_ = c.conn.Close()
	}
	srv.clientsMu.Unlock()
}

// vncBindAddress returns the address to bind on for VNC. Honours
// LocalLoopbackOnly and the IPv4/IPv6 mode flags from NetworkConfig.
//
// VNCAuth is insecure over plain TCP — operators on untrusted
// networks should tunnel via SSH or Tailscale, or front the device
// with LocalLoopbackOnly.
func vncBindAddress(port int) (string, error) {
	useIPv4 := config.NetworkConfig != nil && config.NetworkConfig.IPv4Mode.String != "disabled"
	useIPv6 := config.NetworkConfig != nil && config.NetworkConfig.IPv6Mode.String != "disabled"

	if config.LocalLoopbackOnly {
		switch {
		case useIPv4 && useIPv6:
			return fmt.Sprintf("localhost:%d", port), nil
		case useIPv4:
			return fmt.Sprintf("127.0.0.1:%d", port), nil
		case useIPv6:
			return fmt.Sprintf("[::1]:%d", port), nil
		}
		return "", errors.New("vnc: no IP family enabled")
	}

	switch {
	case useIPv4 && useIPv6:
		return fmt.Sprintf(":%d", port), nil
	case useIPv4:
		return fmt.Sprintf("0.0.0.0:%d", port), nil
	case useIPv6:
		return fmt.Sprintf("[::]:%d", port), nil
	}
	return "", errors.New("vnc: no IP family enabled")
}

// VNCServer holds runtime state for the listener.
type VNCServer struct {
	ln   net.Listener
	quit chan struct{}

	clientsMu sync.RWMutex
	clients   map[*vncConn]struct{}

	// Cached H.264 parameter sets, updated as frames arrive. Sent to
	// each newly-connected client as the first chunk of their first
	// encoding-50 rectangle, so they can decode the next IDR rather
	// than waiting an extra GOP for SPS/PPS to repeat.
	paramsMu sync.Mutex
	sps      []byte
	pps      []byte
}

// acquireConsumerName returns the name used by this VNC server to
// register/release the video stream refcount.
const vncVideoConsumer = "vnc"

func (s *VNCServer) acceptLoop() {
	for {
		nc, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return
			default:
			}
			vncLogger.Warn().Err(err).Msg("VNC accept failed")
			return
		}
		go s.serveClient(nc)
	}
}

// addClient registers a new VNC client, acquiring the video stream
// on the first one. Returns a snapshot of the cached SPS/PPS to seed
// the client's first rectangle.
func (s *VNCServer) addClient(c *vncConn) (sps, pps []byte) {
	s.clientsMu.Lock()
	first := len(s.clients) == 0
	s.clients[c] = struct{}{}
	s.clientsMu.Unlock()
	if first {
		acquireVideoStream(vncVideoConsumer)
	}
	s.paramsMu.Lock()
	defer s.paramsMu.Unlock()
	return append([]byte(nil), s.sps...), append([]byte(nil), s.pps...)
}

// removeClient is the symmetric release.
func (s *VNCServer) removeClient(c *vncConn) {
	s.clientsMu.Lock()
	delete(s.clients, c)
	last := len(s.clients) == 0
	s.clientsMu.Unlock()
	if last {
		releaseVideoStream(vncVideoConsumer)
	}
}

// WriteFrame is the producer side of the frame fan-out. Called from
// the native frame callback for every encoded frame. It updates the
// SPS/PPS cache and pushes the frame to every connected client's
// bounded channel using a non-blocking send (slow clients drop
// their own frames; the producer never blocks).
func (s *VNCServer) WriteFrame(frame []byte, duration time.Duration) {
	if s == nil || len(frame) == 0 {
		return
	}

	nals := splitAnnexB(frame)
	s.cacheParameterSets(nals)

	hasIDR := false
	for _, nal := range nals {
		if len(nal) > 0 && nal[0]&0x1F == 5 {
			hasIDR = true
			break
		}
	}

	// Copy once at fan-out time so receivers can consume after the
	// underlying inboundPacket buffer in internal/native/proxy.go
	// has been overwritten by the next frame.
	pkt := vncFramePacket{
		data:     append([]byte(nil), frame...),
		duration: duration,
		hasIDR:   hasIDR,
	}

	s.clientsMu.RLock()
	for c := range s.clients {
		select {
		case c.frames <- pkt:
		default:
			c.dropped.Add(1)
		}
	}
	s.clientsMu.RUnlock()
}

func (s *VNCServer) cacheParameterSets(nals [][]byte) {
	var newSPS, newPPS []byte
	for _, nal := range nals {
		if len(nal) == 0 {
			continue
		}
		switch nal[0] & 0x1F {
		case 7: // SPS
			newSPS = append([]byte(nil), nal...)
		case 8: // PPS
			newPPS = append([]byte(nil), nal...)
		}
	}
	if newSPS == nil && newPPS == nil {
		return
	}
	s.paramsMu.Lock()
	if newSPS != nil {
		s.sps = newSPS
	}
	if newPPS != nil {
		s.pps = newPPS
	}
	s.paramsMu.Unlock()
}

// notifyResolutionChange tells every connected client that the
// framebuffer dimensions have changed; the dispatcher will emit a
// DesktopSize update + reset-decoder bit on the next outgoing rect.
func (s *VNCServer) notifyResolutionChange(width, height uint16) {
	if s == nil {
		return
	}
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	for c := range s.clients {
		c.markResolutionChanged(width, height)
	}
}

// vncFramePacket is a single frame queued for one client. hasIDR is
// true if any NAL in the frame is an IDR slice (NAL type 5);
// dispatchers waiting for the first decodable frame use this to know
// when they can start emitting.
type vncFramePacket struct {
	data     []byte
	duration time.Duration
	hasIDR   bool
}

// startConn is the entry point for a single client connection. Runs
// the protocol handshake, registers the client, and dispatches.
func (s *VNCServer) serveClient(nc net.Conn) {
	defer func() { _ = nc.Close() }()
	addr := nc.RemoteAddr().String()
	l := vncLogger.With().Str("client", addr).Logger()

	rfbConn := rfb.NewConn(nc)
	if err := nc.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		l.Warn().Err(err).Msg("set handshake deadline")
		return
	}

	if _, err := rfbConn.HandshakeServerVersion(); err != nil {
		l.Warn().Err(err).Msg("version handshake failed")
		return
	}

	if err := s.negotiateSecurity(rfbConn); err != nil {
		l.Warn().Err(err).Msg("security negotiation failed")
		return
	}

	if _, err := rfbConn.ReadClientInit(); err != nil {
		l.Warn().Err(err).Msg("ReadClientInit")
		return
	}

	width, height := vncFramebufferSize()
	if err := rfbConn.SendServerInit(rfb.ServerInit{
		Width:       width,
		Height:      height,
		PixelFormat: rfb.DefaultPixelFormat(),
		Name:        "JetKVM",
	}); err != nil {
		l.Warn().Err(err).Msg("SendServerInit")
		return
	}

	// No further hard deadlines — the client can sit idle on a
	// FramebufferUpdateRequest indefinitely.
	if err := nc.SetDeadline(time.Time{}); err != nil {
		l.Warn().Err(err).Msg("clear deadline")
		return
	}

	conn := &vncConn{
		server:        s,
		conn:          rfbConn,
		netConn:       nc,
		l:             &l,
		frames:        make(chan vncFramePacket, 2),
		updateNeeded:  make(chan struct{}, 1),
		writeQuit:     make(chan struct{}),
		width:         width,
		height:        height,
		needsResetCtx: true,
		waitingForIDR: true,
	}
	cachedSPS, cachedPPS := s.addClient(conn)
	conn.cachedSPS, conn.cachedPPS = cachedSPS, cachedPPS
	defer s.removeClient(conn)

	go conn.dispatchLoop()
	conn.readLoop()
	close(conn.writeQuit)
}

// negotiateSecurity offers either VNCAuth or None depending on
// whether a password is configured, performs the auth, and sends the
// security result.
func (s *VNCServer) negotiateSecurity(c *rfb.Conn) error {
	var offered []rfb.SecurityType
	if config.VncPassword == "" {
		offered = []rfb.SecurityType{rfb.SecNone}
	} else {
		offered = []rfb.SecurityType{rfb.SecVNCAuth}
	}

	chosen, err := c.OfferSecurityTypes(offered)
	if err != nil {
		_ = c.SendSecurityResultFailed("no compatible security type")
		return err
	}

	switch chosen {
	case rfb.SecNone:
		// nothing to do
	case rfb.SecVNCAuth:
		if err := c.PerformVNCAuth(config.VncPassword); err != nil {
			_ = c.SendSecurityResultFailed("authentication failed")
			return err
		}
	default:
		_ = c.SendSecurityResultFailed("unsupported security type")
		return fmt.Errorf("vnc: unsupported security type %d", chosen)
	}

	return c.SendSecurityResultOK()
}

// vncFramebufferSize returns the current capture resolution (or a
// safe default if no signal yet).
func vncFramebufferSize() (uint16, uint16) {
	w, h := lastVideoState.Width, lastVideoState.Height
	if w <= 0 || h <= 0 {
		return 1280, 720
	}
	return uint16(w), uint16(h)
}
