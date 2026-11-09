package osservices

import "testing"

// Ported from test_os_services_video_rate.c: the rate policy at 1080p
// (ceiling 8000 kbps). Cuts follow the estimate at once by at most half,
// raises wait and step, the floor and the frame-rate halving, and a link
// with room keeps the full target.

type rateRun struct {
	t   *testing.T
	r   videoRate
	now int64
}

func startRate(t *testing.T) *rateRun {
	rr := &rateRun{t: t}
	rr.r.reset(8000, 0)
	return rr
}

// run is one evaluation per second for seconds, each with estimate; it
// returns the number of changes.
func (rr *rateRun) run(estimate uint32, seconds int) int {
	changes := 0
	for i := 0; i < seconds; i++ {
		rr.now += 1000
		if rr.r.step(estimate, rr.now) {
			changes++
		}
	}
	return changes
}

// cutTo cuts every 2 s, as the settle time allows, until the target is
// kbps.
func (rr *rateRun) cutTo(kbps uint32) {
	rr.t.Helper()
	for rr.r.kbps > kbps {
		if rr.run(kbps, 2) != 1 {
			rr.t.Fatalf("no cut towards %d at %d", kbps, rr.r.kbps)
		}
	}
	if rr.r.kbps != kbps {
		rr.t.Fatalf("cut to %d, want %d", rr.r.kbps, kbps)
	}
}

func (rr *rateRun) expect(estimate uint32, seconds, changes int, kbps uint32) {
	rr.t.Helper()
	if got := rr.run(estimate, seconds); got != changes || rr.r.kbps != kbps {
		rr.t.Fatalf("estimate %d for %d s: %d changes to %d, want %d to %d", estimate, seconds, got, rr.r.kbps, changes, kbps)
	}
}

func TestRateRoomKeepsTheCeiling(t *testing.T) {
	rr := startRate(t)
	rr.expect(8000, 60, 0, 8000)
	rr.expect(30000, 60, 0, 8000)
	// Dips under 10% are noise.
	rr.expect(7300, 60, 0, 8000)
	if rr.r.fps(30) != 30 {
		t.Fatalf("fps %d", rr.r.fps(30))
	}
}

func TestRateCutAtOnceAfterTheSettleTime(t *testing.T) {
	rr := startRate(t)
	// The stream's first IDR is still crossing.
	rr.expect(5456, 1, 0, 8000)
	rr.expect(5456, 1, 1, 5400)
	if rr.r.cuts != 1 {
		t.Fatalf("cuts %d", rr.r.cuts)
	}
	// The next cut waits for the sender to apply this one, and halves the
	// target at most.
	rr.expect(2000, 1, 0, 5400)
	rr.expect(2000, 1, 1, 2700)
	rr.expect(2000, 2, 1, 2000)
	if rr.r.cuts != 3 {
		t.Fatalf("cuts %d", rr.r.cuts)
	}
	// Half of 5100 is kept in the 100 kbps steps by rounding up.
	rr = startRate(t)
	rr.expect(5100, 2, 1, 5100)
	rr.expect(100, 2, 1, 2600)
}

func TestRateRaiseWaitsAndSteps(t *testing.T) {
	rr := startRate(t)
	rr.cutTo(2000)
	// No raise for 10 s after a change, and none on a small rise.
	rr.expect(8000, 9, 0, 2000)
	rr.expect(8000, 1, 1, 3000)
	rr.expect(2800, 10, 0, 3000)
	rr.expect(3500, 10, 0, 3000)
	// At most half again, at most the estimate, at most the ceiling.
	rr.expect(4321, 1, 1, 4300)
	rr.expect(8000, 10, 1, 6400)
	rr.expect(30000, 10, 1, 8000)
	rr.expect(30000, 60, 0, 8000)
	if rr.r.raises != 4 || rr.r.cuts != 2 {
		t.Fatalf("raises %d cuts %d", rr.r.raises, rr.r.cuts)
	}
}

// An estimate at the ceiling raises from just under it too.
func TestRateRaiseReachesTheCeiling(t *testing.T) {
	rr := startRate(t)
	rr.expect(7000, 2, 1, 7000)
	rr.expect(7900, 30, 0, 7000)
	rr.expect(8000, 1, 1, 8000)
}

func TestRateFailedRaiseDoublesTheWait(t *testing.T) {
	rr := startRate(t)
	rr.cutTo(2000)
	rr.expect(8000, 10, 1, 3000)
	// The raise was too much: cut back, and wait 20 s for the next.
	rr.expect(2000, 2, 1, 2000)
	for i, wait := range []int{20, 40, 60, 60} {
		if rr.run(8000, wait-1) != 0 || rr.run(8000, 1) != 1 {
			t.Fatalf("the raise did not wait %d s", wait)
		}
		// Too much again, except the last.
		if i < 3 && rr.run(2000, 2) != 1 {
			t.Fatalf("no cut after the raise")
		}
	}
	// A raise that holds for 10 s brings back the short wait.
	if rr.run(8000, 10) != 1 || rr.run(8000, 10) != 1 {
		t.Fatalf("the short wait did not come back")
	}
}

func TestRateFloorAndFrameRate(t *testing.T) {
	rr := startRate(t)
	rr.expect(100, 2, 1, 4000)
	rr.expect(100, 2, 1, 2000)
	if rr.r.fps(30) != 30 {
		t.Fatalf("halved at 2000")
	}
	rr.expect(100, 2, 1, 1000)
	if rr.r.fps(30) != 15 || rr.r.fps(60) != 30 || rr.r.fps(1) != 1 {
		t.Fatalf("not halved at 1000: %d %d %d", rr.r.fps(30), rr.r.fps(60), rr.r.fps(1))
	}
	// The floor is an eighth of the ceiling.
	rr.expect(100, 10, 0, 1000)
	// Back to the full rate only well above the floor where it halved.
	rr.expect(1700, 10, 1, 1500)
	if rr.r.fps(30) != 15 {
		t.Fatalf("full rate at 1500")
	}
	rr.expect(3000, 10, 1, 2200)
	if rr.r.fps(30) != 15 {
		t.Fatalf("full rate at 2200")
	}
	rr.expect(3000, 10, 1, 3000)
	if rr.r.fps(30) != 30 {
		t.Fatalf("halved at 3000")
	}
	rr.expect(1200, 2, 1, 1500)
	rr.expect(1200, 2, 1, 1200)
	if rr.r.fps(30) != 15 {
		t.Fatalf("full rate at 1200")
	}
}
