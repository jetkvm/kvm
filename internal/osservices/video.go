package osservices

import "fmt"

// --- Service video ----------------------------------------------------------

func (s *Session) videoPlatform() bool { return s.video != nil }

func (s *Session) videoSettingsFit() bool {
	return s.settings.Width <= s.videoMaxWidth && s.settings.Height <= s.videoMaxHeight
}

// videoCodecOffered: the sender set the codec_flags bit of the codec that
// the browser needs.
func (s *Session) videoCodecOffered() bool {
	return s.videoCodecFlags&s.settings.Codec.flag() != 0
}

func (s *Session) setSource(source VideoSource) {
	s.videoSource = source
	s.videoSourceMS = s.nowMS()
	s.notifyState()
}

// recoveryAllow takes a switch from the retry budget, or refuses it and
// blocks automatic switches until the user turns video off and on.
func (r *videoRecovery) allow(now int64) bool {
	kept := r.startsMS[:0]
	for _, start := range r.startsMS {
		if now-start < videoRetryWindowMS {
			kept = append(kept, start)
		}
	}
	r.startsMS = kept
	r.blocked = r.blocked || len(kept) >= videoRetrySwitches
	if !r.blocked {
		r.startsMS = append(r.startsMS, now)
	}
	return !r.blocked
}

// videoRefundSwitch takes the current switch back out of the retry budget,
// once: its end was no failure of the switch.
func (s *Session) videoRefundSwitch() {
	r := &s.recovery
	if !s.videoSwitchRefunded && !r.blocked && len(r.startsMS) > 0 {
		r.startsMS = r.startsMS[:len(r.startsMS)-1]
	}
	s.videoSwitchRefunded = true
}

// giveBackKeyframe gives the platform's keyframe request back: answered, or
// to be asked again.
func (s *Session) giveBackKeyframe(delivered bool) {
	if s.videoKeyframeGen != 0 {
		s.keyframe.KeyframeDone(s.videoKeyframeGen, delivered)
		s.videoKeyframeGen = 0
	}
}

// takeKeyframeRequest takes the platform's keyframe request unless one is
// held; true while one is held.
func (s *Session) takeKeyframeRequest(now int64) bool {
	if s.videoKeyframeGen == 0 && s.keyframe != nil {
		s.videoKeyframeGen = s.keyframe.KeyframeWanted()
		s.videoKeyframeTaken = now
	}
	return s.videoKeyframeGen != 0
}

// videoStopStream: frames stop; the platform's stream ends if the KVM owned
// it.
func (s *Session) videoStopStream() {
	s.frameRelease()
	s.giveBackKeyframe(false)
	s.videoStreamStarted = false
	s.videoResetPending = false
	if s.videoSource == SourceSwitchingToGuest || s.videoSource == SourceGuest {
		s.video.VideoStopped()
	}
}

// videoRestore returns to hardware capture now (Switch to hardware capture).
func (s *Session) videoRestore() {
	if s.videoSource != SourceSwitchingToGuest && s.videoSource != SourceGuest && !s.videoDisablePending {
		return
	}
	s.videoStopStream()
	s.videoDisablePending = false
	s.videoRestoring = true
	s.video.DisplayRestore()
	s.setSource(SourceSwitchingToPhysical)
}

// videoDisable: the KVM stops service video with video.config enabled 0,
// and its display stays until the sender reports video disabled.
func (s *Session) videoDisable() {
	if !s.videoSenderEnabled {
		s.videoRestore()
		return
	}
	s.videoStopStream()
	s.videoDisablePending = true
	s.setSource(SourceSwitchingToPhysical)
	s.sendVideoConfig(false)
}

// videoDisplaySerial is the EDID serial of the KVM display, not zero.
//
// For a sender that sets the display's mode it is the start of the KVM's key
// with the height in its lowest 12 bits, the same at every presentation: the
// target knows one monitor per KVM and size again, and the sender gives it
// the settings' size.
//
// For any other sender it is new at every presentation: a target adopts the
// preferred timing of a monitor it has not seen, which is the settings'
// size.
func (s *Session) videoDisplaySerial() (uint32, bool) {
	if s.videoCodecFlags&videoFlagSetsDisplay != 0 {
		key, ok := s.p.PublicKey()
		if !ok || len(key) < 4 {
			return 0, false
		}
		return le.Uint32(key)&^0xFFF | s.settings.Height&0xFFF, true
	}
	var serial [4]byte
	for le.Uint32(serial[:]) == 0 {
		s.p.FillRandom(serial[:])
	}
	return le.Uint32(serial[:]), true
}

// videoPresent presents the KVM display at the settings' size.
func (s *Session) videoPresent() bool {
	serial, ok := s.videoDisplaySerial()
	if !ok {
		return false
	}
	return s.video.DisplayPresent(s.settings.Width, s.settings.Height, serial)
}

// videoStartSwitch switches to service video: stop hardware capture and
// present the display; video.config with enabled 1 follows once it is
// presented.
func (s *Session) videoStartSwitch() {
	refusal := videoBlocked
	if s.recovery.allow(s.nowMS()) {
		s.videoSwitchRefunded = false
		s.VideoCounters.Switches++
		// Each switch starts at the static targets.
		kbps, _ := videoTargets(&s.settings)
		s.videoRateReset(kbps, s.nowMS())
		if s.videoPresent() {
			s.videoRestoring = false
			s.videoConfigEnabled = false
			s.setSource(SourceSwitchingToGuest)
			return
		}
		s.VideoCounters.SwitchFailures++
		refusal = videoNotPresented
	}
	s.videoError = refusal
	if s.videoAnswerPending {
		s.sendVideoConfig(false)
	}
	s.notifyState()
}

// videoUpdate acts on a registration or the user's wish, given the source.
func (s *Session) videoUpdate() {
	if s.link != linkOpen || !s.videoRegistered {
		s.videoSwitchWanted = false
		return
	}
	if s.videoSwitchWanted && s.settings.VideoEnabled && s.videoPlatform() && !s.videoCodecOffered() {
		s.videoError = fmt.Sprintf("the browser uses %s, which the sender does not offer", s.settings.Codec)
	}
	s.videoSwitchWanted = s.videoSwitchWanted && s.settings.VideoEnabled && s.videoPlatform() && s.videoCodecOffered()
	switch s.videoSource {
	case SourcePhysical:
		if s.videoSwitchWanted {
			s.videoSwitchWanted = false
			s.videoStartSwitch()
		} else if s.videoAnswerPending {
			s.sendVideoConfig(false)
		}
	case SourceSwitchingToPhysical:
		// A wanted switch starts, and answers, once the display is back.
		if !s.videoSwitchWanted && s.videoAnswerPending {
			s.sendVideoConfig(false)
		}
	default:
		// Before the display is presented, the answer waits for it.
		s.videoSwitchWanted = false
		if s.videoAnswerPending && s.videoConfigEnabled {
			s.sendVideoConfig(true)
		}
	}
}

// DisplayStatus is the KVM display as the platform reports it, at least
// every tick while the source is switching: presented once DisplayPresent's
// EDID is on the target and its hotplug has completed; owned from
// DisplayPresent until DisplayRestore has completed. Owned turning false on
// its own means the display was lost.
func (s *Session) DisplayStatus(presented, owned bool) {
	switch s.videoSource {
	case SourceSwitchingToPhysical:
		if s.videoRestoring && !owned {
			s.videoRestoring = false
			s.setSource(SourcePhysical)
			s.videoUpdate()
		}
	case SourceSwitchingToGuest, SourceGuest:
		if !owned {
			s.faultService(ServiceVideo, videoNotPresented)
		} else if presented && !s.videoConfigEnabled {
			s.sendVideoConfig(true)
		}
	}
}

// DisplayHeld reports, every tick, whether the platform holds display
// handovers. A registration is answered with video disabled meanwhile, and a
// switch's timeout starts again when the hold ends.
func (s *Session) DisplayHeld(held bool) {
	if s.videoDisplayHeld && !held {
		s.videoSourceMS = s.nowMS()
	}
	s.videoDisplayHeld = held
	if held && s.videoAnswerPending {
		s.sendVideoConfig(false)
	}
}

// Source is the source the KVM shows.
func (s *Session) Source() VideoSource { return s.videoSource }

func (s *Session) handleVideoCapabilities(payload []byte) {
	if len(payload) != videoCapabilitiesLen {
		s.faultService(ServiceVideo, "invalid video.capabilities length")
		return
	}
	flags := le.Uint32(payload)
	width := le.Uint32(payload[4:])
	height := le.Uint32(payload[8:])
	fps := le.Uint32(payload[12:])
	withdrawal := flags|width|height|fps == 0
	const known = VideoCodecFlagH264 | VideoCodecFlagH265 | videoFlagSetsDisplay
	if !withdrawal && (flags&VideoCodecFlagH264 == 0 || flags&^uint32(known) != 0 ||
		width == 0 || width > MaxDimension || height == 0 || height > MaxDimension || fps == 0 || fps > MaxFPS) {
		s.faultService(ServiceVideo, "invalid video.capabilities")
		return
	}
	if withdrawal && s.videoSenderEnabled {
		s.faultService(ServiceVideo, "video withdrawn without a disabled video.status")
		return
	}
	s.serviceFaulted[ServiceVideo] = false
	s.videoError = ""
	s.videoRegistered = !withdrawal
	s.videoCodecFlags = flags
	s.videoMaxWidth = width
	s.videoMaxHeight = height
	s.videoMaxFPS = fps
	s.videoAnswerPending = !withdrawal
	s.videoSwitchWanted = !withdrawal
	if withdrawal {
		s.videoRestore()
	} else if !s.videoSettingsFit() {
		s.faultService(ServiceVideo, videoTooLarge)
		return
	}
	s.videoUpdate()
	s.notifyState()
}

// enabledStatusError tells why an enabled video.status breaks the rules, or
// "".
func (s *Session) enabledStatusError(payload []byte) string {
	width := le.Uint32(payload[4:])
	height := le.Uint32(payload[8:])
	nativeFPS := le.Uint32(payload[12:])
	if width == 0 || width > MaxDimension || height == 0 || height > MaxDimension || nativeFPS == 0 || nativeFPS > MaxFPS {
		return "an enabled video.status with an invalid size or rate"
	}
	if Codec(payload[2]) == CodecH265 && s.videoCodecFlags&VideoCodecFlagH265 == 0 {
		return "video.status reports H.265, which the sender did not advertise"
	}
	if width > s.videoMaxWidth || height > s.videoMaxHeight || le.Uint32(payload[24:]) > s.videoMaxFPS {
		return "video.status exceeds the sender's video.capabilities"
	}
	if !s.videoSenderEnabled {
		return "an enabled video.status without a switch to service video"
	}
	if Codec(payload[2]) != s.settings.Codec {
		return fmt.Sprintf("video.status reports %s; the KVM asked for %s", Codec(payload[2]), s.settings.Codec)
	}
	return ""
}

// videoStartStream: a coded sequence of width x height at fps starts with
// the next frame.
func (s *Session) videoStartStream(width, height, fps uint32, codec Codec, reset bool) {
	s.giveBackKeyframe(false)
	capacity := s.video.VideoStream(width, height, fps, codec)
	if capacity == 0 {
		s.faultService(ServiceVideo, fmt.Sprintf("the KVM cannot show %dx%d at %d fps", width, height, fps))
		return
	}
	// Parameter sets of another size or codec never describe this stream.
	if reset || width != s.videoWidth || height != s.videoHeight || codec != s.videoCodec {
		s.parameterSets = s.parameterSets[:0]
	}
	s.videoWidth = width
	s.videoHeight = height
	s.videoFPS = fps
	s.videoCodec = codec
	s.videoFrameCapacity = capacity
	s.videoStreamStarted = true
	s.videoPTSRestart = true
	s.videoSentCaptureOK = false
	// The new stream's first keyframe crosses before the targets change.
	s.rate.hold(s.nowMS())
	s.videoIntervalRestart(s.nowMS())
	if reset {
		s.videoResetPending = true
		s.VideoCounters.StreamResets++
	}
	s.notifyState()
}

func (s *Session) handleEnabledStatus(payload []byte) {
	if err := s.enabledStatusError(payload); err != "" {
		s.faultService(ServiceVideo, err)
		return
	}
	if s.videoSource == SourceSwitchingToGuest && s.videoConfigEnabled {
		s.setSource(SourceGuest)
	} else if s.videoSource != SourceGuest {
		// Stale: the KVM has asked the sender to stop.
		return
	}
	width := le.Uint32(payload[4:])
	height := le.Uint32(payload[8:])
	nativeFPS := le.Uint32(payload[12:])
	maximumFPS := le.Uint32(payload[24:])
	codec := Codec(payload[2])
	s.videoSenderKbps = le.Uint32(payload[20:])
	fps := nativeFPS
	if maximumFPS != 0 {
		fps = min(nativeFPS, maximumFPS)
	}
	if payload[3] == 1 || !s.videoStreamStarted || width != s.videoWidth || height != s.videoHeight ||
		fps != s.videoFPS || codec != s.videoCodec {
		s.videoStartStream(width, height, fps, codec, payload[3] == 1)
	}
}

func (s *Session) handleDisabledStatus(payload []byte) {
	s.videoSenderEnabled = false
	if !s.videoDisablePending && s.videoSource != SourceGuest &&
		!(s.videoSource == SourceSwitchingToGuest && s.videoConfigEnabled) {
		return
	}
	if le.Uint32(payload[28:]) == videoErrorDisplayAsleep {
		// An inactive display does not count against the budget.
		s.videoRefundSwitch()
	}
	s.videoRestore()
}

func (s *Session) handleVideoStatus(payload []byte) {
	if !s.videoRegistered {
		return
	}
	if len(payload) < videoStatusMinLen || payload[0] > 1 || payload[1] > 1 || payload[2] > 1 || payload[3] > 1 ||
		le.Uint32(payload[24:]) > MaxFPS || !UTF8Valid(payload[32:]) {
		s.faultService(ServiceVideo, "invalid video.status")
	} else if payload[0] == 1 {
		s.handleEnabledStatus(payload)
	} else {
		s.handleDisabledStatus(payload)
	}
}

func (s *Session) handleVideo(payload []byte) {
	switch s.kind {
	case videoCapabilities:
		s.handleVideoCapabilities(payload)
	case videoStatus:
		s.handleVideoStatus(payload)
	case videoConfig, videoRequestKeyframe:
		s.faultService(ServiceVideo, "a KVM-to-sender video message from the sender")
	default:
		s.Counters.MessagesSkipped++
	}
}

// videoApplySettings: the user's video settings changed (Switch to service
// video).
func (s *Session) videoApplySettings(previous *Settings) {
	now := &s.settings
	resized := previous.Width != now.Width || previous.Height != now.Height
	codecChanged := previous.Codec != now.Codec
	if !s.videoRegistered ||
		(previous.VideoEnabled == now.VideoEnabled && !resized && previous.Layout == now.Layout &&
			previous.TargetKbps == now.TargetKbps && previous.MaximumFPS == now.MaximumFPS && !codecChanged) {
		return
	}
	if !s.videoSettingsFit() {
		s.faultService(ServiceVideo, videoTooLarge)
		return
	}
	// The adapted targets belong to the previous settings.
	s.rate.ceilingKbps = 0
	showing := s.videoSource == SourceSwitchingToGuest || s.videoSource == SourceGuest
	switch {
	case !showing:
		s.videoSwitchWanted = now.VideoEnabled
		if !now.VideoEnabled {
			s.sendVideoConfig(false)
		}
		s.videoUpdate()
	case !now.VideoEnabled:
		s.videoDisable()
	case codecChanged && !s.videoCodecOffered():
		s.videoDisable()
		s.videoError = fmt.Sprintf("the browser uses %s, which the sender does not offer", now.Codec)
	case resized:
		// The sender hears of the new size once the display is up.
		s.videoConfigEnabled = false
		if !s.videoPresent() {
			s.faultService(ServiceVideo, videoNotPresented)
		}
	case s.videoConfigEnabled:
		s.sendVideoConfig(true)
	}
}

// videoTargets are the static targets: the settings' own, else a bitrate by
// display height, within the hardware path's ceilings.
func videoTargets(settings *Settings) (kbps, fps uint32) {
	switch {
	case settings.Height <= 720:
		kbps = videoKbps720p
	case settings.Height <= 1080:
		kbps = videoKbps1080p
	default:
		kbps = videoKbps1440p
	}
	if settings.TargetKbps != 0 {
		kbps = min(settings.TargetKbps, videoKbpsMax)
	}
	fps = videoFPSMax
	if settings.MaximumFPS != 0 {
		fps = min(settings.MaximumFPS, videoFPSMax)
	}
	return kbps, fps
}

// videoIntervalRestart: slot waits count from now.
func (s *Session) videoIntervalRestart(now int64) {
	s.videoIntervalMS = now
	s.videoIntervalWaitMS = 0
	s.videoIntervalFrames = 0
	s.videoFreeMFPS = 0
	s.videoPressureIntervals = 0
}

func (s *Session) videoRateReset(ceilingKbps uint32, now int64) {
	s.rate.reset(ceilingKbps, now)
	s.videoIntervalRestart(now)
}

// sendVideoConfig: the video settings the sender gets: the KVM display size,
// layout and codec, and the static targets with the frame-rate cap clamped
// to what the sender advertised, or the targets adapted below them for
// these settings.
func (s *Session) sendVideoConfig(enabled bool) bool {
	targetKbps, maximumFPS := videoTargets(&s.settings)
	if s.videoMaxFPS != 0 {
		maximumFPS = min(maximumFPS, s.videoMaxFPS)
	}
	if s.settings.TargetKbps == 0 && s.rate.ceilingKbps == targetKbps {
		targetKbps = s.rate.kbps
		maximumFPS = s.rate.fps(maximumFPS)
	}
	payload := make([]byte, videoConfigLen)
	if enabled {
		payload[0] = 1
	}
	payload[1] = byte(s.settings.Layout)
	payload[2] = byte(s.settings.Codec)
	le.PutUint32(payload[4:], s.settings.Width)
	le.PutUint32(payload[8:], s.settings.Height)
	le.PutUint32(payload[12:], targetKbps)
	le.PutUint32(payload[16:], maximumFPS)
	s.videoAnswerPending = false
	s.videoConfigEnabled = enabled
	s.videoSenderEnabled = s.videoSenderEnabled || enabled
	return s.sendMessage(ServiceVideo, videoConfig, payload)
}

// --- Video frames -----------------------------------------------------------

func (s *Session) videoAccepting() bool {
	return s.link == linkOpen && s.videoRegistered && s.videoSource == SourceGuest && s.videoStreamStarted
}

// frameRelease leaves the frame being read: its slot goes back and its rest
// is skipped.
func (s *Session) frameRelease() {
	if s.frameSlot != nil {
		s.video.FrameCancel(s.frameFill)
		s.frameSlot = nil
	}
	s.frameWaiting = false
	switch s.parse {
	case parseFrameHead, parseFrameWait, parseFrameData, parseFrameSets:
		s.parse = parseSkip
	}
}

// frameDrop: a frame is lost before the platform got it; the viewer needs a
// keyframe.
func (s *Session) frameDrop() {
	s.VideoCounters.FramesDropped++
	s.frameRelease()
	if s.gap != nil {
		s.gap.VideoGap()
	}
}

func (s *Session) nowUS() int64 {
	if s.video != nil {
		return s.video.NowUS()
	}
	return s.nowMS() * 1000
}

// beginFrame handles a video.frame envelope: true when the frame is read
// into a slot, false when it is skipped.
func (s *Session) beginFrame() bool {
	if !s.videoAccepting() {
		s.VideoCounters.FramesDiscarded++
		s.videoSentCaptureOK = false
		return false
	}
	if s.payloadSize <= videoFrameHeaderLen {
		s.faultService(ServiceVideo, "a video.frame without an access unit")
		return false
	}
	s.frameArrivalUS = s.nowUS()
	s.frameHeadFill = 0
	s.parse = parseFrameHead
	return true
}

// frameHeadError tells why a frame's head breaks the rules (video.frame,
// Stream boundaries and keyframes), or "".
func (s *Session) frameHeadError() string {
	if s.frameFlags&^uint32(frameKeyframe|frameParameterSets) != 0 {
		return "a video.frame with unknown flags"
	}
	if s.videoResetPending && s.frameFlags != frameKeyframe|frameParameterSets {
		return "the first frame of a stream is not a keyframe with parameter sets"
	}
	return ""
}

// countSenderSkips runs at the frame's head, which every frame the sender
// sent passes, also one the KVM then drops.
func (s *Session) countSenderSkips() {
	frameUS := uint64(1000000 / s.videoFPS)
	delta := s.frameCaptureUS - s.videoSentCaptureUS
	if s.videoSentCaptureOK && s.frameCaptureUS > s.videoSentCaptureUS && delta <= 1000000 {
		periods := (delta + frameUS/2) / frameUS
		if periods > 1 {
			s.VideoCounters.SenderSkippedFrames += uint32(periods - 1)
		}
	}
	s.videoSentCaptureUS = s.frameCaptureUS
	s.videoSentCaptureOK = true
}

func (s *Session) frameHeadDone() {
	s.frameCaptureUS = le.Uint64(s.frameHead[:])
	s.frameFlags = le.Uint32(s.frameHead[8:])
	s.frameUnitLength = int(s.payloadSize) - videoFrameHeaderLen
	if err := s.frameHeadError(); err != "" {
		s.faultService(ServiceVideo, err)
		return
	}
	s.countSenderSkips()
	// A keyframe without its parameter sets gets the stream's.
	s.framePrefix = 0
	if s.frameFlags == frameKeyframe {
		s.framePrefix = len(s.parameterSets)
	}
	// A keyframe that cannot fit a slot would come back on every request.
	if s.frameFlags&frameKeyframe != 0 && s.framePrefix+s.frameUnitLength > s.videoFrameCapacity {
		s.faultService(ServiceVideo, fmt.Sprintf("a keyframe above %d bytes", s.videoFrameCapacity))
		return
	}
	// The sender started the stream as the rule says, also when no viewer
	// takes this frame or it is dropped.
	s.videoResetPending = false
	s.frameFill = 0
	s.parse = parseFrameWait
}

// intervalWait is how long the frame has waited for a slot in the current
// network interval.
func (s *Session) intervalWait(now int64) uint32 {
	from := max(s.frameWaitMS, s.videoIntervalMS)
	if s.frameWaiting && now > from {
		return uint32(now - from)
	}
	return 0
}

func (s *Session) noteSlotWait(now int64) {
	waited := uint32(0)
	if s.frameWaiting {
		waited = uint32(now - s.frameWaitMS)
	}
	s.videoIntervalWaitMS += s.intervalWait(now)
	s.VideoCounters.SlotWaitMS += waited
	s.VideoCounters.SlotWaitMaxMS = max(s.VideoCounters.SlotWaitMaxMS, waited)
	s.frameWaiting = false
}

// frameTryReserve reserves the frame's slot. False while the frame still
// waits for one.
func (s *Session) frameTryReserve() bool {
	busy := false
	fits := s.frameUnitLength <= s.videoFrameCapacity
	s.frameSlot = nil
	if fits {
		s.frameSlot, busy = s.video.FrameReserve(s.framePrefix + s.frameUnitLength)
	}
	now := s.nowMS()
	switch {
	case !fits:
		s.frameDrop()
	case s.frameSlot != nil:
		s.noteSlotWait(now)
		copy(s.frameSlot, s.parameterSets[:s.framePrefix])
		s.frameFill = s.framePrefix
		s.parse = parseFrameData
	case !busy:
		// No viewer takes video.
		s.VideoCounters.FramesDiscarded++
		s.frameRelease()
	case !s.frameWaiting:
		s.frameWaiting = true
		s.frameWaitMS = now
		s.VideoCounters.SlotWaits++
		return false
	case now-s.frameWaitMS < videoSlotWaitMS:
		return false
	default:
		s.noteSlotWait(now)
		s.frameDrop()
	}
	if s.parse == parseSkip && s.frameFlags&frameParameterSets != 0 {
		// Later keyframes without parameter sets still need this frame's.
		s.parse = parseFrameSets
	}
	return true
}

// startCodeAt finds the start of the next Annex-B start code at or after
// from (its leading zero byte included), with the NAL unit after it; len
// when there is none.
func startCodeAt(data []byte, from int) (start, nal int) {
	for i := from; i+3 <= len(data); i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			if i > from && data[i-1] == 0 {
				return i - 1, i + 3
			}
			return i, i + 3
		}
	}
	return len(data), len(data)
}

// extractParameterSets returns the parameter sets ahead of the first slice
// of an access unit, each behind a 4-byte start code. complete tells
// whether there are all of the codec's and a slice follows them, so none is
// cut off when only the start of the unit was read.
func extractParameterSets(codec Codec, unit []byte) (sets []byte, complete bool) {
	var seen [4]bool
	_, nal := startCodeAt(unit, 0)
	for nal < len(unit) {
		if vcl, _ := nalVCL(codec, unit[nal]); vcl {
			break
		}
		kind := parameterSetKind(codec, unit[nal])
		end, next := startCodeAt(unit, nal+1)
		if kind != 0 {
			sets = append(sets, 0, 0, 0, 1)
			sets = append(sets, unit[nal:end]...)
			seen[kind] = true
		}
		nal = next
	}
	complete = seen[1] && seen[2] && (codec != CodecH265 || seen[3]) && nal < len(unit)
	return sets, complete
}

// keepParameterSets keeps the unit's parameter sets for later keyframes;
// true when it has all of them.
func (s *Session) keepParameterSets(unit []byte) bool {
	sets, complete := extractParameterSets(s.videoCodec, unit)
	if complete {
		if len(sets) <= videoParameterSetsMax {
			s.parameterSets = append(s.parameterSets[:0], sets...)
		} else {
			s.parameterSets = s.parameterSets[:0]
		}
	}
	return complete
}

// readFrameSets reads a frame no slot takes into the payload buffer, as far
// as it holds, and keeps its parameter sets; the rest is skipped. It returns
// the bytes used.
func (s *Session) readFrameSets(data []byte) int {
	fill := int(s.payloadFill) - videoFrameHeaderLen
	size := min(s.frameUnitLength, len(s.payload))
	take := min(size-fill, len(data))
	copy(s.payload[fill:], data[:take])
	s.payloadFill += uint32(take)
	s.Counters.BytesDiscarded += uint64(take)
	if fill+take == size {
		s.keepParameterSets(s.payload[:size])
		s.parse = parseSkip
		if s.payloadFill == s.payloadSize {
			s.skipDone()
		}
	}
	return take
}

// frameUnitError tells why a complete access unit breaks the rules, or "".
func (s *Session) frameUnitError(unit []byte) string {
	if AnnexBIsKeyframe(s.videoCodec, unit) != (s.frameFlags&frameKeyframe != 0) {
		return "the video.frame keyframe flag does not match its access unit"
	}
	if s.frameFlags&frameParameterSets != 0 && !s.keepParameterSets(unit) {
		return "a video.frame flagged with parameter sets has none"
	}
	return ""
}

// framePTS is the frame's presentation time: the sender's capture spacing
// when it is 1 us to 1 s, else one frame at the stream's rate. A stream
// starts at the KVM's clock, never before the frame that came last.
func (s *Session) framePTS() uint32 {
	frameUS := uint64(1000000 / s.videoFPS)
	delta := s.frameCaptureUS - s.videoLastCaptureUS
	if s.videoPTSRestart {
		anchor := uint64(s.frameArrivalUS)
		next := s.videoPTSUS + frameUS
		if s.videoPTSValid && next > anchor {
			s.videoPTSUS = next
		} else {
			s.videoPTSUS = anchor
		}
	} else if delta >= 1 && delta <= 1000000 {
		s.videoPTSUS += delta
	} else {
		s.videoPTSUS += frameUS
	}
	s.videoPTSRestart = false
	s.videoPTSValid = true
	s.videoLastCaptureUS = s.frameCaptureUS
	return uint32(s.videoPTSUS * 9 / 100)
}

// frameCommit: the access unit is complete in its slot. Check it and hand it
// over.
func (s *Session) frameCommit() {
	if err := s.frameUnitError(s.frameSlot[s.framePrefix:s.frameFill]); err != "" {
		s.frameRelease()
		s.faultService(ServiceVideo, err)
		return
	}
	now := s.nowMS()
	keyframe := s.frameFlags&frameKeyframe != 0
	frame := VideoFrame{
		Data:       s.frameSlot[:s.frameFill],
		PTS90k:     s.framePTS(),
		ArrivalUS:  s.frameArrivalUS,
		ReceivedUS: s.nowUS(),
		Width:      s.videoWidth,
		Height:     s.videoHeight,
		FPS:        s.videoFPS,
		Codec:      s.videoCodec,
		Keyframe:   keyframe,
		// A keyframe answers the platform's request, also one not yet asked
		// of the sender.
		ParameterSetsAdded: s.framePrefix != 0,
	}
	if keyframe && s.takeKeyframeRequest(now) {
		frame.KeyframeGeneration = s.videoKeyframeGen
	}
	s.frameSlot = nil
	s.lastMessageMS = now
	s.videoIntervalFrames++
	c := &s.VideoCounters
	c.Frames++
	c.Bytes += uint64(len(frame.Data))
	if keyframe {
		c.Keyframes++
		c.LargestKeyframe = max(c.LargestKeyframe, uint32(len(frame.Data)))
	}
	if frame.ParameterSetsAdded {
		c.ParameterSetsAdded++
	}
	delivered := s.video.FrameCommit(&frame)
	if frame.KeyframeGeneration != 0 {
		s.giveBackKeyframe(delivered)
	}
}

// requestKeyframe asks the sender for a keyframe, at most once per
// interval. A busy IN endpoint skips the request; it goes again after the
// interval.
func (s *Session) requestKeyframe(now int64) {
	if s.videoKeyframeAsked && now-s.videoKeyframeAskMS < videoKeyframeIntervalMS {
		return
	}
	s.videoKeyframeAsked = true
	s.videoKeyframeAskMS = now
	if s.writeMessage(ServiceVideo, videoRequestKeyframe, nil, nil) {
		s.VideoCounters.KeyframeRequests++
	}
}

// carriedKbps is what the KVM carries, from an interval in which frames
// waited for a slot: under what the transport sent and under the target.
func (s *Session) carriedKbps(sentKbps, mfps uint32) uint32 {
	under := uint64(min(sentKbps, s.rate.kbps)) * videoPressureSentPercent / 100
	if mfps < s.videoFreeMFPS {
		return uint32(under * uint64(mfps) / uint64(s.videoFreeMFPS))
	}
	return uint32(under)
}

// videoEstimate is the estimate for an interval that just ended: the
// receiver feedback's (with a manual video quality, the static target), or
// what the KVM carries when frames waited for a slot much of the time,
// interval after interval.
func (s *Session) videoEstimate(network *VideoNetwork, now int64) uint32 {
	waited := uint64(s.videoIntervalWaitMS + s.intervalWait(now))
	length := now - s.videoIntervalMS
	mfps := uint32(0)
	if length > 0 {
		mfps = uint32(uint64(s.videoIntervalFrames) * 1000000 / uint64(length))
	}
	s.videoIntervalMS = now
	s.videoIntervalWaitMS = 0
	s.videoIntervalFrames = 0
	pressed := length > 0 && waited*1000 >= videoPressurePermille*uint64(length)
	if pressed {
		s.videoPressureIntervals++
	} else {
		s.videoPressureIntervals = 0
	}
	if !pressed && mfps != 0 {
		s.videoFreeMFPS = mfps
	}
	var estimate uint32
	switch {
	case !network.Adaptive:
		estimate = s.rate.ceilingKbps
	case network.Cut:
		estimate = network.EstimateKbps
	default:
		estimate = max(network.EstimateKbps, s.rate.kbps)
	}
	if s.videoPressureIntervals < videoPressureIntervals {
		return estimate
	}
	return min(estimate, s.carriedKbps(network.SentKbps, mfps))
}

// videoAdapt adapts the targets: to the receiver feedback with an automatic
// video quality, and to the KVM's own slot waits with any. A fixed bitrate
// in the settings is never adapted.
func (s *Session) videoAdapt(now int64) {
	ceiling, _ := videoTargets(&s.settings)
	adapts := s.network != nil && s.settings.TargetKbps == 0
	var network VideoNetwork
	if adapts {
		network = s.network.VideoNetwork(ceiling)
	}
	r := &s.rate
	adapted := r.ceilingKbps == ceiling && (r.kbps != ceiling || r.lowFPS)
	qualityChanged := network.Adaptive != s.videoRateFollows
	s.videoRateFollows = network.Adaptive
	if !adapts || r.ceilingKbps != ceiling || (qualityChanged && adapted) {
		s.videoRateReset(ceiling, now)
		if adapted {
			s.sendVideoConfig(true)
		}
		return
	}
	if network.Interval && r.step(s.videoEstimate(&network, now), now) {
		// What the sender delivers at the new target is not known yet.
		s.videoFreeMFPS = 0
		s.sendVideoConfig(true)
	}
}

func (s *Session) videoTick(now int64) {
	elapsed := now - s.videoSourceMS
	if s.videoSource == SourceGuest && elapsed >= videoRetryShownMS {
		s.videoRefundSwitch()
	}
	switch {
	case s.videoSource == SourceSwitchingToGuest && !s.videoDisplayHeld && elapsed >= videoSwitchTimeoutMS:
		s.VideoCounters.SwitchFailures++
		s.videoError = videoNoStatus
		s.videoRestore()
		if s.videoRegistered {
			s.sendVideoConfig(false)
		}
	case s.videoDisablePending && elapsed >= videoDisableTimeoutMS:
		s.videoRestore()
	case s.videoAccepting():
		// A request not answered in time is given back, then taken and
		// asked again.
		if s.videoKeyframeGen != 0 && now-s.videoKeyframeTaken >= videoKeyframeIntervalMS {
			s.giveBackKeyframe(false)
		}
		if s.takeKeyframeRequest(now) {
			s.requestKeyframe(now)
		}
		if s.videoConfigEnabled {
			s.videoAdapt(now)
		}
	}
}
