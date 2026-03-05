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

type dataChannelSink struct{ dataChannel *webrtc.DataChannel }

func (sink dataChannelSink) SendText(str string) error { return sink.dataChannel.SendText(str) }

/* ---------- NORMALIZATION (applies to RX & TX) ---------- */

type NormalizeMode int

const (
	ModeCaret NormalizeMode = iota // ^C ^M ^?
	ModeNames                      // <CR>, <LF>, <ESC>, …
	ModeHex                        // \x1B
)

type LineEndingMode int

const (
	LineEnding_AsIs LineEndingMode = iota
	LineEnding_LF
	LineEnding_CR
	LineEnding_CRLF
	LineEnding_LFCR
)

type NormalizationOptions struct {
	Mode         NormalizeMode
	LineEnding   LineEndingMode
	TabRender    string // e.g. "    " or "" to keep '\t'
	PreserveANSI bool
	ShowNLTag    bool // print a visible tag for CR/LF like <CR>, <LF>, <CRLF>
}

func normalize(in []byte, opt NormalizationOptions) string {
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

		// CR/LF normalization (emit real newline(s), optionally tag them visibly)
		if b == '\r' || b == '\n' {
			// detect pair (CRLF or LFCR)
			isPair := i+1 < len(in) &&
				((b == '\r' && in[i+1] == '\n') || (b == '\n' && in[i+1] == '\r'))

			// optional visible tag of what we *saw*
			if opt.ShowNLTag {
				if isPair {
					if b == '\r' { // saw CRLF
						out.WriteString("<CRLF>")
					} else { // saw LFCR
						out.WriteString("<LFCR>")
					}
				} else {
					if b == '\r' {
						out.WriteString("<CR>")
					} else {
						out.WriteString("<LF>")
					}
				}
			}

			// now emit the actual newline(s) per the normalization mode
			switch opt.LineEnding {
			case LineEnding_AsIs:
				if isPair {
					out.WriteByte(b)
					out.WriteByte(in[i+1])
					i += 2
				} else {
					out.WriteByte(b)
					i++
				}
			case LineEnding_LF:
				if isPair {
					i += 2
				} else {
					i++
				}
				out.WriteByte('\n')
			case LineEnding_CR:
				if isPair {
					i += 2
				} else {
					i++
				}
				out.WriteByte('\r')
			case LineEnding_CRLF:
				if isPair {
					i += 2
				} else {
					i++
				}
				out.WriteString("\r\n")
			case LineEnding_LFCR:
				if isPair {
					i += 2
				} else {
					i++
				}
				out.WriteString("\n\r")
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

type TXOrigin int

const (
	TXUser TXOrigin = iota
	TXSystem
)

type consoleEvent struct {
	kind   consoleEventKind
	data   []byte
	origin TXOrigin // TXUser/TXSystem (only used for evTX)
}

type ConsoleBroker struct {
	sink Sink
	in   chan consoleEvent
	done chan struct{}

	// pause control
	terminalPaused bool
	pauseCh        chan bool

	// buffered output while paused
	bufLines    []string
	bufBytes    int
	maxBufLines int
	maxBufBytes int

	// line-aware echo
	rxAtLineEnd  bool
	txLineActive bool // true if we’re mid-line (prefix already written)
	pendingTX    *consoleEvent
	quietTimer   *time.Timer
	quietAfter   time.Duration

	// normalization
	norm NormalizationOptions

	// labels
	labelRX string
	labelTX string
}

func NewConsoleBroker(s Sink, norm NormalizationOptions) *ConsoleBroker {
	return &ConsoleBroker{
		sink:           s,
		in:             make(chan consoleEvent, 256),
		done:           make(chan struct{}),
		pauseCh:        make(chan bool, 8),
		terminalPaused: false,
		rxAtLineEnd:    true,
		txLineActive:   false,
		quietAfter:     120 * time.Millisecond,
		norm:           norm,
		labelRX:        "RX",
		labelTX:        "TX",
		// reasonable defaults; tweak as you like
		maxBufLines: 5000,
		maxBufBytes: 1 << 20, // 1 MiB
	}
}

func (b *ConsoleBroker) Start()                                   { go b.loop() }
func (b *ConsoleBroker) Close()                                   { close(b.done) }
func (b *ConsoleBroker) SetSink(s Sink)                           { b.sink = s }
func (b *ConsoleBroker) SetNormOptions(norm NormalizationOptions) { b.norm = norm }
func (b *ConsoleBroker) SetTerminalPaused(v bool) {
	if b == nil {
		return
	}
	// send to broker loop to avoid data races
	select {
	case b.pauseCh <- v:
	default:
		b.pauseCh <- v
	}
}

func (b *ConsoleBroker) Enqueue(ev consoleEvent) {
	b.in <- ev // blocking is fine; adjust if you want drop semantics
}

func (b *ConsoleBroker) loop() {
	scopedLogger := serialLogger.With().Str("service", "Serial Console Broker").Logger()
	for {
		select {
		case <-b.done:
			return

		case v := <-b.pauseCh:
			// apply pause state
			wasPaused := b.terminalPaused
			b.terminalPaused = v
			if wasPaused && !v {
				// we just unpaused: flush buffered output in order
				scopedLogger.Trace().Msg("Terminal unpaused; flushing buffered output")
				b.flushBuffer()
			} else if !wasPaused && v {
				scopedLogger.Trace().Msg("Terminal paused; buffering output")
			}

		case ev := <-b.in:
			switch ev.kind {
			case evRX:
				scopedLogger.Trace().Msg("Processing RX data from serial port")
				b.handleRX(ev.data)
			case evTX:
				scopedLogger.Trace().Msg("Processing TX echo request")
				b.handleTX(ev.data, ev.origin)
			}

		case <-b.quietCh():
			if b.pendingTX != nil {
				b.emitToTerminal(b.lineSep()) // use CRLF policy
				b.flushPendingTX()
				b.rxAtLineEnd = true
				b.txLineActive = false
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
	scopedLogger := serialLogger.With().Str("service", "Serial Console Broker RX handler").Logger()
	if b.sink == nil || len(data) == 0 {
		return
	}

	// If we’re mid TX line, end it before RX
	if b.txLineActive {
		b.emitToTerminal(b.lineSep())
		b.txLineActive = false
	}

	text := normalize(data, b.norm)
	if text == "" {
		return
	}

	scopedLogger.Trace().Msg("Emitting RX data to sink (with per-line prefixes)")

	// Prefix every line, regardless of how the EOLs look
	lines := splitAfterAnyEOL(text, b.norm.LineEnding)

	// Start from the broker's current RX line state
	atLineEnd := b.rxAtLineEnd

	for _, line := range lines {
		if line == "" {
			continue
		}

		if atLineEnd {
			// New physical line -> prefix with RX:
			b.emitToTerminal(fmt.Sprintf("%s: %s", b.labelRX, line))
		} else {
			// Continuation of previous RX line -> no extra RX: prefix
			b.emitToTerminal(line)
		}

		// Update line-end state based on this piece
		atLineEnd = endsWithEOL(line, b.norm.LineEnding)
	}

	// Persist state for next RX chunk
	b.rxAtLineEnd = atLineEnd

	if b.pendingTX != nil && b.rxAtLineEnd {
		b.flushPendingTX()
		b.stopQuietTimer()
	}
}

func (b *ConsoleBroker) handleTX(data []byte, origin TXOrigin) {
	scopedLogger := serialLogger.With().Str("service", "Serial Console Broker TX handler").Logger()
	if b.sink == nil || len(data) == 0 {
		return
	}
	if b.rxAtLineEnd && b.pendingTX == nil {
		scopedLogger.Trace().Msg("Emitting TX data to sink immediately")
		b.emitTX(data, origin)
		return
	}
	scopedLogger.Trace().Msg("Queuing TX data to emit after RX line completion or quiet period")
	b.pendingTX = &consoleEvent{kind: evTX, data: append([]byte(nil), data...), origin: origin}
	b.startQuietTimer()
}

func (b *ConsoleBroker) emitTX(data []byte, origin TXOrigin) {
	scopedLogger := serialLogger.With().Str("service", "Serial Console Broker TX emiter").Logger()
	if len(data) == 0 {
		return
	}

	text := normalize(data, b.norm)
	if text == "" {
		return
	}

	prefix := fmt.Sprintf("%s: ", b.labelTX)
	if origin == TXSystem {
		prefix = fmt.Sprintf("%s[System]: ", b.labelTX)
	}

	// If RX is currently mid-line (rxAtLineEnd=false), TX should start on a new line
	// (prevents TX from appearing glued to RX)
	if !b.rxAtLineEnd && !b.txLineActive {
		b.emitToTerminal(b.lineSep())
		b.rxAtLineEnd = true
	}

	if !b.txLineActive {
		scopedLogger.Trace().Msg("Emitting TX data to sink with prefix")
		b.emitToTerminal(prefix + text)
		b.txLineActive = true
	} else {
		scopedLogger.Trace().Msg("Emitting TX data to sink without prefix")
		b.emitToTerminal(text)
	}

	ended := endsWithEOL(text, b.norm.LineEnding)

	// System messages should not leave the cursor hanging mid-line
	if origin == TXSystem && !ended {
		b.emitToTerminal(b.lineSep())
		ended = true
	}

	if ended {
		b.txLineActive = false
	}
}

func (b *ConsoleBroker) flushPendingTX() {
	if b.pendingTX == nil {
		return
	}
	b.emitTX(b.pendingTX.data, b.pendingTX.origin)
	b.pendingTX = nil
	b.txLineActive = false
}

func (b *ConsoleBroker) lineSep() string {
	switch b.norm.LineEnding {
	case LineEnding_CRLF:
		return "\r\n"
	case LineEnding_LFCR:
		return "\n\r"
	case LineEnding_CR:
		return "\r"
	case LineEnding_LF:
		return "\n"
	default:
		return "\n"
	}
}

// splitAfterAnyEOL splits text into lines keeping the EOL with each piece.
// For LineEnding_AsIs it treats \r, \n, \r\n, and \n\r as EOLs.
// For other modes it uses the normalized separator.
func splitAfterAnyEOL(text string, mode LineEndingMode) []string {
	if text == "" {
		return nil
	}

	// Fast path for normalized modes
	switch mode {
	case LineEnding_LF:
		return strings.SplitAfter(text, "\n")
	case LineEnding_CR:
		return strings.SplitAfter(text, "\r")
	case LineEnding_CRLF:
		return strings.SplitAfter(text, "\r\n")
	case LineEnding_LFCR:
		return strings.SplitAfter(text, "\n\r")
	}

	// LineEnding_AsIs: scan bytes and treat \r, \n, \r\n, \n\r as one boundary
	b := []byte(text)
	var parts []string
	start := 0
	for i := 0; i < len(b); i++ {
		if b[i] == '\r' || b[i] == '\n' {
			j := i + 1
			// coalesce pair if the next is the "other" newline
			if j < len(b) && ((b[i] == '\r' && b[j] == '\n') || (b[i] == '\n' && b[j] == '\r')) {
				j++
			}
			parts = append(parts, string(b[start:j]))
			start = j
			i = j - 1 // advance past the EOL (or pair)
		}
	}
	if start < len(b) {
		parts = append(parts, string(b[start:]))
	}
	return parts
}

func endsWithEOL(s string, mode LineEndingMode) bool {
	if s == "" {
		return false
	}
	switch mode {
	case LineEnding_CRLF:
		return strings.HasSuffix(s, "\r\n")
	case LineEnding_LFCR:
		return strings.HasSuffix(s, "\n\r")
	case LineEnding_LF:
		return strings.HasSuffix(s, "\n")
	case LineEnding_CR:
		return strings.HasSuffix(s, "\r")
	default: // AsIs: any of \r, \n, \r\n, \n\r
		return strings.HasSuffix(s, "\r\n") ||
			strings.HasSuffix(s, "\n\r") ||
			strings.HasSuffix(s, "\n") ||
			strings.HasSuffix(s, "\r")
	}
}

func (b *ConsoleBroker) emitToTerminal(s string) {
	if b.sink == nil || s == "" {
		return
	}
	if b.terminalPaused {
		b.enqueueBuffered(s)
		return
	}
	_ = b.sink.SendText(s)
}

func (b *ConsoleBroker) enqueueBuffered(s string) {
	b.bufLines = append(b.bufLines, s)
	b.bufBytes += len(s)
	// trim if over limits (drop oldest)
	for b.bufBytes > b.maxBufBytes || len(b.bufLines) > b.maxBufLines {
		if len(b.bufLines) == 0 {
			break
		}
		b.bufBytes -= len(b.bufLines[0])
		b.bufLines = b.bufLines[1:]
	}
}

func (b *ConsoleBroker) flushBuffer() {
	if b.sink == nil || len(b.bufLines) == 0 {
		b.bufLines = nil
		b.bufBytes = 0
		return
	}
	for _, s := range b.bufLines {
		_ = b.sink.SendText(s)
	}
	b.bufLines = nil
	b.bufBytes = 0
}

/* ---------- SERIAL MUX (single reader/writer, emits to broker) ---------- */

type txFrame struct {
	payload []byte // should include terminator already
	source  string // "webrtc" | "button"
	echo    bool   // request TX echo (subject to global toggle)
	origin  TXOrigin
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

func (m *SerialMux) Enqueue(payload []byte, source string, requestEcho bool, origin TXOrigin) {
	serialLogger.Trace().Str("src", source).Bool("echo", requestEcho).Msg("Enqueuing TX data to serial port")
	m.txQ <- txFrame{
		payload: append([]byte(nil), payload...),
		source:  source,
		echo:    requestEcho,
		origin:  origin,
	}
}

func (m *SerialMux) reader() {
	scopedLogger := serialLogger.With().Str("service", "SerialMux reader").Logger()
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
				scopedLogger.Trace().Msg("Sending RX data to console broker")
				m.broker.Enqueue(consoleEvent{kind: evRX, data: append([]byte(nil), buf[:n]...)})
			}
		}
	}
}

func (m *SerialMux) writer() {
	scopedLogger := serialLogger.With().Str("service", "SerialMux writer").Logger()
	for {
		select {
		case <-m.done:
			return
		case f := <-m.txQ:
			scopedLogger.Trace().Msg("Writing TX data to serial port")
			if _, err := m.port.Write(f.payload); err != nil {
				scopedLogger.Warn().Err(err).Str("src", f.source).Msg("serial write failed")
				continue
			}
			// echo (if requested AND globally enabled)
			if f.echo && m.echoEnabled.Load() && m.broker != nil {
				scopedLogger.Trace().Msg("Sending TX echo to console broker")
				m.broker.Enqueue(consoleEvent{
					kind:   evTX,
					data:   append([]byte(nil), f.payload...),
					origin: f.origin,
				})
			}
		}
	}
}
