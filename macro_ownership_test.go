//go:build linux && arm

package kvm

import (
	"context"
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/jetkvm/kvm/internal/hidrpc"
	"github.com/pion/webrtc/v4"
	"github.com/rs/zerolog"
)

func macroOwnershipWait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("macro operation did not finish")
	}
}

// Admit through the real ordered-channel callback and decode/execute the queued
// message exactly as a session's keyboard worker does. Different sessions have
// independent workers; a slow old macro can unwind after a new one has started.
func macroOwnershipDispatch(t *testing.T, s *Session, key byte) <-chan struct{} {
	t.Helper()
	data := make([]byte, 15)
	data[0] = byte(hidrpc.TypeKeyboardMacroReport)
	data[1] = 1
	binary.BigEndian.PutUint32(data[2:6], 1)
	data[7] = key
	log := zerolog.Nop()
	getOnHidMessageHandler(s, &log, "hidrpc")(webrtc.DataChannelMessage{Data: data})
	if len(s.hidQueue[1]) != 1 {
		t.Fatal("macro was not admitted to keyboard queue")
	}
	msg := <-s.hidQueue[1]
	done := make(chan struct{})
	go func() { defer close(done); onHidMessage(msg, s) }()
	return done
}

func TestLateMacroCompletionPreservesReplacementCancellation(t *testing.T) {
	oldCurrent, oldRunner := currentSession, runKeyboardMacroSteps
	currentSession = nil // No transport notifications; assertion is runner cancellation.
	oldSession := &Session{done: make(chan struct{})}
	oldSession.initQueues()
	replacement := &Session{done: make(chan struct{})}
	replacement.initQueues()
	releaseOld := make(chan struct{})
	releaseNew := make(chan struct{})
	var onceOld, onceNew sync.Once
	unblockOld := func() { onceOld.Do(func() { close(releaseOld) }) }
	unblockNew := func() { onceNew.Do(func() { close(releaseNew) }) }
	var oldContext context.Context
	enteredOld, enteredNew, cancelledNew, extraStep := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	runKeyboardMacroSteps = func(ctx context.Context, steps []hidrpc.KeyboardMacroStep, session *Session) error {
		if steps[0].Keys[0] == 4 {
			if session != oldSession {
				t.Error("old macro lost its caller")
			}
			oldContext = ctx
			close(enteredOld)
			<-releaseOld
			return ctx.Err()
		}
		if session != replacement {
			t.Error("replacement macro lost its caller")
		}
		close(enteredNew)
		select {
		case <-ctx.Done():
			close(cancelledNew)
			return ctx.Err()
		case <-releaseNew:
			close(extraStep)
			return nil
		}
	}
	var oldDone, newDone <-chan struct{}
	t.Cleanup(func() {
		unblockOld()
		unblockNew()
		if oldDone != nil {
			macroOwnershipWait(t, oldDone)
		}
		if newDone != nil {
			macroOwnershipWait(t, newDone)
		}
		setKeyboardMacroCancel(nil)
		currentSession, runKeyboardMacroSteps = oldCurrent, oldRunner
	})
	oldDone = macroOwnershipDispatch(t, oldSession, 4)
	macroOwnershipWait(t, enteredOld)
	newDone = macroOwnershipDispatch(t, replacement, 5)
	macroOwnershipWait(t, enteredNew)
	if oldContext.Err() != context.Canceled {
		t.Fatal("replacement entry did not cancel old runner context")
	}
	unblockOld()
	macroOwnershipWait(t, oldDone) // Join the entire real completion, not just its runner.
	log := zerolog.Nop()
	getOnHidMessageHandler(replacement, &log, "hidrpc")(webrtc.DataChannelMessage{Data: []byte{byte(hidrpc.TypeCancelKeyboardMacroReport)}})
	select {
	case <-cancelledNew:
	case <-time.After(50 * time.Millisecond):
		unblockNew()
		macroOwnershipWait(t, newDone)
		t.Fatal("late old completion disowned replacement: cancel allowed another macro step")
	}
	macroOwnershipWait(t, newDone)
	select {
	case <-extraStep:
		t.Fatal("replacement executed another step after cancellation")
	default:
	}
}

func TestMacroCompletionRetiresContextAndCancellationSlot(t *testing.T) {
	oldCurrent, oldRunner := currentSession, runKeyboardMacroSteps
	session := &Session{done: make(chan struct{})}
	currentSession = session
	var running context.Context
	runKeyboardMacroSteps = func(ctx context.Context, steps []hidrpc.KeyboardMacroStep, caller *Session) error {
		running = ctx
		if caller != session || ctx.Err() != nil {
			t.Fatal("owned execution lost live context/caller")
		}
		return nil
	}
	t.Cleanup(func() { setKeyboardMacroCancel(nil); currentSession, runKeyboardMacroSteps = oldCurrent, oldRunner })
	if err := executeKeyboardMacroForSession(nil, session); err != nil {
		t.Fatal(err)
	}
	if running == nil || running.Err() != context.Canceled {
		t.Fatal("completed macro context was not retired")
	}
	keyboardMacroLock.Lock()
	slotCleared := keyboardMacroCancel == nil
	keyboardMacroLock.Unlock()
	if !slotCleared {
		t.Fatal("completed macro retained cancellation slot")
	}
	// Empty input executes the real session-aware step runner without keyboard IO.
	if err := doExecuteKeyboardMacroForSession(context.Background(), nil, session); err != nil {
		t.Fatal(err)
	}
}
