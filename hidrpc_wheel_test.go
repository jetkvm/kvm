package kvm

import (
	"testing"

	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog"
)

type wheelCall struct{ y, x int8 }

func dispatchWheelFrames(t *testing.T, frames ...[]byte) []wheelCall {
	t.Helper()
	var calls []wheelCall
	original := hidWheelReport
	hidWheelReport = func(y, x int8) error {
		calls = append(calls, wheelCall{y, x})
		return nil
	}
	t.Cleanup(func() { hidWheelReport = original })

	session := &Session{done: make(chan struct{})}
	session.initQueues()
	log := zerolog.Nop()
	handle := getOnHidMessageHandler(session, &log, "hidrpc")
	for _, frame := range frames {
		handle(webrtc.DataChannelMessage{Data: frame})
	}
	for len(session.hidQueue[2]) > 0 {
		onHidMessage(<-session.hidQueue[2], session)
	}
	return calls
}

func TestHidRPCWheelFrameReachesWheelReport(t *testing.T) {
	calls := dispatchWheelFrames(t,
		[]byte{0x04, 0x01, 0xff},
		[]byte{0x04, 0xff, 0x01},
		[]byte{0x04, 0x7f, 0x81},
	)
	want := []wheelCall{{1, -1}, {-1, 1}, {127, -127}}
	if len(calls) != len(want) {
		t.Fatalf("got %d wheel reports %v, want %v", len(calls), calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("frame %d: got deltaY=%d deltaX=%d, want deltaY=%d deltaX=%d",
				i, calls[i].y, calls[i].x, want[i].y, want[i].x)
		}
	}
}

func TestHidRPCWheelFrameWithWrongLengthIsRefused(t *testing.T) {
	calls := dispatchWheelFrames(t,
		[]byte{0x04},
		[]byte{0x04, 0x01},
		[]byte{0x04, 0x01, 0x01, 0x00},
	)
	if len(calls) != 0 {
		t.Fatalf("malformed wheel frames reached the wheel report: %v", calls)
	}
}
