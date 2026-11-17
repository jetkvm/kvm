package kvm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jetkvm/kvm/internal/hidrpc"
	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog"
)

func slowMacro() []hidrpc.KeyboardMacroStep {
	return []hidrpc.KeyboardMacroStep{{Keys: make([]byte, hidrpc.HidKeyBufferSize), Delay: 2000}}
}

func registeredKeyboardMacro() *keyboardMacroRun {
	keyboardMacroLock.Lock()
	defer keyboardMacroLock.Unlock()
	return keyboardMacroCurrent
}

func waitForRegisteredMacro(t *testing.T) *keyboardMacroRun {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if run := registeredKeyboardMacro(); run != nil {
			return run
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no keyboard macro was registered")
	return nil
}

func TestHidCancelStopsMacroWaitingToRun(t *testing.T) {
	defer cancelKeyboardMacro()
	session := &Session{done: make(chan struct{})}
	session.initQueues()
	log := zerolog.Nop()
	handle := getOnHidMessageHandler(session, &log, "hidrpc")

	keyboardMacroExec.Lock()
	held := true
	defer func() {
		if held {
			keyboardMacroExec.Unlock()
		}
	}()

	done := make(chan error, 1)
	go func() { done <- rpcExecuteKeyboardMacro(nil, slowMacro()) }()
	waitForRegisteredMacro(t)

	handle(webrtc.DataChannelMessage{Data: []byte{byte(hidrpc.TypeCancelKeyboardMacroReport)}})
	keyboardMacroExec.Unlock()
	held = false

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiting macro returned %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("the macro canceled while waiting ran after the running macro finished")
	}
}

func TestLateCleanupOfCanceledMacroKeepsReplacementCancelable(t *testing.T) {
	defer cancelKeyboardMacro()
	a := registerKeyboardMacro()
	cancelKeyboardMacro()
	b := registerKeyboardMacro()

	a.finish()
	cancelKeyboardMacro()

	if a.ctx.Err() == nil {
		t.Fatal("macro A was not canceled")
	}
	if b.ctx.Err() != context.Canceled {
		t.Fatal("macro A's late cleanup made macro B impossible to cancel")
	}
}
