package hidrpc

import (
	"encoding/binary"
	"fmt"
)

// Message ..
type Message struct {
	t MessageType
	d []byte
}

// Marshal marshals the message to a byte array.
func (m *Message) Marshal() ([]byte, error) {
	return Marshal(m)
}

func (m *Message) Type() MessageType {
	return m.t
}

func (m *Message) String() string {
	switch m.t {
	case TypeHandshake:
		return "Handshake"
	case TypeKeypressReport:
		if len(m.d) < 2 {
			return fmt.Sprintf("KeypressReport{Malformed: %v}", m.d)
		}
		return fmt.Sprintf("KeypressReport{Key: %d, Press: %v}", m.d[0], m.d[1] == uint8(1))
	case TypeKeyboardReport:
		if len(m.d) < 2 {
			return fmt.Sprintf("KeyboardReport{Malformed: %v}", m.d)
		}
		return fmt.Sprintf("KeyboardReport{Modifier: %d, Keys: %v}", m.d[0], m.d[1:])
	case TypePointerReport:
		if len(m.d) < 9 {
			return fmt.Sprintf("PointerReport{Malformed: %v}", m.d)
		}
		return fmt.Sprintf("PointerReport{X: %d, Y: %d, Button: %d}", m.d[0:4], m.d[4:8], m.d[8])
	case TypeMouseReport:
		if len(m.d) < 3 {
			return fmt.Sprintf("MouseReport{Malformed: %v}", m.d)
		}
		return fmt.Sprintf("MouseReport{DX: %d, DY: %d, Button: %d}", m.d[0], m.d[1], m.d[2])
	case TypeKeyboardMacroReport:
		if len(m.d) < 5 {
			return fmt.Sprintf("KeyboardMacroReport{Malformed: %v}", m.d)
		}
		return fmt.Sprintf("KeyboardMacroReport{IsPaste: %v, Length: %d}", m.d[0] == uint8(1), binary.BigEndian.Uint32(m.d[1:5]))
	default:
		return fmt.Sprintf("Unknown{Type: %d, Data: %v}", m.t, m.d)
	}
}

// KeypressReport ..
type KeypressReport struct {
	Key   byte
	Press bool
}

// KeypressReport returns the keypress report from the message.
func (m *Message) KeypressReport() (KeypressReport, error) {
	if m.t != TypeKeypressReport {
		return KeypressReport{}, fmt.Errorf("invalid message type: %d", m.t)
	}

	return KeypressReport{
		Key:   m.d[0],
		Press: m.d[1] == uint8(1),
	}, nil
}

// KeyboardReport ..
type KeyboardReport struct {
	Modifier byte
	Keys     []byte
}

// KeyboardReport returns the keyboard report from the message.
func (m *Message) KeyboardReport() (KeyboardReport, error) {
	if m.t != TypeKeyboardReport {
		return KeyboardReport{}, fmt.Errorf("invalid message type: %d", m.t)
	}

	return KeyboardReport{
		Modifier: m.d[0],
		Keys:     m.d[1:],
	}, nil
}

// Macro ..
type KeyboardMacroStep struct {
	Modifier byte   // 1 byte
	Keys     []byte // 6 bytes: hidKeyBufferSize
	Delay    uint16 // 2 bytes
}
type KeyboardMacroReport struct {
	IsPaste   bool
	StepCount uint32
	Steps     []KeyboardMacroStep
}

// HidKeyBufferSize is the size of the keys buffer in the keyboard report.
const HidKeyBufferSize = 6

// KeyboardMacroReport returns the keyboard macro report from the message.
func (m *Message) KeyboardMacroReport() (KeyboardMacroReport, error) {
	if m.t != TypeKeyboardMacroReport {
		return KeyboardMacroReport{}, fmt.Errorf("invalid message type: %d", m.t)
	}

	isPaste := m.d[0] == uint8(1)
	stepCount := binary.BigEndian.Uint32(m.d[1:5])

	// check total length
	expectedLength := int(stepCount)*9 + 5
	if len(m.d) != expectedLength {
		return KeyboardMacroReport{}, fmt.Errorf("invalid length: %d, expected: %d", len(m.d), expectedLength)
	}

	steps := make([]KeyboardMacroStep, 0, int(stepCount))
	offset := 5
	for i := 0; i < int(stepCount); i++ {
		steps = append(steps, KeyboardMacroStep{
			Modifier: m.d[offset],
			Keys:     m.d[offset+1 : offset+7],
			Delay:    binary.BigEndian.Uint16(m.d[offset+7 : offset+9]),
		})

		offset += 1 + HidKeyBufferSize + 2
	}

	return KeyboardMacroReport{
		IsPaste:   isPaste,
		Steps:     steps,
		StepCount: stepCount,
	}, nil
}

// PointerReport ..
type PointerReport struct {
	X      int
	Y      int
	Button uint8
}

func toInt(b []byte) int {
	return int(b[0])<<24 + int(b[1])<<16 + int(b[2])<<8 + int(b[3])<<0
}

// PointerReport returns the point report from the message.
func (m *Message) PointerReport() (PointerReport, error) {
	if m.t != TypePointerReport {
		return PointerReport{}, fmt.Errorf("invalid message type: %d", m.t)
	}

	if len(m.d) != 9 {
		return PointerReport{}, fmt.Errorf("invalid message length: %d", len(m.d))
	}

	return PointerReport{
		X:      toInt(m.d[0:4]),
		Y:      toInt(m.d[4:8]),
		Button: uint8(m.d[8]),
	}, nil
}

// MouseReport ..
type MouseReport struct {
	DX     int8
	DY     int8
	Button uint8
}

// MouseReport returns the mouse report from the message.
func (m *Message) MouseReport() (MouseReport, error) {
	if m.t != TypeMouseReport {
		return MouseReport{}, fmt.Errorf("invalid message type: %d", m.t)
	}

	return MouseReport{
		DX:     int8(m.d[0]),
		DY:     int8(m.d[1]),
		Button: uint8(m.d[2]),
	}, nil
}

type KeyboardMacroState struct {
	State   bool
	IsPaste bool
}

// KeyboardMacroState returns the keyboard macro state report from the message.
func (m *Message) KeyboardMacroState() (KeyboardMacroState, error) {
	if m.t != TypeKeyboardMacroState {
		return KeyboardMacroState{}, fmt.Errorf("invalid message type: %d", m.t)
	}

	return KeyboardMacroState{
		State:   m.d[0] == uint8(1),
		IsPaste: m.d[1] == uint8(1),
	}, nil
}
