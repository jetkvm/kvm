package hidrpc

import (
	"fmt"

	"github.com/jetkvm/kvm/internal/usbgadget"
)

// HID RPC is a variable-length packet format that is used to exchange keyboard and mouse events between the client and the server.
// The packet format is as follows:
// 1 byte: Event Type

// MessageType is the type of the HID RPC message
type MessageType uint8

const (
	TypeHandshake        MessageType = 0x01
	TypeKeyboardReport   MessageType = 0x02
	TypePointerReport    MessageType = 0x03
	TypeWheelReport      MessageType = 0x04
	TypeKeypressReport   MessageType = 0x05
	TypeMouseReport      MessageType = 0x06
	TypeKeyboardLedState MessageType = 0x32
	TypeKeydownState     MessageType = 0x33
)

// ShouldUseEnqueue returns true if the message type should be enqueued to the HID queue.
func ShouldUseEnqueue(messageType MessageType) bool {
	return messageType == TypeMouseReport
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

// Marshal marshals the HID RPC message to the data.
func Marshal(message *Message) ([]byte, error) {
	if message.t == 0 {
		return nil, fmt.Errorf("invalid message type: %d", message.t)
	}

	data := make([]byte, len(message.d)+1)
	data[0] = byte(message.t)
	copy(data[1:], message.d)

	return data, nil
}

// NewHandshakeMessage creates a new handshake message.
func NewHandshakeMessage() *Message {
	return &Message{
		t: TypeHandshake,
		d: []byte{},
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
