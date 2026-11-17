package kvm

import (
	"errors"

	"github.com/jetkvm/kvm/internal/sync"
	"go.bug.st/serial"
)

var errSerialPortClosed = errors.New("serial port is not open")

type consoleSettings struct {
	mode *serial.Mode
	echo bool
	norm NormalizationOptions
}

type extensionPort struct {
	open func(mode *serial.Mode) (serial.Port, error)

	mu         sync.Mutex
	port       serial.Port
	stop       chan struct{}
	readerDone <-chan struct{}
	mux        *SerialMux
	broker     *ConsoleBroker
	sink       Sink
	settings   consoleSettings
}

func newExtensionPort(open func(mode *serial.Mode) (serial.Port, error)) *extensionPort {
	return &extensionPort{
		open: open,
		settings: consoleSettings{
			mode: defaultMode,
			norm: NormalizationOptions{Mode: ModeNames, LineEnding: LineEnding_LF, PreserveANSI: true},
		},
	}
}

var extPort = newExtensionPort(func(mode *serial.Mode) (serial.Port, error) {
	return serial.Open(serialPortPath, mode)
})

func (e *extensionPort) activate(extension string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.deactivate()

	mode := defaultMode
	if extension == "serial-console" {
		mode = e.settings.mode
	}
	port, err := e.open(mode)
	if err != nil {
		serialLogger.Error().
			Err(err).
			Str("path", serialPortPath).
			Interface("mode", mode).
			Msg("Error opening serial port")
		return err
	}
	e.port = port

	stop := make(chan struct{})
	e.stop = stop

	switch extension {
	case "atx-power":
		e.readerDone = startPortReader(func() { runATXControl(port, stop) })
	case "dc-power":
		registerDCMetrics()
		e.readerDone = startPortReader(func() { runDCControl(port, stop) })
	case "serial-console":
		e.broker = NewConsoleBroker(e.sink, e.settings.norm)
		e.broker.Start()
		e.mux = NewSerialMux(port, e.broker)
		e.mux.SetEchoEnabled(e.settings.echo)
		e.mux.Start()
		e.readerDone = e.mux.readerDone
	}
	return nil
}

func (e *extensionPort) deactivate() {
	if e.port == nil {
		return
	}
	close(e.stop)
	if e.mux != nil {
		e.mux.Close()
		e.broker.Close()
	}
	e.port.Close()
	if e.readerDone != nil {
		<-e.readerDone
	}
	e.port, e.stop, e.readerDone, e.mux, e.broker = nil, nil, nil, nil, nil
}

func startPortReader(run func()) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		run()
	}()
	return done
}

func (e *extensionPort) write(data []byte) error {
	e.mu.Lock()
	port := e.port
	e.mu.Unlock()

	if port == nil {
		return errSerialPortClosed
	}
	_, err := port.Write(data)
	return err
}

func (e *extensionPort) console() (*SerialMux, *ConsoleBroker) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.mux, e.broker
}

func (e *extensionPort) setConsoleSettings(settings consoleSettings) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.settings = settings
	if e.mux == nil {
		return
	}
	_ = e.port.SetMode(settings.mode)
	e.mux.SetEchoEnabled(settings.echo)
	e.broker.SetNormOptions(settings.norm)
}

func (e *extensionPort) attachSink(sink Sink) {
	e.mu.Lock()
	e.sink = sink
	broker := e.broker
	if broker != nil {
		broker.SetSink(sink)
	}
	e.mu.Unlock()

	if broker != nil {
		broker.Enqueue(consoleEvent{kind: evRX, data: []byte("[serial attached]\n")})
	}
}

func (e *extensionPort) detachSink(sink Sink) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.sink == sink {
		e.sink = nil
	}
	if e.broker != nil {
		e.broker.ClearSink(sink)
	}
}
