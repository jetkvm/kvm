//go:build linux && arm

package kvm

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jetkvm/kvm/internal/hidrpc"
	"github.com/jetkvm/kvm/internal/usbgadget"
	"github.com/pion/webrtc/v4"
)

func TestRetiredSessionDiscardsDequeuedKeepAlive(t *testing.T) {
	oldGadget := gadget
	gadget = nil
	t.Cleanup(func() { gadget = oldGadget })
	s := &Session{done: make(chan struct{})}
	s.close()
	onHidMessage(hidQueueMessage{DataChannelMessage: webrtc.DataChannelMessage{Data: []byte{byte(hidrpc.TypeKeypressKeepAliveReport)}}}, s)
	if !s.lastTimerResetTime.IsZero() || !s.lastKeepAliveArrivalTime.IsZero() {
		t.Fatal("retired session revived input keepalive")
	}
}

func TestRetiredSessionDiscardsQueuedRPC(t *testing.T) {
	s := &Session{done: make(chan struct{}), RPCChannel: &webrtc.DataChannel{}}
	s.close()
	old := rpcHandlers["relMouseReport"]
	t.Cleanup(func() { rpcHandlers["relMouseReport"] = old })
	calls := 0
	rpcHandlers["relMouseReport"] = RPCHandler{Func: func() error { calls++; return nil }}
	onRPCMessage(webrtc.DataChannelMessage{Data: []byte(`{"jsonrpc":"2.0","method":"relMouseReport","id":1}`)}, s)
	if calls != 0 {
		t.Fatal("retired session dispatched queued input")
	}
}

func setupRetirementState(t *testing.T) {
	t.Helper()
	oldSession, oldGadget := currentSession, gadget
	keyboardMacroLock.Lock()
	oldCancel := keyboardMacroCancel
	keyboardMacroCancel = nil
	keyboardMacroLock.Unlock()
	gadget = nil
	currentSession = nil
	t.Cleanup(func() {
		currentSession, gadget = oldSession, oldGadget
		keyboardMacroLock.Lock()
		keyboardMacroCancel = oldCancel
		keyboardMacroLock.Unlock()
	})
}

func retirementPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	return r, w
}

func readRetirementBytes(t *testing.T, r *os.File, want []byte) {
	t.Helper()
	if err := r.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(r, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("reports=%v want=%v", got, want)
	}
}

func TestSessionRetirementWaitsForAdmittedRPC(t *testing.T) {
	setupRetirementState(t)
	s := &Session{done: make(chan struct{}), RPCChannel: &webrtc.DataChannel{}}
	currentSession = s
	r, w := retirementPipe(t)
	entered, proceed, handled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	oldHandler := rpcHandlers["relMouseReport"]
	t.Cleanup(func() { rpcHandlers["relMouseReport"] = oldHandler })
	rpcHandlers["relMouseReport"] = RPCHandler{Func: func() error {
		close(entered)
		<-proceed
		_, err := w.Write([]byte{1})
		return err
	}}
	go func() {
		defer close(handled)
		onRPCMessage(webrtc.DataChannelMessage{Data: []byte(`{"jsonrpc":"2.0","method":"relMouseReport","id":1}`)}, s)
	}()
	<-entered
	closed := make(chan struct{})
	go func() { s.closeWithRelease(func() error { _, err := w.Write([]byte{0}); return err }); close(closed) }()
	select {
	case <-closed:
		close(proceed)
		<-handled
		t.Fatal("close passed an admitted report")
	case <-time.After(20 * time.Millisecond):
	}
	close(proceed)
	<-handled
	<-closed
	readRetirementBytes(t, r, []byte{1, 0})
	if !s.isClosed() || currentSession != nil {
		t.Fatal("close did not retire owner")
	}
}

func TestSessionHandoffRejectsOldInputAndLateCleanup(t *testing.T) {
	setupRetirementState(t)
	a := &Session{done: make(chan struct{}), RPCChannel: &webrtc.DataChannel{}}
	b := &Session{done: make(chan struct{}), RPCChannel: &webrtc.DataChannel{}}
	currentSession = a
	r, w := retirementPipe(t)
	releases := 0
	release := func() error { releases++; _, err := w.Write([]byte{0}); return err }
	if previous := replaceSessionInputs(b, release); previous != a || !a.isClosed() {
		t.Fatal("handoff left old input admitted")
	}
	oldHandler := rpcHandlers["relMouseReport"]
	t.Cleanup(func() { rpcHandlers["relMouseReport"] = oldHandler })
	rpcHandlers["relMouseReport"] = RPCHandler{Func: func() error { _, err := w.Write([]byte{2}); return err }}
	packet := webrtc.DataChannelMessage{Data: []byte(`{"jsonrpc":"2.0","method":"relMouseReport","id":1}`)}
	onRPCMessage(packet, a)
	onRPCMessage(packet, b)
	a.closeWithRelease(release)
	if releases != 1 || currentSession != b {
		t.Fatalf("late old close released replacement: releases=%d current=%p", releases, currentSession)
	}
	readRetirementBytes(t, r, []byte{0, 2})
}

type retirementReleaser struct {
	calls []string
	fail  error
}

func (r *retirementReleaser) KeyboardReport(byte, []byte) error {
	r.calls = append(r.calls, "keyboard")
	return r.fail
}
func (r *retirementReleaser) RelMouseReport(int8, int8, uint8) error {
	r.calls = append(r.calls, "relative")
	return nil
}
func (r *retirementReleaser) ReleaseAbsoluteMouseButtons() error {
	r.calls = append(r.calls, "absolute")
	return nil
}

func TestSessionReleaseAttemptsEveryEnabledFunction(t *testing.T) {
	target := &retirementReleaser{fail: errors.New("keyboard failed")}
	err := releaseHIDInputs(&usbgadget.Devices{Keyboard: true, RelativeMouse: true, AbsoluteMouse: true}, target)
	if !errors.Is(err, target.fail) || !reflect.DeepEqual(target.calls, []string{"keyboard", "relative", "absolute"}) {
		t.Fatalf("release: calls=%v err=%v", target.calls, err)
	}
}

func TestSessionRetirementStopsActualHIDMacroBetweenWrites(t *testing.T) {
	setupRetirementState(t)
	a := &Session{done: make(chan struct{}), RPCChannel: &webrtc.DataChannel{}}
	b := &Session{done: make(chan struct{}), RPCChannel: &webrtc.DataChannel{}}
	currentSession = a
	r, w := retirementPipe(t)
	first := make(chan struct{})
	oldRun := runKeyboardMacroSteps
	t.Cleanup(func() { runKeyboardMacroSteps = oldRun })
	runKeyboardMacroSteps = func(ctx context.Context, steps []hidrpc.KeyboardMacroStep, caller *Session) error {
		if caller != a {
			t.Error("HID macro lost caller")
		}
		return executeKeyboardMacroSteps(ctx, steps, caller, func(_ byte, keys []byte) error {
			_, err := w.Write([]byte{keys[0]})
			if keys[0] == 4 {
				close(first)
			}
			return err
		})
	}
	packet := []byte{byte(hidrpc.TypeKeyboardMacroReport), 1, 0, 0, 0, 2, 0, 4, 0, 0, 0, 0, 0, 3, 232, 0, 5, 0, 0, 0, 0, 0, 0, 0}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		onHidMessage(hidQueueMessage{DataChannelMessage: webrtc.DataChannelMessage{Data: packet}}, a)
	}()
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatal("macro first step did not execute")
	}
	replaceSessionInputs(b, func() error { _, err := w.Write([]byte{0}); return err })
	if err := withSessionInput(b, func() error { _, err := w.Write([]byte{6}); return err }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("close retained a macro sleep")
	}
	readRetirementBytes(t, r, []byte{4, 0, 6})
	if err := r.SetReadDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	extra := make([]byte, 1)
	if n, _ := r.Read(extra); n != 0 {
		t.Fatalf("old macro wrote after retirement: %v", extra[:n])
	}
}

func TestMacroSessionAwareEntryPointsRemainUsable(t *testing.T) {
	setupRetirementState(t)
	if err := doExecuteKeyboardMacroForSession(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := executeKeyboardMacroForSession(nil, nil); err != nil {
		t.Fatal(err)
	}
}
