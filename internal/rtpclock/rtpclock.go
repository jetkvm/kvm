package rtpclock

import "time"

const (
	MaxStep     = 6 * time.Hour
	RestartStep = time.Second / 60
)

type Clock struct {
	prev    time.Duration
	started bool
}

func (c *Clock) Advance(pts time.Duration) time.Duration {
	if !c.started {
		c.started = true
		c.prev = pts
		return 0
	}
	step := Step(c.prev, pts)
	c.prev = pts
	return step
}

// Step returns how far the RTP clock moves between two capture times.
//
// A capture time that does not increase, or that jumps by more than MaxStep,
// cannot be sent as it is: a receiver reads an RTP step of 2^31 ticks or more
// (6.6 hours at 90 kHz) as a step back, and two frames with one timestamp as
// one frame. Such a step means that the capture clock started again, so the
// RTP clock moves by one nominal frame instead.
func Step(prev, pts time.Duration) time.Duration {
	step := pts - prev
	if step <= 0 || step > MaxStep {
		return RestartStep
	}
	return step
}
