package udp

import (
	"sync/atomic"
	"time"
)

// AIMD congestion control constants.
// Kept simple because the encoder output rate (not network capacity) is the
// bottleneck for KVM video streaming.
const (
	InitialCwnd = 4   // Initial congestion window (packets)
	MinCwnd     = 2   // Minimum congestion window
	MaxCwnd     = 128 // Maximum congestion window

	InitialRTOMs = 200  // Initial retransmit timeout (ms)
	MinRTOMs     = 200  // Minimum RTO (ms)
	MaxRTOMs     = 5000 // Maximum RTO (ms)

	// Jacobson's algorithm constants for RTT estimation
	rttAlpha = 0.125 // SRTT smoothing factor
	rttBeta  = 0.25  // RTTVAR smoothing factor
)

// congestion holds congestion control state for a Transport.
// Fields are split between atomic (hot path) and mutex-protected (cold path).
type congestion struct {
	cwnd  atomic.Int32 // Current window size in packets
	rtt   atomic.Int64 // Smoothed RTT in microseconds
	rtoMs atomic.Int64 // Retransmit timeout in milliseconds

	// RTT estimation state (updated only under sendBufMu)
	srtt   float64 // Smoothed RTT (seconds)
	rttvar float64 // RTT variance (seconds)
	hasRTT bool    // True after first RTT sample
}

// initCongestion initializes congestion control state.
func (c *congestion) init() {
	c.cwnd.Store(int32(InitialCwnd))
	c.rtoMs.Store(int64(InitialRTOMs))
}

// getCwnd returns the current congestion window size.
func (c *congestion) getCwnd() int {
	return int(c.cwnd.Load())
}

// getRTO returns the current retransmit timeout.
func (c *congestion) getRTO() time.Duration {
	return time.Duration(c.rtoMs.Load()) * time.Millisecond
}

// onAck processes an ACK and updates congestion state.
// Must be called under sendBufMu.
func (c *congestion) onAck(rttSample time.Duration) {
	// Additive increase: cwnd += 1/cwnd (approximately +1 per RTT)
	cwnd := c.cwnd.Load()
	if cwnd < int32(MaxCwnd) {
		c.cwnd.Store(cwnd + 1)
	}

	c.updateRTT(rttSample)
}

// onTimeout handles a retransmission timeout.
// Multiplicative decrease: cwnd = cwnd / 2.
func (c *congestion) onTimeout() {
	cwnd := c.cwnd.Load()
	c.cwnd.Store(max(cwnd/2, int32(MinCwnd)))
}

// updateRTT updates RTT estimates using Jacobson's algorithm (RFC 6298).
// Must be called under sendBufMu.
func (c *congestion) updateRTT(sample time.Duration) {
	r := sample.Seconds()
	if r <= 0 {
		return
	}

	if !c.hasRTT {
		// First measurement
		c.srtt = r
		c.rttvar = r / 2
		c.hasRTT = true
	} else {
		// Jacobson's algorithm
		diff := c.srtt - r
		if diff < 0 {
			diff = -diff
		}
		c.rttvar = (1-rttBeta)*c.rttvar + rttBeta*diff
		c.srtt = (1-rttAlpha)*c.srtt + rttAlpha*r
	}

	// RTO = SRTT + 4*RTTVAR, clamped to [MinRTOMs, MaxRTOMs]
	rtoSec := c.srtt + 4*c.rttvar
	rtoMs := min(max(int64(rtoSec*1000), MinRTOMs), MaxRTOMs)
	c.rtoMs.Store(rtoMs)
	c.rtt.Store(int64(c.srtt * 1e6)) // Store as microseconds
}
