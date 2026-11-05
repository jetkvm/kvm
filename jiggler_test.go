//go:build linux && arm

package kvm

import (
	"context"
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
// the later jump to real time would wedge gocron in a multi-million-step
// catch-up walk. Time sync announces a large jump before making it: the
// schedule built on the old clock must be gone by then, and a fresh one built
// on the new clock after.
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

// A config change outside a clock jump rebuilds at once, as it always did.
func TestRebuildJigglerCronTabReplacesScheduler(t *testing.T) {
	useJigglerTestConfig(t)
	startJigglerCronTab()
	old := scheduler

	if err := rebuildJigglerCronTab(); err != nil {
		t.Fatalf("rebuildJigglerCronTab: %v", err)
	}
	if scheduler == nil || scheduler == old {
		t.Fatal("config change did not replace the scheduler")
	}
}

// Replacing the schedule cancels the running job's context. The jitter wait
// has to return then, or the replacement blocks on it for gocron's stop timeout.
func TestWaitUnlessCancelledReturnsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go cancel()

	start := time.Now()
	if waitUnlessCancelled(ctx, time.Minute) {
		t.Fatal("waitUnlessCancelled reported the wait completing, want cancellation")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("took %v, want to return as soon as the context was cancelled", elapsed)
	}
}

func TestWaitUnlessCancelledWaitsOutTheDelay(t *testing.T) {
	start := time.Now()
	if !waitUnlessCancelled(context.Background(), 20*time.Millisecond) {
		t.Fatal("waitUnlessCancelled reported cancellation, want the wait completing")
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Errorf("returned after %v, want at least the 20ms delay", elapsed)
	}
}
