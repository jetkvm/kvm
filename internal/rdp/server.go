package rdp

import (
	"crypto/sha256"
	"fmt"
	"net"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/rdp/udp"
	"github.com/rs/zerolog"
)

// safeGo launches a goroutine with panic recovery. If fn panics, the panic
// is logged and the goroutine exits without crashing the process.
func safeGo(logger *zerolog.Logger, tag string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error().
					Interface("panic", r).
					Str("stack", string(debug.Stack())).
					Str("errorId", tag).
					Msg("goroutine panicked - this is a bug, please report")
			}
		}()
		fn()
	}()
}

// Default configuration values.
const (
	DefaultPort      = 3389
	MaxConnections   = 10
	DefaultWidth     = 1920
	DefaultHeight    = 1080
	ConnectionRateMs = 100
	RateLimitExpiry  = 60 * time.Second
	RateLimitCleanup = 50
)

// pendingUDP holds state for a UDP connection being established.
type pendingUDP struct {
	conn   *Connection
	cookie [16]byte
}

// cookieKey is the map key for cookie lookup (SHA-256 hash).
type cookieKey = [32]byte

// Server manages RDP client connections.
//
// Lock ordering: When acquiring multiple locks, always acquire mu before rateLimitMu.
type Server struct {
	listener    net.Listener
	connections sync.Map
	connCount   atomic.Int32

	port                  int
	tlsEnabled            bool
	udpEnabled            bool // Config flag for MS-RDPEUDP support
	multitransportEnabled bool // Send Initiate Multitransport Request (disabled: client compat)

	mu       sync.Mutex // protects running, stopChan, width, height
	running  bool
	stopChan chan struct{}

	width  uint16
	height uint16

	rateLimitMu  sync.Mutex
	lastConnTime map[string]time.Time

	// UDP transport (MS-RDPEUDP2)
	udpConn          *net.UDPConn // Shared UDP listener on port 3389
	udpMu            sync.Mutex   // Protects udpConn lifecycle
	cookieMap        sync.Map     // map[cookieKey]*pendingUDP — security cookie hash → pending connection
	transports       sync.Map     // map[addrKey]*udp.Transport — remote addr → active transport
	transportPending sync.Map     // map[addrKey]*pendingUDP — transport addr → pending connection (for setup)

	deps Dependencies
}

// tryAcquireConn atomically checks and increments the connection count.
// Returns true if the connection was acquired, false if max connections reached.
// Uses CAS to prevent exceeding maxConns under concurrent connects.
func (s *Server) tryAcquireConn(maxConns int) bool {
	limit := int32(maxConns)
	for {
		old := s.connCount.Load()
		if old >= limit {
			return false
		}
		if s.connCount.CompareAndSwap(old, old+1) {
			return true
		}
	}
}

// NewServer creates a new RDP server with the given dependencies.
func NewServer(deps Dependencies) *Server {
	return &Server{
		port:                  DefaultPort,
		stopChan:              make(chan struct{}),
		lastConnTime:          make(map[string]time.Time),
		deps:                  deps,
		width:                 DefaultWidth,
		height:                DefaultHeight,
		multitransportEnabled: true,
	}
}

// Start begins accepting RDP connections.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("RDP server already running")
	}

	// Get TLS configuration if enabled
	tlsMode := s.deps.Config.GetTLSMode()
	s.tlsEnabled = tlsMode != "" && tlsMode != "disabled"

	addr := fmt.Sprintf(":%d", s.port)
	listener, err := net.Listen("tcp4", addr)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}

	// Note: We don't wrap the listener with TLS here because RDP requires
	// TLS to be negotiated after the X.224 Connection Request/Confirm exchange.
	// The TLS upgrade happens in Connection.handleX224Connection() if both
	// client and server support TLS.
	if s.tlsEnabled && s.deps.TLS != nil {
		hwAccel := ""
		if s.deps.TLS.IsHardwareAccelerated() {
			hwAccel = " (hardware accelerated: " + s.deps.TLS.HardwareEngine() + ")"
		}
		s.deps.Logger.Info().Int("port", s.port).Msgf("RDP server starting with TLS support%s", hwAccel)
	} else {
		s.deps.Logger.Info().Int("port", s.port).Msg("RDP server starting without TLS")
	}

	s.listener = listener
	s.running = true
	s.stopChan = make(chan struct{})

	go s.acceptLoop()

	// Start UDP listener only when the full multitransport protocol is enabled.
	// Without multitransportEnabled, we don't send Initiate Multitransport Request,
	// so the UDP listener would be idle and waste resources.
	if s.udpEnabled && s.multitransportEnabled && s.tlsEnabled && s.deps.TLS != nil {
		if err := s.startUDPListener(); err != nil {
			s.deps.Logger.Warn().Err(err).Msg("RDP: failed to start UDP listener, continuing TCP-only")
			s.udpEnabled = false
		}
	}

	return nil
}

// Stop gracefully shuts down the RDP server.
func (s *Server) Stop() error {
	s.mu.Lock()

	if !s.running {
		s.mu.Unlock()
		return nil
	}

	s.deps.Logger.Info().Msg("stopping RDP server")

	close(s.stopChan)

	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			s.deps.Logger.Debug().Err(err).Msg("error closing listener")
		}
	}

	// Stop UDP listener
	s.stopUDPListener()

	s.running = false
	s.mu.Unlock()

	s.rateLimitMu.Lock()
	s.lastConnTime = make(map[string]time.Time)
	s.rateLimitMu.Unlock()

	// Close all connections
	s.connections.Range(func(key, value any) bool {
		if conn, ok := key.(*Connection); ok {
			conn.Close()
		}
		return true
	})

	s.deps.Logger.Info().Msg("RDP server stopped")

	return nil
}

// IsRunning returns true if the server is accepting connections.
func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// GetConnectionCount returns the number of active connections.
func (s *Server) GetConnectionCount() int {
	return int(s.connCount.Load())
}

// SetPort sets the port number for the server.
func (s *Server) SetPort(port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.port = port
}

// GetPort returns the configured port number.
func (s *Server) GetPort() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// SetUDPEnabled sets the UDP transport flag. Takes effect on next connection.
func (s *Server) SetUDPEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.udpEnabled = enabled
}

// UpdateVideoState notifies all connections of a resolution change.
func (s *Server) UpdateVideoState(width, height uint16) {
	s.mu.Lock()
	oldWidth, oldHeight := s.width, s.height
	s.width = width
	s.height = height
	s.mu.Unlock()

	if oldWidth != width || oldHeight != height {
		s.deps.Logger.Debug().
			Uint16("oldWidth", oldWidth).
			Uint16("oldHeight", oldHeight).
			Uint16("newWidth", width).
			Uint16("newHeight", height).
			Msg("RDP: video resolution changed")
	}

	// Notify all connections
	s.connections.Range(func(key, value any) bool {
		if conn, ok := key.(*Connection); ok {
			conn.onResolutionChange(width, height)
		}
		return true
	})
}

// GetVideoState returns the current video resolution.
func (s *Server) GetVideoState() (uint16, uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.width, s.height
}

// checkRateLimit returns true if the connection should be rate limited.
func (s *Server) checkRateLimit(ip string) bool {
	s.rateLimitMu.Lock()
	defer s.rateLimitMu.Unlock()

	now := time.Now()
	rateLimited := false

	if lastTime, ok := s.lastConnTime[ip]; ok {
		if now.Sub(lastTime) < ConnectionRateMs*time.Millisecond {
			rateLimited = true
		}
	}

	if !rateLimited {
		s.lastConnTime[ip] = now
	}

	// Cleanup stale entries
	if len(s.lastConnTime) > RateLimitCleanup {
		cutoff := now.Add(-RateLimitExpiry)
		for k, v := range s.lastConnTime {
			if v.Before(cutoff) {
				delete(s.lastConnTime, k)
			}
		}
	}

	return rateLimited
}

// acceptLoop handles incoming RDP connections.
func (s *Server) acceptLoop() {
	for {
		select {
		case <-s.stopChan:
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.stopChan:
				return
			default:
				s.deps.Logger.Error().Err(err).Msg("failed to accept connection")
				continue
			}
		}

		remoteAddr := conn.RemoteAddr().String()
		host, _, err := net.SplitHostPort(remoteAddr)
		if err != nil {
			s.deps.Logger.Warn().Err(err).Str("remote", remoteAddr).
				Msg("failed to parse remote address, rejecting")
			conn.Close()
			continue
		}

		if s.checkRateLimit(host) {
			s.deps.Logger.Warn().Str("remote", remoteAddr).Msg("RDP connection rate limited")
			conn.Close()
			continue
		}

		maxConns := s.deps.Config.GetRDPMaxConnections()
		if maxConns <= 0 || maxConns > MaxConnections {
			maxConns = MaxConnections
		}
		if !s.tryAcquireConn(maxConns) {
			s.deps.Logger.Warn().Str("remote", remoteAddr).Int("max", maxConns).
				Msg("RDP connection rejected: max connections reached")
			conn.Close()
			continue
		}

		s.deps.Logger.Info().Str("remote", remoteAddr).Msg("new RDP connection")

		// TCP tuning for low latency
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			// Disable Nagle's algorithm - critical for low-latency streaming
			// Nagle buffers small writes which adds 40ms+ latency
			_ = tcpConn.SetNoDelay(true)

			// Set socket buffer sizes for video streaming
			// Larger buffers reduce syscall overhead for burst traffic
			_ = tcpConn.SetWriteBuffer(256 * 1024) // 256KB send buffer
			_ = tcpConn.SetReadBuffer(64 * 1024)   // 64KB receive buffer

			// Enable TCP keepalive to detect dead connections
			_ = tcpConn.SetKeepAlive(true)
			_ = tcpConn.SetKeepAlivePeriod(30 * time.Second)
		}

		// Wrap connection for packet capture if enabled
		var capture PacketCapture
		if s.deps.NewCapture != nil {
			capture = s.deps.NewCapture(remoteAddr)
			if capture != nil {
				conn = capture.WrapConn(conn)
			}
		}

		rdpConn := NewConnection(conn, s)
		rdpConn.capture = capture
		s.connections.Store(rdpConn, true)

		// Track session for sleep mode prevention
		if s.deps.OnSessionStart != nil {
			s.deps.OnSessionStart()
		}

		go func(c *Connection, addr string) {
			defer func() {
				if r := recover(); r != nil {
					s.deps.Logger.Error().
						Interface("panic", r).
						Str("stack", string(debug.Stack())).
						Str("remote", addr).
						Str("errorId", "RDP_HANDLER_PANIC").
						Msg("RDP connection handler panicked - this is a bug, please report")
				}

				// Clean up all connection resources (audio streams, channels, etc.)
				// This is critical - without it, audio goroutines leak and keep running.
				c.Close()

				s.connections.Delete(c)
				s.connCount.Add(-1)

				// Track session end for sleep mode prevention
				if s.deps.OnSessionEnd != nil {
					s.deps.OnSessionEnd()
				}

				s.deps.Logger.Info().Str("remote", addr).Msg("RDP connection closed")
			}()

			if err := c.Handle(); err != nil {
				errStr := err.Error()
				if errStr == "EOF" || strings.Contains(errStr, "timeout") ||
					strings.Contains(errStr, "connection reset") ||
					strings.Contains(errStr, "broken pipe") ||
					strings.Contains(errStr, "use of closed") {
					s.deps.Logger.Debug().Err(err).Str("remote", addr).
						Msg("RDP connection ended")
				} else {
					s.deps.Logger.Warn().Err(err).Str("remote", addr).
						Msg("RDP connection ended with unexpected error")
				}
			}
		}(rdpConn, remoteAddr)
	}
}

// HandleGatewayConnection handles an RDP connection arriving via the RD Gateway
// (MS-TSGU) direct pipe. The conn is a tsguConn wrapping the gateway transport;
// the outer HTTPS layer provides encryption so inner TLS is skipped.
//
// This runs in the caller's goroutine (the gateway handler) and blocks until
// the session ends. When the client disconnects, the transport errors,
// Handle() returns, and the gateway cleans up.
func (s *Server) HandleGatewayConnection(conn net.Conn) {
	maxConns := s.deps.Config.GetRDPMaxConnections()
	if maxConns <= 0 || maxConns > MaxConnections {
		maxConns = MaxConnections
	}
	if !s.tryAcquireConn(maxConns) {
		s.deps.Logger.Warn().Str("remote", conn.RemoteAddr().String()).Int("max", maxConns).
			Msg("RDP gateway connection rejected: max connections reached")
		conn.Close()
		return
	}

	// Wrap connection for packet capture if enabled
	var capture PacketCapture
	if s.deps.NewCapture != nil {
		capture = s.deps.NewCapture(conn.RemoteAddr().String())
		if capture != nil {
			conn = capture.WrapConn(conn)
		}
	}

	rdpConn := NewConnection(conn, s)
	rdpConn.capture = capture
	rdpConn.softwareTLS = true // tsguConn has no kernel socket fd for OpenSSL's SSL_set_fd()

	s.connections.Store(rdpConn, true)

	if s.deps.OnSessionStart != nil {
		s.deps.OnSessionStart()
	}

	defer func() {
		if r := recover(); r != nil {
			s.deps.Logger.Error().
				Interface("panic", r).
				Str("stack", string(debug.Stack())).
				Str("remote", conn.RemoteAddr().String()).
				Str("errorId", "RDP_GW_HANDLER_PANIC").
				Msg("RDP gateway handler panicked - this is a bug, please report")
		}

		rdpConn.Close()
		s.connections.Delete(rdpConn)
		s.connCount.Add(-1)

		if s.deps.OnSessionEnd != nil {
			s.deps.OnSessionEnd()
		}

		s.deps.Logger.Info().Str("remote", conn.RemoteAddr().String()).
			Msg("RDP gateway connection closed")
	}()

	s.deps.Logger.Info().Str("remote", conn.RemoteAddr().String()).
		Msg("RDP gateway connection started (direct pipe)")

	if err := rdpConn.Handle(); err != nil {
		errStr := err.Error()
		if errStr == "EOF" || strings.Contains(errStr, "timeout") ||
			strings.Contains(errStr, "connection reset") ||
			strings.Contains(errStr, "broken pipe") ||
			strings.Contains(errStr, "use of closed") {
			s.deps.Logger.Debug().Err(err).Str("remote", conn.RemoteAddr().String()).
				Msg("RDP gateway connection ended")
		} else {
			s.deps.Logger.Warn().Err(err).Str("remote", conn.RemoteAddr().String()).
				Msg("RDP gateway connection ended with unexpected error")
		}
	}
}

// BroadcastFrame sends a video frame to all connected clients.
func (s *Server) BroadcastFrame(frame []byte) {
	if s.connCount.Load() == 0 {
		return // Fast-path: no connections
	}
	s.connections.Range(func(key, value any) bool {
		if conn, ok := key.(*Connection); ok {
			conn.SendFrame(frame)
		}
		return true
	})
}

// startUDPListener creates the shared UDP socket and starts the read loop.
func (s *Server) startUDPListener() error {
	s.udpMu.Lock()
	defer s.udpMu.Unlock()

	addr := &net.UDPAddr{Port: s.port}
	udpConn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return fmt.Errorf("failed to create UDP listener: %w", err)
	}

	// Set read buffer to 512KB for burst traffic
	_ = udpConn.SetReadBuffer(512 * 1024)

	s.udpConn = udpConn
	s.deps.Logger.Info().Int("port", s.port).Msg("RDP: UDP listener started")

	safeGo(s.deps.Logger, "RDP_UDP_READ_LOOP", s.udpReadLoop)
	return nil
}

// stopUDPListener closes the UDP socket and cleans up.
func (s *Server) stopUDPListener() {
	s.udpMu.Lock()
	defer s.udpMu.Unlock()

	if s.udpConn != nil {
		if err := s.udpConn.Close(); err != nil {
			s.deps.Logger.Debug().Err(err).Msg("RDP: error closing UDP listener")
		}
		s.udpConn = nil
	}

	// Close all active transports
	s.transports.Range(func(key, value any) bool {
		if t, ok := value.(*udp.Transport); ok {
			_ = t.Close()
		}
		s.transports.Delete(key)
		return true
	})

	// Clear pending cookie registrations
	s.cookieMap.Range(func(key, value any) bool {
		s.cookieMap.Delete(key)
		return true
	})
	s.transportPending.Range(func(key, value any) bool {
		s.transportPending.Delete(key)
		return true
	})

	s.deps.Logger.Debug().Msg("RDP: UDP listener stopped")
}

// udpReadLoop reads all incoming UDP packets and routes them.
func (s *Server) udpReadLoop() {
	buf := make([]byte, 1500)

	for {
		select {
		case <-s.stopChan:
			return
		default:
		}

		n, remoteAddr, err := s.udpConn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-s.stopChan:
				return
			default:
				s.deps.Logger.Debug().Err(err).Msg("RDP: UDP read error")
				continue
			}
		}

		if n < 2 {
			continue
		}

		// Make a copy of the data since buf is reused
		data := make([]byte, n)
		copy(data, buf[:n])

		addrStr := remoteAddr.String()

		// Route 1: Known transport → forward to ProcessIncomingPacket
		if val, ok := s.transports.Load(addrStr); ok {
			t := val.(*udp.Transport)
			if t.IsClosed() {
				s.transports.Delete(addrStr)
			} else if t.IsHandshakeState() {
				// Transport in handshake — this should be the ACK
				s.handleUDPHandshakeACK(t, data, addrStr)
			} else {
				t.ProcessIncomingPacket(data)
			}
			continue
		}

		// Route 2: Unknown address — check if it's a SYN for cookie matching
		s.handleUDPNewConnection(data, remoteAddr)
	}
}

// handleUDPNewConnection processes a SYN from an unknown client address.
func (s *Server) handleUDPNewConnection(data []byte, remoteAddr *net.UDPAddr) {
	// Check if this is a SYN packet (v1 format)
	if len(data) < 2 {
		return
	}
	flags := uint16(data[0]) | uint16(data[1])<<8
	if flags&udp.RDPUDPFlagSYN == 0 {
		return // Not a SYN
	}

	// Parse the SYN to extract the cookie hash
	syn, err := udp.ParseSynPacket(data)
	if err != nil {
		s.deps.Logger.Debug().Err(err).Msg("RDP: failed to parse UDP SYN")
		return
	}

	if !syn.HasCookieHash {
		s.deps.Logger.Debug().Msg("RDP: UDP SYN without cookie hash, ignoring")
		return
	}

	// Look up the cookie hash in our pending registrations
	key := cookieKey(syn.CookieHash)
	val, ok := s.cookieMap.Load(key)
	if !ok {
		s.deps.Logger.Debug().
			Str("remote", remoteAddr.String()).
			Msg("RDP: UDP SYN with unknown cookie hash")
		return
	}

	pending := val.(*pendingUDP)

	// Create transport for this client
	t := udp.NewTransport(s.udpConn, remoteAddr, s.deps.Logger)

	// Process SYN and send SYN+ACK
	synAck, err := t.HandleSYN(data, pending.cookie)
	if err != nil {
		s.deps.Logger.Warn().Err(err).
			Str("remote", remoteAddr.String()).
			Msg("RDP: UDP SYN handling failed")
		return
	}

	// Register transport and pending connection by address
	addrStr := remoteAddr.String()
	s.transports.Store(addrStr, t)
	s.transportPending.Store(addrStr, pending)

	// Send SYN+ACK
	if _, err := s.udpConn.WriteToUDP(synAck, remoteAddr); err != nil {
		s.deps.Logger.Warn().Err(err).Msg("RDP: failed to send SYN+ACK")
		s.transports.Delete(addrStr)
		s.transportPending.Delete(addrStr)
		return
	}

	s.deps.Logger.Info().
		Str("remote", addrStr).
		Msg("RDP: UDP SYN+ACK sent, waiting for ACK")
}

// handleUDPHandshakeACK processes the final ACK in the 3-way handshake
// and completes TLS + RDPEMT tunnel setup.
func (s *Server) handleUDPHandshakeACK(t *udp.Transport, data []byte, addrStr string) {
	if err := t.HandleACK(data); err != nil {
		s.deps.Logger.Debug().Err(err).Msg("RDP: UDP handshake ACK failed")
		return
	}

	s.deps.Logger.Info().
		Str("remote", addrStr).
		Msg("RDP: UDP handshake complete, starting TLS")

	// Complete TLS + RDPEMT in a goroutine (blocking operations)
	safeGo(s.deps.Logger, "RDP_UDP_SETUP", func() { s.completeUDPSetup(t, addrStr) })
}

// completeUDPSetup runs TLS handshake and RDPEMT tunnel creation over the transport.
func (s *Server) completeUDPSetup(t *udp.Transport, addrStr string) {
	// Look up the pending connection registered during SYN processing
	val, ok := s.transportPending.LoadAndDelete(addrStr)
	if !ok {
		s.deps.Logger.Warn().Str("remote", addrStr).
			Msg("RDP: no pending connection for UDP transport")
		_ = t.Close()
		s.transports.Delete(addrStr)
		return
	}
	pending := val.(*pendingUDP)

	// Set TLS handshake timeout — if client doesn't complete TLS within 10s,
	// abort to avoid leaking goroutines
	_ = t.SetDeadline(time.Now().Add(10 * time.Second))

	// TLS handshake over RDPEUDP2 (Transport implements net.Conn)
	tlsConn, err := s.deps.TLS.UpgradeServerConn(t)
	if err != nil {
		s.deps.Logger.Warn().Err(err).
			Str("remote", addrStr).
			Msg("RDP: TLS over UDP failed")
		_ = t.Close()
		s.transports.Delete(addrStr)
		return
	}

	// Clear TLS deadline, set RDPEMT handshake timeout
	_ = t.SetDeadline(time.Now().Add(10 * time.Second))

	s.deps.Logger.Debug().
		Str("remote", addrStr).
		Msg("RDP: TLS over UDP established")

	// Create RDPEMT tunnel
	tunnel := udp.NewTunnel(tlsConn, t, pending.cookie, s.deps.Logger)

	// Perform RDPEMT handshake (Create Request/Response)
	if err := tunnel.Handshake(); err != nil {
		s.deps.Logger.Warn().Err(err).
			Str("remote", addrStr).
			Msg("RDP: RDPEMT handshake failed")
		_ = tunnel.Close()
		s.transports.Delete(addrStr)
		return
	}

	// Clear deadline — data path has its own timeout handling
	_ = t.SetDeadline(time.Time{})

	s.deps.Logger.Info().
		Str("remote", addrStr).
		Msg("RDP: UDP transport fully established")

	// Notify the Connection that UDP is ready
	pending.conn.onUDPTransportReady(tunnel)
}

// RegisterUDPCookie registers a security cookie for UDP matching.
// Called by Connection after sending Initiate Multitransport Request.
func (s *Server) RegisterUDPCookie(cookie [16]byte, conn *Connection) {
	hash := sha256.Sum256(cookie[:])
	s.cookieMap.Store(cookieKey(hash), &pendingUDP{
		conn:   conn,
		cookie: cookie,
	})
}

// UnregisterUDPCookie removes a security cookie registration.
// Called during connection cleanup.
func (s *Server) UnregisterUDPCookie(cookie [16]byte) {
	hash := sha256.Sum256(cookie[:])
	s.cookieMap.Delete(cookieKey(hash))
}

// GetUDPConn returns the shared UDP connection, or nil if UDP is not enabled.
func (s *Server) GetUDPConn() *net.UDPConn {
	s.udpMu.Lock()
	defer s.udpMu.Unlock()
	return s.udpConn
}
