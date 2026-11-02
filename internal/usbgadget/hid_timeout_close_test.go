package usbgadget

import (
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"testing"
	"time"
)

// Stock writeWithTimeout deliberately swallows deadlines. A subsequent hard
// write failure must not hide the accumulated streak from USB recovery.
func TestKeyboardTimeoutStreakSurvivesFailedClose(t *testing.T) {
	for _, finish := range []string{"reset", "successful reopen"} {
		t.Run(finish, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			defer w.Close()
			u := newTestGadgetWithKeyboard(w)
			t.Cleanup(u.ResetHIDFiles)
			write := func() error {
				keyboardMutex.Lock()
				defer keyboardMutex.Unlock()
				return u.keyboardWriteHidFileLocked(0, make([]byte, hidKeyBufferSize))
			}
			fillPipeBuffer(t, w)
			for i := 1; i <= HidWriteTimeoutEscalationThreshold; i++ {
				if err := write(); err != nil {
					t.Fatalf("legacy timeout should be swallowed: %v", err)
				}
				if got := u.KeyboardWriteTimeoutStreak(); got != i {
					t.Fatalf("live FD streak=%d, want %d", got, i)
				}
			}
			// EPIPE is a real non-timeout error; production closes and clears the FD.
			r.Close()
			if err := write(); !errors.Is(err, unix.EPIPE) {
				t.Fatalf("expected EPIPE, got %v", err)
			}
			if u.keyboardHidFile != nil {
				t.Fatal("hard write error did not close FD")
			}
			if got := u.KeyboardWriteTimeoutStreak(); got != HidWriteTimeoutEscalationThreshold {
				t.Errorf("closed FD hid timeout streak: got %d", got)
			}
			if !ShouldEscalateHidWriteRecovery(USBStateConfigured, true, u.KeyboardWriteTimeoutStreak(), time.Time{}, time.Now()) {
				t.Error("closed FD suppressed recovery escalation")
			}
			u.hidOpenFile = func(string, int, os.FileMode) (*os.File, error) { return nil, os.ErrNotExist }
			if err := u.ReopenKeyboardHidFile(); err == nil {
				t.Fatal("failed open unexpectedly succeeded")
			}
			if got := u.KeyboardWriteTimeoutStreak(); got != HidWriteTimeoutEscalationThreshold {
				t.Errorf("failed reopen hid timeout streak: got %d", got)
			}
			if finish == "reset" {
				u.ResetHIDFiles()
				if got := u.KeyboardWriteTimeoutStreak(); got != 0 {
					t.Fatalf("reset streak=%d", got)
				}
				return
			}
			fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_NONBLOCK, 0)
			if err != nil {
				t.Fatal(err)
			}
			next := os.NewFile(uintptr(fds[0]), w.Name())
			peer := os.NewFile(uintptr(fds[1]), "test-host")
			t.Cleanup(func() { u.ResetHIDFiles(); next.Close(); peer.Close() })
			u.hidOpenFile = func(string, int, os.FileMode) (*os.File, error) { return next, nil }
			if err := u.ReopenKeyboardHidFile(); err != nil {
				t.Fatal(err)
			}
			if got := u.KeyboardWriteTimeoutStreak(); got != HidWriteTimeoutEscalationThreshold {
				t.Fatalf("open alone cleared streak=%d", got)
			}
			if err := write(); err != nil {
				t.Fatal(err)
			}
			peer.SetReadDeadline(time.Now().Add(time.Second))
			report := make([]byte, 8)
			if _, err := io.ReadFull(peer, report); err != nil {
				t.Fatal(err)
			}
			if got := u.KeyboardWriteTimeoutStreak(); got != 0 {
				t.Fatalf("successful write streak=%d", got)
			}
		})
	}
}
