package kvm

import "time"

// A sample more than rtpStallGap after the previous one ends a stall in its
// stream. pion applies a sample's duration after its packets, so the gap has
// to move the RTP clock before the sample, or the next one is stamped that
// far ahead and the browser holds the picture while it catches up. The skip
// stays below maxRTPClockSkip: a receiver reads a jump of 2^31 ticks or more
// as a step back.
const (
	rtpStallGap     = 500 * time.Millisecond
	maxRTPClockSkip = time.Hour
)

// rtpClockSkip is how far the clock moves before a sample that arrives
// sinceLast after the previous one, whose duration previous already applied.
func rtpClockSkip(sinceLast, previous time.Duration) time.Duration {
	if sinceLast <= rtpStallGap {
		return 0
	}
	return min(sinceLast-previous, maxRTPClockSkip)
}
