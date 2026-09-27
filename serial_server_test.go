package kvm

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"go.bug.st/serial"
)

/* ---------- telnet / RFC 2217 ---------- */

func TestTelnetDecodeUnescapesIACAndStripsCRNUL(t *testing.T) {
	tn := newTelnetSession(serial.Mode{}, nil, nil)

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
	tn := newTelnetSession(serial.Mode{BaudRate: 115200}, func(serial.Mode) error { return nil }, nil)

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
	if tn.mode.BaudRate != 9600 || !tn.modeChanged {
		t.Fatalf("mode = %+v changed=%v", tn.mode, tn.modeChanged)
	}
}

func TestTelnetNegotiationDoesNotLoop(t *testing.T) {
	tn := newTelnetSession(serial.Mode{}, nil, nil)
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
	setMode := func(m serial.Mode) error {
		if m.BaudRate == 12345 {
			return errors.New("unsupported")
		}
		applied = append(applied, m)
		return nil
	}
	base := serial.Mode{BaudRate: 115200, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit}
	tn := newTelnetSession(base, setMode, nil)

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
	tn := newTelnetSession(serial.Mode{BaudRate: 255}, nil, nil)
	_, reply := tn.Decode(rfc2217Cmd(rfc2217SetBaudRate, 0, 0, 0, 0))
	want := []byte{telnetIAC, telnetSB, telnetOptComPort, 101, 0, 0, 0, telnetIAC, telnetIAC, telnetIAC, telnetSE}
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply = %v, want %v", reply, want)
	}
}

func TestTelnetBreak(t *testing.T) {
	breaks := 0
	tn := newTelnetSession(serial.Mode{}, nil, func() { breaks++ })
	data, _ := tn.Decode([]byte{'a', telnetIAC, telnetBRK, 'b'})
	if !bytes.Equal(data, []byte("ab")) || breaks != 1 {
		t.Fatalf("data = %q breaks = %d", data, breaks)
	}
}

/* ---------- settings ---------- */

func TestSerialNetworkSettingsDefaultsAndValidation(t *testing.T) {
	var s SerialSettings
	s.normalizeNetwork()
	if s.NetworkMode != SerialNetworkModeDisabled || s.Ser2NetPort != 2217 ||
		s.Ser2NetProtocol != Ser2NetProtocolRaw || s.NetworkMaxClients != 1 {
		t.Fatalf("defaults = %+v", s)
	}
	if err := s.validateNetwork(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}

	bad := []func(*SerialSettings){
		func(s *SerialSettings) { s.NetworkMode = "telnet" },
		func(s *SerialSettings) { s.Ser2NetProtocol = "ssh" },
		func(s *SerialSettings) { s.Ser2NetPort = 70000 },
		func(s *SerialSettings) { s.Ser2NetPort = 80 },
		func(s *SerialSettings) { s.Ser2NetPort = 22 },
		func(s *SerialSettings) { s.NetworkMaxClients = 9 },
	}
	for i, mutate := range bad {
		c := s
		mutate(&c)
		if err := c.validateNetwork(); err == nil {
			t.Errorf("case %d: expected an error for %+v", i, c)
		}
	}
}

/* ---------- fan-out ---------- */

func TestSerialRxHubDropsStalledSubscriber(t *testing.T) {
	h := newSerialRxHub()
	slow := h.subscribe()
	for i := 0; i < serialClientQueueDepth; i++ {
		h.broadcast([]byte{1})
	}
	select {
	case <-slow.done:
		t.Fatal("dropped before the queue was full")
	default:
	}
	h.broadcast([]byte{1})
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

func newTestSerialServer(t *testing.T) (*serialNetServer, *uartRecorder) {
	t.Helper()
	uart := newUARTRecorder()
	s := newSerialNetServer(newSerialRxHub())
	s.write = uart.write
	s.bindAddr = func(int) string { return "127.0.0.1:0" }
	t.Cleanup(s.Stop)
	return s, uart
}

func ser2netSettings(protocol string, maxClients int) serialNetworkSettings {
	return serialNetworkSettings{Mode: SerialNetworkModeSer2Net, Port: 2217, Protocol: protocol, MaxClients: maxClients}
}

// waitClients polls until the server reports n clients; registration runs on
// the accept goroutine.
func waitClients(t *testing.T, s *serialNetServer, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(s.Status().Clients) != n {
		if time.Now().After(deadline) {
			t.Fatalf("clients = %d, want %d", len(s.Status().Clients), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSer2NetRawRelaysBothWays(t *testing.T) {
	s, uart := newTestSerialServer(t)
	if err := s.Apply(true, ser2netSettings(Ser2NetProtocolRaw, 1)); err != nil {
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

	s.hub.broadcast([]byte("login: "))
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
	s, _ := newTestSerialServer(t)
	if err := s.Apply(true, ser2netSettings(Ser2NetProtocolRaw, 1)); err != nil {
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
	s, _ := newTestSerialServer(t)
	if err := s.Apply(true, ser2netSettings(Ser2NetProtocolRaw, 2)); err != nil {
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
	if err := s.Apply(true, ser2netSettings(Ser2NetProtocolRaw, 2)); err != nil {
		t.Fatal(err)
	}
	if n := len(s.Status().Clients); n != 1 {
		t.Fatalf("clients after no-op apply = %d", n)
	}

	// Unloading the extension closes the listener and the session.
	s.Stop()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected the connection to be closed")
	}
	if _, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		t.Fatal("listener still accepting after Stop")
	}
	if st := s.Status(); st.Running || len(st.Clients) != 0 {
		t.Fatalf("status after stop = %+v", st)
	}
}

func TestSer2NetInactiveOrOtherModeDoesNotListen(t *testing.T) {
	s, _ := newTestSerialServer(t)
	if err := s.Apply(false, ser2netSettings(Ser2NetProtocolRaw, 1)); err != nil {
		t.Fatal(err)
	}
	if s.Status().Running {
		t.Fatal("listening while the extension is not loaded")
	}
	if err := s.Apply(true, serialNetworkSettings{Mode: SerialNetworkModeWeb, MaxClients: 1}); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.ListenAddress != "" || !st.Running {
		t.Fatalf("web mode status = %+v", st)
	}
}

func TestSer2NetRFC2217Session(t *testing.T) {
	s, uart := newTestSerialServer(t)
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
	if err := s.Apply(true, ser2netSettings(Ser2NetProtocolRFC2217, 1)); err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("tcp", s.Status().ListenAddress)
	if err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	greeting := newTelnetSession(base, nil, nil).initialNegotiation()
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
	s.hub.broadcast([]byte{'x', telnetIAC})
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
	s, uart := newTestSerialServer(t)
	if err := s.Apply(true, serialNetworkSettings{Mode: SerialNetworkModeWeb, MaxClients: 1}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		s.serveWeb(r.Context(), ws, r.RemoteAddr)
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

	if err := ws.Write(ctx, websocket.MessageBinary, []byte("uname -a\r")); err != nil {
		t.Fatal(err)
	}
	uart.waitFor(t, "uname -a\r")

	s.hub.broadcast([]byte("Linux\r\n"))
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
	if err := s.Apply(true, ser2netSettings(Ser2NetProtocolRaw, 1)); err != nil {
		t.Fatal(err)
	}
	_, _, err = ws.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusGoingAway {
		t.Fatalf("after mode change err = %v", err)
	}
}
