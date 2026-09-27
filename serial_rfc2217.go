package kvm

import (
	"encoding/binary"

	"go.bug.st/serial"
)

// Telnet (RFC 854) and Telnet COM Port Control (RFC 2217) support for the
// ser2net-style serial server. Only what a console server needs is handled:
// option negotiation for BINARY/ECHO/SGA/COM-PORT-OPTION, IAC escaping,
// BREAK, and the RFC 2217 line settings commands.

const (
	telnetSE   byte = 240
	telnetBRK  byte = 243
	telnetSB   byte = 250
	telnetWILL byte = 251
	telnetWONT byte = 252
	telnetDO   byte = 253
	telnetDONT byte = 254
	telnetIAC  byte = 255

	telnetOptBinary  byte = 0
	telnetOptEcho    byte = 1
	telnetOptSGA     byte = 3
	telnetOptComPort byte = 44
)

// RFC 2217 client-to-server commands. The server answers with cmd+100.
const (
	rfc2217Signature          byte = 0
	rfc2217SetBaudRate        byte = 1
	rfc2217SetDataSize        byte = 2
	rfc2217SetParity          byte = 3
	rfc2217SetStopSize        byte = 4
	rfc2217SetControl         byte = 5
	rfc2217FlowControlSuspend byte = 8
	rfc2217FlowControlResume  byte = 9
	rfc2217SetLineStateMask   byte = 10
	rfc2217SetModemStateMask  byte = 11
	rfc2217PurgeData          byte = 12
	rfc2217ServerOffset       byte = 100
)

const (
	rfc2217ServerSignature = "JetKVM"
	telnetMaxSubnegLen     = 256
)

type telnetParseState int

const (
	telnetStateData telnetParseState = iota
	telnetStateIAC
	telnetStateOption
	telnetStateSB
	telnetStateSBIAC
)

// telnetSession decodes one client's byte stream into UART payload and
// protocol replies. It is not safe for concurrent use; the connection's
// reader goroutine owns it.
type telnetSession struct {
	state  telnetParseState
	verb   byte
	sb     []byte
	lastCR bool

	// we holds options the server has enabled (WILL), they the options the
	// client has enabled (DO). Tracking both stops negotiation loops.
	we   map[byte]bool
	they map[byte]bool

	// mode is the UART mode this session last reported or applied.
	mode        serial.Mode
	modeChanged bool
	setMode     func(serial.Mode) error
	sendBreak   func()
}

func newTelnetSession(mode serial.Mode, setMode func(serial.Mode) error, sendBreak func()) *telnetSession {
	return &telnetSession{
		we:        map[byte]bool{},
		they:      map[byte]bool{},
		mode:      mode,
		setMode:   setMode,
		sendBreak: sendBreak,
	}
}

// initialNegotiation is sent when a client connects. The server echoes (the
// UART does), suppresses go-ahead, runs 8-bit clean, and offers RFC 2217.
func (t *telnetSession) initialNegotiation() []byte {
	var out []byte
	for _, opt := range []byte{telnetOptEcho, telnetOptSGA, telnetOptBinary, telnetOptComPort} {
		t.we[opt] = true
		out = append(out, telnetIAC, telnetWILL, opt)
	}
	for _, opt := range []byte{telnetOptSGA, telnetOptBinary} {
		t.they[opt] = true
		out = append(out, telnetIAC, telnetDO, opt)
	}
	return out
}

// Decode splits client input into bytes for the UART and bytes to send back
// to the client.
func (t *telnetSession) Decode(in []byte) (data []byte, reply []byte) {
	for _, b := range in {
		switch t.state {
		case telnetStateData:
			if b == telnetIAC {
				t.state = telnetStateIAC
				continue
			}
			// NVT sends CR as CR NUL outside binary mode.
			if t.lastCR {
				t.lastCR = false
				if b == 0 && !t.they[telnetOptBinary] {
					continue
				}
			}
			t.lastCR = b == '\r'
			data = append(data, b)
		case telnetStateIAC:
			switch b {
			case telnetIAC:
				data = append(data, telnetIAC)
				t.lastCR = false
				t.state = telnetStateData
			case telnetWILL, telnetWONT, telnetDO, telnetDONT:
				t.verb = b
				t.state = telnetStateOption
			case telnetSB:
				t.sb = t.sb[:0]
				t.state = telnetStateSB
			case telnetBRK:
				if t.sendBreak != nil {
					t.sendBreak()
				}
				t.state = telnetStateData
			default:
				// NOP, AYT, GA and friends carry nothing for a UART.
				t.state = telnetStateData
			}
		case telnetStateOption:
			reply = append(reply, t.negotiate(t.verb, b)...)
			t.state = telnetStateData
		case telnetStateSB:
			if b == telnetIAC {
				t.state = telnetStateSBIAC
			} else if len(t.sb) < telnetMaxSubnegLen {
				t.sb = append(t.sb, b)
			}
		case telnetStateSBIAC:
			switch b {
			case telnetIAC:
				if len(t.sb) < telnetMaxSubnegLen {
					t.sb = append(t.sb, telnetIAC)
				}
				t.state = telnetStateSB
			case telnetSE:
				reply = append(reply, t.handleSubneg(t.sb)...)
				t.state = telnetStateData
			default:
				// Malformed subnegotiation: drop it.
				t.state = telnetStateData
			}
		}
	}
	return data, reply
}

func telnetWeSupports(opt byte) bool {
	return opt == telnetOptEcho || opt == telnetOptSGA || opt == telnetOptBinary || opt == telnetOptComPort
}

func telnetTheySupports(opt byte) bool {
	return opt == telnetOptSGA || opt == telnetOptBinary || opt == telnetOptComPort
}

func (t *telnetSession) negotiate(verb, opt byte) []byte {
	switch verb {
	case telnetDO:
		if t.we[opt] {
			return nil
		}
		if telnetWeSupports(opt) {
			t.we[opt] = true
			return []byte{telnetIAC, telnetWILL, opt}
		}
		return []byte{telnetIAC, telnetWONT, opt}
	case telnetDONT:
		if !t.we[opt] {
			return nil
		}
		t.we[opt] = false
		return []byte{telnetIAC, telnetWONT, opt}
	case telnetWILL:
		if t.they[opt] {
			return nil
		}
		if telnetTheySupports(opt) {
			t.they[opt] = true
			return []byte{telnetIAC, telnetDO, opt}
		}
		return []byte{telnetIAC, telnetDONT, opt}
	case telnetWONT:
		if !t.they[opt] {
			return nil
		}
		t.they[opt] = false
		return []byte{telnetIAC, telnetDONT, opt}
	}
	return nil
}

func (t *telnetSession) handleSubneg(sb []byte) []byte {
	if len(sb) < 2 || sb[0] != telnetOptComPort {
		return nil
	}
	cmd, val := sb[1], sb[2:]

	switch cmd {
	case rfc2217Signature:
		if len(val) > 0 {
			return nil // client announcing itself
		}
		return rfc2217Reply(cmd, []byte(rfc2217ServerSignature))

	case rfc2217SetBaudRate:
		if len(val) != 4 {
			return nil
		}
		if baud := binary.BigEndian.Uint32(val); baud != 0 {
			next := t.mode
			next.BaudRate = int(baud)
			t.apply(next)
		}
		out := make([]byte, 4)
		binary.BigEndian.PutUint32(out, uint32(t.mode.BaudRate))
		return rfc2217Reply(cmd, out)

	case rfc2217SetDataSize:
		if len(val) != 1 {
			return nil
		}
		if v := int(val[0]); v >= 5 && v <= 8 {
			next := t.mode
			next.DataBits = v
			t.apply(next)
		}
		return rfc2217Reply(cmd, []byte{byte(t.mode.DataBits)})

	case rfc2217SetParity:
		if len(val) != 1 {
			return nil
		}
		// RFC 2217 codes are 1-based in the same order as serial.Parity.
		if v := val[0]; v >= 1 && v <= 5 {
			next := t.mode
			next.Parity = serial.Parity(v - 1)
			t.apply(next)
		}
		return rfc2217Reply(cmd, []byte{byte(t.mode.Parity) + 1})

	case rfc2217SetStopSize:
		if len(val) != 1 {
			return nil
		}
		next := t.mode
		switch val[0] {
		case 1:
			next.StopBits = serial.OneStopBit
		case 2:
			next.StopBits = serial.TwoStopBits
		case 3:
			next.StopBits = serial.OnePointFiveStopBits
		}
		if next != t.mode {
			t.apply(next)
		}
		code := byte(1)
		switch t.mode.StopBits {
		case serial.TwoStopBits:
			code = 2
		case serial.OnePointFiveStopBits:
			code = 3
		}
		return rfc2217Reply(cmd, []byte{code})

	case rfc2217SetControl:
		if len(val) != 1 {
			return nil
		}
		return rfc2217Reply(cmd, []byte{rfc2217ControlReply(val[0])})

	case rfc2217SetLineStateMask, rfc2217SetModemStateMask, rfc2217PurgeData:
		if len(val) != 1 {
			return nil
		}
		return rfc2217Reply(cmd, val[:1])

	case rfc2217FlowControlSuspend, rfc2217FlowControlResume:
		return nil
	}
	return nil
}

// rfc2217ControlReply answers SET-CONTROL. The extension port has no flow
// control or modem lines, so queries report "none"/"on" and requests to
// toggle DTR, RTS or BREAK are acknowledged as-is.
func rfc2217ControlReply(v byte) byte {
	switch {
	case v <= 3: // outbound flow control: only NONE
		return 1
	case v == 4: // query BREAK
		return 6
	case v == 7: // query DTR
		return 8
	case v == 10: // query RTS
		return 11
	case v >= 13 && v <= 16: // inbound flow control: only NONE
		return 14
	default:
		return v
	}
}

func (t *telnetSession) apply(next serial.Mode) {
	if t.setMode == nil {
		return
	}
	if err := t.setMode(next); err != nil {
		serialLogger.Warn().Err(err).Interface("mode", next).Msg("RFC 2217 client requested an unsupported serial mode")
		return
	}
	t.mode = next
	t.modeChanged = true
}

func rfc2217Reply(cmd byte, payload []byte) []byte {
	out := []byte{telnetIAC, telnetSB, telnetOptComPort, cmd + rfc2217ServerOffset}
	out = append(out, telnetEscapeIAC(payload)...)
	return append(out, telnetIAC, telnetSE)
}

// telnetEscapeIAC doubles every IAC byte so UART data can't be mistaken for
// a telnet command.
func telnetEscapeIAC(p []byte) []byte {
	n := 0
	for _, b := range p {
		if b == telnetIAC {
			n++
		}
	}
	if n == 0 {
		return p
	}
	out := make([]byte, 0, len(p)+n)
	for _, b := range p {
		out = append(out, b)
		if b == telnetIAC {
			out = append(out, telnetIAC)
		}
	}
	return out
}
