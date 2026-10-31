//go:build linux && arm

package kvm

import (
	"testing"
	"time"
)

// useJigglerTestConfig points the package at a throwaway config with a valid
// schedule, and resets scheduler state when the test ends.
func useJigglerTestConfig(t *testing.T) {
	t.Helper()
	originalConfig := config
	c := getDefaultConfig()
	config = &c

	t.Cleanup(func() {
		schedulerLock.Lock()
		shutdownJigglerSchedulerLocked()
		jigglerSchedulePending = false
		jigglerClockChanging = false
		schedulerLock.Unlock()
		config = originalConfig
	})
}

func jigglerSchedulePendingForTest() bool {
	schedulerLock.Lock()
	defer schedulerLock.Unlock()
	return jigglerSchedulePending
}

// A cold boot builds the schedule while the clock is still at the epoch, and
// the later jump to real time wedges gocron in a multi-million-step catch-up
// walk. initJiggler waits this out instead.
// Saving jiggler settings during the deferred-start window must not build the
// schedule: the scheduler is nil, so nothing stops it, and it would land back
// on the epoch clock - the exact catch-up this defer exists to avoid.
func TestRebuildJigglerCronTabSkipsWhileDeferred(t *testing.T) {
	useJigglerTestConfig(t)
	schedulerLock.Lock()
	jigglerSchedulePending = true
	schedulerLock.Unlock()

	if err := rebuildJigglerCronTab(); err != nil {
		t.Fatalf("rebuildJigglerCronTab while deferred: %v", err)
	}
	if scheduler != nil {
		t.Fatal("built a scheduler while the clock was still untrustworthy")
	}
}

// Time sync announces a large jump before making it. The schedule built on the
// old clock must be gone by then, and a fresh one built on the new clock after.
func TestClockHookRebuildsScheduleOnLargeJump(t *testing.T) {
	useJigglerTestConfig(t)
	startJigglerCronTab()
	old := scheduler
	if old == nil {
		t.Fatal("startJigglerCronTab built no scheduler")
	}

	jump := 56 * 365 * 24 * time.Hour
	jigglerBeforeClockSet(jump)
	if scheduler != nil {
		t.Fatal("scheduler still running while the clock is about to jump")
	}
	if !jigglerSchedulePendingForTest() {
		t.Fatal("schedule not marked pending between Before and After")
	}

	jigglerAfterClockSet(jump)
	if scheduler == nil {
		t.Fatal("no scheduler built after the clock jump")
	}
	if scheduler == old {
		t.Fatal("the old scheduler was kept across the clock jump")
	}
	if jigglerSchedulePendingForTest() {
		t.Fatal("schedule still pending after the clock was set")
	}
}

// A config change that lands between Before and After must not build on the
// old clock. It is saved, and After builds from it.
func TestClockHookDefersConfigChangeDuringJump(t *testing.T) {
	useJigglerTestConfig(t)
	startJigglerCronTab()

	jump := 2 * time.Hour
	jigglerBeforeClockSet(jump)
	if err := rebuildJigglerCronTab(); err != nil {
		t.Fatalf("rebuildJigglerCronTab during a clock jump: %v", err)
	}
	if scheduler != nil {
		t.Fatal("a config change built the schedule on the old clock")
	}

	jigglerAfterClockSet(jump)
	if scheduler == nil {
		t.Fatal("no scheduler built after the clock jump")
	}
}

// The hourly NTP correction is milliseconds; gocron absorbs that, and tearing
// the schedule down for it would reset the jiggle phase every hour.
func TestClockHookLeavesScheduleAloneOnSmallCorrection(t *testing.T) {
	useJigglerTestConfig(t)
	startJigglerCronTab()
	old := scheduler

	jigglerBeforeClockSet(-30 * time.Millisecond)
	if scheduler != old {
		t.Fatal("Before replaced the scheduler for a small correction")
	}
	if jigglerSchedulePendingForTest() {
		t.Fatal("Before marked the schedule pending for a small correction")
	}

	jigglerAfterClockSet(-30 * time.Millisecond)
	if scheduler != old {
		t.Fatal("After replaced the scheduler for a small correction")
	}
}

// With the schedule deferred on an untrustworthy clock, the first successful
// sync is what builds it, whatever the size of the jump.
func TestClockHookBuildsDeferredSchedule(t *testing.T) {
	useJigglerTestConfig(t)
	schedulerLock.Lock()
	jigglerSchedulePending = true
	schedulerLock.Unlock()

	jigglerBeforeClockSet(time.Second)
	jigglerAfterClockSet(time.Second)
	if scheduler == nil {
		t.Fatal("deferred schedule not built on the first clock set")
	}
	if jigglerSchedulePendingForTest() {
		t.Fatal("schedule still pending after it was built")
	}
}

// The offline fallback must not replace a schedule the clock hook already
// built on a synced clock.
func TestStartJigglerCronTabIfPendingKeepsExistingSchedule(t *testing.T) {
	useJigglerTestConfig(t)
	startJigglerCronTab()
	old := scheduler

	startJigglerCronTabIfPending()
	if scheduler != old {
		t.Fatal("fallback replaced a schedule that was already built")
	}
}

// The offline fallback firing in the middle of a clock change must not build
// on the old clock. The After hook builds once the clock has moved.
func TestStartJigglerCronTabIfPendingWaitsOutClockChange(t *testing.T) {
	useJigglerTestConfig(t)
	startJigglerCronTab()

	jump := 2 * time.Hour
	jigglerBeforeClockSet(jump)
	startJigglerCronTabIfPending()
	if scheduler != nil {
		t.Fatal("fallback built the schedule while the clock was changing")
	}

	jigglerAfterClockSet(jump)
	if scheduler == nil {
		t.Fatal("no scheduler built after the clock change")
	}
}

func TestStartJigglerCronTabIfPendingBuildsDeferredSchedule(t *testing.T) {
	useJigglerTestConfig(t)
	schedulerLock.Lock()
	jigglerSchedulePending = true
	schedulerLock.Unlock()

	startJigglerCronTabIfPending()
	if scheduler == nil {
		t.Fatal("fallback did not build the deferred schedule")
	}
	if jigglerSchedulePendingForTest() {
		t.Fatal("schedule still pending after the fallback built it")
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
