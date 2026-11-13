package osservices

// The video rate controller: a target bitrate and frame-rate cap that follow
// the network's estimate below a static ceiling (video.config's
// target_kbps and maximum_fps).

const (
	// After any change, no cut for this long: the sender applies the
	// target, and a reopened encoder's IDR crosses the link first.
	videoRateSettleMS = 2000
	// Between a change and a raise; doubled after a raise that had to be
	// cut back within it, up to the maximum.
	videoRateRaiseMS    = 10000
	videoRateRaiseMaxMS = 60000
	// Targets move in steps of this many kbps.
	videoRateStepKbps = 100
)

type videoRate struct {
	// The static target, and the target in force.
	ceilingKbps uint32
	kbps        uint32
	// The frame rate is halved.
	lowFPS      bool
	changedMS   int64
	raiseWaitMS int64
	// The last change was a raise.
	raised bool
	cuts   uint32
	raises uint32
}

// reset starts at the ceiling.
func (r *videoRate) reset(ceilingKbps uint32, nowMS int64) {
	*r = videoRate{
		ceilingKbps: ceilingKbps,
		kbps:        ceilingKbps,
		changedMS:   nowMS,
		raiseWaitMS: videoRateRaiseMS,
	}
}

// bounded is the target for an estimate: in steps, from an eighth of the
// ceiling up to the ceiling.
func (r *videoRate) bounded(kbps uint32) uint32 {
	return max(r.ceilingKbps/8, min(r.ceilingKbps, kbps-kbps%videoRateStepKbps))
}

// next is the next target: the estimate when it is clearly lower, but at
// least half the target, so that one burst of loss (a large keyframe) does
// not take the target to the floor; when it is clearly higher, or reaches
// the ceiling, at most half again as much.
func (r *videoRate) next(estimate uint32, since int64) uint32 {
	kbps := r.kbps
	if estimate < kbps-kbps/10 && since >= videoRateSettleMS {
		half := (kbps/2 + videoRateStepKbps - 1) / videoRateStepKbps * videoRateStepKbps
		return r.bounded(max(estimate, half))
	}
	if (estimate >= kbps+kbps/5 || estimate >= r.ceilingKbps) && since >= r.raiseWaitMS {
		return r.bounded(min(estimate, kbps+kbps/2))
	}
	return kbps
}

// step is one evaluation with the network's estimate. True when the target
// or the frame rate changed.
func (r *videoRate) step(estimateKbps uint32, nowMS int64) bool {
	since := nowMS - r.changedMS
	if r.raised && since >= videoRateRaiseMS {
		// The last raise held.
		r.raiseWaitMS = videoRateRaiseMS
	}
	kbps := r.next(estimateKbps, since)
	if kbps == r.kbps {
		return false
	}
	raise := kbps > r.kbps
	if !raise && r.raised && since < videoRateRaiseMS {
		// The last raise was too much: probe less often.
		r.raiseWaitMS = min(r.raiseWaitMS*2, videoRateRaiseMaxMS)
	}
	// Half the frame rate under 3/16 of the ceiling (1.5 Mbit/s at 1080p);
	// the full rate again from half as much more.
	fpsFloor := r.ceilingKbps * 3 / 16
	r.lowFPS = kbps < fpsFloor || (r.lowFPS && kbps < fpsFloor+fpsFloor/2)
	r.kbps = kbps
	r.raised = raise
	r.changedMS = nowMS
	if raise {
		r.raises++
	} else {
		r.cuts++
	}
	return true
}

// hold is called when a new stream starts: no change for videoRateSettleMS,
// and the raise wait starts again.
func (r *videoRate) hold(nowMS int64) {
	r.changedMS = nowMS
}

// fps is the frame-rate cap under fpsCap.
func (r *videoRate) fps(fpsCap uint32) uint32 {
	if r.lowFPS && fpsCap > 1 {
		return fpsCap / 2
	}
	return fpsCap
}
