package rtpclock

import (
	"math"
	"testing"
	"time"
)

func TestClockAdvance(t *testing.T) {
	const frame = 16667 * time.Microsecond
	boot := 3 * time.Hour

	steps := []struct {
		name string
		pts  time.Duration
		want time.Duration
	}{
		{"first frame of a session makes no step", boot, 0},
		{"normal frame", boot + frame, frame},
		{"long gap", boot + frame + 20*time.Second, 20 * time.Second},
		{"backwards PTS", time.Second, RestartStep},
		{"frame after a restart is measured from the new PTS", time.Second + frame, frame},
		{"jump above the limit", time.Second + frame + MaxStep + time.Microsecond, RestartStep},
		{"jump at the limit", time.Second + frame + 2*MaxStep + time.Microsecond, MaxStep},
		{"repeated PTS", time.Second + frame + 2*MaxStep + time.Microsecond, RestartStep},
	}

	var c Clock
	for _, s := range steps {
		if got := c.Advance(s.pts); got != s.want {
			t.Fatalf("%s: step %v, want %v", s.name, got, s.want)
		}
	}
}

func TestMaxStepStaysBelowHalfTheRTPTimestampRange(t *testing.T) {
	const videoClockRate = 90000
	if ticks := MaxStep.Seconds()*videoClockRate + 1; ticks >= math.MaxInt32 {
		t.Fatalf("MaxStep is %v ticks at 90 kHz, a receiver reads it as a step back", ticks)
	}
}

func TestSilenceGap(t *testing.T) {
	const packet = 20 * time.Millisecond

	for _, tc := range []struct {
		name      string
		sinceLast time.Duration
		want      time.Duration
	}{
		{"continuous audio", packet, 0},
		{"packets read late from the capture buffer", 80 * time.Millisecond, 0},
		{"after silence", 30 * time.Second, 30*time.Second - packet},
		{"after silence longer than the limit", 13 * time.Hour, MaxStep},
	} {
		if got := SilenceGap(tc.sinceLast, packet); got != tc.want {
			t.Fatalf("%s: gap %v, want %v", tc.name, got, tc.want)
		}
	}
}
