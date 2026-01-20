package rdp

import (
	"fmt"
	"net"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

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

// Server manages RDP client connections.
//
// Lock ordering: When acquiring multiple locks, always acquire mu before rateLimitMu.
type Server struct {
	listener    net.Listener
	connections sync.Map
	connCount   atomic.Int32

	port       int
	tlsEnabled bool

	mu       sync.Mutex // protects running, stopChan, width, height
	running  bool
	stopChan chan struct{}

	width  uint16
	height uint16

	rateLimitMu  sync.Mutex
	lastConnTime map[string]time.Time

	startError error

	deps Dependencies
}

// NewServer creates a new RDP server with the given dependencies.
func NewServer(deps Dependencies) *Server {
	return &Server{
		port:         DefaultPort,
		stopChan:     make(chan struct{}),
		lastConnTime: make(map[string]time.Time),
		deps:         deps,
		width:        DefaultWidth,
		height:       DefaultHeight,
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
		s.startError = err
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
	s.startError = nil
	s.stopChan = make(chan struct{})

	go s.acceptLoop()

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
		if s.connCount.Load() >= int32(maxConns) {
			s.deps.Logger.Warn().Str("remote", remoteAddr).Int("max", maxConns).
				Msg("RDP connection rejected: max connections reached")
			conn.Close()
			continue
		}

		s.deps.Logger.Info().Str("remote", remoteAddr).Msg("new RDP connection")

		rdpConn := NewConnection(conn, s)
		s.connections.Store(rdpConn, true)
		s.connCount.Add(1)

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

				s.connections.Delete(c)
				s.connCount.Add(-1)
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

// BroadcastFrame sends a video frame to all connected clients.
func (s *Server) BroadcastFrame(frame []byte) {
	s.connections.Range(func(key, value any) bool {
		if conn, ok := key.(*Connection); ok {
			conn.SendFrame(frame)
		}
		return true
	})
}
