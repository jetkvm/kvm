package usbgadget

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestHIDWriteSurvivesBriefBackpressure(t *testing.T) {
	const maxAttempts = 5
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if tryHIDWriteWithBriefBackpressure(t, attempt) {
			return
		}
	}
	t.Fatalf("no valid backpressure timing window in %d attempts", maxAttempts)
}

func tryHIDWriteWithBriefBackpressure(t *testing.T, attempt int) bool {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	fillPipeBuffer(t, w)
	if err := r.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	// The previous report can remain pending while the host or the device
	// is briefly descheduled. The next report must wait, not disappear.
	type drainResult struct {
		started, finished time.Time
		err               error
	}
	start := make(chan struct{})
	drained := make(chan drainResult, 1)
	go func() {
		<-start
		time.Sleep(25 * time.Millisecond)
		started := time.Now()
		_, err := io.CopyN(io.Discard, r, 4096)
		drained <- drainResult{started, time.Now(), err}
	}()
	report := []byte{2, 0, 6, 0, 0, 0, 0, 0}
	u := newTestGadgetWithKeyboard(w)
	started := time.Now()
	close(start)
	n, err := u.writeWithTimeout(w, report)
	finished := time.Now()
	drain := <-drained
	if os.IsTimeout(drain.err) {
		t.Logf("attempt %d: pipe setup/drain missed its deadline", attempt)
		return false
	}
	if drain.err != nil {
		t.Fatal(drain.err)
	}
	// The drain must occur after the old 10 ms deadline, with headroom before
	// the new 100 ms deadline. Oversleeping the intended window is a fixture
	// scheduling failure, not evidence that a report was dropped.
	if drain.started.Sub(started) < 20*time.Millisecond || drain.finished.Sub(started) > 75*time.Millisecond {
		t.Logf("attempt %d: invalid drain window [%v, %v]", attempt, drain.started.Sub(started), drain.finished.Sub(started))
		return false
	}
	if n == len(report) && err == nil && (finished.Before(drain.started) || finished.Sub(started) > hidWriteTimeout) {
		// An early success did not encounter backpressure. A success outside
		// the write budget may have started late after the caller was paused;
		// do not let that make the old 10 ms implementation appear to pass.
		t.Logf("attempt %d: invalid successful write duration %v", attempt, finished.Sub(started))
		return false
	}
	if err != nil || n != len(report) {
		t.Fatalf("report lost during brief backpressure: wrote %d/%d bytes, error %v", n, len(report), err)
	}
	return true
}

func TestWriteTimeoutLoggingResumesAfterSuccessfulWrite(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	var logs bytes.Buffer
	logger := zerolog.New(&logs)
	u := newTestGadgetWithKeyboard(w)
	u.log = &logger
	report := make([]byte, hidKeyBufferSize)

	// Consecutive timeouts produce one error, but a new timeout after a
	// successful write must be visible even if it is an isolated failure.
	for episode := 1; episode <= 2; episode++ {
		fillPipeBuffer(t, w)
		for range 2 {
			if err := u.keyboardWriteHidFileLocked(0, report); err != nil {
				t.Fatalf("timed-out write: %v", err)
			}
		}
		if got := strings.Count(logs.String(), "write timed out:"); got != episode {
			t.Fatalf("after episode %d: got %d timeout logs, want %d", episode, got, episode)
		}
		drainPipe(t, r)
		if err := u.keyboardWriteHidFileLocked(0, report); err != nil {
			t.Fatalf("successful write: %v", err)
		}
	}
}

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
		if err := u.keyboardWriteHidFileLocked(0, report); err != nil {
			t.Fatalf("write %d: expected timeout to be swallowed, got %v", i, err)
		}
		if got := u.KeyboardWriteTimeoutStreak(); got != i {
			t.Fatalf("after %d timed-out writes, streak = %d, want %d", i, got, i)
		}
	}

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
			name:     "skip when gadget is detached",
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

	fillPipeBuffer(t, w)
	if err := u.VerifyKeyboardWritable(); err == nil {
		t.Fatal("expected probe to fail while writes stall")
	}

	drainPipe(t, r)
	if err := u.VerifyKeyboardWritable(); err != nil {
		t.Fatalf("probe on writable keyboard file: %v", err)
	}
}
