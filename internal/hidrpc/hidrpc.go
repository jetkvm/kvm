package hidrpc

import (
	"fmt"

	"github.com/jetkvm/kvm/internal/usbgadget"
)

const (
	Version       byte = 0x01 // Version of the HID RPC protocol
	MaximumQueues int  = 4    // Maximum number of HID RPC queues
)

// GetQueueIndex returns the index of the queue to which the message should be enqueued.
func GetQueueIndex(messageType MessageType) int {
	switch messageType {
	case TypeHandshake:
		return 0
	case TypeKeyboardReport, TypeKeypressReport, TypeKeyboardMacroReport, TypeKeyboardLedState, TypeKeydownState, TypeKeyboardMacroState:
		return 1
	case TypePointerReport, TypeMouseReport, TypeWheelReport:
		return 2
	// we don't want to block the queue for this message
	case TypeCancelKeyboardMacroReport:
		return 3
	default:
		return 3
	}
}

// Unmarshal unmarshals the HID RPC message from the data.
func Unmarshal(data []byte, message *Message) error {
	l := len(data)
	if l < 1 {
		return fmt.Errorf("invalid data length: %d", l)
	}

	message.t = MessageType(data[0])
	message.d = data[1:]
	return nil
}

// NewHandshakeMessage creates a new handshake message.
func NewHandshakeMessage() *Message {
	return &Message{
		t: TypeHandshake,
		d: []byte{Version},
	}
}

// NewKeyboardReportMessage creates a new keyboard report message.
func NewKeyboardReportMessage(keys []byte, modifier uint8) *Message {
	return &Message{
		t: TypeKeyboardReport,
		d: append([]byte{modifier}, keys...),
	}
}

// NewKeyboardLedMessage creates a new keyboard LED message.
func NewKeyboardLedMessage(state usbgadget.KeyboardState) *Message {
	return &Message{
		t: TypeKeyboardLedState,
		d: []byte{state.Byte()},
	}
}

// NewKeydownStateMessage creates a new keydown state message.
func NewKeydownStateMessage(state usbgadget.KeysDownState) *Message {
	data := make([]byte, len(state.Keys)+1)
	data[0] = state.Modifier
	copy(data[1:], state.Keys)

	return &Message{
		t: TypeKeydownState,
		d: data,
	}
}

// NewKeyboardMacroStateMessage creates a new keyboard macro state message.
func NewKeyboardMacroStateMessage(state bool, isPaste bool) *Message {
	data := make([]byte, 2)
	if state {
		data[0] = 1
	}
	if isPaste {
		data[1] = 1
	}

	return &Message{
		t: TypeKeyboardMacroState,
		d: data,
	}
}
