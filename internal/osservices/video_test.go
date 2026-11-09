package osservices

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// Ported from test_os_services_session.c: service video, the handover and
// the retry budget.

var (
	testSPS = []byte{0, 0, 0, 1, 0x67, 0x42, 0x00, 0x1f, 0xe9, 0x02}
	// With a 3-byte start code; the KVM keeps it behind a 4-byte one.
	testPPS  = []byte{0, 0, 1, 0x68, 0xce, 0x3c, 0x80}
	testPPS4 = []byte{0, 0, 0, 1, 0x68, 0xce, 0x3c, 0x80}
)

// makeUnit is an H.264 access unit: SPS and PPS when sets, then one IDR or
// non-IDR slice with size bytes of data.
func makeUnit(idr, sets bool, size int, seed byte) []byte {
	var unit []byte
	if sets {
		unit = cat(testSPS, testPPS)
	}
	slice := byte(0x41)
	if idr {
		slice = 0x65
	}
	unit = append(unit, 0, 0, 0, 1, slice)
	for i := 0; i < size; i++ {
		unit = append(unit, 0x80|((seed+byte(i))&0x7f))
	}
	return unit
}

func encodeFrame(captureUS uint64, flags uint32, unit []byte) []byte {
	return wireMsg(svcVideo, kVideoFrame, le64(captureUS), le32(flags), unit)
}

// sendFrame sends one frame of a slice of size bytes; it returns its unit.
func (h *harness) sendFrame(captureUS uint64, flags uint32, idr, sets bool, size int) []byte {
	h.t.Helper()
	unit := makeUnit(idr, sets, size, byte(captureUS))
	h.feed(encodeFrame(captureUS, flags, unit))
	return unit
}

// sendUnit sends one frame of unit; true when the viewer got it.
func (h *harness) sendUnit(captureUS uint64, flags uint32, unit []byte) bool {
	h.t.Helper()
	frames := len(h.v.frames)
	h.feed(encodeFrame(captureUS, flags, unit))
	return len(h.v.frames) == frames+1
}

func (h *harness) registerVideo(width, height, fps uint32) {
	h.t.Helper()
	h.send(svcVideo, kVideoCaps, le32(h.videoFlags), le32(width), le32(height), le32(fps))
}

func (h *harness) withdrawVideo() {
	h.t.Helper()
	h.send(svcVideo, kVideoCaps, make([]byte, 16))
}

// videoStatusPayload is video.status with the codec of the settings.
func (h *harness) videoStatusPayload(enabled, reset bool, width, height, fps, errorCode uint32) []byte {
	p := make([]byte, 32)
	if enabled {
		p[0] = 1
		p[2] = byte(h.s.Settings().Codec)
	}
	if reset {
		p[3] = 1
	}
	wle.PutUint32(p[4:], width)
	wle.PutUint32(p[8:], height)
	wle.PutUint32(p[12:], fps)
	wle.PutUint32(p[28:], errorCode)
	return p
}

func (h *harness) videoStatus(enabled, reset bool, width, height, fps, errorCode uint32) {
	h.t.Helper()
	h.send(svcVideo, kVideoStatus, h.videoStatusPayload(enabled, reset, width, height, fps, errorCode))
}

// expectVideoConfig reads video.config: enabled, the settings' layout and
// codec, the size and the targets.
func (h *harness) expectVideoConfig(enabled bool, width, height, kbps, fps uint32) {
	h.t.Helper()
	m := h.expect(svcVideo, kVideoConfig)
	p := m.payload
	settings := h.s.Settings()
	want := byte(0)
	if enabled {
		want = 1
	}
	if len(p) != 20 || p[0] != want || p[1] != byte(settings.Layout) || p[2] != byte(settings.Codec) || p[3] != 0 ||
		wle.Uint32(p[4:]) != width || wle.Uint32(p[8:]) != height || wle.Uint32(p[12:]) != kbps || wle.Uint32(p[16:]) != fps {
		h.t.Fatalf("video.config % x, want enabled %v %dx%d %d kbps %d fps", p, enabled, width, height, kbps, fps)
	}
}

func (h *harness) source() VideoSource { return h.s.State().Source }

// videoOpen is an open session on the video platform, nothing written
// since.
func videoOpen(t *testing.T, settings Settings) *harness {
	t.Helper()
	h := newHarness(t, hooksVideo, settings)
	h.openSession()
	return h
}

// videoShow: registered, presented and showing a 1920x1080 stream at 30
// fps, after its first keyframe; nothing written since.
func videoShow(t *testing.T) *harness {
	t.Helper()
	h := videoOpen(t, settings1080p())
	h.registerVideo(3840, 2160, 60)
	h.s.DisplayStatus(true, true)
	h.expectVideoConfig(true, 1920, 1080, videoKbps1080p, 60)
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	if h.source() != SourceGuest {
		t.Fatalf("source %v after the enabled status", h.source())
	}
	h.sendFrame(1000000, flagKey|flagSets, true, true, 100)
	if len(h.v.frames) != 1 {
		t.Fatalf("%d frames", len(h.v.frames))
	}
	h.expectNone()
	return h
}

// videoWait: time passes with a ping from the sender every second and a
// tick every 100 ms.
func (h *harness) videoWait(ms int64) {
	h.t.Helper()
	for waited := int64(0); waited < ms; waited += 100 {
		h.f.now += 100
		if h.f.now%1000 == 0 {
			h.ping()
		}
		h.tick()
	}
}

func (h *harness) expectVideoFault(reason string) {
	h.t.Helper()
	st := h.s.State()
	if st.VideoError != reason || st.VideoRegistered || st.Source != SourceSwitchingToPhysical ||
		h.v.restores != 1 || h.v.stopped != 1 || !h.s.Open() {
		h.t.Fatalf("fault: error %q registered %v source %v restores %d stopped %d open %v, want %q",
			st.VideoError, st.VideoRegistered, st.Source, h.v.restores, h.v.stopped, h.s.Open(), reason)
	}
	h.expectVideoConfig(false, 1920, 1080, videoKbps1080p, 60)
}

// The registration is answered with video.config; a platform without video
// answers with enabled 0.
func TestVideoRegistrationIsAnswered(t *testing.T) {
	h := newHarness(t, hooksServices, defaultSettings())
	h.openSession()
	h.videoFlags = VideoCodecFlagH264 | VideoCodecFlagH265
	h.registerVideo(3840, 2160, 60)
	h.expectVideoConfig(false, 2560, 1440, videoKbps1440p, 60)
	if st := h.s.State(); !st.VideoRegistered || st.VideoCodecFlags != 3 || st.Source != SourcePhysical {
		t.Fatalf("registered: %+v", st)
	}
	// An enabled status without a switch faults video only.
	h.videoStatus(true, false, 2560, 1440, 60, 0)
	if !h.s.Open() || h.s.State().VideoRegistered {
		t.Fatalf("an enabled status without a switch: open %v", h.s.Open())
	}
	h.expectVideoConfig(false, 2560, 1440, videoKbps1440p, 60)
	if h.s.State().VideoError != "an enabled video.status without a switch to service video" {
		t.Fatalf("error %q", h.s.State().VideoError)
	}
	// Missing H.264 is invalid.
	h.videoFlags = VideoCodecFlagH265
	h.registerVideo(3840, 2160, 60)
	if h.s.State().VideoRegistered {
		t.Fatalf("registered without H.264")
	}
	h.expectNone()
	// Withdrawal: 16 zero bytes.
	h.videoFlags = VideoCodecFlagH264
	h.registerVideo(3840, 2160, 60)
	if !h.s.State().VideoRegistered {
		t.Fatalf("not registered")
	}
	h.expect(svcVideo, kVideoConfig)
	h.withdrawVideo()
	if h.s.State().VideoRegistered {
		t.Fatalf("registered after the withdrawal")
	}
	h.expectNone()
}

func TestVideoInvalidCapabilities(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
	}{
		{"15 bytes", make([]byte, 15)},
		{"17 bytes", make([]byte, 17)},
		{"unknown flag", cat(le32(1|8), le32(1920), le32(1080), le32(60))},
		{"width 0", cat(le32(1), le32(0), le32(1080), le32(60))},
		{"width over 16384", cat(le32(1), le32(16385), le32(1080), le32(60))},
		{"height over 16384", cat(le32(1), le32(1920), le32(16385), le32(60))},
		{"fps 0", cat(le32(1), le32(1920), le32(1080), le32(0))},
		{"fps over 240", cat(le32(1), le32(1920), le32(1080), le32(241))},
		{"flags 0 with a size", cat(le32(0), le32(1920), le32(1080), le32(60))},
		{"config from the sender", nil},
		{"request_keyframe from the sender", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := videoOpen(t, settings1080p())
			switch c.name {
			case "config from the sender":
				h.send(svcVideo, kVideoConfig, make([]byte, 20))
			case "request_keyframe from the sender":
				h.send(svcVideo, kVideoKeyframe)
			default:
				h.send(svcVideo, kVideoCaps, c.payload)
			}
			if !h.s.Open() || h.s.Counters.ServiceFaults != 1 || h.s.State().VideoRegistered || h.v.presents != 0 {
				t.Fatalf("open %v faults %d presents %d", h.s.Open(), h.s.Counters.ServiceFaults, h.v.presents)
			}
			// Not registered before: no video.config with enabled 0.
			h.expectNone()
			// The largest valid capabilities register.
			h.send(svcVideo, kVideoCaps, le32(1), le32(16384), le32(16384), le32(240))
			if !h.s.State().VideoRegistered || h.v.presents != 1 {
				t.Fatalf("the largest capabilities did not register")
			}
		})
	}
}

// Settings that need a size above the sender's capabilities fault video.
func TestVideoSettingsAboveCapabilities(t *testing.T) {
	h := videoOpen(t, defaultSettings())
	h.registerVideo(1920, 1080, 60)
	st := h.s.State()
	if st.VideoRegistered || st.VideoError != videoTooLarge || h.v.presents != 0 {
		t.Fatalf("registration above the settings: %+v", st)
	}
	// The fault answers with enabled 0 at the settings' size.
	h.expectVideoConfig(false, 2560, 1440, videoKbps1440p, 60)
	h.expectNone()
	// Smaller settings, and video registers.
	h.s.ApplySettings(settings1080p())
	h.registerVideo(1920, 1080, 60)
	if h.v.presents != 1 {
		t.Fatalf("no switch at 1080p")
	}
}

// Switch to service video: capture stops and the display is presented
// first; video.config with enabled 1 waits for it, frames wait for the
// enabled status.
func TestVideoSwitchWaitsForTheDisplay(t *testing.T) {
	h := videoOpen(t, defaultSettings())
	h.registerVideo(3840, 2160, 60)
	if h.v.presents != 1 || h.v.presentWidth != 2560 || h.v.presentHeight != 1440 || h.source() != SourceSwitchingToGuest {
		t.Fatalf("presents %d at %dx%d, source %v", h.v.presents, h.v.presentWidth, h.v.presentHeight, h.source())
	}
	h.s.DisplayStatus(false, true)
	h.expectNone()
	h.s.DisplayStatus(true, true)
	h.expectVideoConfig(true, 2560, 1440, videoKbps1440p, 60)
	h.sendFrame(1, flagKey|flagSets, true, true, 10)
	if h.v.reserves != 0 || h.s.VideoCounters.FramesDiscarded != 1 {
		t.Fatalf("a frame before the status: reserves %d", h.v.reserves)
	}
	h.videoStatus(true, true, 2560, 1440, 30, 0)
	if h.source() != SourceGuest || h.v.streams != 1 || h.v.streamWidth != 2560 || h.v.streamFPS != 30 || h.v.streamCodec != CodecH264 {
		t.Fatalf("stream %d %dx%d at %d", h.v.streams, h.v.streamWidth, h.v.streamHeight, h.v.streamFPS)
	}
	if st := h.s.State(); !st.StreamStarted || st.StreamWidth != 2560 || st.StreamFPS != 30 {
		t.Fatalf("state %+v", st)
	}
	// The effective rate is the cap when it is lower than the native one.
	p := h.videoStatusPayload(true, true, 2560, 1440, 60, 0)
	wle.PutUint32(p[24:], 24)
	h.send(svcVideo, kVideoStatus, p)
	if h.v.streams != 2 || h.v.streamFPS != 24 {
		t.Fatalf("stream %d at %d fps", h.v.streams, h.v.streamFPS)
	}
	h.expectNone()
}

// Frames split across USB reads at every boundary reach the slot intact.
func TestVideoFramesAcrossUSBReads(t *testing.T) {
	for _, chunk := range []int{1, 5, 12, 13, 511, 4096, 65536} {
		h := videoShow(t)
		var stream []byte
		var units [][]byte
		for i := 0; i < 6; i++ {
			idr := i == 3
			flags := uint32(0)
			if idr {
				flags = flagKey | flagSets
			}
			unit := makeUnit(idr, idr, 1000+i*517, byte(i))
			units = append(units, unit)
			stream = append(stream, encodeFrame(1033333+uint64(i)*33333, flags, unit)...)
		}
		h.feedChunked(stream, chunk)
		if len(h.v.frames) != 7 {
			t.Fatalf("chunk %d: %d frames", chunk, len(h.v.frames))
		}
		for i, unit := range units {
			f := h.v.frames[1+i]
			if !bytes.Equal(f.data, unit) || f.frame.Keyframe != (i == 3) || f.frame.Width != 1920 || f.frame.FPS != 30 ||
				f.frame.Codec != CodecH264 {
				t.Fatalf("chunk %d frame %d: %+v", chunk, i, f.frame)
			}
		}
		c := h.s.VideoCounters
		if c.Frames != 7 || c.Keyframes != 2 || !h.s.Open() {
			t.Fatalf("chunk %d: counters %+v", chunk, c)
		}
	}
}

// After stream_reset 1 the next frame is a keyframe with SPS and PPS.
func TestVideoStreamReset(t *testing.T) {
	h := videoShow(t)
	h.sendFrame(1033333, 0, false, false, 100)
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	// The platform discards what is queued from before.
	if h.v.streams != 2 || len(h.s.parameterSets) != 0 || h.s.VideoCounters.StreamResets != 2 {
		t.Fatalf("streams %d, sets %d", h.v.streams, len(h.s.parameterSets))
	}
	h.sendFrame(1066666, 0, false, false, 100)
	h.expectVideoFault("the first frame of a stream is not a keyframe with parameter sets")
	if h.v.reserves != 2 {
		t.Fatalf("reserves %d", h.v.reserves)
	}
	// A keyframe without parameter sets is no start either.
	h = videoShow(t)
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	h.sendFrame(1066666, flagKey, true, false, 100)
	h.expectVideoFault("the first frame of a stream is not a keyframe with parameter sets")

	h = videoShow(t)
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	h.sendFrame(1066666, flagKey|flagSets, true, false, 100)
	h.expectVideoFault("a video.frame flagged with parameter sets has no SPS and PPS")
	// The slot went back with what was written to it.
	if h.v.cancels != 1 || h.v.cancelWritten != 5+100 {
		t.Fatalf("cancels %d of %d bytes", h.v.cancels, h.v.cancelWritten)
	}

	// A size change without stream_reset is a new stream too, without the
	// SPS and PPS of the previous size.
	h = videoShow(t)
	if len(h.s.parameterSets) == 0 {
		t.Fatalf("no parameter sets kept")
	}
	h.videoStatus(true, false, 1280, 720, 30, 0)
	if h.v.streams != 2 || h.v.streamWidth != 1280 || len(h.s.parameterSets) != 0 {
		t.Fatalf("streams %d at %d, sets %d", h.v.streams, h.v.streamWidth, len(h.s.parameterSets))
	}
	h.sendFrame(1066666, 0, false, false, 100)
	if len(h.v.frames) != 2 || h.v.frames[1].frame.Width != 1280 {
		t.Fatalf("frames %d", len(h.v.frames))
	}
	// The same status again starts nothing.
	h.videoStatus(true, false, 1280, 720, 30, 0)
	if h.v.streams != 2 {
		t.Fatalf("an unchanged status started a stream")
	}
}

// The rule holds for what the sender sends: a first keyframe that no viewer
// takes, or that is dropped, still starts the stream, and the deltas after
// it are read past the same way.
func TestVideoStreamResetWithoutViewer(t *testing.T) {
	h := videoShow(t)
	h.v.noViewer = true
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	h.sendFrame(1033333, flagKey|flagSets, true, true, 100)
	h.sendFrame(1066666, 0, false, false, 100)
	if st := h.s.State(); !st.VideoRegistered || st.VideoError != "" || h.s.VideoCounters.FramesDiscarded != 2 {
		t.Fatalf("no viewer: %+v discarded %d", st, h.s.VideoCounters.FramesDiscarded)
	}
	// With a viewer again, frames reach it.
	h.v.noViewer = false
	h.sendFrame(1099999, flagKey|flagSets, true, true, 100)
	if len(h.v.frames) != 2 || !h.v.frames[1].frame.Keyframe {
		t.Fatalf("frames %d", len(h.v.frames))
	}
}

// A stream's first keyframe that no viewer takes still gives its SPS and
// PPS to a later keyframe without them; it is larger than the payload
// buffer and comes over several reads.
func TestVideoParameterSetsFromADiscardedKeyframe(t *testing.T) {
	h := videoShow(t)
	h.v.noViewer = true
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	first := makeUnit(true, true, specControlMax+100, 3)
	h.feedChunked(encodeFrame(1033333, flagKey|flagSets, first), 16384)
	if h.s.VideoCounters.FramesDiscarded != 1 {
		t.Fatalf("discarded %d", h.s.VideoCounters.FramesDiscarded)
	}
	h.v.noViewer = false
	unit := h.sendFrame(1066666, flagKey, true, false, 100)
	if len(h.v.frames) != 2 || !h.v.frames[1].frame.ParameterSetsAdded {
		t.Fatalf("frames %d", len(h.v.frames))
	}
	if want := cat(testSPS, testPPS4, unit); !bytes.Equal(h.v.frames[1].data, want) {
		t.Fatalf("frame % x", trim(h.v.frames[1].data))
	}
}

// A discarded keyframe whose PPS ends past the payload buffer gives
// nothing: a cut PPS is never put in front of a keyframe.
func TestVideoParameterSetsCutShortNotKept(t *testing.T) {
	h := videoShow(t)
	h.v.noViewer = true
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	// An SEI that puts the end of the buffer inside the PPS.
	sei := specControlMax - len(testSPS) - 5
	first := append([]byte{0, 0, 0, 1, 0x06}, bytes.Repeat([]byte{0x80}, sei-5)...)
	first = append(first, makeUnit(true, true, 100, 3)...)
	h.feed(encodeFrame(1033333, flagKey|flagSets, first))
	h.v.noViewer = false
	h.sendFrame(1066666, flagKey, true, false, 100)
	if len(h.v.frames) != 2 || h.v.frames[1].frame.ParameterSetsAdded {
		t.Fatalf("a cut PPS was added")
	}
}

// A keyframe without SPS and PPS gets the stream's, from the cache.
func TestVideoParameterSetsPrepended(t *testing.T) {
	h := videoShow(t)
	cached := cat(testSPS, testPPS4)
	if !bytes.Equal(h.s.parameterSets, cached) {
		t.Fatalf("cache % x", h.s.parameterSets)
	}
	unit := makeUnit(true, false, 300, 9)
	h.feed(encodeFrame(1033333, flagKey, unit))
	f := h.v.frames[1]
	if !f.frame.Keyframe || !f.frame.ParameterSetsAdded || !bytes.Equal(f.data, cat(cached, unit)) ||
		h.s.VideoCounters.ParameterSetsAdded != 1 {
		t.Fatalf("frame %+v", f.frame)
	}
	// Delta frames and keyframes that carry their own get nothing.
	h.sendFrame(1066666, 0, false, false, 50)
	h.sendFrame(1099999, flagKey|flagSets, true, true, 50)
	if h.v.frames[2].frame.ParameterSetsAdded || h.v.frames[3].frame.ParameterSetsAdded ||
		h.s.VideoCounters.ParameterSetsAdded != 1 {
		t.Fatalf("parameter sets added to frames that need none")
	}
	if c := h.s.VideoCounters; c.LargestKeyframe != uint32(len(cached)+len(unit)) || c.Keyframes != 3 {
		t.Fatalf("counters %+v", c)
	}
}

// VideoStream says what each slot of the stream takes, and that is the
// limit: a keyframe that does not fit, with the cached SPS and PPS it would
// get, faults video; a larger delta frame is dropped as a gap.
func TestVideoFrameLimitsFollowTheStream(t *testing.T) {
	key := makeUnit(true, true, 2000, 1)
	bare := makeUnit(true, false, 2000, 2)
	delta := makeUnit(false, false, 2000, 3)

	h := videoShow(t)
	cached := len(h.s.parameterSets)
	h.v.capacity = len(bare) + cached
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	if h.v.streams != 2 {
		t.Fatalf("streams %d", h.v.streams)
	}
	if !h.sendUnit(1033333, flagKey|flagSets, key) || !h.sendUnit(1066666, flagKey, bare) {
		t.Fatalf("frames that fit were not delivered")
	}
	if len(h.v.frames[2].data) != h.v.capacity {
		t.Fatalf("frame of %d bytes", len(h.v.frames[2].data))
	}
	h.v.capacity = len(delta) - 1
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	h.sendFrame(1099999, flagKey|flagSets, true, true, 100)
	if h.sendUnit(1133332, 0, delta) {
		t.Fatalf("a delta frame above the slot was delivered")
	}
	if h.s.VideoCounters.FramesDropped != 1 || h.v.gaps != 1 || !h.s.State().VideoRegistered {
		t.Fatalf("dropped %d gaps %d", h.s.VideoCounters.FramesDropped, h.v.gaps)
	}

	h = videoShow(t)
	h.v.capacity = len(bare) + cached - 1
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	h.sendFrame(1033333, flagKey|flagSets, true, true, 100)
	h.sendUnit(1066666, flagKey, bare)
	h.expectVideoFault(fmt.Sprintf("a keyframe above %d bytes", h.v.capacity))
}

func pts90k(us uint64) uint32 { return uint32(us * 9 / 100) }

// pts follows the sender's capture spacing from 1 us to 1 s, else one frame
// at the stream's rate; a new stream starts at the KVM's clock, never
// before the frame before it.
func TestVideoPTS(t *testing.T) {
	h := videoShow(t)
	ptsUS := uint64(h.v.frames[0].frame.ArrivalUS)
	if h.v.frames[0].frame.PTS90k != pts90k(ptsUS) {
		t.Fatalf("first pts %d", h.v.frames[0].frame.PTS90k)
	}
	steps := []struct{ captureUS, stepUS uint64 }{
		{1033333, 33333},   // the capture spacing
		{1033333, 33333},   // no spacing: one frame at 30 fps
		{3033333, 33333},   // above 1 s
		{3000000, 33333},   // backwards
		{3000001, 1},       // 1 us
		{4000001, 1000000}, // 1 s
	}
	for i, step := range steps {
		h.sendFrame(step.captureUS, 0, false, false, 20)
		ptsUS += step.stepUS
		if got := h.v.frames[1+i].frame.PTS90k; got != pts90k(ptsUS) {
			t.Fatalf("step %d: pts %d, want %d", i, got, pts90k(ptsUS))
		}
	}
	// A new stream right away continues one frame after the last.
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	h.sendFrame(7, flagKey|flagSets, true, true, 20)
	ptsUS += 33333
	if got := h.v.frames[7].frame.PTS90k; got != pts90k(ptsUS) {
		t.Fatalf("new stream: pts %d, want %d", got, pts90k(ptsUS))
	}
	// Later, at the KVM's clock.
	h.f.now += 10000
	h.ping()
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	h.v.usOffset = 250
	h.sendFrame(9, flagKey|flagSets, true, true, 20)
	f := h.v.frames[8].frame
	if f.PTS90k != pts90k(uint64(h.f.now)*1000+250) || f.ArrivalUS != h.f.now*1000+250 || f.ReceivedUS != f.ArrivalUS {
		t.Fatalf("later stream: pts %d arrival %d", f.PTS90k, f.ArrivalUS)
	}
}

// The frames the sender left out at the stream's rate, from the capture
// times of the frames it sent: a gap of 1.5 frame periods or more is one, of
// 3 periods two. Frames the KVM did not deliver were still sent; gaps above
// 1 s, backwards and across a stream's start do not count.
func TestVideoSenderSkippedFrames(t *testing.T) {
	h := videoShow(t)
	steps := []struct {
		captureUS uint64
		skipped   uint32
	}{
		{1033333, 0}, // one period at 30 fps
		{1083333, 1}, // 1.5 periods
		{1133332, 1}, // just under 1.5 periods
		{1233332, 3}, // 3 periods
		{2233333, 3}, // above 1 s
		{2200000, 3}, // backwards
		{2266666, 4}, // 2 periods after the frame that went backwards
	}
	for i, step := range steps {
		h.sendFrame(step.captureUS, 0, false, false, 20)
		if got := h.s.VideoCounters.SenderSkippedFrames; got != step.skipped {
			t.Fatalf("step %d: %d skipped, want %d", i, got, step.skipped)
		}
	}
	h.v.noViewer = true
	h.sendFrame(2300000, 0, false, false, 20)
	h.sendFrame(2366666, 0, false, false, 20)
	h.v.noViewer = false
	h.sendFrame(2400000, 0, false, false, 20)
	if h.s.VideoCounters.SenderSkippedFrames != 5 || h.s.VideoCounters.FramesDiscarded != 2 {
		t.Fatalf("skipped %d discarded %d", h.s.VideoCounters.SenderSkippedFrames, h.s.VideoCounters.FramesDiscarded)
	}

	// A new stream, at 60 fps: its first frame comes 0.5 s after the last
	// one of the stream before.
	h.videoStatus(true, true, 1920, 1080, 60, 0)
	h.sendFrame(2900000, flagKey|flagSets, true, true, 20)
	if h.s.VideoCounters.SenderSkippedFrames != 5 {
		t.Fatalf("a stream's start counted")
	}
	h.sendFrame(2916667, 0, false, false, 20)
	h.sendFrame(2950000, 0, false, false, 20)
	if h.s.VideoCounters.SenderSkippedFrames != 6 {
		t.Fatalf("skipped %d at 60 fps", h.s.VideoCounters.SenderSkippedFrames)
	}
	// Frames read past while no stream is shown start the count again.
	h.videoStatus(false, false, 0, 0, 0, 0)
	h.sendFrame(3000000, 0, false, false, 20)
	if h.s.VideoCounters.SenderSkippedFrames != 6 {
		t.Fatalf("a frame read past counted")
	}
}

func (h *harness) keyframeRequests() int {
	h.t.Helper()
	count := 0
	for {
		m, ok := h.next()
		if !ok {
			return count
		}
		if m.service != svcVideo || m.kind != kVideoKeyframe || len(m.payload) != 0 {
			h.t.Fatalf("service %d kind %d (% x), want video.request_keyframe", m.service, m.kind, m.payload)
		}
		count++
	}
}

// The platform's keyframe request is asked of the sender at most every 600
// ms, given back undelivered when no keyframe came in time, and answered by
// the next keyframe, which carries its generation.
func TestVideoKeyframeRequestRate(t *testing.T) {
	h := videoShow(t)
	h.v.keyframePending = 7
	h.tick()
	if n := h.keyframeRequests(); n != 1 {
		t.Fatalf("%d requests", n)
	}
	h.videoWait(500)
	if n := h.keyframeRequests(); n != 0 || len(h.v.keyframeDone) != 0 {
		t.Fatalf("%d requests within 600 ms", n)
	}
	h.videoWait(100)
	if n := h.keyframeRequests(); n != 1 {
		t.Fatalf("%d requests at 600 ms", n)
	}
	if len(h.v.keyframeDone) != 1 || h.v.keyframeDone[0] != (keyframeDoneEvent{7, false}) {
		t.Fatalf("given back: %+v", h.v.keyframeDone)
	}
	h.videoWait(3000)
	if n := h.keyframeRequests(); n != 5 {
		t.Fatalf("%d requests in 3 s", n)
	}
	if h.s.VideoCounters.KeyframeRequests != 7 {
		t.Fatalf("KeyframeRequests %d", h.s.VideoCounters.KeyframeRequests)
	}
	// Deltas carry no generation; the keyframe answers the request.
	h.sendFrame(1033333, 0, false, false, 20)
	if g := h.v.frames[1].frame.KeyframeGeneration; g != 0 {
		t.Fatalf("a delta answered %d", g)
	}
	h.sendFrame(1066666, flagKey|flagSets, true, true, 20)
	if g := h.v.frames[2].frame.KeyframeGeneration; g != 7 || h.v.lastKeyframeDone() != (keyframeDoneEvent{7, true}) {
		t.Fatalf("the keyframe answered %d", g)
	}
	h.videoWait(3000)
	if n := h.keyframeRequests(); n != 0 {
		t.Fatalf("%d requests after the answer", n)
	}
	// A keyframe that comes first answers a request not yet asked.
	h.v.keyframePending = 8
	h.sendFrame(1099999, flagKey|flagSets, true, true, 20)
	if g := h.v.frames[3].frame.KeyframeGeneration; g != 8 {
		t.Fatalf("the keyframe answered %d", g)
	}
	h.videoWait(1000)
	if n := h.keyframeRequests(); n != 0 {
		t.Fatalf("%d requests", n)
	}
	// A keyframe the platform could not take answers nothing.
	h.v.keyframePending = 9
	h.v.refuseCommit = true
	h.sendFrame(1133332, flagKey|flagSets, true, true, 20)
	if h.v.lastKeyframeDone() != (keyframeDoneEvent{9, false}) || h.v.keyframePending != 9 {
		t.Fatalf("refused keyframe: %+v pending %d", h.v.lastKeyframeDone(), h.v.keyframePending)
	}
	// A request is given back when the stream stops.
	h.v.refuseCommit = false
	h.tick()
	h.keyframeRequests()
	given := len(h.v.keyframeDone)
	h.s.End("test")
	if h.v.lastKeyframeDone() != (keyframeDoneEvent{9, false}) || len(h.v.keyframeDone) != given+1 {
		t.Fatalf("the request held at the end: %+v", h.v.keyframeDone)
	}
}

// A busy IN endpoint skips a keyframe request; it goes again after the
// interval, and the session stays.
func TestVideoKeyframeRequestOnABusyEndpoint(t *testing.T) {
	h := videoShow(t)
	h.v.keyframePending = 3
	h.f.failWrites = true
	h.tick()
	if !h.s.Open() || h.s.VideoCounters.KeyframeRequests != 0 {
		t.Fatalf("open %v requests %d", h.s.Open(), h.s.VideoCounters.KeyframeRequests)
	}
	h.f.failWrites = false
	h.videoWait(500)
	if n := h.keyframeRequests(); n != 0 {
		t.Fatalf("%d requests within 600 ms", n)
	}
	h.videoWait(100)
	if n := h.keyframeRequests(); n != 1 {
		t.Fatalf("%d requests after 600 ms", n)
	}
}

// No free slot holds the read (backpressure), also for as long as a weak
// link takes to send a keyframe; after 1 s the frame is dropped and the
// platform hears of the gap.
func TestVideoBackpressureAndGap(t *testing.T) {
	h := videoShow(t)
	unit := makeUnit(false, false, 2000, 3)
	message := encodeFrame(1033333, 0, unit)
	head := specEnvelope + specFrameHead
	h.v.busy = true
	if n := h.s.Receive(message); n != head || !h.s.FrameWaiting() {
		t.Fatalf("Receive took %d, waiting %v", n, h.s.FrameWaiting())
	}
	h.f.now += 800
	if n := h.s.Receive(message[head:]); n != 0 {
		t.Fatalf("Receive while waiting took %d", n)
	}
	h.v.busy = false
	h.feed(message[head:])
	if len(h.v.frames) != 2 || !bytes.Equal(h.v.frames[1].data, unit) {
		t.Fatalf("%d frames", len(h.v.frames))
	}
	if c := h.s.VideoCounters; c.SlotWaits != 1 || c.SlotWaitMaxMS != 800 || c.SlotWaitMS != 800 || h.v.gaps != 0 {
		t.Fatalf("counters %+v gaps %d", c, h.v.gaps)
	}

	h.v.busy = true
	if n := h.s.Receive(message); n != head {
		t.Fatalf("Receive took %d", n)
	}
	h.f.now += 1000 - 1
	if n := h.s.Receive(message[head:]); n != 0 {
		t.Fatalf("Receive took %d", n)
	}
	h.f.now++
	h.feed(message[head:])
	if len(h.v.frames) != 2 || h.v.gaps != 1 || h.s.VideoCounters.FramesDropped != 1 {
		t.Fatalf("frames %d gaps %d dropped %d", len(h.v.frames), h.v.gaps, h.s.VideoCounters.FramesDropped)
	}
	// The stream stays aligned.
	h.v.busy = false
	h.sendFrame(1100000, 0, false, false, 30)
	if len(h.v.frames) != 3 {
		t.Fatalf("frames %d", len(h.v.frames))
	}
	// Without a viewer, frames are read past at once.
	h.v.noViewer = true
	h.sendFrame(1133333, 0, false, false, 30)
	if len(h.v.frames) != 3 || h.v.gaps != 1 || h.s.VideoCounters.FramesDiscarded != 1 {
		t.Fatalf("no viewer: frames %d gaps %d", len(h.v.frames), h.v.gaps)
	}
	// The session ends while a frame waits: the read is given up.
	h.v.noViewer = false
	h.v.busy = true
	if n := h.s.Receive(message); n != head {
		t.Fatalf("Receive took %d", n)
	}
	h.s.End("test")
	if h.s.FrameWaiting() || h.s.Receive(message[head:]) != 0 {
		t.Fatalf("a frame waits after the end")
	}
}

// 45 s for the first enabled status; 15 s for the disabled status.
func TestVideoTimers(t *testing.T) {
	h := videoOpen(t, settings1080p())
	h.registerVideo(1920, 1080, 60)
	h.s.DisplayStatus(true, true)
	h.expectVideoConfig(true, 1920, 1080, videoKbps1080p, 60)
	h.videoWait(45000 - 100)
	if h.source() != SourceSwitchingToGuest {
		t.Fatalf("source %v before 45 s", h.source())
	}
	h.videoWait(100)
	if h.source() != SourceSwitchingToPhysical || h.v.restores != 1 || h.s.VideoCounters.SwitchFailures != 1 ||
		h.s.State().VideoError != "the sender did not start video within 45 seconds" {
		t.Fatalf("after 45 s: source %v restores %d error %q", h.source(), h.v.restores, h.s.State().VideoError)
	}
	h.expectVideoConfig(false, 1920, 1080, videoKbps1080p, 60)
	h.s.DisplayStatus(false, false)
	if h.source() != SourcePhysical {
		t.Fatalf("source %v after the restore", h.source())
	}
	// The status the sender sent before it saw enabled 0 is stale, not a
	// fault; after its disabled status, an enabled one is.
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	if !h.s.State().VideoRegistered || h.v.streams != 0 {
		t.Fatalf("a stale status: registered %v streams %d", h.s.State().VideoRegistered, h.v.streams)
	}
	h.videoStatus(false, false, 0, 0, 0, 0)
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	if h.s.State().VideoRegistered {
		t.Fatalf("an enabled status after the disabled one was accepted")
	}

	// The user turns video off: the display stays until the disabled
	// status, or 15 seconds.
	for _, acknowledged := range []bool{false, true} {
		h := videoShow(t)
		off := settings1080p()
		off.VideoEnabled = false
		h.s.ApplySettings(off)
		h.expectVideoConfig(false, 1920, 1080, videoKbps1080p, 60)
		if h.source() != SourceSwitchingToPhysical || h.v.stopped != 1 || h.v.restores != 0 {
			t.Fatalf("off: source %v stopped %d restores %d", h.source(), h.v.stopped, h.v.restores)
		}
		h.sendFrame(1033333, 0, false, false, 30)
		if len(h.v.frames) != 1 {
			t.Fatalf("a frame after video off")
		}
		if acknowledged {
			h.videoStatus(false, false, 0, 0, 0, 0)
		} else {
			h.videoWait(15000 - 100)
			if h.v.restores != 0 {
				t.Fatalf("restored before 15 s")
			}
			h.videoWait(100)
		}
		if h.v.restores != 1 {
			t.Fatalf("acknowledged %v: restores %d", acknowledged, h.v.restores)
		}
		h.s.DisplayStatus(false, true)
		if h.source() != SourceSwitchingToPhysical {
			t.Fatalf("physical while the display is owned")
		}
		h.s.DisplayStatus(false, false)
		if h.source() != SourcePhysical {
			t.Fatalf("source %v", h.source())
		}
		h.expectNone()
	}
}

// A firmware install holds display handovers. A registration meanwhile is
// answered at once with video disabled, the switch outlasts the install,
// its timeout starts again when the hold ends, and video starts once the
// display is presented.
func TestVideoRegistrationWhileDisplayHeld(t *testing.T) {
	h := videoOpen(t, settings1080p())
	h.s.DisplayHeld(true)
	h.registerVideo(1920, 1080, 60)
	if h.source() != SourceSwitchingToGuest {
		t.Fatalf("source %v", h.source())
	}
	h.s.DisplayHeld(true)
	h.expectVideoConfig(false, 1920, 1080, videoKbps1080p, 60)
	h.videoStatus(false, false, 0, 0, 0, 0)
	h.videoWait(2 * 45000)
	if h.source() != SourceSwitchingToGuest || h.v.restores != 0 {
		t.Fatalf("held: source %v restores %d", h.source(), h.v.restores)
	}
	h.expectNone()
	h.s.DisplayHeld(false)
	h.videoWait(45000 - 100)
	if h.source() != SourceSwitchingToGuest {
		t.Fatalf("the timeout did not start again")
	}
	h.s.DisplayStatus(true, true)
	h.expectVideoConfig(true, 1920, 1080, videoKbps1080p, 60)
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	if h.source() != SourceGuest || h.v.streams != 1 {
		t.Fatalf("source %v streams %d", h.source(), h.v.streams)
	}
}

// videoReconnect: a new session after a restore; the display is back.
func (h *harness) videoReconnect() {
	h.t.Helper()
	h.openSession()
	h.s.DisplayStatus(false, false)
	if h.source() != SourcePhysical {
		h.t.Fatalf("source %v after the reconnect", h.source())
	}
}

// Three switches in two minutes, across sessions; the fourth is refused
// and automatic switches stay blocked until the user turns video off and
// on.
func TestVideoRetryBudget(t *testing.T) {
	settings := settings1080p()
	h := videoOpen(t, settings)
	for i := 0; i < 3; i++ {
		h.registerVideo(1920, 1080, 60)
		if h.v.presents != i+1 {
			t.Fatalf("switch %d: presents %d", i, h.v.presents)
		}
		h.f.now += 30000
		h.videoReconnect()
	}
	h.registerVideo(1920, 1080, 60)
	if h.v.presents != 3 || !h.s.recovery.blocked {
		t.Fatalf("fourth switch: presents %d blocked %v", h.v.presents, h.s.recovery.blocked)
	}
	h.expectVideoConfig(false, 1920, 1080, videoKbps1080p, 60)
	if !strings.Contains(h.s.State().VideoError, "too often") {
		t.Fatalf("error %q", h.s.State().VideoError)
	}
	// The block outlives the window and the session.
	h.f.now += 120000
	h.videoReconnect()
	h.registerVideo(1920, 1080, 60)
	if h.v.presents != 3 {
		t.Fatalf("a switch while blocked")
	}
	h.expectVideoConfig(false, 1920, 1080, videoKbps1080p, 60)
	off := settings
	off.VideoEnabled = false
	h.s.ApplySettings(off)
	h.expectVideoConfig(false, 1920, 1080, videoKbps1080p, 60)
	h.s.ApplySettings(settings)
	if h.v.presents != 4 || h.s.recovery.blocked {
		t.Fatalf("off and on: presents %d blocked %v", h.v.presents, h.s.recovery.blocked)
	}

	// An inactive display gives the current switch back to the budget.
	h = videoOpen(t, settings)
	for i := 0; i < 5; i++ {
		h.registerVideo(1920, 1080, 60)
		if h.v.presents != i+1 {
			t.Fatalf("inactive display %d: presents %d", i, h.v.presents)
		}
		h.s.DisplayStatus(true, true)
		h.expectVideoConfig(true, 1920, 1080, videoKbps1080p, 60)
		h.videoStatus(false, false, 0, 0, 0, 1)
		if h.v.restores != i+1 {
			t.Fatalf("inactive display %d: restores %d", i, h.v.restores)
		}
		h.s.DisplayStatus(false, false)
		h.withdrawVideo()
	}
	if h.s.recovery.blocked {
		t.Fatalf("inactive displays blocked video")
	}

	// The KVM's own USB restarts (mounts, unmounts) give the current switch
	// back; the host's reconfigurations count.
	h = videoOpen(t, settings)
	for i := 0; i < 5; i++ {
		h.registerVideo(1920, 1080, 60)
		if h.v.presents != i+1 {
			t.Fatalf("KVM restart %d: presents %d", i, h.v.presents)
		}
		h.s.USBReconfigured(true)
		h.videoReconnect()
	}
	if h.s.recovery.blocked {
		t.Fatalf("the KVM's restarts blocked video")
	}
	for i := 0; i < 3; i++ {
		h.registerVideo(1920, 1080, 60)
		h.s.USBReconfigured(false)
		h.videoReconnect()
	}
	h.registerVideo(1920, 1080, 60)
	if h.v.presents != 8 || !h.s.recovery.blocked {
		t.Fatalf("host restarts: presents %d blocked %v", h.v.presents, h.s.recovery.blocked)
	}
	h.expectVideoConfig(false, 1920, 1080, videoKbps1080p, 60)
}

// Video turned off and on while no session runs clears the retry budget,
// as two separate settings changes do (the Mini's
// test_video_off_on_between_passes).
func TestVideoOffAndOnClearsTheBlock(t *testing.T) {
	settings := settings1080p()
	h := videoOpen(t, settings)
	for i := 0; i < 3; i++ {
		h.registerVideo(1920, 1080, 60)
		h.videoReconnect()
	}
	h.registerVideo(1920, 1080, 60)
	if !h.s.recovery.blocked || h.s.State().VideoError != videoBlocked {
		t.Fatalf("not blocked: %q", h.s.State().VideoError)
	}
	h.s.End("test")
	off := settings
	off.VideoEnabled = false
	h.s.ApplySettings(off)
	h.s.ApplySettings(settings)
	if h.s.recovery.blocked || len(h.s.recovery.startsMS) != 0 {
		t.Fatalf("off and on: %+v", h.s.recovery)
	}
	h.videoReconnect()
	h.registerVideo(1920, 1080, 60)
	if h.v.presents != 4 {
		t.Fatalf("presents %d", h.v.presents)
	}
	// Unchanged video settings leave the block.
	for i := 0; i < 3; i++ {
		h.videoReconnect()
		h.registerVideo(1920, 1080, 60)
	}
	h.s.ApplySettings(settings)
	if !h.s.recovery.blocked {
		t.Fatalf("an unchanged setting cleared the block")
	}
}

// videoSwitchShown: a switch that reaches service video at 1920x1080.
func (h *harness) videoSwitchShown() {
	h.t.Helper()
	h.registerVideo(1920, 1080, 60)
	h.s.DisplayStatus(true, true)
	h.expectVideoConfig(true, 1920, 1080, videoKbps1080p, 60)
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	if h.source() != SourceGuest {
		h.t.Fatalf("source %v", h.source())
	}
}

// A switch that has shown service video for 10 s has succeeded: sender
// restarts after such switches never block video. A switch whose session
// ends sooner still counts.
func TestVideoRetryBudgetShown(t *testing.T) {
	settings := settings1080p()
	h := videoOpen(t, settings)
	for i := 0; i < 5; i++ {
		h.videoSwitchShown()
		if h.v.presents != i+1 {
			t.Fatalf("presents %d", h.v.presents)
		}
		h.videoWait(10000)
		h.videoReconnect()
	}
	if h.s.recovery.blocked || len(h.s.recovery.startsMS) != 0 {
		t.Fatalf("shown switches counted: %+v", h.s.recovery)
	}

	h = videoOpen(t, settings)
	for i := 0; i < 3; i++ {
		h.videoSwitchShown()
		h.videoWait(10000 - 1000)
		h.videoReconnect()
	}
	h.registerVideo(1920, 1080, 60)
	if h.v.presents != 3 || !h.s.recovery.blocked {
		t.Fatalf("short switches: presents %d blocked %v", h.v.presents, h.s.recovery.blocked)
	}
	h.expectVideoConfig(false, 1920, 1080, videoKbps1080p, 60)

	// A switch leaves the budget once: an inactive display after it has
	// been shown does not take an earlier switch out as well.
	h = videoOpen(t, settings)
	h.registerVideo(1920, 1080, 60)
	h.videoReconnect()
	h.videoSwitchShown()
	h.videoWait(10000)
	h.videoStatus(false, false, 0, 0, 0, 1)
	if len(h.s.recovery.startsMS) != 1 {
		t.Fatalf("%d switches counted", len(h.s.recovery.startsMS))
	}
}

// relayUpdate is the KVM's update into the open session, which ends with
// the sender's update.status.
func (h *harness) relayUpdate(status byte) {
	h.t.Helper()
	data := []byte{1, 2, 3, 4}
	h.registerUpdate()
	id, refused := h.s.UpdateBegin(uint64(len(data)), [32]byte{}, "0.4.2")
	if refused != "" {
		h.t.Fatalf("UpdateBegin: %q", refused)
	}
	h.expect(svcUpdate, kUpdateBegin)
	h.senderUpdateCredit(kUpdateReady, id, uint32(len(data)))
	if n := h.s.UpdateSend(id, data); n != len(data) {
		h.t.Fatalf("UpdateSend %d", n)
	}
	h.readUpdateChunks(id, 0, nil)
	message := ""
	if status == 1 {
		message = "no space"
	}
	h.senderUpdateStatus(id, status, message)
	if u := h.s.Update(); !u.Ended || u.Status != status {
		h.t.Fatalf("update %+v", u)
	}
}

// The sender exits to install the KVM's update, and the next session
// switches back; as with the KVM's own USB restart, that switch does not
// count. Video on, a sender restart, the update, then one more sender
// restart within two minutes.
func TestVideoRetryBudgetAfterUpdate(t *testing.T) {
	settings := settings1080p()
	h := videoOpen(t, settings)
	h.registerVideo(1920, 1080, 60)
	h.f.now += 17000
	h.videoReconnect()
	h.registerVideo(1920, 1080, 60)
	h.f.now += 21000
	h.relayUpdate(0)
	h.videoReconnect()
	h.registerVideo(1920, 1080, 60)
	h.f.now += 18000
	h.videoReconnect()
	h.registerVideo(1920, 1080, 60)
	if h.v.presents != 4 || h.s.recovery.blocked {
		t.Fatalf("after the update: presents %d blocked %v", h.v.presents, h.s.recovery.blocked)
	}
	// Only the update's own session end is free: the session after it
	// counts, so one more restart in the window is refused.
	h.videoReconnect()
	h.registerVideo(1920, 1080, 60)
	if h.v.presents != 4 || !h.s.recovery.blocked {
		t.Fatalf("one more: presents %d blocked %v", h.v.presents, h.s.recovery.blocked)
	}

	// A failed update leaves the sender running; its later end counts.
	h = videoOpen(t, settings)
	h.registerVideo(1920, 1080, 60)
	h.videoReconnect()
	h.registerVideo(1920, 1080, 60)
	h.relayUpdate(1)
	h.videoReconnect()
	h.registerVideo(1920, 1080, 60)
	h.videoReconnect()
	h.registerVideo(1920, 1080, 60)
	if h.v.presents != 3 || !h.s.recovery.blocked {
		t.Fatalf("failed update: presents %d blocked %v", h.v.presents, h.s.recovery.blocked)
	}
}

// Rule breaks fault video only: back to hardware capture, video.config with
// enabled 0, the reason for the browser; new capabilities clear it.
func TestVideoFaults(t *testing.T) {
	h := videoShow(t)
	h.sendFrame(1033333, flagKey|flagSets, false, true, 100)
	h.expectVideoFault("the video.frame keyframe flag does not match its access unit")
	if h.s.VideoCounters.Faults != 1 {
		t.Fatalf("faults %d", h.s.VideoCounters.Faults)
	}
	h.registerVideo(3840, 2160, 60)
	if st := h.s.State(); !st.VideoRegistered || st.VideoError != "" {
		t.Fatalf("registered again: %+v", st)
	}

	h = videoShow(t)
	h.sendFrame(1033333, 0, true, false, 100)
	h.expectVideoFault("the video.frame keyframe flag does not match its access unit")

	h = videoShow(t)
	h.sendFrame(1033333, 4, false, false, 100)
	h.expectVideoFault("a video.frame with unknown flags")
	// The sender's disabled status goes unread while video is detached; its
	// withdrawal is no second fault.
	h.videoStatus(false, false, 0, 0, 0, 0)
	h.withdrawVideo()
	if h.s.VideoCounters.Faults != 1 {
		t.Fatalf("faults %d", h.s.VideoCounters.Faults)
	}

	// A frame with no access unit.
	h = videoShow(t)
	h.feed(wireMsg(svcVideo, kVideoFrame, le64(1033333), le32(0)))
	h.expectVideoFault("a video.frame without an access unit")

	// Too large for the stream's slots: refused at its head, then read
	// past.
	h = videoShow(t)
	big := encodeFrame(0, flagKey|flagSets, make([]byte, fakeSlotBytes+1))
	h.feedChunked(big, 16384)
	h.expectVideoFault("a keyframe above 3145728 bytes")
	if h.v.reserves != 1 {
		t.Fatalf("reserves %d", h.v.reserves)
	}
	h.ping()
	if !h.s.Open() {
		t.Fatalf("the stream lost its alignment")
	}

	h = videoShow(t)
	h.withdrawVideo()
	h.expectVideoFault("video withdrawn without a disabled video.status")

	h = videoShow(t)
	h.videoStatus(true, true, 3840, 2400, 30, 0)
	h.expectVideoFault("video.status exceeds the sender's video.capabilities")

	h = videoShow(t)
	p := h.videoStatusPayload(true, false, 1920, 1080, 30, 0)
	wle.PutUint32(p[24:], 61)
	h.send(svcVideo, kVideoStatus, p)
	h.expectVideoFault("video.status exceeds the sender's video.capabilities")

	h = videoShow(t)
	h.videoStatus(true, true, 1920, 0, 30, 0)
	h.expectVideoFault("an enabled video.status with an invalid size or rate")

	h = videoShow(t)
	h.videoStatus(true, true, 1920, 1080, 241, 0)
	h.expectVideoFault("an enabled video.status with an invalid size or rate")

	for _, bad := range []func(p []byte) []byte{
		func(p []byte) []byte { return p[:31] },
		func(p []byte) []byte { p[0] = 2; return p },
		func(p []byte) []byte { p[1] = 2; return p },
		func(p []byte) []byte { p[2] = 2; return p },
		func(p []byte) []byte { p[3] = 2; return p },
		func(p []byte) []byte { wle.PutUint32(p[24:], 241); return p },
		func(p []byte) []byte { return append(p, 0xff) },
	} {
		h = videoShow(t)
		h.send(svcVideo, kVideoStatus, bad(h.videoStatusPayload(true, false, 1920, 1080, 30, 0)))
		h.expectVideoFault("invalid video.status")
	}

	h = videoShow(t)
	h.v.streamRefused = true
	h.videoStatus(true, true, 3840, 2160, 30, 0)
	h.expectVideoFault("the KVM cannot show 3840x2160 at 30 fps")

	h = videoShow(t)
	h.s.DisplayStatus(true, false)
	h.expectVideoFault("the KVM display could not be presented")

	h = videoShow(t)
	h.send(svcVideo, kVideoConfig, make([]byte, 20))
	h.expectVideoFault("a KVM-to-sender video message from the sender")

	// The session ends: back to hardware capture.
	h = videoShow(t)
	h.s.End("test")
	if h.source() != SourceSwitchingToPhysical || h.v.restores != 1 || h.v.stopped != 1 {
		t.Fatalf("session end: source %v restores %d stopped %d", h.source(), h.v.restores, h.v.stopped)
	}
	// The display could not be presented at all.
	h = videoOpen(t, settings1080p())
	h.v.presentFails = true
	h.registerVideo(1920, 1080, 60)
	if st := h.s.State(); st.Source != SourcePhysical || st.VideoError != "the KVM display could not be presented" ||
		h.s.VideoCounters.SwitchFailures != 1 {
		t.Fatalf("present failed: %+v", st)
	}
	h.expectVideoConfig(false, 1920, 1080, videoKbps1080p, 60)
}

// A faulted video service ignores everything but video.capabilities.
func TestVideoFaultIgnoresMessages(t *testing.T) {
	h := videoShow(t)
	h.sendFrame(1033333, 4, false, false, 100)
	h.expectVideoFault("a video.frame with unknown flags")
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	h.send(svcVideo, kVideoConfig, make([]byte, 20))
	h.sendFrame(1066666, flagKey|flagSets, true, true, 100)
	if h.s.Counters.ServiceFaults != 1 || len(h.v.frames) != 1 || h.v.streams != 1 {
		t.Fatalf("faults %d frames %d streams %d", h.s.Counters.ServiceFaults, len(h.v.frames), h.v.streams)
	}
	h.expectNone()
}

// Settings while showing: a new size presents the display again, as a
// monitor that the target has not seen, and video.config follows once it
// is up; other changes only send video.config.
func TestVideoSettingsWhileShowing(t *testing.T) {
	h := videoShow(t)
	settings := settings1080p()
	settings.Layout = LayoutMirror
	settings.TargetKbps = 20000
	h.s.ApplySettings(settings)
	h.expectVideoConfig(true, 1920, 1080, videoKbpsMax, 60)
	if h.v.presents != 1 {
		t.Fatalf("presents %d", h.v.presents)
	}
	settings.MaximumFPS = 90
	settings.TargetKbps = 3000
	h.s.ApplySettings(settings)
	h.expectVideoConfig(true, 1920, 1080, 3000, 60)
	settings.MaximumFPS = 0
	settings.Width, settings.Height = 1280, 720
	settings.TargetKbps = 0
	h.s.ApplySettings(settings)
	if h.v.presents != 2 || h.v.presentWidth != 1280 || h.v.serials[1] == h.v.serials[0] {
		t.Fatalf("resize: presents %d at %d, serials %x", h.v.presents, h.v.presentWidth, h.v.serials)
	}
	// The sender hears of it once the new display is up.
	h.s.DisplayStatus(false, true)
	h.expectNone()
	h.s.DisplayStatus(true, true)
	h.expectVideoConfig(true, 1280, 720, videoKbps720p, 60)
	h.s.DisplayStatus(true, true)
	h.expectNone()
	if h.source() != SourceGuest {
		t.Fatalf("source %v", h.source())
	}
	// Settings video does not use: no message.
	settings.ClipboardEnabled = false
	h.s.ApplySettings(settings)
	h.expectNone()
	settings.Width, settings.Height = 3840, 2400
	h.s.ApplySettings(settings)
	if st := h.s.State(); st.VideoError != "the KVM display size is above the sender's video.capabilities" || st.VideoRegistered {
		t.Fatalf("above capabilities: %+v", st)
	}
	// A display that cannot be presented at the new size faults video.
	h = videoShow(t)
	h.v.presentFails = true
	settings = settings1080p()
	settings.Width, settings.Height = 1280, 720
	h.s.ApplySettings(settings)
	if h.s.State().VideoError != "the KVM display could not be presented" || h.s.State().VideoRegistered {
		t.Fatalf("present failed: %+v", h.s.State())
	}
}

// A sender that says it sets the display's mode gets one monitor per KVM
// and size: the start of the KVM's key and the height, at each
// presentation.
func TestVideoDisplaySerial(t *testing.T) {
	h := newHarness(t, hooksVideo, settings1080p())
	h.openSession()
	h.videoFlags = VideoCodecFlagH264 | 4
	h.registerVideo(3840, 2160, 60)
	h.s.DisplayStatus(true, true)
	h.expectVideoConfig(true, 1920, 1080, videoKbps1080p, 60)
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	settings := settings1080p()
	settings.Width, settings.Height = 1280, 720
	h.s.ApplySettings(settings)
	h.s.DisplayStatus(true, true)
	h.s.ApplySettings(settings1080p())
	kvm := wle.Uint32(h.f.pub) &^ 0xfff
	if h.v.presents != 3 || h.v.serials[0] != kvm|1080 || h.v.serials[1] != kvm|720 || h.v.serials[2] != kvm|1080 {
		t.Fatalf("serials %x, want %x", h.v.serials, kvm)
	}
}

// A platform without the gap, keyframe and network hooks shows video: no
// keyframe requests, no adaptation, and a dropped frame is only counted.
func TestVideoWithoutOptionalHooks(t *testing.T) {
	h := newHarness(t, hooksVideoBare, settings1080p())
	h.openSession()
	h.registerVideo(1920, 1080, 60)
	h.s.DisplayStatus(true, true)
	h.expectVideoConfig(true, 1920, 1080, videoKbps1080p, 60)
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	h.sendFrame(1000000, flagKey|flagSets, true, true, 100)
	h.v.keyframePending = 5
	h.videoWait(5000)
	h.expectNone()
	h.v.capacity = 10
	h.videoStatus(true, false, 1920, 1080, 25, 0)
	h.sendFrame(1040000, 0, false, false, 100)
	if len(h.v.frames) != 1 || h.s.VideoCounters.FramesDropped != 1 || h.v.gaps != 0 || h.v.frames[0].frame.KeyframeGeneration != 0 {
		t.Fatalf("frames %d dropped %d", len(h.v.frames), h.s.VideoCounters.FramesDropped)
	}
	if !h.s.Open() || !h.s.State().VideoRegistered {
		t.Fatalf("open %v", h.s.Open())
	}
}

// Video turned off before the display is presented: hardware capture
// returns at once, and the registration is answered with enabled 0 once the
// display is back.
func TestVideoOffBeforeTheDisplayIsPresented(t *testing.T) {
	h := videoOpen(t, settings1080p())
	h.registerVideo(1920, 1080, 60)
	off := settings1080p()
	off.VideoEnabled = false
	h.s.ApplySettings(off)
	if h.source() != SourceSwitchingToPhysical || h.v.restores != 1 {
		t.Fatalf("source %v restores %d", h.source(), h.v.restores)
	}
	h.expectNone()
	h.s.DisplayStatus(false, false)
	h.expectVideoConfig(false, 1920, 1080, videoKbps1080p, 60)
	if h.source() != SourcePhysical || h.v.presents != 1 {
		t.Fatalf("source %v presents %d", h.source(), h.v.presents)
	}
}

// A repeated video.capabilities registers again and is answered: with
// enabled 1 while service video shows, after the display while it is being
// presented, and once hardware capture is back while it returns.
func TestVideoRegistrationAgain(t *testing.T) {
	h := videoShow(t)
	h.registerVideo(3840, 2160, 60)
	h.expectVideoConfig(true, 1920, 1080, videoKbps1080p, 60)
	if h.source() != SourceGuest || h.v.presents != 1 || h.v.streams != 1 {
		t.Fatalf("source %v presents %d streams %d", h.source(), h.v.presents, h.v.streams)
	}

	h = videoOpen(t, settings1080p())
	h.registerVideo(1920, 1080, 60)
	h.registerVideo(1920, 1080, 60)
	h.expectNone()
	h.s.DisplayStatus(true, true)
	h.expectVideoConfig(true, 1920, 1080, videoKbps1080p, 60)
	h.expectNone()
	if h.v.presents != 1 {
		t.Fatalf("presents %d", h.v.presents)
	}

	// After a fault: the switch starts again once the display is back.
	h = videoShow(t)
	h.sendFrame(1033333, 4, false, false, 10)
	h.expectVideoFault("a video.frame with unknown flags")
	h.registerVideo(1920, 1080, 60)
	h.expectNone()
	if h.v.presents != 1 {
		t.Fatalf("presented while the display returns")
	}
	h.s.DisplayStatus(false, false)
	if h.v.presents != 2 || h.source() != SourceSwitchingToGuest {
		t.Fatalf("presents %d source %v", h.v.presents, h.source())
	}
	// With video off, the answer comes at once.
	h = videoShow(t)
	h.sendFrame(1033333, 4, false, false, 10)
	h.expectVideoFault("a video.frame with unknown flags")
	off := settings1080p()
	off.VideoEnabled = false
	h.s.ApplySettings(off)
	h.registerVideo(1920, 1080, 60)
	h.expectVideoConfig(false, 1920, 1080, videoKbps1080p, 60)
}

// The KVM ignores a status while video is not registered.
func TestVideoStatusBeforeRegistration(t *testing.T) {
	h := videoOpen(t, settings1080p())
	h.videoStatus(true, true, 1920, 1080, 30, 0)
	h.send(svcVideo, kVideoStatus, []byte{9})
	if h.s.Counters.ServiceFaults != 0 || h.v.streams != 0 || !h.s.Open() {
		t.Fatalf("faults %d streams %d", h.s.Counters.ServiceFaults, h.v.streams)
	}
	h.expectNone()
}

// Parameter sets above 1024 bytes are not kept: a later keyframe without
// them gets nothing.
func TestVideoParameterSetsAboveTheCache(t *testing.T) {
	h := videoShow(t)
	sps := cat([]byte{0, 0, 0, 1, 0x67}, bytes.Repeat([]byte{0x42}, 1100))
	unit := cat(sps, testPPS, makeUnit(true, false, 10, 1))
	if !h.sendUnit(1033333, flagKey|flagSets, unit) || len(h.s.parameterSets) != 0 {
		t.Fatalf("cache of %d bytes", len(h.s.parameterSets))
	}
	h.sendFrame(1066666, flagKey, true, false, 10)
	if len(h.v.frames) != 3 || h.v.frames[2].frame.ParameterSetsAdded {
		t.Fatalf("parameter sets added from an empty cache")
	}
}

// A video fault whose video.config cannot be written ends the session, and
// the frame's bytes are not read.
func TestVideoFaultOnABusyEndpoint(t *testing.T) {
	h := videoShow(t)
	h.f.failWrites = true
	frame := wireMsg(svcVideo, kVideoFrame, le64(1033333), le32(0))
	if n := h.s.Receive(cat(frame, wireMsg(svcSession, kPing))); n != specEnvelope || h.s.Open() {
		t.Fatalf("Receive took %d, open %v", n, h.s.Open())
	}
	if h.f.endReason != "the IN endpoint did not drain" || h.v.restores != 1 {
		t.Fatalf("reason %q restores %d", h.f.endReason, h.v.restores)
	}
}

// A sender that sets the display's mode needs the KVM's key for the
// display's serial; without it the display is not presented.
func TestVideoDisplaySerialWithoutAKey(t *testing.T) {
	f := newFakePlatform(t)
	v := newFakeVideo(t, f)
	h := &harness{t: t, f: f, v: v, videoFlags: VideoCodecFlagH264 | 4}
	h.s = New(struct {
		noKeyLater
		*fakeVideo
	}{noKeyLater{f}, v}, settings1080p())
	h.openSession()
	f.keyGone = true
	h.registerVideo(1920, 1080, 60)
	if v.presents != 0 || h.s.State().VideoError != "the KVM display could not be presented" {
		t.Fatalf("presents %d error %q", v.presents, h.s.State().VideoError)
	}
	h.expectVideoConfig(false, 1920, 1080, videoKbps1080p, 60)
}

// noKeyLater loses the KVM's key once keyGone is set.
type noKeyLater struct{ *fakePlatform }

func (p noKeyLater) PublicKey() ([]byte, bool) {
	if p.keyGone {
		return nil, false
	}
	return p.fakePlatform.PublicKey()
}
