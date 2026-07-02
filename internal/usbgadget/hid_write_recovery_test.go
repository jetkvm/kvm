package usbgadget

import (
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// fillPipeBuffer writes to w until the kernel pipe buffer is full, so that
// subsequent writes block and exceed their write deadline — simulating a HID
// endpoint whose host side stopped draining reports (issue #1512).
func fillPipeBuffer(t *testing.T, w *os.File) {
	t.Helper()
	chunk := make([]byte, 4096)
	for {
		if err := w.SetWriteDeadline(time.Now().Add(5 * time.Millisecond)); err != nil {
			t.Fatalf("SetWriteDeadline: %v", err)
		}
		if _, err := w.Write(chunk); err != nil {
			return
		}
	}
}

func drainPipe(t *testing.T, r *os.File) {
	t.Helper()
	buf := make([]byte, 65536)
	for {
		if err := r.SetReadDeadline(time.Now().Add(5 * time.Millisecond)); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		if _, err := r.Read(buf); err != nil {
			return
		}
	}
}

func newTestGadgetWithKeyboard(w *os.File) *UsbGadget {
	logger := zerolog.Nop()
	return &UsbGadget{
		log:                   &logger,
		logSuppressionCounter: make(map[string]int),
		keyboardHidFile:       w,
	}
}

func TestKeyboardWriteTimeoutStreak(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	u := newTestGadgetWithKeyboard(w)
	fillPipeBuffer(t, w)

	report := make([]byte, hidKeyBufferSize)
	for i := 1; i <= HidWriteTimeoutEscalationThreshold; i++ {
		// Timed-out writes are deliberately swallowed (host-suspend tolerance),
		// but each one must be counted so recovery can escalate.
		if err := u.keyboardWriteHidFileLocked(0, report); err != nil {
			t.Fatalf("write %d: expected timeout to be swallowed, got %v", i, err)
		}
		if got := u.KeyboardWriteTimeoutStreak(); got != i {
			t.Fatalf("after %d timed-out writes, streak = %d, want %d", i, got, i)
		}
	}

	// A successful write means the endpoint is healthy again: streak resets.
	drainPipe(t, r)
	if err := u.keyboardWriteHidFileLocked(0, report); err != nil {
		t.Fatalf("write after drain: %v", err)
	}
	if got := u.KeyboardWriteTimeoutStreak(); got != 0 {
		t.Fatalf("streak after successful write = %d, want 0", got)
	}
}

func TestResetHIDFilesClearsWriteTimeoutStreaks(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	u := newTestGadgetWithKeyboard(w)
	fillPipeBuffer(t, w)

	report := make([]byte, hidKeyBufferSize)
	if err := u.keyboardWriteHidFileLocked(0, report); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Recovery closes and reopens the HID files; stale streaks must not
	// survive into the new gadget instance and immediately re-trigger it.
	u.ResetHIDFiles()
	if got := len(u.hidWriteTimeoutStreaks); got != 0 {
		t.Fatalf("streak map has %d entries after ResetHIDFiles, want 0", got)
	}
	if got := u.KeyboardWriteTimeoutStreak(); got != 0 {
		t.Fatalf("streak after ResetHIDFiles = %d, want 0", got)
	}
}

func TestShouldEscalateHidWriteRecovery(t *testing.T) {
	now := time.Unix(100000, 0)
	streak := HidWriteTimeoutEscalationThreshold

	tests := []struct {
		name        string
		state       string
		desired     bool
		timeouts    int
		lastAttempt time.Time
		want        bool
	}{
		{
			name:     "escalate when writes time out while configured",
			state:    USBStateConfigured,
			desired:  true,
			timeouts: streak,
			want:     true,
		},
		{
			name:     "skip below timeout threshold",
			state:    USBStateConfigured,
			desired:  true,
			timeouts: streak - 1,
			want:     false,
		},
		{
			name:     "skip when host is suspended",
			state:    "suspended",
			desired:  true,
			timeouts: streak,
			want:     false,
		},
		{
			name:     "skip when gadget is detached (handled by rebind recovery)",
			state:    USBStateNotAttached,
			desired:  true,
			timeouts: streak,
			want:     false,
		},
		{
			name:     "skip when emulation intentionally disabled",
			state:    USBStateConfigured,
			desired:  false,
			timeouts: streak,
			want:     false,
		},
		{
			name:        "rate limit repeated escalations",
			state:       USBStateConfigured,
			desired:     true,
			timeouts:    streak,
			lastAttempt: now.Add(-HidWriteRecoveryRetryInterval + time.Second),
			want:        false,
		},
		{
			name:        "allow retry after interval passes",
			state:       USBStateConfigured,
			desired:     true,
			timeouts:    streak,
			lastAttempt: now.Add(-HidWriteRecoveryRetryInterval),
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldEscalateHidWriteRecovery(tt.state, tt.desired, tt.timeouts, tt.lastAttempt, now)
			if got != tt.want {
				t.Fatalf("ShouldEscalateHidWriteRecovery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVerifyKeyboardWritable(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	u := newTestGadgetWithKeyboard(w)

	// Stalled endpoint: the probe must surface the timeout, not swallow it
	// like the regular report path does.
	fillPipeBuffer(t, w)
	if err := u.VerifyKeyboardWritable(); err == nil {
		t.Fatal("expected probe to fail while writes stall")
	}

	// Healthy endpoint: the probe passes and is a no-op for the host.
	drainPipe(t, r)
	if err := u.VerifyKeyboardWritable(); err != nil {
		t.Fatalf("probe on writable keyboard file: %v", err)
	}
}
