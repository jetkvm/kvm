package kvm

import (
	"context"
	"testing"

	"github.com/jetkvm/kvm/internal/hidrpc"
	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog"
)

func TestHidMacroCancelCannotReachReplacement(t *testing.T) {
	defer cancelKeyboardMacro()
	session := &Session{done: make(chan struct{})}
	session.initQueues()
	log := zerolog.Nop()
	handle := getOnHidMessageHandler(session, &log, "hidrpc")
	old := registerKeyboardMacro()

	// Hold the control queue idle while the ordered channel delivers cancel.
	// The old macro can finish naturally before that queue gets CPU time.
	handle(webrtc.DataChannelMessage{Data: []byte{byte(hidrpc.TypeCancelKeyboardMacroReport)}})
	if old.ctx.Err() != context.Canceled {
		t.Fatal("the running macro was not canceled before admitting its replacement")
	}
	replacement := registerKeyboardMacro()
	for len(session.hidQueue[3]) > 0 {
		onHidMessage(<-session.hidQueue[3], session)
	}
	if replacement.ctx.Err() != nil {
		t.Fatal("the previous macro's queued cancellation stopped its replacement")
	}
}

func TestClosedHidSessionCannotCancelMacro(t *testing.T) {
	defer cancelKeyboardMacro()
	session := &Session{done: make(chan struct{})}
	session.initQueues()
	session.close()
	log := zerolog.Nop()
	run := registerKeyboardMacro()
	getOnHidMessageHandler(session, &log, "hidrpc")(
		webrtc.DataChannelMessage{Data: []byte{byte(hidrpc.TypeCancelKeyboardMacroReport)}},
	)
	if run.ctx.Err() != nil {
		t.Fatal("a closed session canceled the active macro")
	}
}
