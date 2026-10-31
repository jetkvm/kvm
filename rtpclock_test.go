//go:build linux && arm

package kvm

import (
	"testing"
	"time"
)

func TestRTPClockSkip(t *testing.T) {
	if skip := rtpClockSkip(17*time.Millisecond, 16*time.Millisecond); skip != 0 {
		t.Fatalf("steady frame skipped %v", skip)
	}
	if skip, want := rtpClockSkip(20*time.Second, 20*time.Millisecond), 20*time.Second-20*time.Millisecond; skip != want {
		t.Fatalf("after a stall skip %v, want %v", skip, want)
	}
	if skip := rtpClockSkip(13*time.Hour, 16*time.Millisecond); skip != maxRTPClockSkip {
		t.Fatalf("after thirteen hours skip %v, want the cap %v", skip, maxRTPClockSkip)
	}
}

func TestVideoFrameTimingKeepsThePacingAcrossAStall(t *testing.T) {
	lastVideoFrameDuration = 16 * time.Millisecond
	if skip, d := videoFrameTiming(17 * time.Millisecond); skip != 0 || d != 17*time.Millisecond {
		t.Fatalf("steady frame: skip %v, duration %v", skip, d)
	}
	if skip, d := videoFrameTiming(20 * time.Second); skip != 20*time.Second-17*time.Millisecond || d != 17*time.Millisecond {
		t.Fatalf("first frame after a stall: skip %v, duration %v", skip, d)
	}
	if skip, d := videoFrameTiming(16 * time.Millisecond); skip != 0 || d != 16*time.Millisecond {
		t.Fatalf("next frame: skip %v, duration %v", skip, d)
	}
}
