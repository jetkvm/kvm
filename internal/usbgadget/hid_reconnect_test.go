package usbgadget

import (
	"bytes"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// The callback replaces only controller IO; the actual reset finalizer and
// recovery probe must not retain/replay a keyboard state from the old host.
func TestReconnectDoesNotReplayHeldKeyboard(t *testing.T) {
	u := inputTestGadget(t)
	beforeR, beforeW := reportPipe(t)
	u.keyboardHidFile = beforeW
	if err := u.KeyboardReport(2, []byte{30, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	readReport(t, beforeR, []byte{2, 0, 30, 0, 0, 0, 0, 0})
	if err := u.rebindUsbWith(func() error { u.ResetHIDFiles(); u.resetHidHandover(); return nil }); err != nil {
		t.Fatal(err)
	}
	state := u.GetKeysDownState()
	if state.Modifier != 0 || !bytes.Equal(state.Keys, []byte{0, 0, 0, 0, 0, 0}) {
		t.Errorf("reconnect retained old keyboard state: %+v", state)
	}
	afterR, afterW := reportPipe(t)
	u.keyboardHidFile = afterW
	if err := u.VerifyKeyboardWritable(); err != nil {
		t.Fatal(err)
	}
	readReport(t, afterR, []byte{0, 0, 0, 0, 0, 0, 0, 0})
}

func TestReconnectWaitsForLateKeyboardStateCommit(t *testing.T) {
	u := inputTestGadget(t)
	r, w := reportPipe(t)
	u.keyboardHidFile = w
	u.keyboardStateLock.Lock()
	var once sync.Once
	release := func() { once.Do(u.keyboardStateLock.Unlock) }
	defer release()
	written := make(chan error, 1)
	go func() { written <- u.KeyboardReport(2, []byte{30, 0, 0, 0, 0, 0}) }()
	readReport(t, r, []byte{2, 0, 30, 0, 0, 0, 0, 0})
	// IO has completed; prove the old lifecycle read admission ended while the
	// real writer remains blocked at its state commit under keyboardMutex.
	deadline := time.Now().Add(2 * time.Second)
	for !u.hidLifecycle.TryLock() {
		if time.Now().After(deadline) {
			release()
			select {
			case <-written:
			case <-time.After(2 * time.Second):
				t.Error("fixture writer failed to stop")
			}
			t.Fatal("fixture: writer did not leave IO admission")
		}
		runtime.Gosched()
	}
	u.hidLifecycle.Unlock()
	entered := make(chan struct{})
	rebound := make(chan error, 1)
	go func() {
		rebound <- u.rebindUsbWith(func() error { close(entered); u.ResetHIDFiles(); u.resetHidHandover(); return nil })
	}()
	select {
	case <-entered:
		t.Error("reconnect overtook old writer before its state commit")
	case <-time.After(25 * time.Millisecond):
	}
	release()
	select {
	case err := <-written:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not complete")
	}
	select {
	case err := <-rebound:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect deadlocked")
	}
	select {
	case <-entered:
	default:
		t.Fatal("reconnect never entered after writer completed")
	}
	state := u.GetKeysDownState()
	if state.Modifier != 0 || !bytes.Equal(state.Keys, []byte{0, 0, 0, 0, 0, 0}) {
		t.Errorf("old writer state survived reconnect: %+v", state)
	}
	afterR, afterW := reportPipe(t)
	u.keyboardHidFile = afterW
	if err := u.VerifyKeyboardWritable(); err != nil {
		t.Fatal(err)
	}
	readReport(t, afterR, []byte{0, 0, 0, 0, 0, 0, 0, 0})
}

func TestAutoReleaseStillReleasesOrdinaryKey(t *testing.T) {
	u := inputTestGadget(t)
	defer u.CancelAllAutoReleaseTimers()
	r, w := reportPipe(t)
	u.keyboardHidFile = w
	if err := u.KeypressReport(30, true); err != nil {
		t.Fatal(err)
	}
	readReport(t, r, []byte{0, 0, 30, 0, 0, 0, 0, 0})
	readReport(t, r, []byte{0, 0, 0, 0, 0, 0, 0, 0})
	// Peer receipt precedes the callback's state commit; join the production
	// keyboard transaction before reading the completed callback's memory.
	keyboardMutex.Lock()
	state := u.GetKeysDownState()
	keyboardMutex.Unlock()
	if state.Modifier != 0 || !bytes.Equal(state.Keys, []byte{0, 0, 0, 0, 0, 0}) {
		t.Fatalf("ordinary timer left key held: %+v", state)
	}
}

func TestFDReopenPreservesHeldKeyboardAndModifier(t *testing.T) {
	u := inputTestGadget(t)
	beforeR, beforeW := reportPipe(t)
	u.keyboardHidFile = beforeW
	if err := u.KeyboardReport(2, []byte{30, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	readReport(t, beforeR, []byte{2, 0, 30, 0, 0, 0, 0, 0})
	// A duplex socket is the local reopened endpoint: the real LED reader may
	// block on its input while the probe writes to the peer, without a HID node.
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	afterW := os.NewFile(uintptr(fds[0]), "reopened-test-keyboard")
	afterR := os.NewFile(uintptr(fds[1]), "test-host-peer")
	t.Cleanup(func() { u.ResetHIDFiles(); afterW.Close(); afterR.Close() })
	u.hidOpenFile = func(string, int, os.FileMode) (*os.File, error) { return afterW, nil }
	if err := u.ReopenKeyboardHidFile(); err != nil {
		t.Fatal(err)
	}
	if err := u.VerifyKeyboardWritable(); err != nil {
		t.Fatal(err)
	}
	readReport(t, afterR, []byte{2, 0, 30, 0, 0, 0, 0, 0})
	// No logical reconnect occurred: a long full-state hold must survive beyond
	// the ordinary individual-key timer duration without a modifier/mouse TTL.
	time.Sleep(250 * time.Millisecond)
	if state := u.GetKeysDownState(); state.Modifier != 2 || !bytes.Equal(state.Keys, []byte{30, 0, 0, 0, 0, 0}) {
		t.Fatalf("FD reopen/250ms hold lost state: %+v", state)
	}
	if err := u.KeyboardReport(0, nil); err != nil {
		t.Fatal(err)
	}
	readReport(t, afterR, []byte{0, 0, 0, 0, 0, 0, 0, 0})
}
func TestReconnectRetiresOldAutoReleaseCallback(t *testing.T) {
	u := inputTestGadget(t)
	defer u.CancelAllAutoReleaseTimers()
	beforeR, beforeW := reportPipe(t)
	u.keyboardHidFile = beforeW
	if err := u.KeypressReport(30, true); err != nil {
		t.Fatal(err)
	}
	readReport(t, beforeR, []byte{0, 0, 30, 0, 0, 0, 0, 0})
	u.kbdAutoReleaseLock.Lock()
	oldTimer := u.kbdAutoReleaseTimers[30]
	oldStopped := oldTimer != nil && oldTimer.Stop()
	u.kbdAutoReleaseLock.Unlock()
	if !oldStopped {
		t.Fatal("fixture: old timer expired before callback fencing setup")
	}
	if err := u.rebindUsbWith(func() error { u.ResetHIDFiles(); u.resetHidHandover(); return nil }); err != nil {
		t.Fatal(err)
	}
	afterR, afterW := reportPipe(t)
	u.keyboardHidFile = afterW
	if err := u.KeypressReport(30, true); err != nil {
		t.Fatal(err)
	}
	readReport(t, afterR, []byte{0, 0, 30, 0, 0, 0, 0, 0})
	u.kbdAutoReleaseLock.Lock()
	newTimer := u.kbdAutoReleaseTimers[30]
	newStopped := newTimer != nil && newTimer.Stop()
	u.kbdAutoReleaseLock.Unlock()
	if !newStopped {
		t.Fatal("fixture: new timer expired before stale callback invocation")
	}
	keyboardMutex.Lock()
	u.performAutoReleaseLocked(30, oldTimer)
	keyboardMutex.Unlock()
	if state := u.GetKeysDownState(); state.Keys[0] != 30 {
		t.Fatalf("old callback released new-generation key: %+v", state)
	}
	noInputReport(t, afterR)
	keyboardMutex.Lock()
	u.performAutoReleaseLocked(30, newTimer)
	keyboardMutex.Unlock()
	readReport(t, afterR, []byte{0, 0, 0, 0, 0, 0, 0, 0})
}

// An expired AfterFunc may already be queued on keyboardMutex when a key's
// keep-alive replaces its timer. Stop alone cannot retire that callback.
func TestAutoReleaseQueuedCallbackCannotReleaseReplacement(t *testing.T) {
	u := inputTestGadget(t)
	defer u.CancelAllAutoReleaseTimers()
	r, w := reportPipe(t)
	u.keyboardHidFile = w
	if err := u.KeypressReport(30, true); err != nil {
		t.Fatal(err)
	}
	readReport(t, r, []byte{0, 0, 30, 0, 0, 0, 0, 0})
	u.kbdAutoReleaseLock.Lock()
	oldTimer := u.kbdAutoReleaseTimers[30]
	u.kbdAutoReleaseLock.Unlock()
	keyboardMutex.Lock()
	// Allow the real old callback to expire while it cannot enter a keyboard transaction.
	time.Sleep(2 * DefaultAutoReleaseDuration)
	if oldTimer.Stop() {
		keyboardMutex.Unlock()
		t.Fatal("fixture: old timer had not expired")
	}
	u.cancelAutoRelease(30)
	u.scheduleAutoRelease(30)
	u.DelayAutoReleaseWithDuration(5 * time.Second)
	keyboardMutex.Unlock()
	noInputReport(t, r)
	if state := u.GetKeysDownState(); state.Keys[0] != 30 {
		t.Fatalf("queued old timer released replacement: %+v", state)
	}
}
