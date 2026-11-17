package kvm

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
	"go.bug.st/serial"
)

type fakeSerialPort struct {
	serial.Port

	rx        chan []byte
	closed    chan struct{}
	closeOnce sync.Once

	readers    atomic.Int32
	maxReaders atomic.Int32

	mu      sync.Mutex
	written bytes.Buffer
}

func newFakeSerialPort() *fakeSerialPort {
	return &fakeSerialPort{rx: make(chan []byte), closed: make(chan struct{})}
}

func (p *fakeSerialPort) Read(buf []byte) (int, error) {
	n := p.readers.Add(1)
	defer p.readers.Add(-1)
	for {
		seen := p.maxReaders.Load()
		if n <= seen || p.maxReaders.CompareAndSwap(seen, n) {
			break
		}
	}

	select {
	case data := <-p.rx:
		return copy(buf, data), nil
	case <-p.closed:
		return 0, errors.New("port closed")
	}
}

func (p *fakeSerialPort) Write(data []byte) (int, error) {
	select {
	case <-p.closed:
		return 0, errors.New("port closed")
	default:
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.written.Write(data)
}

func (p *fakeSerialPort) Close() error {
	p.closeOnce.Do(func() { close(p.closed) })
	return nil
}

func (p *fakeSerialPort) SetMode(*serial.Mode) error { return nil }

func (p *fakeSerialPort) isClosed() bool {
	select {
	case <-p.closed:
		return true
	default:
		return false
	}
}

func (p *fakeSerialPort) writtenString() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.written.String()
}

func (p *fakeSerialPort) deliver(t *testing.T, data string) {
	t.Helper()
	select {
	case p.rx <- []byte(data):
	case <-time.After(2 * time.Second):
		t.Fatalf("no reader took %q from the serial port", data)
	}
}

type fakeSerialPorts struct {
	mu        sync.Mutex
	opened    []*fakeSerialPort
	failOpens int
}

func (f *fakeSerialPorts) failNextOpens(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failOpens = n
}

func (f *fakeSerialPorts) open(*serial.Mode) (serial.Port, error) {
	port := newFakeSerialPort()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOpens > 0 {
		f.failOpens--
		return nil, errors.New("serial port busy")
	}
	f.opened = append(f.opened, port)
	return port, nil
}

func (f *fakeSerialPorts) all() []*fakeSerialPort {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*fakeSerialPort(nil), f.opened...)
}

func (f *fakeSerialPorts) current() *fakeSerialPort {
	ports := f.all()
	return ports[len(ports)-1]
}

type recordingSink struct {
	mu   sync.Mutex
	text strings.Builder
}

func (s *recordingSink) SendText(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.text.WriteString(text)
	return nil
}

func (s *recordingSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.text.String()
}

func useFakeExtensionPort(t *testing.T) *fakeSerialPorts {
	t.Helper()
	ports := &fakeSerialPorts{}
	original := extPort
	extPort = newExtensionPort(ports.open)
	t.Cleanup(func() {
		_ = extPort.activate("")
		extPort = original
	})
	return ports
}

func eventually(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestATXWorkerReceivesEveryLineAfterSerialConsole(t *testing.T) {
	ports := useFakeExtensionPort(t)

	if err := extPort.activate("serial-console"); err != nil {
		t.Fatal(err)
	}
	if err := extPort.activate("atx-power"); err != nil {
		t.Fatal(err)
	}
	port := ports.current()

	for i := range 100 {
		powerOn := i%2 == 0
		line := "1111\n"
		if powerOn {
			line = "1011\n"
		}
		port.deliver(t, line)
		eventually(t, "the ATX power LED state of line "+line, func() bool {
			return ledPWRState.Load() == powerOn
		})
	}
}

func TestDCWorkerReceivesEveryLineAfterSerialConsole(t *testing.T) {
	ports := useFakeExtensionPort(t)

	if err := extPort.activate("serial-console"); err != nil {
		t.Fatal(err)
	}
	if err := extPort.activate("dc-power"); err != nil {
		t.Fatal(err)
	}
	port := ports.current()

	for i := range 100 {
		isOn := i%2 == 0
		line := "0;12000;500;6000\n"
		if isOn {
			line = "1;12000;500;6000\n"
		}
		port.deliver(t, line)
		eventually(t, "the DC power state of line "+line, func() bool {
			return getDCState().IsOn == isOn
		})
	}
}

func TestExtensionSwitchLeavesOneReader(t *testing.T) {
	ports := useFakeExtensionPort(t)

	sequence := []string{
		"atx-power", "serial-console", "dc-power", "", "serial-console",
		"atx-power", "dc-power", "atx-power", "", "dc-power", "serial-console",
	}
	for _, extension := range sequence {
		if err := extPort.activate(extension); err != nil {
			t.Fatal(err)
		}

		opened := ports.all()
		current := opened[len(opened)-1]
		for i, old := range opened[:len(opened)-1] {
			if !old.isClosed() {
				t.Fatalf("port %d is still open after the switch to %q", i, extension)
			}
			if readers := old.readers.Load(); readers != 0 {
				t.Fatalf("port %d still has %d readers after the switch to %q", i, readers, extension)
			}
		}

		if extension == "" {
			if readers := current.readers.Load(); readers != 0 {
				t.Fatalf("%d readers with no extension active", readers)
			}
			continue
		}
		eventually(t, "the reader of "+extension, func() bool {
			return current.readers.Load() == 1
		})
		for range 5 {
			current.deliver(t, "1011\n")
		}
	}

	_ = extPort.activate("")
	for i, port := range ports.all() {
		if readers := port.maxReaders.Load(); readers > 1 {
			t.Fatalf("port %d had %d readers at the same time", i, readers)
		}
	}
}

func TestSerialConsoleReachesChannelOpenedBeforeSwitch(t *testing.T) {
	ports := useFakeExtensionPort(t)

	if err := extPort.activate("atx-power"); err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	extPort.attachSink(sink)

	if err := extPort.activate("serial-console"); err != nil {
		t.Fatal(err)
	}
	port := ports.current()

	port.deliver(t, "login:\n")
	eventually(t, "serial data in the terminal", func() bool {
		return strings.Contains(sink.String(), "login:")
	})

	handleSerialChannelMessage(webrtc.DataChannelMessage{
		IsString: true,
		Data:     []byte(`{"type":"serial","data":"root\n"}`),
	})
	eventually(t, "typed text on the serial port", func() bool {
		return port.writtenString() == "root\n"
	})
}

func TestSerialConsoleUseDuringExtensionSwitch(t *testing.T) {
	ports := useFakeExtensionPort(t)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	loop := func(action func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					action()
				}
			}
		}()
	}

	sink := &recordingSink{}
	loop(func() {
		handleSerialChannelMessage(webrtc.DataChannelMessage{
			IsString: true,
			Data:     []byte(`{"type":"serial","data":"ls\n"}`),
		})
	})
	loop(func() {
		handleSerialChannelMessage(webrtc.DataChannelMessage{
			IsString: true,
			Data:     []byte(`{"type":"system","name":"term.size","data":{}}`),
		})
	})
	loop(func() { handleSerialChannelMessage(webrtc.DataChannelMessage{Data: []byte("raw")}) })
	loop(func() { _ = sendCustomCommand("reboot\n") })
	loop(func() {
		setTerminalPaused(true)
		setTerminalPaused(false)
	})
	loop(func() {
		extPort.setConsoleSettings(consoleSettings{
			mode: defaultMode,
			echo: true,
			norm: NormalizationOptions{Mode: ModeHex, LineEnding: LineEnding_CRLF},
		})
	})
	loop(func() {
		extPort.attachSink(sink)
		extPort.detachSink(sink)
	})

	if err := extPort.activate("serial-console"); err != nil {
		t.Fatal(err)
	}
	loop(func() {
		select {
		case ports.current().rx <- []byte("1011\n"):
		case <-time.After(time.Millisecond):
		}
	})

	extensions := []string{"atx-power", "serial-console", "dc-power", "serial-console", ""}
	for i := range 300 {
		if err := extPort.activate(extensions[i%len(extensions)]); err != nil {
			t.Fatal(err)
		}
	}

	close(stop)
	wg.Wait()
}

func TestFailedSwitchKeepsPreviousExtension(t *testing.T) {
	ports := useFakeExtensionPort(t)

	if err := extPort.activate("atx-power"); err != nil {
		t.Fatal(err)
	}

	ports.failNextOpens(1)
	if err := extPort.activate("serial-console"); err == nil {
		t.Fatal("the switch reported success although the serial port did not open")
	}
	if mux, _ := extPort.console(); mux != nil {
		t.Fatal("the serial console exists after a failed switch")
	}
	if !extPort.active("atx-power") {
		t.Fatal("the ATX extension is not active after a failed switch")
	}

	port := ports.current()
	for _, powerOn := range []bool{true, false, true} {
		line := "1111\n"
		if powerOn {
			line = "1011\n"
		}
		port.deliver(t, line)
		eventually(t, "the ATX power LED state of line "+line, func() bool {
			return ledPWRState.Load() == powerOn
		})
	}

	if err := extPort.activate("serial-console"); err != nil {
		t.Fatal(err)
	}
	if !extPort.active("serial-console") {
		t.Fatal("the serial console is not active after the retry")
	}
}

func TestSameExtensionCanBeRetriedAfterPortFailure(t *testing.T) {
	ports := useFakeExtensionPort(t)

	if err := extPort.activate("atx-power"); err != nil {
		t.Fatal(err)
	}

	ports.failNextOpens(2)
	if err := extPort.activate("dc-power"); err == nil {
		t.Fatal("the switch reported success although the serial port did not open")
	}
	if extPort.active("atx-power") || extPort.active("dc-power") {
		t.Fatal("an extension is reported active without an open serial port")
	}
	if err := extPort.write([]byte("x")); !errors.Is(err, errSerialPortClosed) {
		t.Fatalf("write without a port returned %v", err)
	}

	if err := extPort.activate("atx-power"); err != nil {
		t.Fatal(err)
	}
	port := ports.current()
	port.deliver(t, "1011\n")
	eventually(t, "the ATX power LED to turn on", func() bool { return ledPWRState.Load() })
	port.deliver(t, "1111\n")
	eventually(t, "the ATX power LED to turn off", func() bool { return !ledPWRState.Load() })
}
