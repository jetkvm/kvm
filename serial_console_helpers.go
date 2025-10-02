package kvm

import (
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
	"go.bug.st/serial"
)

/* ---------- SINK (terminal output) ---------- */

type Sink interface {
	SendText(s string) error
}

type dataChannelSink struct{ d *webrtc.DataChannel }

func (s dataChannelSink) SendText(str string) error { return s.d.SendText(str) }

/* ---------- NORMALIZATION (applies to RX & TX) ---------- */

type NormalizeMode int

const (
	ModeCaret NormalizeMode = iota // ^C ^M ^?
	ModeNames                      // <CR>, <LF>, <ESC>, …
	ModeHex                        // \x1B
)

type CRLFMode int

const (
	CRLFAsIs CRLFMode = iota
	CRLF_CRLF
	CRLF_LF
	CRLF_CR
)

type NormOptions struct {
	Mode         NormalizeMode
	CRLF         CRLFMode
	TabRender    string // e.g. "    " or "" to keep '\t'
	PreserveANSI bool
}

func normalize(in []byte, opt NormOptions) string {
	var out strings.Builder
	esc := byte(0x1B)
	for i := 0; i < len(in); {
		b := in[i]

		// ANSI preservation (CSI/OSC)
		if opt.PreserveANSI && b == esc && i+1 < len(in) {
			if in[i+1] == '[' { // CSI
				j := i + 2
				for j < len(in) {
					c := in[j]
					if c >= 0x40 && c <= 0x7E {
						j++
						break
					}
					j++
				}
				out.Write(in[i:j])
				i = j
				continue
			} else if in[i+1] == ']' { // OSC ... BEL or ST
				j := i + 2
				for j < len(in) {
					if in[j] == 0x07 {
						j++
						break
					} // BEL
					if j+1 < len(in) && in[j] == esc && in[j+1] == '\\' {
						j += 2
						break
					} // ST
					j++
				}
				out.Write(in[i:j])
				i = j
				continue
			}
		}

		// CR/LF normalization
		if b == '\r' || b == '\n' {
			switch opt.CRLF {
			case CRLFAsIs:
				out.WriteByte(b)
				i++
			case CRLF_CRLF:
				if i+1 < len(in) && ((b == '\r' && in[i+1] == '\n') || (b == '\n' && in[i+1] == '\r')) {
					out.WriteString("\r\n")
					i += 2
				} else {
					out.WriteString("\r\n")
					i++
				}
			case CRLF_LF:
				if i+1 < len(in) && ((b == '\r' && in[i+1] == '\n') || (b == '\n' && in[i+1] == '\r')) {
					i += 2
				} else {
					i++
				}
				out.WriteByte('\n')
			case CRLF_CR:
				if i+1 < len(in) && ((b == '\r' && in[i+1] == '\n') || (b == '\n' && in[i+1] == '\r')) {
					i += 2
				} else {
					i++
				}
				out.WriteByte('\r')
			}
			continue
		}

		// Tabs
		if b == '\t' {
			if opt.TabRender != "" {
				out.WriteString(opt.TabRender)
			} else {
				out.WriteByte('\t')
			}
			i++
			continue
		}

		// Controls
		if b < 0x20 || b == 0x7F {
			switch opt.Mode {
			case ModeCaret:
				if b == 0x7F {
					out.WriteString("^?")
				} else {
					out.WriteByte('^')
					out.WriteByte(byte('@' + b))
				}
			case ModeNames:
				names := map[byte]string{
					0: "NUL", 1: "SOH", 2: "STX", 3: "ETX", 4: "EOT", 5: "ENQ", 6: "ACK", 7: "BEL",
					8: "BS", 9: "TAB", 10: "LF", 11: "VT", 12: "FF", 13: "CR", 14: "SO", 15: "SI",
					16: "DLE", 17: "DC1", 18: "DC2", 19: "DC3", 20: "DC4", 21: "NAK", 22: "SYN", 23: "ETB",
					24: "CAN", 25: "EM", 26: "SUB", 27: "ESC", 28: "FS", 29: "GS", 30: "RS", 31: "US", 127: "DEL",
				}
				if n, ok := names[b]; ok {
					out.WriteString("<" + n + ">")
				} else {
					out.WriteString(fmt.Sprintf("0x%02X", b))
				}
			case ModeHex:
				out.WriteString(fmt.Sprintf("\\x%02X", b))
			}
			i++
			continue
		}

		out.WriteByte(b)
		i++
	}
	return out.String()
}

/* ---------- CONSOLE BROKER (ordering + normalization + RX/TX) ---------- */

type consoleEventKind int

const (
	evRX consoleEventKind = iota
	evTX                  // local echo after a successful write
)

type consoleEvent struct {
	kind consoleEventKind
	data []byte
}

type ConsoleBroker struct {
	sink Sink
	in   chan consoleEvent
	done chan struct{}

	// line-aware echo
	rxAtLineEnd bool
	pendingTX   *consoleEvent
	quietTimer  *time.Timer
	quietAfter  time.Duration

	// normalization
	norm NormOptions

	// labels
	labelRX string
	labelTX string
}

func NewConsoleBroker(s Sink, norm NormOptions) *ConsoleBroker {
	return &ConsoleBroker{
		sink:        s,
		in:          make(chan consoleEvent, 256),
		done:        make(chan struct{}),
		rxAtLineEnd: true,
		quietAfter:  120 * time.Millisecond,
		norm:        norm,
		labelRX:     "RX",
		labelTX:     "TX",
	}
}

func (b *ConsoleBroker) Start()         { go b.loop() }
func (b *ConsoleBroker) Close()         { close(b.done) }
func (b *ConsoleBroker) SetSink(s Sink) { b.sink = s }

func (b *ConsoleBroker) Enqueue(ev consoleEvent) {
	b.in <- ev // blocking is fine; adjust if you want drop semantics
}

func (b *ConsoleBroker) loop() {
	for {
		select {
		case <-b.done:
			return
		case ev := <-b.in:
			switch ev.kind {
			case evRX:
				b.handleRX(ev.data)
			case evTX:
				b.handleTX(ev.data)
			}
		case <-b.quietCh():
			if b.pendingTX != nil {
				_ = b.sink.SendText("\r\n")
				b.flushPendingTX()
				b.rxAtLineEnd = true
			}
		}
	}
}

func (b *ConsoleBroker) quietCh() <-chan time.Time {
	if b.quietTimer != nil {
		return b.quietTimer.C
	}
	return make(<-chan time.Time)
}

func (b *ConsoleBroker) startQuietTimer() {
	if b.quietTimer == nil {
		b.quietTimer = time.NewTimer(b.quietAfter)
	} else {
		b.quietTimer.Reset(b.quietAfter)
	}
}

func (b *ConsoleBroker) stopQuietTimer() {
	if b.quietTimer != nil {
		if !b.quietTimer.Stop() {
			select {
			case <-b.quietTimer.C:
			default:
			}
		}
	}
}

func (b *ConsoleBroker) handleRX(data []byte) {
	if b.sink == nil || len(data) == 0 {
		return
	}
	text := normalize(data, b.norm)
	if text != "" {
		_ = b.sink.SendText(fmt.Sprintf("%s: %s", b.labelRX, text))
	}

	last := data[len(data)-1]
	b.rxAtLineEnd = (last == '\r' || last == '\n')

	if b.pendingTX != nil && b.rxAtLineEnd {
		b.flushPendingTX()
		b.stopQuietTimer()
	}
}

func (b *ConsoleBroker) handleTX(data []byte) {
	if b.sink == nil || len(data) == 0 {
		return
	}
	if b.rxAtLineEnd && b.pendingTX == nil {
		_ = b.sink.SendText("\r\n")
		b.emitTX(data)
		b.rxAtLineEnd = true
		return
	}
	b.pendingTX = &consoleEvent{kind: evTX, data: append([]byte(nil), data...)}
	b.startQuietTimer()
}

func (b *ConsoleBroker) emitTX(data []byte) {
	text := normalize(data, b.norm)
	if text != "" {
		_ = b.sink.SendText(fmt.Sprintf("%s: %s\r\n", b.labelTX, text))
	}
}

func (b *ConsoleBroker) flushPendingTX() {
	if b.pendingTX == nil {
		return
	}
	b.emitTX(b.pendingTX.data)
	b.pendingTX = nil
}

/* ---------- SERIAL MUX (single reader/writer, emits to broker) ---------- */

type txFrame struct {
	payload []byte // should include terminator already
	source  string // "webrtc" | "button"
	echo    bool   // request TX echo (subject to global toggle)
}

type SerialMux struct {
	port   serial.Port
	txQ    chan txFrame
	done   chan struct{}
	broker *ConsoleBroker

	echoEnabled atomic.Bool // controlled via SetEchoEnabled
}

func NewSerialMux(p serial.Port, broker *ConsoleBroker) *SerialMux {
	m := &SerialMux{
		port:   p,
		txQ:    make(chan txFrame, 128),
		done:   make(chan struct{}),
		broker: broker,
	}
	return m
}

func (m *SerialMux) Start() {
	go m.reader()
	go m.writer()
}

func (m *SerialMux) Close() { close(m.done) }

func (m *SerialMux) SetEchoEnabled(v bool) { m.echoEnabled.Store(v) }

func (m *SerialMux) Enqueue(payload []byte, source string, requestEcho bool) {
	m.txQ <- txFrame{payload: append([]byte(nil), payload...), source: source, echo: requestEcho}
}

func (m *SerialMux) reader() {
	buf := make([]byte, 4096)
	for {
		select {
		case <-m.done:
			return
		default:
			n, err := m.port.Read(buf)
			if err != nil {
				if err != io.EOF {
					serialLogger.Warn().Err(err).Msg("serial read failed")
				}
				time.Sleep(50 * time.Millisecond)
				continue
			}
			if n > 0 && m.broker != nil {
				m.broker.Enqueue(consoleEvent{kind: evRX, data: append([]byte(nil), buf[:n]...)})
			}
		}
	}
}

func (m *SerialMux) writer() {
	for {
		select {
		case <-m.done:
			return
		case f := <-m.txQ:
			if _, err := m.port.Write(f.payload); err != nil {
				serialLogger.Warn().Err(err).Str("src", f.source).Msg("serial write failed")
				continue
			}
			// echo (if requested AND globally enabled)
			if f.echo && m.echoEnabled.Load() && m.broker != nil {
				m.broker.Enqueue(consoleEvent{kind: evTX, data: append([]byte(nil), f.payload...)})
			}
		}
	}
}
