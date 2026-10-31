//go:build linux && arm

package kvm

import (
	"testing"
	"time"
)

func TestVideoFrameTimingSkipsAStallBeforeTheFrame(t *testing.T) {
	lastVideoFrameDuration = 16 * time.Millisecond
	if skip, d := videoFrameTiming(17 * time.Millisecond); skip != 0 || d != 17*time.Millisecond {
		t.Fatalf("steady frame: skip %v, duration %v", skip, d)
	}
	skip, d := videoFrameTiming(20 * time.Second)
	if want := 20*time.Second - 17*time.Millisecond; skip != want || d != 17*time.Millisecond {
		t.Fatalf("first frame after a stall: skip %v, duration %v", skip, d)
	}
	if skip, d := videoFrameTiming(16 * time.Millisecond); skip != 0 || d != 16*time.Millisecond {
		t.Fatalf("next frame: skip %v, duration %v", skip, d)
	}
}
