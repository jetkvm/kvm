package usbgadget

import (
	"bytes"
	"errors"
	"io"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A duplex, pollable descriptor lets the real LED listener block in Read while
// reports are written, without opening any device node.
func hidTestSocket(t *testing.T) *os.File {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(fds[0]), "test-hid")
	peer := os.NewFile(uintptr(fds[1]), "test-host")
	t.Cleanup(func() { file.Close(); peer.Close() })
	return file
}

func waitHIDTest(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("HID lifecycle operation did not complete")
	}
}

func TestHIDRebindBlocksDescriptorAdmissions(t *testing.T) {
	operations := map[string]func(*UsbGadget) error{
		"keyboard open":   (*UsbGadget).OpenKeyboardHidFile,
		"keyboard reopen": (*UsbGadget).ReopenKeyboardHidFile,
		"keyboard probe":  (*UsbGadget).VerifyKeyboardWritable,
		"keyboard write": func(u *UsbGadget) error {
			keyboardMutex.Lock()
			defer keyboardMutex.Unlock()
			return u.keyboardWriteHidFileLocked(0, make([]byte, hidKeyBufferSize))
		},
		"wake write":     func(u *UsbGadget) error { return u.wakeWriteHidFile(0) },
		"absolute mouse": func(u *UsbGadget) error { return u.AbsMouseReport(0, 0, 0) },
		"absolute wheel": func(u *UsbGadget) error { return u.AbsMouseWheelReport(1, 0) },
		"relative mouse": func(u *UsbGadget) error { return u.RelMouseReport(0, 0, 0) },
		"relative wheel": func(u *UsbGadget) error { return u.RelMouseWheelReport(1, 0) },
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			u := newTestGadgetWithKeyboard(nil)
			u.enabledDevices = defaultUsbGadgetDevices
			file := hidTestSocket(t)
			u.hidOpenFile = func(string, int, os.FileMode) (*os.File, error) { return file, nil }
			t.Cleanup(u.ResetHIDFiles)

			cleaned := make(chan struct{})
			resume := make(chan struct{})
			var resumeOnce sync.Once
			release := func() { resumeOnce.Do(func() { close(resume) }) }
			defer release()
			rebound := make(chan struct{})
			go func() {
				_ = u.rebindUsbWith(func() error {
					u.ResetHIDFiles()
					close(cleaned)
					<-resume // Controller rebind is paused after descriptor cleanup.
					return nil
				})
				close(rebound)
			}()
			waitHIDTest(t, cleaned)

			started := make(chan struct{})
			result := make(chan error, 1)
			go func() { close(started); result <- operation(u) }()
			waitHIDTest(t, started)
			select {
			case err := <-result:
				t.Errorf("descriptor operation crossed paused rebind: %v", err)
				release()
				waitHIDTest(t, rebound)
				return
			case <-time.After(25 * time.Millisecond):
			}
			release()
			waitHIDTest(t, rebound)
			select {
			case err := <-result:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("descriptor operation deadlocked after rebind")
			}
		})
	}
}

func TestHIDRebindDrainsAdmittedOpenAndClosesLEDReader(t *testing.T) {
	u := newTestGadgetWithKeyboard(nil)
	file := hidTestSocket(t)
	t.Cleanup(u.ResetHIDFiles)
	opening := make(chan struct{})
	resume := make(chan struct{})
	var resumeOnce sync.Once
	release := func() { resumeOnce.Do(func() { close(resume) }) }
	defer release()
	u.hidOpenFile = func(string, int, os.FileMode) (*os.File, error) {
		close(opening)
		<-resume
		return file, nil
	}
	opened := make(chan error, 1)
	go func() { opened <- u.OpenKeyboardHidFile() }()
	waitHIDTest(t, opening)

	entered := make(chan struct{})
	rebound := make(chan struct{})
	go func() {
		_ = u.rebindUsbWith(func() error {
			close(entered)
			u.ResetHIDFiles() // Must close the admitted open, including its LED reader.
			return nil
		})
		close(rebound)
	}()
	select {
	case <-entered:
		t.Error("rebind started while an admitted open was still running")
	case <-time.After(25 * time.Millisecond):
	}
	release()
	if err := <-opened; err != nil {
		t.Fatal(err)
	}
	waitHIDTest(t, rebound)
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("old descriptor survived rebind: %v", err)
	}
}

func TestHIDRebindDrainsTimedOutBackgroundOpen(t *testing.T) {
	u := newTestGadgetWithKeyboard(nil)
	file := hidTestSocket(t)
	opening := make(chan struct{})
	resume := make(chan struct{})
	var resumeOnce sync.Once
	release := func() { resumeOnce.Do(func() { close(resume) }) }
	defer release()
	u.hidOpenFile = func(string, int, os.FileMode) (*os.File, error) {
		close(opening)
		<-resume
		return file, nil
	}
	// Match admission by a real keyboard/wake opener, but shorten its deadline.
	u.hidLifecycle.RLock()
	_, err := u.openWithTimeout("test-hid", os.O_RDWR, 0, time.Millisecond)
	u.hidLifecycle.RUnlock()
	if err == nil {
		t.Fatal("expected the open to time out")
	}
	waitHIDTest(t, opening)

	result := make(chan error, 1)
	go func() {
		result <- u.rebindUsbWith(func() error {
			_, err := file.Stat()
			if !errors.Is(err, os.ErrClosed) {
				return errors.New("rebind began before the timed-out open closed its descriptor")
			}
			return nil
		})
	}()
	select {
	case err := <-result:
		t.Fatalf("rebind did not drain the background open: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	release()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("rebind did not resume after background-open cleanup")
	}
}

func TestHIDRebindDrainsAdmittedWriter(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	u := newTestGadgetWithKeyboard(w)
	keys := []byte{4, 0, 0, 0, 0, 0}

	// Stop a writer after lifecycle admission but before it opens/writes the
	// descriptor. Queue rebind behind it, then allow the writer to finish.
	// A nested lifecycle RLock in the open path would deadlock this sequence.
	u.keyboardLock.Lock()
	var unlockOnce sync.Once
	unlock := func() { unlockOnce.Do(u.keyboardLock.Unlock) }
	defer unlock()
	written := make(chan error, 1)
	go func() {
		keyboardMutex.Lock()
		defer keyboardMutex.Unlock()
		written <- u.keyboardWriteHidFileLocked(2, keys)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for u.hidLifecycle.TryLock() {
		u.hidLifecycle.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("writer did not acquire lifecycle admission")
		}
		runtime.Gosched()
	}
	entered := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- u.rebindUsbWith(func() error {
			close(entered)
			u.ResetHIDFiles()
			return nil
		})
	}()
	select {
	case <-entered:
		t.Error("rebind overtook an admitted writer")
	case <-time.After(25 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("writer and rebind deadlocked")
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r) // Rebind closed w, so this must reach EOF.
	want := append([]byte{2, 0}, keys...)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("admitted report lost before descriptor cleanup: got %v, error %v", got, err)
	}
}
