package serialnet

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"go.bug.st/serial"
)

/* ---------- telnet / RFC 2217 ---------- */

func TestTelnetDecodeUnescapesIACAndStripsCRNUL(t *testing.T) {
	tn := newTelnetSession(serial.Mode{}, nil, nil, nil)

	data, reply := tn.Decode([]byte{'a', telnetIAC, telnetIAC, 'b', '\r', 0, 'c'})
	if want := []byte{'a', telnetIAC, 'b', '\r', 'c'}; !bytes.Equal(data, want) {
		t.Fatalf("data = %v, want %v", data, want)
	}
	if len(reply) != 0 {
		t.Fatalf("unexpected reply %v", reply)
	}

	// In binary mode CR NUL is data.
	tn.they[telnetOptBinary] = true
	data, _ = tn.Decode([]byte{'\r', 0})
	if want := []byte{'\r', 0}; !bytes.Equal(data, want) {
		t.Fatalf("binary data = %v, want %v", data, want)
	}
}

func TestTelnetDecodeAcrossChunks(t *testing.T) {
	tn := newTelnetSession(serial.Mode{BaudRate: 115200}, func(m serial.Mode) (serial.Mode, error) { return m, nil }, nil, nil)

	msg := []byte{telnetIAC, telnetSB, telnetOptComPort, rfc2217SetBaudRate, 0, 0, 0x25, 0x80, telnetIAC, telnetSE, 'x'}
	var data, reply []byte
	for _, b := range msg {
		d, r := tn.Decode([]byte{b})
		data = append(data, d...)
		reply = append(reply, r...)
	}
	if !bytes.Equal(data, []byte{'x'}) {
		t.Fatalf("data = %v", data)
	}
	want := []byte{telnetIAC, telnetSB, telnetOptComPort, rfc2217SetBaudRate + rfc2217ServerOffset, 0, 0, 0x25, 0x80, telnetIAC, telnetSE}
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply = %v, want %v", reply, want)
	}
	if tn.mode.BaudRate != 9600 {
		t.Fatalf("mode = %+v", tn.mode)
	}
}

func TestTelnetNegotiationDoesNotLoop(t *testing.T) {
	tn := newTelnetSession(serial.Mode{}, nil, nil, nil)
	_ = tn.initialNegotiation()

	cases := []struct {
		name string
		in   []byte
		want []byte
	}{
		{"ack of our WILL ECHO", []byte{telnetIAC, telnetDO, telnetOptEcho}, nil},
		{"ack of our DO BINARY", []byte{telnetIAC, telnetWILL, telnetOptBinary}, nil},
		{"client offers RFC 2217", []byte{telnetIAC, telnetWILL, telnetOptComPort}, []byte{telnetIAC, telnetDO, telnetOptComPort}},
		{"client offers RFC 2217 again", []byte{telnetIAC, telnetWILL, telnetOptComPort}, nil},
		{"unsupported DO", []byte{telnetIAC, telnetDO, 24}, []byte{telnetIAC, telnetWONT, 24}},
		{"unsupported WILL", []byte{telnetIAC, telnetWILL, 31}, []byte{telnetIAC, telnetDONT, 31}},
		{"client refuses echo", []byte{telnetIAC, telnetDONT, telnetOptEcho}, []byte{telnetIAC, telnetWONT, telnetOptEcho}},
		{"client refuses echo again", []byte{telnetIAC, telnetDONT, telnetOptEcho}, nil},
	}
	for _, tc := range cases {
		_, reply := tn.Decode(tc.in)
		if !bytes.Equal(reply, tc.want) {
			t.Errorf("%s: reply = %v, want %v", tc.name, reply, tc.want)
		}
	}
}

func rfc2217Cmd(cmd byte, val ...byte) []byte {
	out := []byte{telnetIAC, telnetSB, telnetOptComPort, cmd}
	out = append(out, telnetEscapeIAC(val)...)
	return append(out, telnetIAC, telnetSE)
}

func TestRFC2217LineSettings(t *testing.T) {
	var applied []serial.Mode
	base := serial.Mode{BaudRate: 115200, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit}
	current := base
	setMode := func(m serial.Mode) (serial.Mode, error) {
		if m.BaudRate == 12345 {
			return current, errors.New("unsupported")
		}
		applied = append(applied, m)
		current = m
		return m, nil
	}
	tn := newTelnetSession(base, setMode, nil, nil)

	cases := []struct {
		name string
		in   []byte
		want []byte
	}{
		{"query baud", rfc2217Cmd(rfc2217SetBaudRate, 0, 0, 0, 0), rfc2217Reply(rfc2217SetBaudRate, []byte{0, 1, 0xC2, 0x00})},
		{"rejected baud keeps old", rfc2217Cmd(rfc2217SetBaudRate, 0, 0, 0x30, 0x39), rfc2217Reply(rfc2217SetBaudRate, []byte{0, 1, 0xC2, 0x00})},
		{"set data size 7", rfc2217Cmd(rfc2217SetDataSize, 7), rfc2217Reply(rfc2217SetDataSize, []byte{7})},
		{"set parity even", rfc2217Cmd(rfc2217SetParity, 3), rfc2217Reply(rfc2217SetParity, []byte{3})},
		{"query parity", rfc2217Cmd(rfc2217SetParity, 0), rfc2217Reply(rfc2217SetParity, []byte{3})},
		{"set stop size 2", rfc2217Cmd(rfc2217SetStopSize, 2), rfc2217Reply(rfc2217SetStopSize, []byte{2})},
		{"flow control only none", rfc2217Cmd(rfc2217SetControl, 2), rfc2217Reply(rfc2217SetControl, []byte{1})},
		{"DTR on acknowledged", rfc2217Cmd(rfc2217SetControl, 8), rfc2217Reply(rfc2217SetControl, []byte{8})},
		{"signature query", rfc2217Cmd(rfc2217Signature), rfc2217Reply(rfc2217Signature, []byte("JetKVM"))},
		{"client signature", rfc2217Cmd(rfc2217Signature, 'p', 'y'), nil},
		{"purge", rfc2217Cmd(rfc2217PurgeData, 3), rfc2217Reply(rfc2217PurgeData, []byte{3})},
		{"suspend", rfc2217Cmd(rfc2217FlowControlSuspend), nil},
	}
	for _, tc := range cases {
		_, reply := tn.Decode(tc.in)
		if !bytes.Equal(reply, tc.want) {
			t.Errorf("%s: reply = %v, want %v", tc.name, reply, tc.want)
		}
	}

	want := serial.Mode{BaudRate: 115200, DataBits: 7, Parity: serial.EvenParity, StopBits: serial.TwoStopBits}
	if tn.mode != want {
		t.Fatalf("mode = %+v, want %+v", tn.mode, want)
	}
	if len(applied) != 3 {
		t.Fatalf("applied %d modes, want 3", len(applied))
	}
}

func TestRFC2217ReplyEscapesIAC(t *testing.T) {
	// 0xFF in a baud rate must be doubled inside the subnegotiation.
	tn := newTelnetSession(serial.Mode{BaudRate: 255}, nil, nil, nil)
	_, reply := tn.Decode(rfc2217Cmd(rfc2217SetBaudRate, 0, 0, 0, 0))
	want := []byte{telnetIAC, telnetSB, telnetOptComPort, 101, 0, 0, 0, telnetIAC, telnetIAC, telnetIAC, telnetSE}
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply = %v, want %v", reply, want)
	}
}

func TestTelnetBreak(t *testing.T) {
	breaks := 0
	tn := newTelnetSession(serial.Mode{}, nil, func() { breaks++ }, nil)
	data, _ := tn.Decode([]byte{'a', telnetIAC, telnetBRK, 'b'})
	if !bytes.Equal(data, []byte("ab")) || breaks != 1 {
		t.Fatalf("data = %q breaks = %d", data, breaks)
	}
}

/* ---------- settings ---------- */

func TestSettingsDefaultsAndValidation(t *testing.T) {
	var s Settings
	s.Normalize()
	if s != (Settings{Mode: ModeDisabled, Port: 2217, Protocol: ProtocolRaw, MaxClients: 1}) {
		t.Fatalf("defaults = %+v", s)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}

	bad := []func(*Settings){
		func(s *Settings) { s.Mode = "telnet" },
		func(s *Settings) { s.Protocol = "ssh" },
		func(s *Settings) { s.Port = 70000 },
		func(s *Settings) { s.Port = 80 },
		func(s *Settings) { s.Port = 22 },
		func(s *Settings) { s.MaxClients = 9 },
	}
	for i, mutate := range bad {
		c := s
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("case %d: expected an error for %+v", i, c)
		}
	}
}

/* ---------- fan-out ---------- */

func TestHubDropsStalledSubscriber(t *testing.T) {
	h := NewHub()
	slow := h.subscribe()
	for i := 0; i < clientQueueDepth; i++ {
		h.Broadcast([]byte{1})
	}
	select {
	case <-slow.done:
		t.Fatal("dropped before the queue was full")
	default:
	}
	h.Broadcast([]byte{1})
	select {
	case <-slow.done:
	default:
		t.Fatal("stalled subscriber was not dropped")
	}
}

/* ---------- server ---------- */

type uartRecorder struct {
	mu   sync.Mutex
	data []byte
	src  []string
	ch   chan struct{}
}

func newUARTRecorder() *uartRecorder { return &uartRecorder{ch: make(chan struct{}, 64)} }

func (u *uartRecorder) write(p []byte, source string) {
	u.mu.Lock()
	u.data = append(u.data, p...)
	u.src = append(u.src, source)
	u.mu.Unlock()
	u.ch <- struct{}{}
}

func (u *uartRecorder) waitFor(t *testing.T, want string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		u.mu.Lock()
		got := string(u.data)
		u.mu.Unlock()
		if got == want {
			return
		}
		select {
		case <-u.ch:
		case <-deadline:
			t.Fatalf("UART got %q, want %q", got, want)
		}
	}
}

func newTestServer(t *testing.T) (*Server, *uartRecorder) {
	t.Helper()
	uart := newUARTRecorder()
	s := NewServer(NewHub(), Deps{
		Write:    uart.write,
		BindAddr: func(int) string { return "127.0.0.1:0" },
	})
	t.Cleanup(s.Stop)
	return s, uart
}

func ser2netSettings(protocol string, maxClients int) Settings {
	return Settings{Mode: ModeSer2Net, Port: 2217, Protocol: protocol, MaxClients: maxClients}
}

// waitClients polls until the server reports n clients; registration runs on
// the accept goroutine.
func waitClients(t *testing.T, s *Server, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(s.Status().Clients) != n {
		if time.Now().After(deadline) {
			t.Fatalf("clients = %d, want %d", len(s.Status().Clients), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitSubscribers polls until n clients receive UART output. A client counts
// as connected a moment before it subscribes, so broadcasting right after
// waitClients could miss it.
func waitSubscribers(t *testing.T, s *Server, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.hub.mu.Lock()
		got := len(s.hub.subs)
		s.hub.mu.Unlock()
		if got == n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("subscribers = %d, want %d", got, n)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSer2NetRawRelaysBothWays(t *testing.T) {
	s, uart := newTestServer(t)
	if err := s.Apply(true, ser2netSettings(ProtocolRaw, 1)); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	if !st.Running || st.ListenAddress == "" {
		t.Fatalf("status = %+v", st)
	}

	conn, err := net.Dial("tcp", st.ListenAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	waitClients(t, s, 1)
	waitSubscribers(t, s, 1)

	s.hub.Broadcast([]byte("login: "))
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 7)
	if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "login: " {
		t.Fatalf("read %q, %v", buf, err)
	}

	// Raw mode passes IAC through untouched.
	if _, err := conn.Write([]byte{'r', 'o', 'o', 't', telnetIAC, '\n'}); err != nil {
		t.Fatal(err)
	}
	uart.waitFor(t, "root\xff\n")
}

func TestSer2NetRejectsClientsOverLimit(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.Apply(true, ser2netSettings(ProtocolRaw, 1)); err != nil {
		t.Fatal(err)
	}
	addr := s.Status().ListenAddress

	first, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	waitClients(t, s, 1)

	second, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	_ = second.SetReadDeadline(time.Now().Add(2 * time.Second))
	msg, _ := io.ReadAll(second)
	if !strings.Contains(string(msg), "too many serial network clients") {
		t.Fatalf("second client got %q", msg)
	}
}

func TestSer2NetStopDropsClients(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.Apply(true, ser2netSettings(ProtocolRaw, 2)); err != nil {
		t.Fatal(err)
	}
	addr := s.Status().ListenAddress
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	waitClients(t, s, 1)

	// Re-applying identical settings keeps the session.
	if err := s.Apply(true, ser2netSettings(ProtocolRaw, 2)); err != nil {
		t.Fatal(err)
	}
	if n := len(s.Status().Clients); n != 1 {
		t.Fatalf("clients after no-op apply = %d", n)
	}

	// Unloading the extension closes the listener and the session.
	s.Stop()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	// A closed connection reads EOF or a reset; a timeout means it stayed open.
	var netErr net.Error
	if _, err := conn.Read(make([]byte, 1)); err == nil || (errors.As(err, &netErr) && netErr.Timeout()) {
		t.Fatalf("expected the connection to be closed, read returned %v", err)
	}
	if _, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		t.Fatal("listener still accepting after Stop")
	}
	if st := s.Status(); st.Running || len(st.Clients) != 0 {
		t.Fatalf("status after stop = %+v", st)
	}
}

func TestSer2NetInactiveOrOtherModeDoesNotListen(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.Apply(false, ser2netSettings(ProtocolRaw, 1)); err != nil {
		t.Fatal(err)
	}
	if s.Status().Running {
		t.Fatal("listening while the extension is not loaded")
	}
	if err := s.Apply(true, Settings{Mode: ModeWeb, MaxClients: 1}); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.ListenAddress != "" || !st.Running {
		t.Fatalf("web mode status = %+v", st)
	}
}

func TestSer2NetRFC2217Session(t *testing.T) {
	s, uart := newTestServer(t)
	base := serial.Mode{BaudRate: 115200, DataBits: 8}
	var mu sync.Mutex
	var applied []serial.Mode
	s.mode = func() serial.Mode { return base }
	s.setMode = func(m serial.Mode) error {
		mu.Lock()
		applied = append(applied, m)
		mu.Unlock()
		return nil
	}
	if err := s.Apply(true, ser2netSettings(ProtocolRFC2217, 1)); err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("tcp", s.Status().ListenAddress)
	if err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	greeting := newTelnetSession(base, nil, nil, nil).initialNegotiation()
	got := make([]byte, len(greeting))
	if _, err := io.ReadFull(r, got); err != nil || !bytes.Equal(got, greeting) {
		t.Fatalf("greeting = %v, %v", got, err)
	}

	req := append(rfc2217Cmd(rfc2217SetBaudRate, 0, 0, 0x25, 0x80), 'h', 'i', telnetIAC, telnetIAC)
	if _, err := conn.Write(req); err != nil {
		t.Fatal(err)
	}
	want := rfc2217Reply(rfc2217SetBaudRate, []byte{0, 0, 0x25, 0x80})
	got = make([]byte, len(want))
	if _, err := io.ReadFull(r, got); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("baud reply = %v, %v", got, err)
	}
	uart.waitFor(t, "hi\xff")

	// UART output containing IAC reaches the client escaped.
	waitSubscribers(t, s, 1)
	s.hub.Broadcast([]byte{'x', telnetIAC})
	got = make([]byte, 3)
	if _, err := io.ReadFull(r, got); err != nil || !bytes.Equal(got, []byte{'x', telnetIAC, telnetIAC}) {
		t.Fatalf("escaped data = %v, %v", got, err)
	}

	// The configured mode comes back once the client leaves.
	conn.Close()
	waitClients(t, s, 0)
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(applied)
		var last serial.Mode
		if n > 0 {
			last = applied[n-1]
		}
		mu.Unlock()
		if n == 2 && last == base {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("applied = %+v", applied)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if applied[0].BaudRate != 9600 {
		t.Fatalf("first applied mode = %+v", applied[0])
	}
}

func TestSerialWebSocketRelaysBothWays(t *testing.T) {
	s, uart := newTestServer(t)
	if err := s.Apply(true, Settings{Mode: ModeWeb, MaxClients: 1}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		s.ServeWeb(r.Context(), ws, r.RemoteAddr, WebAuth{})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	waitClients(t, s, 1)
	waitSubscribers(t, s, 1)

	if err := ws.Write(ctx, websocket.MessageBinary, []byte("uname -a\r")); err != nil {
		t.Fatal(err)
	}
	uart.waitFor(t, "uname -a\r")

	s.hub.Broadcast([]byte("Linux\r\n"))
	_, p, err := ws.Read(ctx)
	if err != nil || string(p) != "Linux\r\n" {
		t.Fatalf("read %q, %v", p, err)
	}

	// A second browser tab is turned away with a retry-later close.
	ws2, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws2.CloseNow()
	_, _, err = ws2.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusTryAgainLater {
		t.Fatalf("second client err = %v", err)
	}

	// Switching to ser2net closes the web session.
	if err := s.Apply(true, ser2netSettings(ProtocolRaw, 1)); err != nil {
		t.Fatal(err)
	}
	_, _, err = ws.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusGoingAway {
		t.Fatalf("after mode change err = %v", err)
	}
}

/* ---------- review follow-ups ---------- */

func TestRFC2217BreakOnSendsBreak(t *testing.T) {
	breaks := 0
	tn := newTelnetSession(serial.Mode{}, nil, func() { breaks++ }, nil)

	for _, tc := range []struct {
		name  string
		value byte
		want  int // breaks sent so far
	}{
		{"query break state", 4, 0},
		{"break on", rfc2217BreakOn, 1},
		{"break off", 6, 1},
	} {
		_, reply := tn.Decode(rfc2217Cmd(rfc2217SetControl, tc.value))
		if len(reply) == 0 {
			t.Errorf("%s: no reply", tc.name)
		}
		if breaks != tc.want {
			t.Errorf("%s: breaks = %d, want %d", tc.name, breaks, tc.want)
		}
	}
}

func TestRFC2217PurgeDropsQueuedReceiveData(t *testing.T) {
	purges := 0
	tn := newTelnetSession(serial.Mode{}, nil, nil, func() { purges++ })

	for _, value := range []byte{1, 2, 3} {
		_, reply := tn.Decode(rfc2217Cmd(rfc2217PurgeData, value))
		if !bytes.Equal(reply, rfc2217Reply(rfc2217PurgeData, []byte{value})) {
			t.Errorf("purge %d: reply = %v", value, reply)
		}
	}
	// Receive (1) and both (3) purge; transmit (2) has nothing to purge.
	if purges != 2 {
		t.Fatalf("purges = %d, want 2", purges)
	}

	h := NewHub()
	sub := h.subscribe()
	h.Broadcast([]byte("a"))
	h.Broadcast([]byte("b"))
	sub.purge()
	if n := len(sub.ch); n != 0 {
		t.Fatalf("%d chunks left after purge", n)
	}
}

// dialRFC2217 connects, reads the server's opening negotiation and returns the
// connection with a reader for its replies.
func dialRFC2217(t *testing.T, s *Server) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("tcp", s.Status().ListenAddress)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(conn)
	greeting := newTelnetSession(serial.Mode{}, nil, nil, nil).initialNegotiation()
	if _, err := io.ReadFull(r, make([]byte, len(greeting))); err != nil {
		t.Fatal(err)
	}
	return conn, r
}

// setBaud asks for a baud rate and returns the one the server reports.
func setBaud(t *testing.T, conn net.Conn, r *bufio.Reader, baud uint32) uint32 {
	t.Helper()
	req := make([]byte, 4)
	binary.BigEndian.PutUint32(req, baud)
	if _, err := conn.Write(rfc2217Cmd(rfc2217SetBaudRate, req...)); err != nil {
		t.Fatal(err)
	}
	// IAC SB COM-PORT <cmd+100> <4 bytes> IAC SE, assuming no byte is 0xFF.
	reply := make([]byte, 10)
	if _, err := io.ReadFull(r, reply); err != nil {
		t.Fatal(err)
	}
	return binary.BigEndian.Uint32(reply[4:8])
}

func TestRFC2217LineSettingsBelongToOneClient(t *testing.T) {
	s, _ := newTestServer(t)
	base := serial.Mode{BaudRate: 115200, DataBits: 8}
	var mu sync.Mutex
	var applied []serial.Mode
	s.mode = func() serial.Mode { return base }
	s.setMode = func(m serial.Mode) error {
		mu.Lock()
		applied = append(applied, m)
		mu.Unlock()
		return nil
	}
	appliedModes := func() []serial.Mode {
		mu.Lock()
		defer mu.Unlock()
		return append([]serial.Mode(nil), applied...)
	}
	if err := s.Apply(true, ser2netSettings(ProtocolRFC2217, 2)); err != nil {
		t.Fatal(err)
	}

	a, ar := dialRFC2217(t, s)
	b, br := dialRFC2217(t, s)
	waitClients(t, s, 2)

	// The first client to change the settings owns them.
	if got := setBaud(t, a, ar, 9600); got != 9600 {
		t.Fatalf("owner's baud rate = %d", got)
	}
	// The second is refused and is told what is really set.
	if got := setBaud(t, b, br, 19200); got != 9600 {
		t.Fatalf("refused client was told %d, want the owner's 9600", got)
	}
	if n := len(appliedModes()); n != 1 {
		t.Fatalf("applied %d modes, want only the owner's", n)
	}

	// The owner leaving restores the configured mode once, and the other
	// client can then take over.
	a.Close()
	waitClients(t, s, 1)
	deadline := time.Now().Add(2 * time.Second)
	for len(appliedModes()) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("applied = %+v", appliedModes())
		}
		time.Sleep(time.Millisecond)
	}
	if modes := appliedModes(); modes[1] != base {
		t.Fatalf("restored %+v, want %+v", modes[1], base)
	}
	if got := setBaud(t, b, br, 19200); got != 19200 {
		t.Fatalf("second client's baud rate after takeover = %d", got)
	}
}

func TestStopEndsLineSettingsChanges(t *testing.T) {
	s, _ := newTestServer(t)
	changed := 0
	s.mode = func() serial.Mode { return serial.Mode{BaudRate: 115200} }
	s.setMode = func(serial.Mode) error { changed++; return nil }
	if err := s.Apply(true, ser2netSettings(ProtocolRFC2217, 1)); err != nil {
		t.Fatal(err)
	}
	dialRFC2217(t, s)
	waitClients(t, s, 1)

	s.mu.Lock()
	var c *client
	for c = range s.clients {
	}
	s.mu.Unlock()

	// A client that is still winding down after Stop must not reach the port.
	s.Stop()
	if _, err := s.setLineModeFor(c, serial.Mode{BaudRate: 9600}); err == nil {
		t.Fatal("a dropped client changed the line settings")
	}
	if changed != 0 {
		t.Fatalf("setMode ran %d times after Stop", changed)
	}
}

func TestConnectionAcceptedBeforeReconfigureIsRefused(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.Apply(true, ser2netSettings(ProtocolRaw, 1)); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	old := s.gen
	s.mu.Unlock()

	// Same mode, new protocol: the listener restarts under a new generation.
	if err := s.Apply(true, ser2netSettings(ProtocolRFC2217, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.register(transportTCP, "late", old, func() {}); !errors.Is(err, errDisabled) {
		t.Fatalf("a connection from the old generation registered: %v", err)
	}
	s.mu.Lock()
	cur := s.gen
	s.mu.Unlock()
	c, err := s.register(transportTCP, "current", cur, func() {})
	if err != nil {
		t.Fatal(err)
	}
	s.unregister(c)
}

func webAuthServer(t *testing.T, auth WebAuth) (*Server, *uartRecorder, string) {
	t.Helper()
	s, uart := newTestServer(t)
	if err := s.Apply(true, Settings{Mode: ModeWeb, MaxClients: 1}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		s.ServeWeb(r.Context(), ws, r.RemoteAddr, auth)
	}))
	t.Cleanup(srv.Close)
	return s, uart, "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestWebConsoleEndsWhenLoginEnds(t *testing.T) {
	old := webAuthCheckInterval
	webAuthCheckInterval = 10 * time.Millisecond
	t.Cleanup(func() { webAuthCheckInterval = old })

	var valid atomic.Bool
	valid.Store(true)
	_, _, url := webAuthServer(t, WebAuth{Valid: valid.Load})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()

	// Logging out closes an open console, with nobody typing.
	valid.Store(false)
	_, _, err = ws.Read(ctx)
	if websocket.CloseStatus(err) != CloseSessionExpired {
		t.Fatalf("close status = %v (%v), want %v", websocket.CloseStatus(err), err, CloseSessionExpired)
	}
}

func TestWebConsoleInputAfterLoginEndsReachesNothing(t *testing.T) {
	var valid atomic.Bool
	valid.Store(true)
	var activity atomic.Int32
	s, uart, url := webAuthServer(t, WebAuth{Valid: valid.Load, Activity: func() { activity.Add(1) }})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	waitClients(t, s, 1)

	if err := ws.Write(ctx, websocket.MessageBinary, []byte("ls\r")); err != nil {
		t.Fatal(err)
	}
	uart.waitFor(t, "ls\r")
	if activity.Load() != 1 {
		t.Fatalf("typing counted as activity %d times, want 1", activity.Load())
	}

	// Before the periodic check notices, input is already refused.
	valid.Store(false)
	_ = ws.Write(ctx, websocket.MessageBinary, []byte("reboot\r"))
	_, _, err = ws.Read(ctx)
	if websocket.CloseStatus(err) != CloseSessionExpired {
		t.Fatalf("close status = %v (%v), want %v", websocket.CloseStatus(err), err, CloseSessionExpired)
	}
	uart.mu.Lock()
	got := string(uart.data)
	uart.mu.Unlock()
	if got != "ls\r" {
		t.Fatalf("UART got %q, want only the input from before the login ended", got)
	}
	if activity.Load() != 1 {
		t.Fatalf("input after the login ended counted as activity")
	}
}
