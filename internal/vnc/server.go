package vnc

import (
	"fmt"
	"net"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Server manages VNC client connections and JPEG encoder lifecycle.
//
// Lock ordering: When acquiring multiple locks, always acquire mu before rateLimitMu.
type Server struct {
	listener    net.Listener
	connections sync.Map
	connCount   atomic.Int32

	port       int
	tlsEnabled bool

	mu       sync.Mutex // protects running, stopChan, width, height, jpegClientCount, jpegEncoderOn
	running  bool
	stopChan chan struct{}

	width  uint16
	height uint16

	// JPEG encoder state - protected by mu
	jpegClientCount     int32
	jpegEncoderOn       bool
	jpegEncoderFailures int       // consecutive failures for circuit breaker
	jpegEncoderCooldown time.Time // don't retry until after this time

	rateLimitMu  sync.Mutex           // protects lastConnTime only; acquire after mu
	lastConnTime map[string]time.Time // rate limiting per IP

	startError error // track if server failed to start for status reporting

	// Dependencies injected from kvm package
	deps Dependencies
}

// NewServer creates a new VNC server with the given dependencies.
func NewServer(deps Dependencies) *Server {
	return &Server{
		port:         DefaultPort,
		stopChan:     make(chan struct{}),
		lastConnTime: make(map[string]time.Time),
		deps:         deps,
	}
}

// Start begins accepting VNC connections.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("VNC server already running")
	}

	// Validate TLS hooks are configured when TLS is enabled
	tlsMode := s.deps.Config.GetTLSMode()
	tlsWanted := s.tlsEnabled && tlsMode != "" && tlsMode != "disabled"
	if tlsWanted {
		if GetCertificateFunc == nil {
			return fmt.Errorf("TLS enabled but GetCertificateFunc not set - call vnc.SetTLSHooks() first")
		}
		if TLSConnUpgrader == nil {
			return fmt.Errorf("TLS enabled but TLSConnUpgrader not set - call vnc.SetTLSHooks() first")
		}
	}

	addr := fmt.Sprintf(":%d", s.port)
	listener, err := net.Listen("tcp4", addr)
	if err != nil {
		s.startError = err
		return fmt.Errorf("failed to create listener: %w", err)
	}

	if tlsWanted {
		s.deps.Logger.Info().Int("port", s.port).Msg("VNC server starting with VeNCrypt TLS support")
	} else {
		s.deps.Logger.Info().Int("port", s.port).Msg("VNC server starting without TLS")
	}

	s.listener = listener
	s.running = true
	s.startError = nil
	s.stopChan = make(chan struct{})

	go s.acceptLoop()

	return nil
}

// requestJPEGEncoder increments the JPEG client count and starts the encoder if needed.
// Returns an error if the encoder fails to start or is in cooldown due to repeated failures.
func (s *Server) requestJPEGEncoder() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Circuit breaker: check if we're in cooldown after repeated failures
	if s.jpegEncoderFailures >= jpegEncoderMaxFailures {
		if time.Now().Before(s.jpegEncoderCooldown) {
			remaining := time.Until(s.jpegEncoderCooldown).Round(time.Second)
			return fmt.Errorf("JPEG encoder in cooldown after %d failures (retry in %v)", s.jpegEncoderFailures, remaining)
		}
		// Cooldown expired, reset failures and try again
		s.deps.Logger.Info().Msg("JPEG encoder cooldown expired, retrying")
		s.jpegEncoderFailures = 0
	}

	s.jpegClientCount++
	s.deps.Logger.Debug().Int32("jpegClients", s.jpegClientCount).Msg("client requesting JPEG encoder")

	if !s.jpegEncoderOn {
		// Notify the kvm package that VNC needs the video stream.
		// This must happen BEFORE JpegStart so the native video capture
		// pipeline is running when the JPEG encoder starts producing frames.
		if s.deps.OnVideoNeeded != nil {
			s.deps.OnVideoNeeded()
		}

		if err := s.deps.Encoder.JpegStart(s.deps.Config.GetVNCQuality()); err != nil {
			s.jpegClientCount-- // Rollback count on failure
			s.jpegEncoderFailures++

			// Rollback the video stream reference since JPEG encoder failed
			if s.deps.OnVideoReleased != nil {
				s.deps.OnVideoReleased()
			}

			if s.jpegEncoderFailures >= jpegEncoderMaxFailures {
				s.jpegEncoderCooldown = time.Now().Add(jpegEncoderCooldownPeriod)
				s.deps.Logger.Error().Err(err).Int("failures", s.jpegEncoderFailures).
					Dur("cooldown", jpegEncoderCooldownPeriod).
					Msg("JPEG encoder failed repeatedly, entering cooldown")
			} else {
				s.deps.Logger.Error().Err(err).Int("failures", s.jpegEncoderFailures).Msg("failed to start JPEG encoder")
			}
			return fmt.Errorf("failed to start JPEG encoder: %w", err)
		}
		s.jpegEncoderOn = true
		s.jpegEncoderFailures = 0 // Reset on success
		s.deps.Logger.Info().Int("quality", s.deps.Config.GetVNCQuality()).Msg("JPEG encoder started on-demand")
	}
	return nil
}

// releaseJPEGEncoder decrements the JPEG client count and stops the encoder if no clients need it.
func (s *Server) releaseJPEGEncoder() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.jpegClientCount--
	s.deps.Logger.Debug().Int32("jpegClients", s.jpegClientCount).Msg("client releasing JPEG encoder")

	if s.jpegClientCount <= 0 {
		s.jpegClientCount = 0 // Prevent negative counts
		if s.jpegEncoderOn {
			if err := s.deps.Encoder.JpegStop(); err != nil {
				s.deps.Logger.Warn().Err(err).Msg("failed to stop JPEG encoder, resource may be leaked")
			} else {
				s.deps.Logger.Info().Msg("JPEG encoder stopped (no clients need it)")
			}
			s.jpegEncoderOn = false

			// Notify the kvm package that VNC no longer needs the video stream.
			// This happens AFTER JpegStop so the encoder is fully stopped before
			// the video capture pipeline might be shut down.
			if s.deps.OnVideoReleased != nil {
				s.deps.OnVideoReleased()
			}
		}
	}
}

// Stop gracefully shuts down the VNC server.
func (s *Server) Stop() error {
	s.mu.Lock()

	if !s.running {
		s.mu.Unlock()
		return nil
	}

	s.deps.Logger.Info().Msg("stopping VNC server")

	close(s.stopChan)

	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			s.deps.Logger.Debug().Err(err).Msg("error closing listener")
		}
	}

	if s.jpegEncoderOn {
		if err := s.deps.Encoder.JpegStop(); err != nil {
			// Best-effort cleanup. On restart, JpegStart will reinitialize the encoder.
			// Keeping jpegEncoderOn=true would prevent future starts, so we clear it.
			s.deps.Logger.Warn().Err(err).Msg("failed to stop JPEG encoder during shutdown")
		}
		s.jpegEncoderOn = false
	}
	s.jpegClientCount = 0

	s.running = false
	s.mu.Unlock()

	s.rateLimitMu.Lock()
	s.lastConnTime = make(map[string]time.Time)
	s.rateLimitMu.Unlock()

	s.connections.Range(func(key, value any) bool {
		if conn, ok := key.(*Connection); ok {
			conn.Close()
		}
		return true
	})

	s.deps.Logger.Info().Msg("VNC server stopped")

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

// SetTLSEnabled enables or disables TLS for the server.
func (s *Server) SetTLSEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tlsEnabled = enabled
}

// IsTLSEnabled returns whether TLS is enabled.
func (s *Server) IsTLSEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tlsEnabled
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
			Msg("VNC: video resolution changed")
	}

	if s.connCount.Load() == 0 {
		return // Fast-path: no connections to notify
	}
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
		if now.Sub(lastTime) < time.Duration(connectionRateLimitMs)*time.Millisecond {
			rateLimited = true
		}
	}

	// Always update timestamp for legitimate connections to track last activity
	if !rateLimited {
		s.lastConnTime[ip] = now
	}

	// Clean up stale entries when map exceeds threshold.
	// Use len check before expensive iteration. The map is capped at ~MaxConnections
	// worth of legitimate IPs, plus stale entries. With 60s expiry and 100ms rate limit,
	// at most 600 IPs per minute could accumulate before expiring.
	if len(s.lastConnTime) > rateLimitCleanupThreshold {
		cutoff := now.Add(-time.Duration(rateLimitExpirySeconds) * time.Second)
		for k, v := range s.lastConnTime {
			if v.Before(cutoff) {
				delete(s.lastConnTime, k)
			}
		}
	}

	return rateLimited
}

// acceptLoop handles incoming VNC connections until the server stops.
// Each connection runs in its own goroutine with panic recovery.
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
			s.deps.Logger.Warn().Err(err).Str("remote", remoteAddr).Msg("failed to parse remote address, rejecting")
			if closeErr := conn.Close(); closeErr != nil && s.deps.Logger.Debug().Enabled() {
				s.deps.Logger.Debug().Err(closeErr).Str("remote", remoteAddr).Msg("failed to close rejected connection")
			}
			continue
		}

		if s.checkRateLimit(host) {
			s.deps.Logger.Warn().Str("remote", remoteAddr).Msg("VNC connection rate limited")
			if closeErr := conn.Close(); closeErr != nil && s.deps.Logger.Debug().Enabled() {
				s.deps.Logger.Debug().Err(closeErr).Str("remote", remoteAddr).Msg("failed to close rate-limited connection")
			}
			continue
		}

		// Use configured max or hardware limit, whichever is lower
		maxConns := s.deps.Config.GetVNCMaxConnections()
		if maxConns <= 0 || maxConns > MaxConnections {
			maxConns = MaxConnections
		}
		if s.connCount.Load() >= int32(maxConns) {
			s.deps.Logger.Warn().Str("remote", remoteAddr).Int("max", maxConns).Msg("VNC connection rejected: max connections reached")
			if closeErr := conn.Close(); closeErr != nil && s.deps.Logger.Debug().Enabled() {
				s.deps.Logger.Debug().Err(closeErr).Str("remote", remoteAddr).Msg("failed to close max-connections connection")
			}
			continue
		}

		s.deps.Logger.Info().Str("remote", remoteAddr).Msg("new VNC connection")

		vncConn := NewConnection(conn, s)
		s.connections.Store(vncConn, true)
		s.connCount.Add(1)

		go func(vc *Connection, remoteAddr string) {
			defer func() {
				if r := recover(); r != nil {
					// Include error ID for tracking and potential alerting
					// Use captured remoteAddr in case vc.conn is nil during panic
					s.deps.Logger.Error().
						Interface("panic", r).
						Str("stack", string(debug.Stack())).
						Str("remote", remoteAddr).
						Str("errorId", "VNC_HANDLER_PANIC").
						Msg("VNC connection handler panicked - this is a bug, please report")
				}

				s.connections.Delete(vc)
				s.connCount.Add(-1)

				if vc.needsJPEGEncoder.Load() {
					s.releaseJPEGEncoder()
				}
				s.deps.Logger.Info().Str("remote", remoteAddr).Msg("VNC connection closed")
			}()

			if err := vc.Handle(); err != nil {
				// Differentiate between normal disconnects and unexpected errors
				// EOF and timeout are expected when client disconnects
				errStr := err.Error()
				if errStr == "EOF" || strings.Contains(errStr, "timeout") || strings.Contains(errStr, "connection reset") ||
					strings.Contains(errStr, "broken pipe") || strings.Contains(errStr, "use of closed") {
					s.deps.Logger.Debug().Err(err).Str("remote", remoteAddr).Msg("VNC connection ended")
				} else {
					s.deps.Logger.Warn().Err(err).Str("remote", remoteAddr).Msg("VNC connection ended with unexpected error")
				}
			}
		}(vncConn, remoteAddr)
	}
}

// BroadcastJPEGFrame sends a JPEG frame to all connected clients that have requested one.
// Design note: This uses synchronous iteration which means a slow client can delay others.
// However, this is intentional for an embedded device because:
// 1. Each client has a 5-second write timeout (writeTimeout) preventing complete stalls
// 2. Async dispatch would create goroutine allocation GC pressure per frame
// 3. Frame channels would add memory overhead and frame duplication complexity
// 4. Maximum 10 clients (MaxConnections) limits worst-case latency
// If a client is consistently slow, frames are dropped via frameRequested flag backpressure.
func (s *Server) BroadcastJPEGFrame(frame []byte) {
	if s.connCount.Load() == 0 {
		return // Fast-path: no connections
	}
	s.connections.Range(func(key, value any) bool {
		if conn, ok := key.(*Connection); ok {
			conn.SendJPEGFrameDirect(frame)
		}
		return true
	})
}
