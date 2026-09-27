package kvm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.bug.st/serial"
)

// Network access to the extension port UART (jetkvm/kvm#1520).
//
// With the Serial Console extension loaded, the UART can also be reached
// over the network, either as a ser2net-style TCP port (raw or telnet with
// RFC 2217) or as an authenticated WebSocket behind the /serial web console.
// Both go through the SerialMux, which stays the only reader and writer of
// the port, so network clients, the in-session console and quick buttons
// never race each other on the file descriptor.

const (
	SerialNetworkModeDisabled = "disabled"
	SerialNetworkModeSer2Net  = "ser2net"
	SerialNetworkModeWeb      = "web"

	Ser2NetProtocolRaw     = "raw"
	Ser2NetProtocolRFC2217 = "rfc2217"

	defaultSer2NetPort             = 2217
	defaultSerialNetworkMaxClients = 1
	maxSerialNetworkMaxClients     = 8

	serialTransportTCP = "tcp"
	serialTransportWeb = "web"

	// serialClientQueueDepth is how many UART reads (up to 4 KiB each) may
	// queue for one client before it counts as stalled and is dropped. The
	// UART reader never waits on a network client.
	serialClientQueueDepth   = 256
	serialClientWriteTimeout = 30 * time.Second
	serialBreakDuration      = 250 * time.Millisecond
)

var (
	errSerialNetworkDisabled = errors.New("serial network access is disabled")
	errSerialNetworkBusy     = errors.New("too many serial network clients")

	// Ports the device already serves on.
	reservedSer2NetPorts = map[int]string{22: "SSH", 80: "HTTP", 443: "HTTPS"}
)

var (
	serialNetworkClientsGauge = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jetkvm_serial_network_clients",
			Help: "Network clients attached to the extension port UART",
		},
		[]string{"transport"},
	)
	serialNetworkBytesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "jetkvm_serial_network_bytes_total",
			Help: "Bytes relayed between network clients and the extension port UART",
		},
		[]string{"transport", "direction"},
	)
)

// normalizeNetwork fills in defaults for settings files written before
// network access existed.
func (s *SerialSettings) normalizeNetwork() {
	if s.NetworkMode == "" {
		s.NetworkMode = SerialNetworkModeDisabled
	}
	if s.Ser2NetPort == 0 {
		s.Ser2NetPort = defaultSer2NetPort
	}
	if s.Ser2NetProtocol == "" {
		s.Ser2NetProtocol = Ser2NetProtocolRaw
	}
	if s.NetworkMaxClients == 0 {
		s.NetworkMaxClients = defaultSerialNetworkMaxClients
	}
}

func (s *SerialSettings) validateNetwork() error {
	switch s.NetworkMode {
	case SerialNetworkModeDisabled, SerialNetworkModeSer2Net, SerialNetworkModeWeb:
	default:
		return fmt.Errorf("invalid network mode: %s", s.NetworkMode)
	}
	switch s.Ser2NetProtocol {
	case Ser2NetProtocolRaw, Ser2NetProtocolRFC2217:
	default:
		return fmt.Errorf("invalid ser2net protocol: %s", s.Ser2NetProtocol)
	}
	if s.Ser2NetPort < 1 || s.Ser2NetPort > 65535 {
		return fmt.Errorf("invalid ser2net port: %d", s.Ser2NetPort)
	}
	if name, ok := reservedSer2NetPorts[s.Ser2NetPort]; ok {
		return fmt.Errorf("ser2net port %d is already used by %s", s.Ser2NetPort, name)
	}
	if s.NetworkMaxClients < 1 || s.NetworkMaxClients > maxSerialNetworkMaxClients {
		return fmt.Errorf("network max clients must be between 1 and %d", maxSerialNetworkMaxClients)
	}
	return nil
}

type serialNetworkSettings struct {
	Mode       string
	Port       int
	Protocol   string
	MaxClients int
}

func serialNetworkSettingsFrom(s SerialSettings) serialNetworkSettings {
	return serialNetworkSettings{
		Mode:       s.NetworkMode,
		Port:       s.Ser2NetPort,
		Protocol:   s.Ser2NetProtocol,
		MaxClients: s.NetworkMaxClients,
	}
}

/* ---------- RX FAN-OUT ---------- */

// serialRxHub hands every chunk read from the UART to the network clients.
type serialRxHub struct {
	mu   sync.Mutex
	subs map[*serialRxSub]struct{}
}

type serialRxSub struct {
	ch   chan []byte
	done chan struct{}
	once sync.Once
}

func (s *serialRxSub) close() { s.once.Do(func() { close(s.done) }) }

func newSerialRxHub() *serialRxHub {
	return &serialRxHub{subs: map[*serialRxSub]struct{}{}}
}

func (h *serialRxHub) subscribe() *serialRxSub {
	sub := &serialRxSub{
		ch:   make(chan []byte, serialClientQueueDepth),
		done: make(chan struct{}),
	}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

func (h *serialRxHub) unsubscribe(sub *serialRxSub) {
	h.mu.Lock()
	delete(h.subs, sub)
	h.mu.Unlock()
	sub.close()
}

// broadcast must not be handed a slice that the caller reuses; subscribers
// share it read-only.
func (h *serialRxHub) broadcast(p []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		select {
		case sub.ch <- p:
		default:
			serialLogger.Warn().Msg("serial network client is not keeping up; disconnecting it")
			delete(h.subs, sub)
			sub.close()
		}
	}
}

/* ---------- SERVER ---------- */

type serialNetClient struct {
	transport string
	remote    string
	since     time.Time
	kick      func()
}

type SerialNetworkClientInfo struct {
	Transport   string    `json:"transport"`
	Remote      string    `json:"remote"`
	ConnectedAt time.Time `json:"connectedAt"`
}

type SerialNetworkStatus struct {
	Mode          string                    `json:"mode"`
	Protocol      string                    `json:"protocol,omitempty"`
	Running       bool                      `json:"running"`
	ListenAddress string                    `json:"listenAddress,omitempty"`
	MaxClients    int                       `json:"maxClients"`
	Clients       []SerialNetworkClientInfo `json:"clients"`
	Error         string                    `json:"error,omitempty"`
}

type serialNetServer struct {
	hub       *serialRxHub
	write     func(p []byte, source string)
	mode      func() serial.Mode
	setMode   func(serial.Mode) error
	sendBreak func()
	bindAddr  func(port int) string

	mu        sync.Mutex
	active    bool
	settings  serialNetworkSettings
	listener  net.Listener
	listenErr error
	clients   map[*serialNetClient]struct{}
}

func newSerialNetServer(hub *serialRxHub) *serialNetServer {
	return &serialNetServer{
		hub:     hub,
		clients: map[*serialNetClient]struct{}{},
		settings: serialNetworkSettings{
			Mode:       SerialNetworkModeDisabled,
			Port:       defaultSer2NetPort,
			Protocol:   Ser2NetProtocolRaw,
			MaxClients: defaultSerialNetworkMaxClients,
		},
	}
}

// Apply starts, stops or reconfigures network access. active is whether the
// Serial Console extension is loaded. A change drops connected clients;
// re-applying the same settings keeps them.
func (s *serialNetServer) Apply(active bool, st serialNetworkSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active == active && s.settings == st && s.listenErr == nil {
		return nil
	}

	s.stopLocked()
	s.active = active
	s.settings = st
	s.listenErr = nil

	if !active || st.Mode != SerialNetworkModeSer2Net {
		if active && st.Mode == SerialNetworkModeWeb {
			serialLogger.Info().Msg("serial web console enabled")
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
		serialLogger.Error().Err(err).Str("address", addr).Msg("failed to start ser2net listener")
		return s.listenErr
	}
	s.listener = ln
	serialLogger.Info().
		Str("address", ln.Addr().String()).
		Str("protocol", st.Protocol).
		Int("max_clients", st.MaxClients).
		Msg("ser2net listener started")
	go s.acceptLoop(ln, st.Protocol)
	return nil
}

// Stop closes the listener and drops every network client.
func (s *serialNetServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
	s.active = false
	s.listenErr = nil
}

func (s *serialNetServer) stopLocked() {
	if s.listener != nil {
		_ = s.listener.Close()
		serialLogger.Info().Str("address", s.listener.Addr().String()).Msg("ser2net listener stopped")
		s.listener = nil
	}
	for c := range s.clients {
		s.dropLocked(c)
		// A WebSocket close waits for the peer's reply; don't hold s.mu.
		go c.kick()
	}
}

func (s *serialNetServer) Status() SerialNetworkStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := SerialNetworkStatus{
		Mode:       s.settings.Mode,
		MaxClients: s.settings.MaxClients,
		Clients:    []SerialNetworkClientInfo{},
	}
	if s.listenErr != nil {
		st.Error = s.listenErr.Error()
	}
	switch s.settings.Mode {
	case SerialNetworkModeSer2Net:
		st.Protocol = s.settings.Protocol
		st.Running = s.listener != nil
		if s.listener != nil {
			st.ListenAddress = s.listener.Addr().String()
		}
	case SerialNetworkModeWeb:
		st.Running = s.active
	}
	for c := range s.clients {
		st.Clients = append(st.Clients, SerialNetworkClientInfo{
			Transport:   c.transport,
			Remote:      c.remote,
			ConnectedAt: c.since,
		})
	}
	return st
}

func (s *serialNetServer) register(transport, remote string, kick func()) (*serialNetClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	want := SerialNetworkModeSer2Net
	if transport == serialTransportWeb {
		want = SerialNetworkModeWeb
	}
	if !s.active || s.settings.Mode != want {
		return nil, errSerialNetworkDisabled
	}
	if len(s.clients) >= s.settings.MaxClients {
		return nil, errSerialNetworkBusy
	}
	c := &serialNetClient{transport: transport, remote: remote, since: time.Now(), kick: kick}
	s.clients[c] = struct{}{}
	serialNetworkClientsGauge.WithLabelValues(transport).Inc()
	serialLogger.Info().Str("transport", transport).Str("remote", remote).Msg("serial network client connected")
	return c, nil
}

func (s *serialNetServer) unregister(c *serialNetClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropLocked(c)
}

func (s *serialNetServer) dropLocked(c *serialNetClient) {
	if _, ok := s.clients[c]; !ok {
		return
	}
	delete(s.clients, c)
	serialNetworkClientsGauge.WithLabelValues(c.transport).Dec()
	serialLogger.Info().Str("transport", c.transport).Str("remote", c.remote).Msg("serial network client disconnected")
}

func (s *serialNetServer) toUART(p []byte, transport string) {
	if len(p) == 0 || s.write == nil {
		return
	}
	serialNetworkBytesTotal.WithLabelValues(transport, "to_uart").Add(float64(len(p)))
	s.write(p, transport)
}

func (s *serialNetServer) acceptLoop(ln net.Listener, protocol string) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			serialLogger.Warn().Err(err).Msg("ser2net accept failed")
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
	_ = l.conn.SetWriteDeadline(time.Now().Add(serialClientWriteTimeout))
	_, err := l.conn.Write(p)
	return err
}

func (s *serialNetServer) serveTCP(conn net.Conn, protocol string) {
	defer conn.Close()
	remote := conn.RemoteAddr().String()

	client, err := s.register(serialTransportTCP, remote, func() { _ = conn.Close() })
	if err != nil {
		serialLogger.Info().Err(err).Str("remote", remote).Msg("rejected ser2net connection")
		_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
		_, _ = conn.Write([]byte("jetkvm: " + err.Error() + "\r\n"))
		return
	}
	defer s.unregister(client)

	out := &lockedConn{conn: conn}

	var tn *telnetSession
	if protocol == Ser2NetProtocolRFC2217 {
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
				serialNetworkBytesTotal.WithLabelValues(serialTransportTCP, "from_uart").Add(float64(len(p)))
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
			s.toUART(data, serialTransportTCP)
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
			serialLogger.Warn().Err(err).Msg("failed to restore serial mode after RFC 2217 session")
		}
	}
}

func (s *serialNetServer) serveWeb(ctx context.Context, ws *websocket.Conn, remote string) {
	client, err := s.register(serialTransportWeb, remote, func() {
		_ = ws.Close(websocket.StatusGoingAway, "serial web console stopped")
	})
	if err != nil {
		code := websocket.StatusPolicyViolation
		if errors.Is(err, errSerialNetworkBusy) {
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
				serialNetworkBytesTotal.WithLabelValues(serialTransportWeb, "from_uart").Add(float64(len(p)))
				wctx, wcancel := context.WithTimeout(ctx, serialClientWriteTimeout)
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
		s.toUART(p, serialTransportWeb)
	}
}

/* ---------- WIRING ---------- */

var (
	serialRx      = newSerialRxHub()
	serialNetwork = newDeviceSerialNetServer(serialRx)
)

// newDeviceSerialNetServer wires the server to the extension port. The web
// server takes RPCs before initSerialPort runs, so this can't wait for it.
func newDeviceSerialNetServer(hub *serialRxHub) *serialNetServer {
	s := newSerialNetServer(hub)
	s.write = func(p []byte, source string) {
		if m := serialMux; m != nil {
			m.Enqueue(p, source, true, TXUser)
		}
	}
	s.mode = func() serial.Mode { return *serialPortMode }
	s.setMode = func(m serial.Mode) error { return setSerialPortMode(&m) }
	s.sendBreak = func() {
		if port == nil {
			return
		}
		if err := port.Break(serialBreakDuration); err != nil {
			serialLogger.Warn().Err(err).Msg("failed to send serial break")
		}
	}
	s.bindAddr = getBindAddress
	return s
}

func applySerialNetwork(settings SerialSettings) error {
	return serialNetwork.Apply(
		config.ActiveExtension == "serial-console",
		serialNetworkSettingsFrom(settings),
	)
}

func mountSerialConsole() error {
	// Load the stored UART settings now rather than when the UI next opens,
	// so a network client gets the configured line settings after boot.
	settings, err := getSerialSettings()
	if err != nil {
		serialLogger.Warn().Err(err).Msg("failed to load serial settings; using defaults")
	}
	if err := applySerialNetwork(settings); err != nil {
		serialLogger.Error().Err(err).Msg("failed to start serial network access")
		return err
	}
	return nil
}

func unmountSerialConsole() error {
	serialNetwork.Stop()
	return nil
}

func rpcGetSerialNetworkStatus() (SerialNetworkStatus, error) {
	return serialNetwork.Status(), nil
}

// handleSerialWebSocket backs the /serial web console: binary frames carry
// raw UART bytes in both directions.
func handleSerialWebSocket(c *gin.Context) {
	if st := serialNetwork.Status(); st.Mode != SerialNetworkModeWeb || !st.Running {
		c.JSON(http.StatusNotFound, gin.H{"error": "Serial web console is not enabled"})
		return
	}

	// Default options: the Origin must match the Host, since the auth
	// cookie would otherwise let any page open the UART.
	ws, err := websocket.Accept(c.Writer, c.Request, nil)
	if err != nil {
		serialLogger.Warn().Err(err).Msg("failed to accept serial websocket")
		return
	}
	defer func() { _ = ws.CloseNow() }()

	serialNetwork.serveWeb(c.Request.Context(), ws, c.ClientIP())
}
