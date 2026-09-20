//go:build linux && arm

package kvm

import (
	"testing"
	"time"
)

// A cold boot builds the schedule while the clock is still at the epoch, and
// the later jump to real time wedges gocron in a multi-million-step catch-up
// walk. initJiggler waits this out instead.
// Saving jiggler settings during the deferred-start window must not build the
// schedule: the scheduler is nil, so nothing stops it, and it would land back
// on the epoch clock - the exact catch-up this defer exists to avoid.
func TestRebuildJigglerCronTabSkipsWhileDeferred(t *testing.T) {
	schedulerLock.Lock()
	jigglerSchedulePending = true
	schedulerLock.Unlock()
	t.Cleanup(func() {
		schedulerLock.Lock()
		jigglerSchedulePending = false
		schedulerLock.Unlock()
	})

	if err := rebuildJigglerCronTab(); err != nil {
		t.Fatalf("rebuildJigglerCronTab while deferred: %v", err)
	}
	if scheduler != nil {
		t.Fatal("built a scheduler while the clock was still untrustworthy")
	}
}

func TestWaitForTrustworthyClockReturnsOnSync(t *testing.T) {
	calls := 0
	synced := func() bool {
		calls++
		return calls >= 3
	}

	start := time.Now()
	if !waitForTrustworthyClock(synced, time.Millisecond, time.Minute) {
		t.Fatal("waitForTrustworthyClock reported a timeout, want success")
	}
	if calls != 3 {
		t.Errorf("synced called %d times, want 3", calls)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("took %v, want to return as soon as the clock synced", elapsed)
	}
}

// An offline device never syncs. Waiting forever would leave it without a
// jiggler at all, so the wait has to give up and let the caller schedule.
func TestWaitForTrustworthyClockGivesUpAtLimit(t *testing.T) {
	start := time.Now()
	if waitForTrustworthyClock(func() bool { return false }, time.Millisecond, 20*time.Millisecond) {
		t.Fatal("waitForTrustworthyClock reported success, want a timeout")
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Errorf("returned after %v, want at least the 20ms limit", elapsed)
	}
}

func TestWaitForTrustworthyClockSucceedsImmediately(t *testing.T) {
	if !waitForTrustworthyClock(func() bool { return true }, time.Millisecond, time.Minute) {
		t.Fatal("waitForTrustworthyClock reported a timeout for an already-synced clock")
	}
}
