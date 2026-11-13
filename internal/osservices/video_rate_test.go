package osservices

import "testing"

// Ported from test_os_services_session.c: the video targets follow the
// network and the KVM's own slot waits.

// sendFrameAfterWait sends a delta frame that waits ms for a slot while the
// clock ticks (no ping can come while the frame is read).
func (h *harness) sendFrameAfterWait(ms int64) {
	h.t.Helper()
	h.captureUS += 33333
	message := encodeFrame(h.captureUS, 0, makeUnit(false, false, 2000, 3))
	head := specEnvelope + specFrameHead
	h.v.busy = true
	if n := h.s.Receive(message); n != head {
		h.t.Fatalf("Receive took %d", n)
	}
	for waited := int64(0); waited < ms; waited += 100 {
		h.f.now += 100
		h.tick()
	}
	h.v.busy = false
	h.feed(message[head:])
}

// videoPressure: seconds in which a frame waits 600 ms for a slot and the
// KVM takes this many frames.
func (h *harness) videoPressure(seconds, frames int) {
	h.t.Helper()
	for i := 0; i < seconds; i++ {
		h.sendFrameAfterWait(600)
		for frame := 1; frame < frames; frame++ {
			h.sendFrameAfterWait(0)
		}
		h.videoWait(400)
	}
}

// videoFrames: seconds in which the KVM takes this many frames and none
// waits.
func (h *harness) videoFrames(seconds, frames int) {
	h.t.Helper()
	for i := 0; i < seconds; i++ {
		for frame := 0; frame < frames; frame++ {
			h.sendFrameAfterWait(0)
		}
		h.videoWait(1000)
	}
}

// The target follows the network's estimate: down once the stream's first
// 2 s are over and by half at most every 2 s, up 10 s after a change and by
// half at most. A bitrate-only change starts no new stream.
func TestVideoRateFollowsTheNetwork(t *testing.T) {
	h := videoShow(t)
	h.v.estimateKbps = 3000
	h.videoWait(1000)
	h.expectNone()
	h.videoWait(1000)
	h.expectVideoConfig(true, 1920, 1080, 4000, 60)
	h.videoWait(1000)
	h.expectNone()
	h.videoWait(1000)
	h.expectVideoConfig(true, 1920, 1080, 3000, 60)
	h.videoStatus(true, false, 1920, 1080, 30, 0)
	if h.v.streams != 1 {
		t.Fatalf("a bitrate change started a stream")
	}
	h.v.estimateKbps = 0
	h.videoWait(9000)
	h.expectNone()
	h.videoWait(1000)
	h.expectVideoConfig(true, 1920, 1080, 4500, 60)
	h.videoWait(10000)
	h.expectVideoConfig(true, 1920, 1080, 6700, 60)
	h.videoWait(10000)
	h.expectVideoConfig(true, 1920, 1080, videoKbps1080p, 60)
	h.videoWait(60000)
	h.expectNone()
	if h.v.ceilingKbps != videoKbps1080p {
		t.Fatalf("ceiling %d", h.v.ceilingKbps)
	}
}

// A feedback target that stays low after its cut, as after one burst of
// loss while it recovers, cuts once.
func TestVideoRateStaleEstimate(t *testing.T) {
	h := videoShow(t)
	h.v.estimateKbps = 1000
	h.videoWait(2000)
	h.expectVideoConfig(true, 1920, 1080, 4000, 60)
	h.v.stale = true
	h.videoWait(20000)
	h.expectNone()
}

// Frames that wait for a slot much of the time, three intervals in a row,
// take the target under what the transport sent; one long wait, as for a
// keyframe, spans two intervals and does not.
func TestVideoRateUnderPressure(t *testing.T) {
	h := videoShow(t)
	h.v.sentKbps = 6000
	h.videoWait(500)
	h.sendFrameAfterWait(900)
	h.videoWait(2600)
	h.expectNone()
	h.videoPressure(2, 30)
	h.expectNone()
	h.videoPressure(1, 30)
	h.expectVideoConfig(true, 1920, 1080, 5100, 60)
}

// A sender that sends more than its target is cut from the target: 5800
// kbit/s sent for a target of 5100 give 4300, which 85 % of 5800 would not
// do.
func TestVideoRateUnderPressureFromASenderOverItsTarget(t *testing.T) {
	h := videoShow(t)
	h.v.sentKbps = 6000
	h.videoPressure(3, 30)
	h.expectVideoConfig(true, 1920, 1080, 5100, 60)
	h.v.sentKbps = 5800
	h.videoPressure(1, 30)
	h.expectNone()
	h.videoPressure(1, 30)
	h.expectVideoConfig(true, 1920, 1080, 4300, 60)
}

// The sender delivered 30 frames per second while none waited. With
// frames waiting, the KVM takes 20 of them: it carries two thirds of the
// 7000 kbit/s it sent, and the cut is half at most. At the new target the
// sender has not shown what it delivers, so 20 frames are not counted
// against 30.
func TestVideoRateUnderPressureWithFewerFrames(t *testing.T) {
	h := videoShow(t)
	h.v.sentKbps = 7000
	h.videoFrames(3, 30)
	h.expectNone()
	h.videoPressure(3, 20)
	h.expectVideoConfig(true, 1920, 1080, 4000, 60)
	h.v.sentKbps = 4400
	h.videoPressure(2, 20)
	h.expectVideoConfig(true, 1920, 1080, 3400, 60)
}

// A sender that delivers 20 frames per second by itself is not taken for a
// stream that the KVM holds back.
func TestVideoRateUnderPressureAtTheSendersFrameRate(t *testing.T) {
	h := videoShow(t)
	h.v.sentKbps = 7000
	h.videoFrames(3, 20)
	h.expectNone()
	h.videoPressure(3, 20)
	h.expectVideoConfig(true, 1920, 1080, 5900, 60)
}

// Under 1.5 Mbit/s at 1080p the frame rate halves; the stream the sender
// restarts at 15 fps keeps the adapted target.
func TestVideoRateHalvesTheFrameRate(t *testing.T) {
	h := videoShow(t)
	h.v.estimateKbps = 1000
	h.videoWait(2000)
	h.expectVideoConfig(true, 1920, 1080, 4000, 60)
	h.videoWait(2000)
	h.expectVideoConfig(true, 1920, 1080, 2000, 60)
	h.videoWait(2000)
	h.expectVideoConfig(true, 1920, 1080, 1000, 30)
	p := h.videoStatusPayload(true, true, 1920, 1080, 30, 0)
	wle.PutUint32(p[20:], 1000)
	wle.PutUint32(p[24:], 15)
	h.send(svcVideo, kVideoStatus, p)
	if h.v.streams != 2 || h.v.streamFPS != 15 || h.s.videoSenderKbps != 1000 {
		t.Fatalf("streams %d at %d fps, sender %d kbps", h.v.streams, h.v.streamFPS, h.s.videoSenderKbps)
	}
	h.v.estimateKbps = 0
	h.videoWait(10000)
	h.expectVideoConfig(true, 1920, 1080, 1500, 30)
	h.videoWait(10000)
	h.expectVideoConfig(true, 1920, 1080, 2200, 30)
	h.videoWait(10000)
	h.expectVideoConfig(true, 1920, 1080, 3300, 60)
}

// A manual video quality puts the static target back at once and does not
// follow the receiver feedback. Frames that wait for a slot still lower the
// target, and it returns to the static target in steps. A fixed bitrate in
// the settings is never adapted.
func TestVideoRateManualAndFixed(t *testing.T) {
	h := videoShow(t)
	h.v.estimateKbps = 3000
	h.videoWait(2000)
	h.expectVideoConfig(true, 1920, 1080, 4000, 60)
	h.v.manual = true
	h.videoWait(100)
	h.expectVideoConfig(true, 1920, 1080, videoKbps1080p, 60)
	h.videoWait(30000)
	h.expectNone()
	h.v.sentKbps = 5000
	h.videoPressure(3, 26)
	h.expectVideoConfig(true, 1920, 1080, 4200, 60)
	h.videoWait(9000)
	h.expectNone()
	h.videoWait(1000)
	h.expectVideoConfig(true, 1920, 1080, 6300, 60)
	h.videoWait(10000)
	h.expectVideoConfig(true, 1920, 1080, videoKbps1080p, 60)

	settings := settings1080p()
	settings.TargetKbps = 6000
	h = videoOpen(t, settings)
	h.registerVideo(3840, 2160, 60)
	h.s.DisplayStatus(true, true)
	h.expectVideoConfig(true, 1920, 1080, 6000, 60)
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	h.v.estimateKbps = 2000
	h.videoWait(30000)
	h.expectNone()
	h.sendFrame(3000000, flagKey|flagSets, true, true, 100)
	h.v.sentKbps = 6600
	h.videoPressure(4, 26)
	h.expectNone()
}

// The first 2 s of a stream are its own, however long the switch took; new
// settings start again from the static targets, also at an equal ceiling;
// a 1 fps cap is not halved to none.
func TestVideoRateRestarts(t *testing.T) {
	h := videoOpen(t, defaultSettings())
	h.registerVideo(3840, 2160, 60)
	h.s.DisplayStatus(true, true)
	h.expectVideoConfig(true, 2560, 1440, videoKbps1440p, 60)
	h.videoWait(3000)
	h.videoStatus(true, true, 2560, 1440, 30, 0)
	h.v.estimateKbps = 1500
	h.videoWait(1000)
	h.expectNone()
	h.videoWait(1000)
	h.expectVideoConfig(true, 2560, 1440, 6000, 60)
	h.videoWait(2000)
	h.expectVideoConfig(true, 2560, 1440, 3000, 60)
	h.videoWait(2000)
	h.expectVideoConfig(true, 2560, 1440, 1500, 30)

	larger := defaultSettings()
	larger.Width, larger.Height = 3840, 2160
	h.s.ApplySettings(larger)
	h.s.DisplayStatus(true, true)
	h.expectVideoConfig(true, 3840, 2160, videoKbps1440p, 60)

	oneFPS := settings1080p()
	oneFPS.MaximumFPS = 1
	h = videoOpen(t, oneFPS)
	h.registerVideo(3840, 2160, 60)
	h.s.DisplayStatus(true, true)
	h.expectVideoConfig(true, 1920, 1080, videoKbps1080p, 1)
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	h.v.estimateKbps = 1000
	h.videoWait(2000)
	h.expectVideoConfig(true, 1920, 1080, 4000, 1)
	h.videoWait(2000)
	h.expectVideoConfig(true, 1920, 1080, 2000, 1)
	h.videoWait(2000)
	h.expectVideoConfig(true, 1920, 1080, 1000, 1)
}

// The sender's max_fps caps maximum_fps in video.config.
func TestVideoConfigFollowsTheSendersMaxFPS(t *testing.T) {
	h := videoOpen(t, settings1080p())
	h.registerVideo(1920, 1080, 30)
	h.s.DisplayStatus(true, true)
	h.expectVideoConfig(true, 1920, 1080, videoKbps1080p, 30)
}
