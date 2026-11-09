package osservices

import (
	"bytes"
	"testing"
)

// The browser's codec (Settings.Codec), which the Go port adds to the
// Mini's session: the KVM requests it in video.config, switches only to a
// sender that offers it, holds the stream to it, and checks frames with the
// stream's codec (Switch to service video, video.frame).

var (
	testVPS    = []byte{0, 0, 0, 1, 0x40, 0x01, 0x0c, 0x01, 0xff}
	testSPS265 = []byte{0, 0, 0, 1, 0x42, 0x01, 0x01, 0x01, 0x60}
	// With a 3-byte start code; the KVM keeps it behind a 4-byte one.
	testPPS265  = []byte{0, 0, 1, 0x44, 0x01, 0xc1, 0x72}
	testPPS2654 = []byte{0, 0, 0, 1, 0x44, 0x01, 0xc1, 0x72}
)

const (
	hevcTrailR   = 1
	hevcIDRWRADL = 19
	hevcIDRNLP   = 20
	hevcCRA      = 21
)

// makeHEVCUnit is an H.265 access unit: VPS, SPS and PPS when sets, then one
// slice of the NAL type with size bytes of data.
func makeHEVCUnit(nalType byte, sets bool, size int, seed byte) []byte {
	var unit []byte
	if sets {
		unit = cat(testVPS, testSPS265, testPPS265)
	}
	unit = append(unit, 0, 0, 0, 1, nalType<<1, 0x01)
	for i := 0; i < size; i++ {
		unit = append(unit, 0x80|((seed+byte(i))&0x7f))
	}
	return unit
}

// hevcShow: a sender with H.264 and H.265 shows a 1920x1080 H.265 stream,
// after its first keyframe; nothing written since.
func hevcShow(t *testing.T) *harness {
	t.Helper()
	settings := settings1080p()
	settings.Codec = CodecH265
	h := newHarness(t, hooksVideo, settings)
	h.openSession()
	h.videoFlags = VideoCodecFlagH264 | VideoCodecFlagH265
	h.registerVideo(3840, 2160, 60)
	if h.v.presents != 1 {
		t.Fatalf("no switch to an H.265 sender")
	}
	h.s.DisplayStatus(true, true)
	m := h.expect(svcVideo, kVideoConfig)
	if m.payload[0] != 1 || m.payload[2] != 1 {
		t.Fatalf("video.config % x, want enabled 1 codec 1", m.payload)
	}
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	if h.source() != SourceGuest || h.v.streams != 1 || h.v.streamCodec != CodecH265 || h.s.State().StreamCodec != CodecH265 {
		t.Fatalf("stream: source %v codec %v", h.source(), h.v.streamCodec)
	}
	unit := makeHEVCUnit(hevcIDRWRADL, true, 100, 1)
	if !h.sendUnit(1000000, flagKey|flagSets, unit) {
		t.Fatalf("the first H.265 keyframe was not delivered")
	}
	if f := h.v.frames[0]; !f.frame.Keyframe || f.frame.Codec != CodecH265 || !bytes.Equal(f.data, unit) {
		t.Fatalf("frame %+v", f.frame)
	}
	h.expectNone()
	return h
}

func TestVideoH265Stream(t *testing.T) {
	h := hevcShow(t)
	sets := cat(testVPS, testSPS265, testPPS2654)
	if !bytes.Equal(h.s.parameterSets, sets) {
		t.Fatalf("cache % x", h.s.parameterSets)
	}
	// A delta, a bare CRA that gets the cache, an IDR_N_LP with its own.
	delta := makeHEVCUnit(hevcTrailR, false, 50, 2)
	cra := makeHEVCUnit(hevcCRA, false, 50, 3)
	idr := makeHEVCUnit(hevcIDRNLP, true, 50, 4)
	if !h.sendUnit(1033333, 0, delta) || !h.sendUnit(1066666, flagKey, cra) || !h.sendUnit(1099999, flagKey|flagSets, idr) {
		t.Fatalf("H.265 frames not delivered")
	}
	f := h.v.frames
	if f[1].frame.Keyframe || !bytes.Equal(f[1].data, delta) || f[1].frame.Codec != CodecH265 {
		t.Fatalf("delta %+v", f[1].frame)
	}
	if !f[2].frame.Keyframe || !f[2].frame.ParameterSetsAdded || !bytes.Equal(f[2].data, cat(sets, cra)) {
		t.Fatalf("CRA %+v", f[2].frame)
	}
	if !f[3].frame.Keyframe || f[3].frame.ParameterSetsAdded || !bytes.Equal(f[3].data, idr) {
		t.Fatalf("IDR_N_LP %+v", f[3].frame)
	}
	if !h.s.Open() || h.s.Counters.ServiceFaults != 0 {
		t.Fatalf("open %v faults %d", h.s.Open(), h.s.Counters.ServiceFaults)
	}
}

// Frames are checked with the rules of the stream's codec.
func TestVideoH265FrameRules(t *testing.T) {
	cases := []struct {
		name   string
		flags  uint32
		unit   []byte
		reason string
	}{
		// SPS and PPS are not enough for H.265.
		{"keyframe without VPS", flagKey | flagSets, cat(testSPS265, testPPS265, makeHEVCUnit(hevcIDRWRADL, false, 10, 1)),
			"a video.frame flagged with parameter sets has no VPS, SPS and PPS"},
		// An H.264 IDR has no H.265 VCL NAL unit, so it is no keyframe.
		{"H.264 keyframe", flagKey | flagSets, makeUnit(true, true, 10, 1),
			"the video.frame keyframe flag does not match its access unit"},
		{"TRAIL_R flagged as keyframe", flagKey, makeHEVCUnit(hevcTrailR, false, 10, 1),
			"the video.frame keyframe flag does not match its access unit"},
		{"CRA without the keyframe flag", 0, makeHEVCUnit(hevcCRA, false, 10, 1),
			"the video.frame keyframe flag does not match its access unit"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := hevcShow(t)
			h.sendUnit(1033333, c.flags, c.unit)
			if st := h.s.State(); st.VideoError != c.reason || st.VideoRegistered || st.Source != SourceSwitchingToPhysical {
				t.Fatalf("error %q registered %v source %v", st.VideoError, st.VideoRegistered, st.Source)
			}
			if m := h.expect(svcVideo, kVideoConfig); m.payload[0] != 0 {
				t.Fatalf("video.config % x", m.payload)
			}
		})
	}
	// After a stream reset the first keyframe needs VPS, SPS and PPS.
	h := hevcShow(t)
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	h.sendUnit(1033333, flagKey|flagSets, cat(testSPS265, testPPS265, makeHEVCUnit(hevcIDRWRADL, false, 10, 1)))
	if h.s.State().VideoError != "a video.frame flagged with parameter sets has no VPS, SPS and PPS" {
		t.Fatalf("error %q", h.s.State().VideoError)
	}
}

// The KVM switches only to a sender that offers the browser's codec.
func TestVideoCodecNotOffered(t *testing.T) {
	settings := settings1080p()
	settings.Codec = CodecH265
	h := newHarness(t, hooksVideo, settings)
	h.openSession()
	h.registerVideo(1920, 1080, 60)
	st := h.s.State()
	if h.v.presents != 0 || st.Source != SourcePhysical || !st.VideoRegistered ||
		st.VideoError != "the browser uses H.265, which the sender does not offer" {
		t.Fatalf("H.264 sender: presents %d %+v", h.v.presents, st)
	}
	// The registration is answered, with video disabled.
	if m := h.expect(svcVideo, kVideoConfig); m.payload[0] != 0 {
		t.Fatalf("video.config % x", m.payload)
	}
	h.expectNone()
	// An enabled status is no switch.
	h.send(svcVideo, kVideoStatus, h.videoStatusPayload(true, true, 1920, 1080, 30, 0))
	if h.s.State().VideoRegistered || h.v.streams != 0 {
		t.Fatalf("an enabled status was accepted")
	}

	// The browser moves to H.264: the switch starts.
	h = newHarness(t, hooksVideo, settings)
	h.openSession()
	h.registerVideo(1920, 1080, 60)
	h.expect(svcVideo, kVideoConfig)
	h.s.ApplySettings(settings1080p())
	if h.v.presents != 1 || h.source() != SourceSwitchingToGuest {
		t.Fatalf("H.264 browser: presents %d source %v", h.v.presents, h.source())
	}
	h.s.DisplayStatus(true, true)
	h.expectVideoConfig(true, 1920, 1080, videoKbps1080p, 60)
}

// video.status must report the codec the KVM asked for, and only one the
// sender advertised.
func TestVideoStatusCodec(t *testing.T) {
	// An H.265 browser and a status that reports H.264.
	settings := settings1080p()
	settings.Codec = CodecH265
	h := newHarness(t, hooksVideo, settings)
	h.openSession()
	h.videoFlags = VideoCodecFlagH264 | VideoCodecFlagH265
	h.registerVideo(1920, 1080, 60)
	h.s.DisplayStatus(true, true)
	h.expect(svcVideo, kVideoConfig)
	p := h.videoStatusPayload(true, true, 1920, 1080, 30, 0)
	p[2] = 0
	h.send(svcVideo, kVideoStatus, p)
	if st := h.s.State(); st.VideoError != "video.status reports H.264; the KVM asked for H.265" || st.VideoRegistered {
		t.Fatalf("H.264 status: %+v", st)
	}

	// An H.264 browser and a status that reports H.265.
	h = newHarness(t, hooksVideo, settings1080p())
	h.openSession()
	h.videoFlags = VideoCodecFlagH264 | VideoCodecFlagH265
	h.registerVideo(1920, 1080, 60)
	h.s.DisplayStatus(true, true)
	h.expectVideoConfig(true, 1920, 1080, videoKbps1080p, 60)
	p = h.videoStatusPayload(true, true, 1920, 1080, 30, 0)
	p[2] = 1
	h.send(svcVideo, kVideoStatus, p)
	if st := h.s.State(); st.VideoError != "video.status reports H.265; the KVM asked for H.264" || st.VideoRegistered {
		t.Fatalf("H.265 status: %+v", st)
	}

	// A status that reports H.265 from a sender without it.
	h = videoShow(t)
	p = h.videoStatusPayload(true, false, 1920, 1080, 30, 0)
	p[2] = 1
	h.send(svcVideo, kVideoStatus, p)
	h.expectVideoFault("video.status reports H.265, which the sender did not advertise")
}

// A codec change while service video shows: video.config with the new
// codec when the sender offers it, and the stream that follows is checked
// with that codec; otherwise video stops with the reason.
func TestVideoCodecChangeWhileShowing(t *testing.T) {
	h := newHarness(t, hooksVideo, settings1080p())
	h.openSession()
	h.videoFlags = VideoCodecFlagH264 | VideoCodecFlagH265
	h.registerVideo(3840, 2160, 60)
	h.s.DisplayStatus(true, true)
	h.expectVideoConfig(true, 1920, 1080, videoKbps1080p, 60)
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	h.sendFrame(1000000, flagKey|flagSets, true, true, 100)

	settings := settings1080p()
	settings.Codec = CodecH265
	h.s.ApplySettings(settings)
	h.expectVideoConfig(true, 1920, 1080, videoKbps1080p, 60)
	if h.v.presents != 1 || h.source() != SourceGuest {
		t.Fatalf("a codec change presented the display again")
	}
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	if h.v.streams != 2 || h.v.streamCodec != CodecH265 || len(h.s.parameterSets) != 0 {
		t.Fatalf("streams %d codec %v sets %d", h.v.streams, h.v.streamCodec, len(h.s.parameterSets))
	}
	unit := makeHEVCUnit(hevcIDRWRADL, true, 100, 1)
	if !h.sendUnit(1033333, flagKey|flagSets, unit) || h.v.frames[1].frame.Codec != CodecH265 {
		t.Fatalf("the H.265 keyframe was not delivered")
	}
	// Back to H.264, with a stream reset and without one: an H.264 stream
	// starts with the status, whatever stream_reset says.
	h.s.ApplySettings(settings1080p())
	h.expectVideoConfig(true, 1920, 1080, videoKbps1080p, 60)
	h.videoStatus(true, false, 1920, 1080, 30, 0)
	if h.v.streams != 3 || h.v.streamCodec != CodecH264 || len(h.s.parameterSets) != 0 {
		t.Fatalf("back to H.264: streams %d codec %v sets %d", h.v.streams, h.v.streamCodec, len(h.s.parameterSets))
	}

	// A sender without H.265.
	h = videoShow(t)
	h.s.ApplySettings(settings)
	if m := h.expect(svcVideo, kVideoConfig); m.payload[0] != 0 {
		t.Fatalf("video.config % x, want enabled 0", m.payload)
	}
	st := h.s.State()
	if st.Source != SourceSwitchingToPhysical || st.VideoError != "the browser uses H.265, which the sender does not offer" ||
		h.v.stopped != 1 || h.v.restores != 0 {
		t.Fatalf("H.265 browser: %+v stopped %d restores %d", st, h.v.stopped, h.v.restores)
	}
	h.videoStatus(false, false, 0, 0, 0, 0)
	h.s.DisplayStatus(false, false)
	if h.source() != SourcePhysical || h.v.restores != 1 || h.v.presents != 1 {
		t.Fatalf("after the disabled status: source %v restores %d presents %d", h.source(), h.v.restores, h.v.presents)
	}
	h.expectNone()
	// Back to H.264: video switches again.
	h.s.ApplySettings(settings1080p())
	if h.v.presents != 2 {
		t.Fatalf("no switch back to H.264")
	}
}
