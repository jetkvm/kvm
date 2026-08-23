package usbgadget

import (
	"bytes"
	"errors"
	"io"
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
		enabledDevices:        Devices{Keyboard: true},
	}
}

func TestWriteHIDReportRejectsEveryNonConfiguredUSBState(t *testing.T) {
	for _, state := range []string{
		USBStateUnknown,
		USBStateNotAttached,
		"suspended",
	} {
		t.Run(state, func(t *testing.T) {
			called := false
			err := WriteHIDReport(state, func() error {
				called = true
				return nil
			})
			if err == nil {
				t.Fatal("WriteHIDReport returned success while USB was not configured")
			}
			if called {
				t.Fatal("WriteHIDReport attempted a write while USB was not configured")
			}
		})
	}
}

func TestWriteHIDReportReturnsWriteResult(t *testing.T) {
	want := errors.New("HID write failed")
	if err := WriteHIDReport(USBStateConfigured, func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("WriteHIDReport error = %v, want %v", err, want)
	}
	if err := WriteHIDReport(USBStateConfigured, func() error { return nil }); err != nil {
		t.Fatalf("WriteHIDReport successful write returned error: %v", err)
	}
}

type hidReportTestCase struct {
	name    string
	report  func(*UsbGadget) error
	setHID  func(*UsbGadget, *os.File)
	cleanup func(*UsbGadget)
	want    []byte
}

func hidReportTestCases() []hidReportTestCase {
	return []hidReportTestCase{
		{
			name:   "keyboard",
			report: func(u *UsbGadget) error { return u.KeyboardReport(0, []byte{4}) },
			setHID: func(u *UsbGadget, file *os.File) { u.keyboardHidFile = file },
			want:   []byte{0, 0, 4, 0, 0, 0, 0, 0},
		},
		{
			name:    "keypress",
			report:  func(u *UsbGadget) error { return u.KeypressReport(4, true) },
			setHID:  func(u *UsbGadget, file *os.File) { u.keyboardHidFile = file },
			cleanup: func(u *UsbGadget) { u.cancelAutoRelease(4) },
			want:    []byte{0, 0, 4, 0, 0, 0, 0, 0},
		},
		{
			name:   "absolute mouse",
			report: func(u *UsbGadget) error { return u.AbsMouseReport(1, 2, 0) },
			setHID: func(u *UsbGadget, file *os.File) { u.absMouseHidFile = file },
			want:   []byte{1, 0, 1, 0, 2, 0},
		},
		{
			name:   "relative mouse",
			report: func(u *UsbGadget) error { return u.RelMouseReport(1, 2, 0) },
			setHID: func(u *UsbGadget, file *os.File) { u.relMouseHidFile = file },
			want:   []byte{0, 1, 2, 0, 0},
		},
		{
			name:   "absolute mouse wheel",
			report: func(u *UsbGadget) error { return u.AbsMouseWheelReport(1, -1) },
			setHID: func(u *UsbGadget, file *os.File) { u.absMouseHidFile = file },
			want:   []byte{2, 1, 255},
		},
		{
			name:   "relative mouse wheel",
			report: func(u *UsbGadget) error { return u.RelMouseWheelReport(1, -1) },
			setHID: func(u *UsbGadget, file *os.File) { u.relMouseHidFile = file },
			want:   []byte{0, 0, 0, 1, 255},
		},
	}
}

func newHIDReportTestGadget() *UsbGadget {
	logger := zerolog.Nop()
	return &UsbGadget{
		log:                   &logger,
		logSuppressionCounter: make(map[string]int),
		keysDownState:         KeysDownState{Keys: make([]byte, hidKeyBufferSize)},
		kbdAutoReleaseTimers:  make(map[byte]*time.Timer),
		enabledDevices: Devices{
			Keyboard:      true,
			AbsoluteMouse: true,
			RelativeMouse: true,
		},
	}
}

func TestHIDReportsReturnFirstWriteTimeout(t *testing.T) {
	for _, tt := range hidReportTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()

			u := newHIDReportTestGadget()
			tt.setHID(u, w)
			fillPipeBuffer(t, w)

			if err := tt.report(u); !errors.Is(err, os.ErrDeadlineExceeded) {
				t.Fatalf("first stalled report error = %v, want deadline exceeded", err)
			}
		})
	}
}

func TestAcknowledgedHIDReportsAreWritten(t *testing.T) {
	for _, tt := range hidReportTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			defer w.Close()

			u := newHIDReportTestGadget()
			tt.setHID(u, w)

			if err := tt.report(u); err != nil {
				t.Fatalf("report returned error: %v", err)
			}
			if tt.cleanup != nil {
				tt.cleanup(u)
			}
			got := make([]byte, len(tt.want))
			if _, err := io.ReadFull(r, got); err != nil {
				t.Fatalf("read acknowledged report: %v", err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("written report = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFailedKeyboardReportDoesNotAdvanceDeliveredState(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	u := newTestGadgetWithKeyboard(w)
	u.keysDownState = KeysDownState{Keys: make([]byte, hidKeyBufferSize)}
	fillPipeBuffer(t, w)

	if err := u.KeyboardReport(0, []byte{4}); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("KeyboardReport error = %v, want deadline exceeded", err)
	}
	state := u.GetKeysDownState()
	if state.Modifier != 0 || !bytes.Equal(state.Keys, make([]byte, hidKeyBufferSize)) {
		t.Fatalf("keys-down state advanced after failed report: %+v", state)
	}
}

func TestDisabledHIDEndpointsReturnError(t *testing.T) {
	u := &UsbGadget{}
	for name, report := range map[string]func() error{
		"keyboard":             func() error { return u.KeyboardReport(0, []byte{4}) },
		"keypress":             func() error { return u.KeypressReport(4, true) },
		"absolute mouse":       func() error { return u.AbsMouseReport(1, 2, 0) },
		"relative mouse":       func() error { return u.RelMouseReport(1, 2, 0) },
		"absolute mouse wheel": func() error { return u.AbsMouseWheelReport(1, 0) },
		"relative mouse wheel": func() error { return u.RelMouseWheelReport(1, 0) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := report(); err == nil {
				t.Fatal("disabled endpoint returned success")
			}
		})
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
		// Every timed-out write is returned to its caller and counted so
		// asynchronous recovery can also escalate.
		if _, err := u.writeWithTimeout(w, report); !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("write %d: error = %v, want deadline exceeded", i, err)
		}
		if got := u.HIDWriteTimeoutStreak(); got != i {
			t.Fatalf("after %d timed-out writes, streak = %d, want %d", i, got, i)
		}
	}

	// A successful write means the endpoint is healthy again: streak resets.
	drainPipe(t, r)
	if _, err := u.writeWithTimeout(w, report); err != nil {
		t.Fatalf("write after drain: %v", err)
	}
	if got := u.HIDWriteTimeoutStreak(); got != 0 {
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
	if err := u.keyboardWriteHidFileLocked(0, report); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("write error = %v, want deadline exceeded", err)
	}

	// Recovery closes and reopens the HID files; stale streaks must not
	// survive into the new gadget instance and immediately re-trigger it.
	u.ResetHIDFiles()
	if got := len(u.hidWriteTimeoutStreaks); got != 0 {
		t.Fatalf("streak map has %d entries after ResetHIDFiles, want 0", got)
	}
	if got := u.HIDWriteTimeoutStreak(); got != 0 {
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

	// Stalled endpoint: the probe must surface the timeout.
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
