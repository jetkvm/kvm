//go:build linux && arm

package kvm

import (
	"context"
	"testing"

	"github.com/jetkvm/kvm/internal/hidrpc"
	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog"
)

func TestHidMacroCancelCannotReachReplacement(t *testing.T) {
	defer setKeyboardMacroCancel(nil)
	session := &Session{done: make(chan struct{})}
	session.initQueues()
	log := zerolog.Nop()
	handle := getOnHidMessageHandler(session, &log, "hidrpc")
	old, cancelOld := context.WithCancel(context.Background())
	defer cancelOld()
	setKeyboardMacroCancel(cancelOld)

	// Hold the control queue idle while the ordered channel delivers cancel.
	// The old macro can finish naturally before that queue gets CPU time.
	handle(webrtc.DataChannelMessage{Data: []byte{byte(hidrpc.TypeCancelKeyboardMacroReport)}})
	replacement, cancelReplacement := context.WithCancel(context.Background())
	defer cancelReplacement()
	setKeyboardMacroCancel(cancelReplacement)
	for len(session.hidQueue[3]) > 0 {
		onHidMessage(<-session.hidQueue[3], session)
	}
	if replacement.Err() != nil {
		t.Fatal("the previous macro's queued cancellation stopped its replacement")
	}
	if old.Err() != context.Canceled {
		t.Fatal("the running macro was not canceled before admitting its replacement")
	}
}

func TestClosedHidSessionCannotCancelMacro(t *testing.T) {
	defer setKeyboardMacroCancel(nil)
	session := &Session{done: make(chan struct{})}
	session.initQueues()
	session.close()
	log := zerolog.Nop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setKeyboardMacroCancel(cancel)
	getOnHidMessageHandler(session, &log, "hidrpc")(
		webrtc.DataChannelMessage{Data: []byte{byte(hidrpc.TypeCancelKeyboardMacroReport)}},
	)
	if ctx.Err() != nil {
		t.Fatal("a closed session canceled the active macro")
	}
}
