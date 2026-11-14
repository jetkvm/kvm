package timesync

import (
	"errors"
	"testing"
	"time"

	"github.com/jetkvm/kvm/internal/sync"
	"github.com/rs/zerolog"
)

func newClockHookTestTimeSync(setClock func(now time.Time) error) *TimeSync {
	nop := zerolog.Nop()
	return &TimeSync{
		l:             &nop,
		clockHookLock: &sync.Mutex{},
		setClock:      setClock,
	}
}

func TestApplyTimeRunsHookAroundClockSet(t *testing.T) {
	var events []string
	ts := newClockHookTestTimeSync(func(time.Time) error {
		events = append(events, "set")
		return nil
	})
	ts.SetClockHook(ClockHook{
		Before: func(jump time.Duration) { events = append(events, "before "+jump.String()) },
		After:  func(jump time.Duration) { events = append(events, "after "+jump.String()) },
	})

	if err := ts.applyTime(zerolog.Nop(), 2*time.Hour); err != nil {
		t.Fatalf("applyTime: %v", err)
	}

	want := []string{"before 2h0m0s", "set", "after 2h0m0s"}
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events = %v, want %v", events, want)
		}
	}
}

// After must run even when the clock could not be set, or whatever Before
// stopped stays stopped.
func TestApplyTimeRunsAfterHookWhenClockSetFails(t *testing.T) {
	setErr := errors.New("date -s refused")
	ts := newClockHookTestTimeSync(func(time.Time) error { return setErr })
	afterRan := false
	ts.SetClockHook(ClockHook{After: func(time.Duration) { afterRan = true }})

	err := ts.applyTime(zerolog.Nop(), time.Hour)
	if !errors.Is(err, setErr) {
		t.Fatalf("applyTime error = %v, want it to wrap %v", err, setErr)
	}
	if !afterRan {
		t.Fatal("After hook did not run after a failed clock set")
	}
}

func TestApplyTimeWithoutHookSetsClock(t *testing.T) {
	setCalls := 0
	ts := newClockHookTestTimeSync(func(time.Time) error {
		setCalls++
		return nil
	})

	if err := ts.applyTime(zerolog.Nop(), time.Hour); err != nil {
		t.Fatalf("applyTime: %v", err)
	}
	if setCalls != 1 {
		t.Fatalf("clock set %d times, want 1", setCalls)
	}
}

// The time the Before hook takes must not end up in the clock: the offset is
// applied after the hook returns, not before it is called.
func TestApplyTimeAppliesOffsetAfterBeforeHook(t *testing.T) {
	const hookDelay = 50 * time.Millisecond
	var set time.Time
	ts := newClockHookTestTimeSync(func(now time.Time) error {
		set = now
		return nil
	})
	ts.SetClockHook(ClockHook{Before: func(time.Duration) { time.Sleep(hookDelay) }})

	if err := ts.applyTime(zerolog.Nop(), time.Hour); err != nil {
		t.Fatalf("applyTime: %v", err)
	}

	// set was computed from time.Now() just before the clock set, so it is at
	// most a few microseconds behind this; applying the offset before the
	// hook would put it a full hookDelay behind.
	if lag := time.Now().Add(time.Hour).Sub(set); lag >= hookDelay/2 {
		t.Fatalf("clock set lagged by %v, want the Before hook's delay (%v) kept out of it", lag, hookDelay)
	}
}
