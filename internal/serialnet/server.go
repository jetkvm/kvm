package serialnet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.bug.st/serial"
)

// Package serialnet serves a UART to network clients (jetkvm/kvm#1520),
// either as a ser2net-style TCP port (raw or telnet with RFC 2217) or over
// WebSockets. It never touches the port itself: the caller feeds UART reads
// to a Hub and supplies functions to write and reconfigure the port, so the
// device keeps a single reader and writer and this package builds and tests
// without the device's native libraries.

const (
	ModeDisabled = "disabled"
	ModeSer2Net  = "ser2net"
	ModeWeb      = "web"

	ProtocolRaw     = "raw"
	ProtocolRFC2217 = "rfc2217"

	DefaultPort       = 2217
	DefaultMaxClients = 1
	MaxClientsLimit   = 8

	transportTCP = "tcp"
	transportWeb = "web"

	// clientQueueDepth is how many UART reads (up to 4 KiB each) may
	// queue for one client before it counts as stalled and is dropped. The
	// UART reader never waits on a network client.
	clientQueueDepth   = 256
	clientWriteTimeout = 30 * time.Second
	BreakDuration      = 250 * time.Millisecond
)

var (
	errDisabled = errors.New("serial network access is disabled")
	errBusy     = errors.New("too many serial network clients")

	// Ports the device already serves on.
	reservedPorts = map[int]string{22: "SSH", 80: "HTTP", 443: "HTTPS"}
)

var (
	clientsGauge = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetkvm_serial_network_clients",
			Help: "Network clients attached to the extension port UART",
		},
		[]string{"transport"},
	)
	bytesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "jetkvm_serial_network_bytes_total",
			Help: "Bytes relayed between network clients and the extension port UART",
		},
		[]string{"transport", "direction"},
	)
)

var logger = logging.GetSubsystemLogger("serial")

// Settings configure network access.
type Settings struct {
	Mode       string // ModeDisabled, ModeSer2Net or ModeWeb
	Port       int    // TCP port in ser2net mode
	Protocol   string // ProtocolRaw or ProtocolRFC2217
	MaxClients int    // concurrent clients, 1 to MaxClientsLimit
}

// Normalize fills in defaults for settings saved before network access
// existed.
func (s *Settings) Normalize() {
	if s.Mode == "" {
		s.Mode = ModeDisabled
	}
	if s.Port == 0 {
		s.Port = DefaultPort
	}
	if s.Protocol == "" {
		s.Protocol = ProtocolRaw
	}
	if s.MaxClients == 0 {
		s.MaxClients = DefaultMaxClients
	}
}

func (s Settings) Validate() error {
	switch s.Mode {
	case ModeDisabled, ModeSer2Net, ModeWeb:
	default:
		return fmt.Errorf("invalid network mode: %s", s.Mode)
	}
	switch s.Protocol {
	case ProtocolRaw, ProtocolRFC2217:
	default:
		return fmt.Errorf("invalid ser2net protocol: %s", s.Protocol)
	}
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("invalid ser2net port: %d", s.Port)
	}
	if name, ok := reservedPorts[s.Port]; ok {
		return fmt.Errorf("ser2net port %d is already used by %s", s.Port, name)
	}
	if s.MaxClients < 1 || s.MaxClients > MaxClientsLimit {
		return fmt.Errorf("network max clients must be between 1 and %d", MaxClientsLimit)
	}
	return nil
}

/* ---------- RX FAN-OUT ---------- */

// Hub hands every chunk read from the UART to the network clients.
type Hub struct {
	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

type subscriber struct {
	ch   chan []byte
	done chan struct{}
	once sync.Once
}

func (s *subscriber) close() { s.once.Do(func() { close(s.done) }) }

func NewHub() *Hub {
	return &Hub{subs: map[*subscriber]struct{}{}}
}

func (h *Hub) subscribe() *subscriber {
	sub := &subscriber{
		ch:   make(chan []byte, clientQueueDepth),
		done: make(chan struct{}),
	}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

func (h *Hub) unsubscribe(sub *subscriber) {
	h.mu.Lock()
	delete(h.subs, sub)
	h.mu.Unlock()
	sub.close()
}

// Broadcast must not be handed a slice that the caller reuses; subscribers
// share it read-only.
func (h *Hub) Broadcast(p []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		select {
		case sub.ch <- p:
		default:
			logger.Warn().Msg("serial network client is not keeping up; disconnecting it")
			delete(h.subs, sub)
			sub.close()
		}
	}
}

/* ---------- SERVER ---------- */

type client struct {
	transport string
	remote    string
	since     time.Time
	kick      func()
}

type ClientInfo struct {
	Transport   string    `json:"transport"`
	Remote      string    `json:"remote"`
	ConnectedAt time.Time `json:"connectedAt"`
}

type Status struct {
	Mode          string       `json:"mode"`
	Protocol      string       `json:"protocol,omitempty"`
	Running       bool         `json:"running"`
	ListenAddress string       `json:"listenAddress,omitempty"`
	MaxClients    int          `json:"maxClients"`
	Clients       []ClientInfo `json:"clients"`
	Error         string       `json:"error,omitempty"`
}

type Server struct {
	hub       *Hub
	write     func(p []byte, source string)
	mode      func() serial.Mode
	setMode   func(serial.Mode) error
	sendBreak func()
	bindAddr  func(port int) string

	mu        sync.Mutex
	active    bool
	settings  Settings
	listener  net.Listener
	listenErr error
	clients   map[*client]struct{}
}

// Deps connect a Server to the UART.
type Deps struct {
	// Write sends client input to the UART; source names the transport.
	Write func(p []byte, source string)
	// LineMode is the configured UART mode, restored after an RFC 2217
	// client that changed it disconnects.
	LineMode func() serial.Mode
	// SetLineMode applies a mode requested by an RFC 2217 client.
	SetLineMode func(serial.Mode) error
	// SendBreak sends a serial break (telnet IAC BRK).
	SendBreak func()
	// BindAddr returns the listen address for a TCP port, or "" if there
	// is nothing to listen on.
	BindAddr func(port int) string
}

func NewServer(hub *Hub, deps Deps) *Server {
	return &Server{
		hub:       hub,
		write:     deps.Write,
		mode:      deps.LineMode,
		setMode:   deps.SetLineMode,
		sendBreak: deps.SendBreak,
		bindAddr:  deps.BindAddr,
		clients:   map[*client]struct{}{},
		settings: Settings{
			Mode:       ModeDisabled,
			Port:       DefaultPort,
			Protocol:   ProtocolRaw,
			MaxClients: DefaultMaxClients,
		},
	}
}

// Apply starts, stops or reconfigures network access. active is whether the
// caller wants the service up (on the device: whether the Serial Console
// extension is loaded). A change drops connected clients; re-applying the
// same settings keeps them.
func (s *Server) Apply(active bool, st Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active == active && s.settings == st && s.listenErr == nil {
		return nil
	}

	s.stopLocked()
	s.active = active
	s.settings = st
	s.listenErr = nil

	if !active || st.Mode != ModeSer2Net {
		if active && st.Mode == ModeWeb {
			logger.Info().Msg("serial web console enabled")
		}
		return nil
	}

	addr := s.bindAddr(st.Port)
	if addr == "" {
		s.listenErr = errors.New("no IP stack is enabled to listen on")
		return s.listenErr
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		s.listenErr = fmt.Errorf("failed to listen on %s: %w", addr, err)
		logger.Error().Err(err).Str("address", addr).Msg("failed to start ser2net listener")
		return s.listenErr
	}
	s.listener = ln
	logger.Info().
		Str("address", ln.Addr().String()).
		Str("protocol", st.Protocol).
		Int("max_clients", st.MaxClients).
		Msg("ser2net listener started")
	go s.acceptLoop(ln, st.Protocol)
	return nil
}

// Stop closes the listener and drops every network client.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
	s.active = false
	s.listenErr = nil
}

func (s *Server) stopLocked() {
	if s.listener != nil {
		_ = s.listener.Close()
		logger.Info().Str("address", s.listener.Addr().String()).Msg("ser2net listener stopped")
		s.listener = nil
	}
	for c := range s.clients {
		s.dropLocked(c)
		// A WebSocket close waits for the peer's reply; don't hold s.mu.
		go c.kick()
	}
}

func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := Status{
		Mode:       s.settings.Mode,
		MaxClients: s.settings.MaxClients,
		Clients:    []ClientInfo{},
	}
	if s.listenErr != nil {
		st.Error = s.listenErr.Error()
	}
	switch s.settings.Mode {
	case ModeSer2Net:
		st.Protocol = s.settings.Protocol
		st.Running = s.listener != nil
		if s.listener != nil {
			st.ListenAddress = s.listener.Addr().String()
		}
	case ModeWeb:
		st.Running = s.active
	}
	for c := range s.clients {
		st.Clients = append(st.Clients, ClientInfo{
			Transport:   c.transport,
			Remote:      c.remote,
			ConnectedAt: c.since,
		})
	}
	return st
}

func (s *Server) register(transport, remote string, kick func()) (*client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	want := ModeSer2Net
	if transport == transportWeb {
		want = ModeWeb
	}
	if !s.active || s.settings.Mode != want {
		return nil, errDisabled
	}
	if len(s.clients) >= s.settings.MaxClients {
		return nil, errBusy
	}
	c := &client{transport: transport, remote: remote, since: time.Now(), kick: kick}
	s.clients[c] = struct{}{}
	clientsGauge.WithLabelValues(transport).Inc()
	logger.Info().Str("transport", transport).Str("remote", remote).Msg("serial network client connected")
	return c, nil
}

func (s *Server) unregister(c *client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropLocked(c)
}

func (s *Server) dropLocked(c *client) {
	if _, ok := s.clients[c]; !ok {
		return
	}
	delete(s.clients, c)
	clientsGauge.WithLabelValues(c.transport).Dec()
	logger.Info().Str("transport", c.transport).Str("remote", c.remote).Msg("serial network client disconnected")
}

func (s *Server) toUART(p []byte, transport string) {
	if len(p) == 0 || s.write == nil {
		return
	}
	bytesTotal.WithLabelValues(transport, "to_uart").Add(float64(len(p)))
	s.write(p, transport)
}

func (s *Server) acceptLoop(ln net.Listener, protocol string) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			logger.Warn().Err(err).Msg("ser2net accept failed")
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go s.serveTCP(conn, protocol)
	}
}

// lockedConn serialises writes from the UART relay and telnet replies.
type lockedConn struct {
	mu   sync.Mutex
	conn net.Conn
}

func (l *lockedConn) write(p []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.conn.SetWriteDeadline(time.Now().Add(clientWriteTimeout))
	_, err := l.conn.Write(p)
	return err
}

func (s *Server) serveTCP(conn net.Conn, protocol string) {
	defer conn.Close()
	remote := conn.RemoteAddr().String()

	client, err := s.register(transportTCP, remote, func() { _ = conn.Close() })
	if err != nil {
		logger.Info().Err(err).Str("remote", remote).Msg("rejected ser2net connection")
		_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
		_, _ = conn.Write([]byte("jetkvm: " + err.Error() + "\r\n"))
		return
	}
	defer s.unregister(client)

	out := &lockedConn{conn: conn}

	var tn *telnetSession
	if protocol == ProtocolRFC2217 {
		var mode serial.Mode
		if s.mode != nil {
			mode = s.mode()
		}
		tn = newTelnetSession(mode, s.setMode, s.sendBreak)
		if err := out.write(tn.initialNegotiation()); err != nil {
			return
		}
	}

	sub := s.hub.subscribe()
	defer s.hub.unsubscribe(sub)

	relayDone := make(chan struct{})
	go func() {
		defer close(relayDone)
		// Unblock the reader below when the relay stops for any reason.
		defer conn.Close()
		for {
			select {
			case p := <-sub.ch:
				bytesTotal.WithLabelValues(transportTCP, "from_uart").Add(float64(len(p)))
				if tn != nil {
					p = telnetEscapeIAC(p)
				}
				if err := out.write(p); err != nil {
					return
				}
			case <-sub.done:
				return
			}
		}
	}()

	buf := make([]byte, 1024)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			if tn != nil {
				var reply []byte
				data, reply = tn.Decode(data)
				if len(reply) > 0 {
					if werr := out.write(reply); werr != nil {
						break
					}
				}
			}
			s.toUART(data, transportTCP)
		}
		if err != nil {
			break
		}
	}

	sub.close()
	<-relayDone

	// Line settings an RFC 2217 client changed only last for its session.
	if tn != nil && tn.modeChanged && s.setMode != nil && s.mode != nil {
		if err := s.setMode(s.mode()); err != nil {
			logger.Warn().Err(err).Msg("failed to restore serial mode after RFC 2217 session")
		}
	}
}

func (s *Server) ServeWeb(ctx context.Context, ws *websocket.Conn, remote string) {
	client, err := s.register(transportWeb, remote, func() {
		_ = ws.Close(websocket.StatusGoingAway, "serial web console stopped")
	})
	if err != nil {
		code := websocket.StatusPolicyViolation
		if errors.Is(err, errBusy) {
			code = websocket.StatusTryAgainLater
		}
		_ = ws.Close(code, err.Error())
		return
	}
	defer s.unregister(client)

	sub := s.hub.subscribe()
	defer s.hub.unsubscribe(sub)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		defer cancel()
		for {
			select {
			case p := <-sub.ch:
				bytesTotal.WithLabelValues(transportWeb, "from_uart").Add(float64(len(p)))
				wctx, wcancel := context.WithTimeout(ctx, clientWriteTimeout)
				err := ws.Write(wctx, websocket.MessageBinary, p)
				wcancel()
				if err != nil {
					return
				}
			case <-sub.done:
				_ = ws.Close(websocket.StatusGoingAway, "serial client fell behind")
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		_, p, err := ws.Read(ctx)
		if err != nil {
			return
		}
		s.toUART(p, transportWeb)
	}
}
