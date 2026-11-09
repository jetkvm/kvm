package osservices

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Limits and timeouts of the session (Handshake, Liveness, services).
const (
	// The KVM keeps this many clipboard requests open at once.
	clipboardPendingMax = 4
	// The KVM abandons a clipboard request after this long.
	ClipboardTimeoutMS = 2000
	handshakeTimeoutMS = 15000
	livenessTimeoutMS  = 15000

	// Terminal sessions the KVM keeps open at once: as many as a sender runs.
	TerminalSessionsMax = 4
	// The KVM closes a terminal session that terminal.opened did not answer
	// in this long.
	terminalOpenTimeoutMS = 5000
	// The longest a terminal.output waits for room in the browser's channel,
	// with the OUT endpoint unread, before the terminal ends.
	terminalOutputHoldMS = ClipboardTimeoutMS
	// TerminalChannelLabel is the browser's terminal channel.
	TerminalChannelLabel = "guest-terminal"

	// FileDropMax is the size of the KVM's table of files in one drop.
	FileDropMax = 256
	// The KVM gives up on a drop when neither side makes progress for this
	// long.
	fileIdleTimeoutMS = 30000
	// A drop still without a browser channel this long after it was
	// accepted fails.
	fileAttachTimeoutMS = 10000
	// FileChannelPrefix and the drop ID in decimal name a drop's channel.
	FileChannelPrefix = "guestfile_"

	// The KVM gives up on an update when neither side makes progress for
	// this long.
	updateIdleTimeoutMS = 30000
	// Why an update failed, cut to 64 bytes on a UTF-8 boundary.
	updateMessageMax = 64

	// A switch to service video that gets no enabled video.status in this
	// long returns to hardware capture.
	videoSwitchTimeoutMS = 45000
	// After video.config with enabled 0, the KVM waits this long for the
	// disabled video.status before it removes its display anyway.
	videoDisableTimeoutMS = 15000
	// The retry budget: this many switches in the window, across sessions.
	videoRetrySwitches = 3
	videoRetryWindowMS = 120000
	// A switch that has shown service video this long has succeeded and
	// leaves the budget.
	videoRetryShownMS = 10000
	// At most one video.request_keyframe in this long.
	videoKeyframeIntervalMS = 600
	// A frame waits this long for an access-unit slot; then it is dropped.
	// Until then the transport holds the USB read, so the bus NAKs the
	// sender instead of losing frames.
	videoSlotWaitMS = 1000
	// SPS and PPS (and VPS) kept from the stream for keyframes that come
	// without them.
	videoParameterSetsMax = 1024
	// Static targets sent in video.config when the settings name none.
	videoKbps720p  = 5000
	videoKbps1080p = 8000
	videoKbps1440p = 12000
	// The hardware path's ceiling for any target.
	videoKbpsMax = 12000
	videoFPSMax  = 60
	// The uplink holds the stream back when frames wait for a slot this
	// share of the time (permille) in this many intervals in a row.
	videoPressurePermille    = 250
	videoPressureIntervals   = 3
	videoPressureSentPercent = 85
	videoErrorMax            = 96
)

// What the browser's terminal shows when its session ends.
const (
	terminalShellExited  = "The shell exited"
	terminalShellLost    = "The target computer lost its shell"
	terminalDisconnected = "OS Services disconnected"
	terminalDisabled     = "Terminal disabled"
	terminalUnavailable  = "no OS Services terminal is connected"
	terminalNotOpened    = "Terminal unavailable: "
	terminalNoAnswer     = terminalNotOpened + "the target computer did not answer"
	terminalBusy         = terminalNotOpened + "the target computer already runs its maximum number of terminals"
	terminalNotSent      = terminalNotOpened + "the IN endpoint did not drain"
	terminalSlowBrowser  = "Terminal closed: the browser did not keep up"
	// TerminalInputLost is the line a terminal ends with when keystrokes
	// were lost.
	TerminalInputLost = "Terminal closed: keystrokes were not delivered"
	terminalWithdrawn = "Terminal closed: the target computer withdrew its shell"
	terminalFaulted   = "Terminal faulted: "

	fileUnavailable = "no OS Services file transfer is connected"
	fileBusy        = "a file transfer is already in progress"
	fileNoFiles     = "file.drop needs at least one file"

	inEndpointStuck = "the IN endpoint did not drain"

	updateUnavailable = "no OS Services sender that installs updates is connected"
	updateBusy        = "an OS Services update is already in progress"

	videoBlocked      = "OS Services video switched too often; turn video off and on to try again"
	videoNotPresented = "the KVM display could not be presented"
	videoNoStatus     = "the sender did not start video within 45 seconds"
	videoTooLarge     = "the KVM display size is above the sender's video.capabilities"
)

// Settings are the KVM settings the session acts on.
type Settings struct {
	ClipboardEnabled bool
	TerminalEnabled  bool
	FilesEnabled     bool
	VideoEnabled     bool
	// Empty lets the sender select.
	CaptureDisplay string
	Width          uint32
	Height         uint32
	Layout         Layout
	// 0 selects the encoder default and the native rate.
	TargetKbps uint32
	MaximumFPS uint32
	// Codec is the codec the browser needs. The KVM switches to service
	// video only with a sender that offers it (Switch to service video).
	Codec Codec
}

// VideoSource is the source the KVM shows (Handover).
type VideoSource int

const (
	SourcePhysical VideoSource = iota
	SourceSwitchingToGuest
	SourceGuest
	SourceSwitchingToPhysical
)

// String is "physical", "switching_to_guest", "guest" or
// "switching_to_physical".
func (s VideoSource) String() string {
	switch s {
	case SourceSwitchingToGuest:
		return "switching_to_guest"
	case SourceGuest:
		return "guest"
	case SourceSwitchingToPhysical:
		return "switching_to_physical"
	default:
		return "physical"
	}
}

// Platform is what every session needs. Hooks must not block or call back
// into the session.
type Platform interface {
	NowMS() int64
	FillRandom(b []byte)
	// PublicKey is the KVM's Ed25519 key, made on first use and kept until
	// a factory reset. Sign signs with the same key.
	PublicKey() ([]byte, bool)
	Sign(message []byte) ([]byte, bool)
	// Write queues one message for the IN endpoint: the envelope, then head,
	// then body (either can be empty), in order and atomically, all of it
	// or none. It returns false when it does not fit; the session then
	// ends, as the KVM ends it when a write does not complete in 2 seconds.
	Write(envelope, head, body []byte) bool
	// SessionEnded: the transport drops what is queued for the IN endpoint
	// and stops reading OUT until the next RESET.
	SessionEnded(reason string)
	// ResetTransport: a RESET. The transport cancels every bulk request of
	// the previous session and, once each has completed, calls
	// TransportDrained() and reads OUT again.
	ResetTransport()
	// StateChanged: anything the browser shows changed.
	StateChanged()
	// ClipboardDone: a clipboard request finished: the sender answered, the
	// request timed out, or the session or the service went away. text is
	// only valid for the call.
	ClipboardDone(requestID uint32, status ClipboardStatus, answered bool, text []byte, truncated bool)
}

// TerminalPlatform offers terminals.
type TerminalPlatform interface {
	// TerminalOutput is shell output of a terminal session, in order; data
	// is only valid for the call. It returns false when the browser cannot
	// take it now: the session keeps the message and reads nothing more
	// until the hook takes it (Holding), and offers it again on each
	// Receive and Tick. After terminalOutputHoldMS it closes the terminal.
	TerminalOutput(terminalID uint32, data []byte) bool
	// TerminalClosed: a terminal session ended other than by
	// TerminalClose. reason is the line the browser shows ("" for none).
	TerminalClosed(terminalID uint32, reason string, exited bool)
}

// FilePlatform receives files.
type FilePlatform interface {
	// FileCredit: the sender accepts bytes more of a transfer. Held until
	// the drop's channel is attached; never for a transfer that has ended.
	FileCredit(dropID, transferID uint32, bytes uint64)
	// FileDone: a transfer ended, exactly once per transfer. path is the
	// sender's final path for OK. last is set on the drop's final
	// transfer.
	FileDone(dropID, transferID uint32, status FileStatus, path string, last bool)
}

// VideoFrame is one access unit of service video, handed to FrameCommit.
type VideoFrame struct {
	// Data is the reserved slot up to its length, the SPS and PPS the KVM
	// added included.
	Data   []byte
	PTS90k uint32
	// NowUS() when the frame's envelope was read and when its last byte was
	// copied.
	ArrivalUS  int64
	ReceivedUS int64
	Width      uint32
	Height     uint32
	FPS        uint32
	Codec      Codec
	// KeyframeGeneration is the platform's keyframe request this keyframe
	// answers; 0 for none.
	KeyframeGeneration uint32
	Keyframe           bool
	ParameterSetsAdded bool
}

// VideoPlatform shows service video.
type VideoPlatform interface {
	NowUS() int64
	// DisplayPresent stops hardware capture and presents the KVM display
	// to the target: width x height preferred, an EDID with this serial.
	// It returns false when the display cannot be presented. The platform
	// reports progress with DisplayStatus.
	DisplayPresent(width, height, serial uint32) bool
	// DisplayRestore removes the KVM display and restarts hardware capture.
	DisplayRestore()
	// VideoStream: a coded sequence starts. It returns the bytes one access
	// unit of the stream may take, or 0 when the KVM cannot show it.
	VideoStream(width, height, fps uint32, codec Codec) int
	// VideoStopped: service video stopped for a switch to hardware capture.
	VideoStopped()
	// FrameReserve is a slot for one access unit of length bytes. nil with
	// busy: every slot is taken, try again; nil without: no viewer takes
	// video, so the frame is discarded.
	FrameReserve(length int) (slot []byte, busy bool)
	// FrameCommit hands over the reserved slot. It returns whether the
	// viewer's queue took it.
	FrameCommit(frame *VideoFrame) bool
	// FrameCancel gives the reserved slot back.
	FrameCancel(written int)
}

// VideoGapPlatform hears of frames lost before the platform got them.
type VideoGapPlatform interface {
	VideoGap()
}

// KeyframePlatform asks for keyframes.
type KeyframePlatform interface {
	// KeyframeWanted is a non-zero generation that the session owns until
	// it calls KeyframeDone, or 0 for none.
	KeyframeWanted() uint32
	KeyframeDone(generation uint32, delivered bool)
}

// VideoNetwork is the network's view of service video.
type VideoNetwork struct {
	// Adaptive: the target follows the receiver feedback.
	Adaptive bool
	// Interval: an interval of about a second ended since the last call.
	Interval     bool
	EstimateKbps uint32
	Cut          bool
	SentKbps     uint32
}

// VideoNetworkPlatform adapts the targets to the network.
type VideoNetworkPlatform interface {
	VideoNetwork(ceilingKbps uint32) VideoNetwork
}

type linkState int

const (
	linkIdle      linkState = iota // no session; waiting for RESET
	linkResetting                  // RESET answered; old transfers draining
	linkAdmitted                   // phase Ready; searching for session.hello
	linkOpen                       // handshake done
)

type parseState int

const (
	parseSearch parseState = iota
	parseHeader
	parsePayload
	parseSkip
	// The terminal.output in payload waits for the browser.
	parseHeld
	parseStopped
	// A video.frame: its 12-byte head, then the wait for a slot, then the
	// access unit straight into the slot. A frame with parameter sets that
	// gets no slot is read into the payload buffer for its parameter sets.
	parseFrameHead
	parseFrameWait
	parseFrameData
	parseFrameSets
)

type clipboardRequest struct {
	requestID  uint32
	deadlineMS int64
	kind       uint16
	inUse      bool
}

type terminalState int

const (
	terminalFree terminalState = iota
	terminalOpening
	terminalRunning
)

type terminal struct {
	id         uint32
	state      terminalState
	deadlineMS int64
}

// FileEntry is one file of a drop the browser offers.
type FileEntry struct {
	Name string
	Size uint64
}

type fileTransfer struct {
	size           uint64
	sent           uint64
	credit         uint64
	creditReported uint64
	status         FileStatus
	ready          bool
	ended          bool
	reported       bool
}

type dropState struct {
	dropID           uint32
	count            uint32
	ended            uint32
	reported         uint32
	attached         bool
	progressMS       int64
	attachDeadlineMS int64
	transfers        []fileTransfer
}

// Update is the update in progress, or the last one; UpdateID 0 means none
// yet.
type Update struct {
	UpdateID uint32
	Size     uint64
	Sent     uint64
	credit   uint64
	ready    bool
	Ended    bool
	// Valid once ended.
	Status     uint8
	Message    string
	progressMS int64
}

// Counters are for the logs and the diagnostics.
type Counters struct {
	SessionsOpened  uint32
	SessionsEnded   uint32
	Resets          uint32
	MessagesIn      uint32
	MessagesSkipped uint32
	ServiceFaults   uint32
	BytesIn         uint64
	BytesDiscarded  uint64
	BytesOut        uint64
	LastHandshakeMS int64
}

// VideoCounters are kept across sessions.
type VideoCounters struct {
	Frames              uint32
	Keyframes           uint32
	Bytes               uint64
	FramesDiscarded     uint32
	FramesDropped       uint32
	SlotWaits           uint32
	SlotWaitMaxMS       uint32
	KeyframeRequests    uint32
	ParameterSetsAdded  uint32
	StreamResets        uint32
	Switches            uint32
	SwitchFailures      uint32
	Faults              uint32
	LargestKeyframe     uint32
	SlotWaitMS          uint32
	SenderSkippedFrames uint32
}

type videoRecovery struct {
	startsMS []int64
	blocked  bool
}

// Session is the KVM side of one OS Services USB function.
type Session struct {
	p        Platform
	terminal TerminalPlatform
	files    FilePlatform
	video    VideoPlatform
	gap      VideoGapPlatform
	keyframe KeyframePlatform
	network  VideoNetworkPlatform

	settings Settings

	// Reset exchange.
	link          linkState
	faulted       bool
	token         [TokenLen]byte
	admittedMS    int64
	lastMessageMS int64

	// Stream parser.
	parse       parseState
	header      [EnvelopeLen]byte
	headerFill  int
	service     uint16
	kind        uint16
	payloadSize uint32
	payloadFill uint32
	payload     []byte
	heldMS      int64
	searched    uint64
	searchTail  []byte

	// Open session.
	protocolMinor   uint16
	approvalPending bool
	senderInfoValid bool
	senderVersion   string
	displays        []string
	fingerprint     string
	sessionMessages uint32
	serviceFaulted  [serviceCount]bool
	lastFaultReason string

	// Clipboard service.
	clipboardRegistered  bool
	clipboardFlags       uint32
	clipboardOS          TargetOS
	clipboardRequests    [clipboardPendingMax]clipboardRequest
	nextClipboardRequest uint32

	// Terminal service.
	terminalShell  bool
	terminals      [TerminalSessionsMax]terminal
	nextTerminalID uint32

	// File service.
	fileReceive bool
	drop        dropState
	nextFileID  uint32

	// Update service.
	updateInstall bool
	updatePackage string
	update        Update
	nextUpdateID  uint32

	// Video service: the sender's registration.
	videoRegistered    bool
	videoCodecFlags    uint32
	videoMaxWidth      uint32
	videoMaxHeight     uint32
	videoMaxFPS        uint32
	videoSwitchWanted  bool
	videoAnswerPending bool
	// video.config with enabled 1 went out for the display presented now.
	videoConfigEnabled bool
	// The sender was told to enable video and has not reported it disabled
	// since.
	videoSenderEnabled  bool
	videoDisablePending bool
	videoStreamStarted  bool
	// The codec changed while video showed; enabled statuses of the old
	// codec are stale until one of the new codec arrives.
	videoCodecChanging bool
	videoResetPending  bool
	videoPTSRestart    bool
	videoWidth         uint32
	videoHeight        uint32
	videoFPS           uint32
	videoCodec         Codec
	videoFrameCapacity int
	videoLastCaptureUS uint64
	videoSentCaptureOK bool
	videoSentCaptureUS uint64
	videoKeyframeGen   uint32
	videoKeyframeTaken int64
	videoKeyframeAsked bool
	videoKeyframeAskMS int64
	videoError         string
	parameterSets      []byte

	// The video.frame being read.
	frameHead       [videoFrameHeaderLen]byte
	frameHeadFill   int
	frameFlags      uint32
	frameCaptureUS  uint64
	frameArrivalUS  int64
	frameUnitLength int
	framePrefix     int
	frameSlot       []byte
	frameFill       int
	frameWaiting    bool
	frameWaitMS     int64

	// The adapted targets and the slot waits of the current interval.
	rate                   videoRate
	videoIntervalMS        int64
	videoIntervalWaitMS    uint32
	videoIntervalFrames    uint32
	videoPressureIntervals uint32
	videoFreeMFPS          uint32
	videoRateFollows       bool
	videoSenderKbps        uint32
	// The session ends for the KVM's own reason, and the next session
	// switches back: this session's switch does not count.
	videoSwitchBack bool

	// Kept across sessions.
	videoSource         VideoSource
	videoSourceMS       int64
	videoRestoring      bool
	videoDisplayHeld    bool
	recovery            videoRecovery
	videoSwitchRefunded bool
	videoPTSValid       bool
	videoPTSUS          uint64
	VideoCounters       VideoCounters

	Counters      Counters
	LastEndReason string
}

// New makes a session with no USB session in progress. The platform may also
// implement TerminalPlatform, FilePlatform, VideoPlatform, VideoGapPlatform,
// KeyframePlatform and VideoNetworkPlatform.
func New(p Platform, settings Settings) *Session {
	s := &Session{
		p:                    p,
		settings:             settings,
		payload:              make([]byte, MaxControlPayload),
		searchTail:           make([]byte, 0, searchTailLen),
		link:                 linkIdle,
		parse:                parseStopped,
		nextClipboardRequest: 1,
		nextTerminalID:       1,
		nextFileID:           1,
	}
	s.terminal, _ = p.(TerminalPlatform)
	s.files, _ = p.(FilePlatform)
	s.video, _ = p.(VideoPlatform)
	s.gap, _ = p.(VideoGapPlatform)
	s.keyframe, _ = p.(KeyframePlatform)
	s.network, _ = p.(VideoNetworkPlatform)
	// A STATUS before the first RESET has a token too.
	s.fillToken(s.token[:])
	return s
}

func (s *Session) nowMS() int64 { return s.p.NowMS() }

func (s *Session) notifyState() { s.p.StateChanged() }

func (s *Session) failClipboardRequests(status ClipboardStatus) {
	for i := range s.clipboardRequests {
		r := &s.clipboardRequests[i]
		if !r.inUse {
			continue
		}
		r.inUse = false
		s.p.ClipboardDone(r.requestID, status, false, nil, false)
	}
}

func (s *Session) clearVideo() {
	s.videoRegistered = false
	s.videoCodecFlags = 0
	s.videoMaxWidth = 0
	s.videoMaxHeight = 0
	s.videoMaxFPS = 0
	s.videoSwitchWanted = false
	s.videoAnswerPending = false
	s.videoConfigEnabled = false
	s.videoSenderEnabled = false
	s.videoDisablePending = false
	s.videoStreamStarted = false
	s.videoResetPending = false
	s.videoCodecChanging = false
	s.videoError = ""
	s.parameterSets = s.parameterSets[:0]
}

func (s *Session) clearRegistrations() {
	s.clipboardRegistered = false
	s.clipboardFlags = 0
	s.clipboardOS = OSUnknown
	s.terminalShell = false
	s.fileReceive = false
	s.updateInstall = false
	s.updatePackage = ""
	s.clearVideo()
	s.serviceFaulted = [serviceCount]bool{}
}

// endSession ends the current USB session, if any: fails what every service
// owes, forgets the registrations and tells the transport to stop reading.
// The link goes to Idle; only a new RESET admits the next session. A KVM that
// shows service video returns to hardware capture.
func (s *Session) endSession(reason string) {
	wasOpen := s.link == linkOpen
	hadSession := s.link != linkIdle
	s.link = linkIdle
	s.frameRelease()
	s.parse = parseStopped
	if s.videoSwitchBack && (s.videoSource == SourceSwitchingToGuest || s.videoSource == SourceGuest) {
		s.videoRefundSwitch()
	}
	s.videoSwitchBack = false
	s.videoRestore()
	s.failClipboardRequests(ClipboardFailed)
	s.endTerminals(terminalDisconnected, false)
	s.endTransfers(FileFailed, false)
	s.endUpdate(UpdateFailed, "the OS Services session ended", false)
	s.clearRegistrations()
	s.senderInfoValid = false
	s.senderVersion = ""
	s.displays = nil
	if hadSession {
		s.LastEndReason = reason
		if wasOpen {
			s.Counters.SessionsEnded++
		}
		s.p.SessionEnded(reason)
	}
	if wasOpen {
		s.notifyState()
	}
}

// writeMessage queues one message of a head and a body; false when it did
// not fit. The caller ends the session then.
func (s *Session) writeMessage(service, kind uint16, head, body []byte) bool {
	var envelope [EnvelopeLen]byte
	putEnvelope(envelope[:], service, kind, uint32(len(head)+len(body)))
	if !s.p.Write(envelope[:], head, body) {
		return false
	}
	s.Counters.BytesOut += uint64(EnvelopeLen + len(head) + len(body))
	return true
}

func (s *Session) sendMessage(service, kind uint16, payload []byte) bool {
	if !s.writeMessage(service, kind, payload, nil) {
		s.endSession(inEndpointStuck)
		return false
	}
	return true
}

func (s *Session) sendSessionConfig() bool {
	config, err := json.Marshal(struct {
		TerminalEnabled bool   `json:"terminal_enabled"`
		FilesEnabled    bool   `json:"files_enabled"`
		CaptureDisplay  string `json:"capture_display"`
		// This KVM presents the same monitor each time to a sender that
		// sets the display's mode (videoDisplaySerial).
		DisplayModeBySender bool `json:"display_mode_by_sender"`
	}{s.settings.TerminalEnabled, s.settings.FilesEnabled, s.settings.CaptureDisplay, true})
	if err != nil {
		s.endSession("session.config: " + err.Error())
		return false
	}
	return s.sendMessage(ServiceSession, sessionConfig, config)
}

func (s *Session) sendClipboardConfig() bool {
	enabled := byte(0)
	if s.settings.ClipboardEnabled {
		enabled = 1
	}
	return s.sendMessage(ServiceClipboard, clipboardConfig, []byte{enabled})
}

// faultService detaches a service after an invalid message (Errors): it
// fails what the service owes and ignores its messages until it registers
// again.
func (s *Session) faultService(service uint16, reason string) {
	if service >= serviceCount || service == ServiceSession {
		s.endSession(reason)
		return
	}
	s.Counters.ServiceFaults++
	s.serviceFaulted[service] = true
	s.lastFaultReason = reason
	switch service {
	case ServiceClipboard:
		s.clipboardRegistered = false
		s.clipboardFlags = 0
		s.clipboardOS = OSUnknown
		s.failClipboardRequests(ClipboardFailed)
	case ServiceTerminal:
		// The sender does not know of the fault; terminal.close ends the
		// shells that the KVM no longer serves.
		s.terminalShell = false
		s.endTerminals(terminalFaulted+reason, true)
	case ServiceFile:
		s.fileReceive = false
		s.endTransfers(FileFailed, true)
	case ServiceUpdate:
		s.updateInstall = false
		s.updatePackage = ""
		s.endUpdate(UpdateFailed, reason, true)
	case ServiceVideo:
		// Back to hardware capture first: a failed write ends the session.
		registered := s.videoRegistered
		s.videoRegistered = false
		s.videoSwitchWanted = false
		// No status is expected: they go unread until video registers again.
		s.videoSenderEnabled = false
		s.VideoCounters.Faults++
		s.videoError = truncate(reason, videoErrorMax)
		s.videoRestore()
		if registered {
			s.sendVideoConfig(false)
		}
	}
	s.notifyState()
}

func (s *Session) signAndSendReady(hello []byte) {
	ready := make([]byte, readyLen)
	// The lower of the two minor versions (Version).
	minor := le.Uint16(hello[2:])
	s.protocolMinor = min(minor, ProtocolMinor)
	le.PutUint16(ready, ProtocolMajor)
	le.PutUint16(ready[2:], s.protocolMinor)
	copy(ready[4:], s.token[:])
	key, ok := s.p.PublicKey()
	if !ok || len(key) != PublicKeyLen {
		s.endSession("the KVM key is unavailable")
		return
	}
	copy(ready[20:], key)
	// The signed bytes: the context text, the hello payload and the first
	// 52 bytes of this payload (KVM approval).
	signed := make([]byte, 0, len(signatureContext)+helloLen+readySignedLen)
	signed = append(signed, signatureContext...)
	signed = append(signed, hello[:helloLen]...)
	signed = append(signed, ready[:readySignedLen]...)
	signature, ok := s.p.Sign(signed)
	if !ok || len(signature) != SignatureLen {
		s.endSession("the KVM could not sign session.ready")
		return
	}
	copy(ready[readySignedLen:], signature)
	s.link = linkOpen
	s.lastMessageMS = s.nowMS()
	s.Counters.LastHandshakeMS = s.lastMessageMS - s.admittedMS
	if !s.sendMessage(ServiceSession, sessionReady, ready) {
		return
	}
	if !s.sendSessionConfig() {
		return
	}
	s.Counters.SessionsOpened++
	s.sessionMessages = 0
	s.notifyState()
}

// handleHello: the hello was found with the RESET token; its version decides
// the rest.
func (s *Session) handleHello(hello []byte) {
	if le.Uint16(hello) != ProtocolMajor {
		s.endSession("session.hello has another protocol major version")
		return
	}
	s.signAndSendReady(hello)
}

func (s *Session) handleSessionInfo(payload []byte) {
	var info map[string]json.RawMessage
	if err := json.Unmarshal(payload, &info); err != nil || info == nil {
		s.endSession("session.info is not a JSON object")
		return
	}
	version := ""
	var displays []string
	valid := true
	if raw, ok := info["version"]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &version); err != nil || len(version) > infoVersionMax {
			valid = false
		}
	}
	if raw, ok := info["displays"]; ok && valid && string(raw) != "null" {
		if err := json.Unmarshal(raw, &displays); err != nil || len(displays) > infoDisplaysMax {
			valid = false
		}
		for _, name := range displays {
			if len(name) > infoDisplayMax {
				valid = false
			}
		}
	}
	if !valid {
		s.endSession("session.info is over a limit")
		return
	}
	changed := !s.senderInfoValid || version != s.senderVersion || len(displays) != len(s.displays)
	for i := 0; !changed && i < len(displays); i++ {
		changed = displays[i] != s.displays[i]
	}
	s.senderVersion = version
	s.displays = displays
	s.senderInfoValid = true
	if changed {
		s.notifyState()
	}
}

func (s *Session) handleSession(payload []byte) {
	switch s.kind {
	case sessionHello:
		s.endSession("a second session.hello")
	case sessionPing:
		if len(payload) != 0 {
			s.endSession("session.ping with a payload")
		}
	case sessionClose:
		if len(payload) != 1 || (payload[0] != closeShutdown && payload[0] != closeProtocolError && payload[0] != closeNotApproved) {
			s.endSession("invalid session.close")
			return
		}
		if payload[0] == closeNotApproved && !s.approvalPending {
			s.approvalPending = true
			s.notifyState()
		}
		switch payload[0] {
		case closeNotApproved:
			s.endSession("the sender's user has not approved this KVM")
		case closeProtocolError:
			s.endSession("the sender closed the session: protocol error")
		default:
			s.endSession("the sender closed the session")
		}
	case sessionInfo:
		s.handleSessionInfo(payload)
	case sessionReady, sessionConfig:
		s.endSession("a KVM-to-sender session message from the sender")
	default:
		s.Counters.MessagesSkipped++
	}
}

func (s *Session) dispatch(payload []byte) {
	s.Counters.MessagesIn++
	s.lastMessageMS = s.nowMS()
	// A sender that its user did not approve closes the session right after
	// the handshake; anything else means the KVM is approved.
	first := s.sessionMessages == 0
	s.sessionMessages++
	if first && s.approvalPending && !(s.service == ServiceSession && s.kind == sessionClose) {
		s.approvalPending = false
		s.notifyState()
	}
	if s.service != ServiceSession && s.service < serviceCount && s.serviceFaulted[s.service] {
		// A faulted service is ignored until it registers again.
		if s.kind != 1 {
			return
		}
	}
	switch s.service {
	case ServiceSession:
		s.handleSession(payload)
	case ServiceVideo:
		s.handleVideo(payload)
	case ServiceClipboard:
		s.handleClipboard(payload)
	case ServiceTerminal:
		s.handleTerminal(payload)
	case ServiceFile:
		s.handleFile(payload)
	case ServiceUpdate:
		s.handleUpdate(payload)
	default:
		s.Counters.MessagesSkipped++
	}
}

// keepsPayload: the known sender-to-KVM kinds. Everything else is skipped by
// its envelope.
func keepsPayload(service, kind uint16) bool {
	switch service {
	case ServiceSession:
		return kind >= sessionHello && kind <= sessionInfo
	case ServiceVideo:
		return kind == videoCapabilities || kind == videoStatus || kind == videoConfig || kind == videoRequestKeyframe
	case ServiceClipboard:
		return (kind >= clipboardCapabilities && kind <= clipboardStatus) || kind == clipboardConfig
	case ServiceTerminal:
		return kind >= terminalCapabilities && kind <= terminalCredit
	case ServiceFile:
		return kind >= fileCapabilities && kind <= fileCancel
	case ServiceUpdate:
		return kind >= updateCapabilities && kind <= updateCancel
	}
	return false
}

func (s *Session) skipDone() {
	s.parse = parseHeader
	s.Counters.MessagesSkipped++
	s.lastMessageMS = s.nowMS()
}

// beginMessage: a complete envelope. Decide where its payload goes.
func (s *Session) beginMessage() {
	s.service = le.Uint16(s.header[:])
	s.kind = le.Uint16(s.header[2:])
	s.payloadSize = le.Uint32(s.header[4:])
	s.payloadFill = 0
	s.headerFill = 0
	if s.payloadSize > payloadLimit(s.service, s.kind) {
		s.endSession("a payload_size above its limit")
		return
	}
	if s.service == ServiceVideo && s.kind == videoFrame && s.beginFrame() {
		return
	}
	if s.parse == parseStopped {
		// The frame faulted video, and the reply could not be written.
		return
	}
	if keepsPayload(s.service, s.kind) && int(s.payloadSize) <= len(s.payload) {
		s.parse = parsePayload
	} else {
		// Unknown kinds, and video.frame while service video is not shown.
		s.parse = parseSkip
	}
	if s.payloadSize == 0 {
		if s.parse == parsePayload {
			s.parse = parseHeader
			s.dispatch(s.payload[:0])
		} else {
			s.skipDone()
		}
	}
}

func (s *Session) helloAt(p []byte) bool {
	return p[0] == ServiceSession && p[1] == 0 && p[2] == sessionHello && p[3] == 0 &&
		le.Uint32(p[4:]) == helloLen &&
		bytes.Equal(p[helloTokenOffset:helloTokenOffset+TokenLen], s.token[:])
}

// searchHello searches for the complete session.hello with the RESET token
// (Byte stream). It returns the bytes of data consumed: up to the end of the
// hello when found, else all of them.
func (s *Session) searchHello(data []byte) int {
	// A hello that starts in the kept tail and ends in data.
	if n := len(s.searchTail); n > 0 {
		head := min(len(data), searchTailLen)
		joined := make([]byte, 0, n+head)
		joined = append(joined, s.searchTail...)
		joined = append(joined, data[:head]...)
		for p := 0; p < n && p+helloMessageLen <= len(joined); p++ {
			if s.helloAt(joined[p:]) {
				hello := append([]byte(nil), joined[p+EnvelopeLen:p+helloMessageLen]...)
				consumed := p + helloMessageLen - n
				s.Counters.BytesDiscarded += uint64(p)
				s.searchTail = s.searchTail[:0]
				s.parse = parseHeader
				s.handleHello(hello)
				return consumed
			}
		}
	}
	for p := 0; p+helloMessageLen <= len(data); p++ {
		// The kind byte, then the full check.
		if data[p+2] == sessionHello && s.helloAt(data[p:]) {
			s.Counters.BytesDiscarded += uint64(len(s.searchTail) + p)
			s.searchTail = s.searchTail[:0]
			s.parse = parseHeader
			hello := append([]byte(nil), data[p+EnvelopeLen:p+helloMessageLen]...)
			s.handleHello(hello)
			return p + helloMessageLen
		}
	}
	// Not found: keep the last searchTailLen bytes of tail + data.
	s.searched += uint64(len(data))
	if len(data) >= searchTailLen {
		s.Counters.BytesDiscarded += uint64(len(s.searchTail) + len(data) - searchTailLen)
		s.searchTail = append(s.searchTail[:0], data[len(data)-searchTailLen:]...)
	} else {
		total := len(s.searchTail) + len(data)
		if total > searchTailLen {
			drop := total - searchTailLen
			s.searchTail = append(s.searchTail[:0], s.searchTail[drop:]...)
			s.Counters.BytesDiscarded += uint64(drop)
		}
		s.searchTail = append(s.searchTail, data...)
	}
	if s.searched > HelloSearchLimit {
		s.endSession("no session.hello in 8 MiB of input")
	}
	return len(data)
}

// Receive takes bytes from the OUT endpoint. It returns the number of bytes
// consumed; it is less than len(data) when the session ended, after which the
// transport stops reading, or when a video frame waits for an access-unit
// slot (FrameWaiting) or a terminal.output for the browser (Holding). The
// caller then keeps the rest of the read, and with it the bus, and offers it
// again: when the platform frees a slot or the browser takes output, and at
// least every tick. After videoSlotWaitMS the frame is dropped and the rest
// is consumed.
func (s *Session) Receive(data []byte) int {
	if s.parse == parseHeld && !s.deliverHeld() {
		return 0
	}
	length := len(data)
	used := 0
	for used < length && s.parse != parseHeld {
		if s.link != linkAdmitted && s.link != linkOpen {
			break
		}
		p := data[used:length]
		switch s.parse {
		case parseSearch:
			used += s.searchHello(p)
		case parseHeader:
			take := copy(s.header[s.headerFill:], p)
			s.headerFill += take
			used += take
			if s.headerFill == EnvelopeLen {
				s.beginMessage()
			}
		case parsePayload:
			take := copy(s.payload[s.payloadFill:s.payloadSize], p)
			s.payloadFill += uint32(take)
			used += take
			if s.payloadFill == s.payloadSize {
				s.parse = parseHeader
				s.dispatch(s.payload[:s.payloadSize])
			}
		case parseSkip:
			take := min(int(s.payloadSize-s.payloadFill), len(p))
			s.payloadFill += uint32(take)
			s.Counters.BytesDiscarded += uint64(take)
			used += take
			if s.payloadFill == s.payloadSize {
				s.skipDone()
			}
		case parseFrameHead:
			take := copy(s.frameHead[s.frameHeadFill:], p)
			s.frameHeadFill += take
			s.payloadFill += uint32(take)
			used += take
			if s.frameHeadFill == videoFrameHeaderLen {
				s.frameHeadDone()
			}
		case parseFrameWait:
			if !s.frameTryReserve() {
				// The caller keeps the rest of the read.
				length = used
			}
		case parseFrameData:
			take := min(int(s.payloadSize-s.payloadFill), len(p))
			copy(s.frameSlot[s.frameFill:], p[:take])
			s.frameFill += take
			s.payloadFill += uint32(take)
			used += take
			if s.payloadFill == s.payloadSize {
				s.parse = parseHeader
				s.frameCommit()
			}
		case parseFrameSets:
			used += s.readFrameSets(p)
		default:
			s.Counters.BytesIn += uint64(used)
			return used
		}
	}
	s.Counters.BytesIn += uint64(used)
	return used
}

// FrameWaiting: a video frame waits for an access-unit slot.
func (s *Session) FrameWaiting() bool { return s.parse == parseFrameWait }

// Holding: a terminal.output waits for room in the browser's channel.
func (s *Session) Holding() bool { return s.parse == parseHeld }

// fillToken: the protocol has no all-zero token, so a sender reads any change
// of the token as a new owner.
func (s *Session) fillToken(token []byte) {
	for {
		s.p.FillRandom(token)
		for _, b := range token {
			if b != 0 {
				return
			}
		}
	}
}

// USBReset is the vendor RESET request: it ends any session and answers with
// a new token in phase Resetting.
func (s *Session) USBReset() [USBStatusLen]byte {
	var token [TokenLen]byte
	s.fillToken(token[:])
	return s.USBResetWithToken(token)
}

// USBResetWithToken is RESET with a token the caller made (non-zero), for a
// transport that answers the control request before the session runs.
func (s *Session) USBResetWithToken(token [TokenLen]byte) [USBStatusLen]byte {
	s.endSession("RESET")
	s.token = token
	s.link = linkResetting
	s.Counters.Resets++
	status := s.USBStatus()
	// The previous token's transfers must be cancelled and reaped before
	// the new session is admitted.
	s.p.ResetTransport()
	return status
}

// USBStatus is the answer to the vendor STATUS request.
func (s *Session) USBStatus() [USBStatusLen]byte {
	var status [USBStatusLen]byte
	copy(status[:], "JKV5")
	phase := PhaseResetting
	switch {
	case s.faulted:
		phase = PhaseFaulted
	case s.link == linkAdmitted:
		phase = PhaseReady
	case s.link == linkOpen:
		phase = PhaseActive
	}
	status[4] = byte(phase)
	copy(status[8:], s.token[:])
	return status
}

// TransportDrained: the transport completed every bulk request of the
// previous session and reads OUT again; the session for the RESET token is
// admitted.
func (s *Session) TransportDrained() {
	if s.link != linkResetting || s.faulted {
		return
	}
	s.link = linkAdmitted
	s.admittedMS = s.nowMS()
	s.parse = parseSearch
	s.searched = 0
	s.searchTail = s.searchTail[:0]
	s.headerFill = 0
}

// Reading reports whether the transport should read the OUT endpoint.
func (s *Session) Reading() bool {
	return s.link == linkAdmitted || s.link == linkOpen
}

// End ends the session, for a reason outside the stream.
func (s *Session) End(reason string) { s.endSession(reason) }

// USBReconfigured: the USB function was unbound, disabled, enabled or lost
// the bus; byKVM when the KVM restarted it for a mount or a USB setting.
func (s *Session) USBReconfigured(byKVM bool) {
	// The KVM's own USB restart does not count against the budget: the next
	// session switches back, and mounts would block video.
	s.videoSwitchBack = s.videoSwitchBack || byKVM
	s.endSession("the USB function was reconfigured")
	// A new token tells the sender of the ended session that it is
	// superseded; no hello can use it before the next RESET.
	s.fillToken(s.token[:])
}

// SetFaulted: the USB function failed; no session can start until it
// recovers.
func (s *Session) SetFaulted(faulted bool) {
	if faulted {
		s.endSession("the USB function failed")
	}
	s.faulted = faulted
}

// Tick runs the timers. Call it at least every 100 ms.
func (s *Session) Tick() {
	now := s.nowMS()
	if s.link == linkAdmitted && now-s.admittedMS > handshakeTimeoutMS {
		s.endSession("no session.hello within 15 seconds of admission")
		return
	}
	if s.link == linkOpen && now-s.lastMessageMS > livenessTimeoutMS {
		s.endSession("no message from the sender for 15 seconds")
		return
	}
	if s.parse == parseHeld {
		// Answers may wait unread behind the output: no request times out
		// before the session has read on.
		s.deliverHeld()
		return
	}
	for i := range s.clipboardRequests {
		r := &s.clipboardRequests[i]
		if r.inUse && now >= r.deadlineMS {
			r.inUse = false
			s.p.ClipboardDone(r.requestID, ClipboardFailed, false, nil, false)
		}
	}
	// An answer that never came may still have started a shell, so the
	// timeout closes it on the sender too.
	for i := range s.terminals {
		t := &s.terminals[i]
		if t.state == terminalOpening && now >= t.deadlineMS {
			s.endTerminal(t, terminalNoAnswer, false, true)
		}
	}
	if s.updateOpen(s.update.UpdateID) && now-s.update.progressMS >= updateIdleTimeoutMS {
		s.endUpdate(UpdateFailed, "the update made no progress for 30 seconds", true)
	}
	d := &s.drop
	if d.dropID != 0 && !d.attached && d.ended < d.count && now >= d.attachDeadlineMS {
		s.endTransfers(FileFailed, true)
	}
	if d.dropID != 0 && now-d.progressMS >= fileIdleTimeoutMS {
		if d.ended < d.count {
			s.endTransfers(FileFailed, true)
		} else if !d.attached {
			// Its channel never opened to hear the outcomes.
			s.forgetDrop()
		}
	}
	s.videoTick(now)
}

// ApplySettings applies new KVM settings, and sends what the change requires.
func (s *Session) ApplySettings(settings Settings) {
	previous := s.settings
	s.settings = settings
	if previous.VideoEnabled != settings.VideoEnabled {
		// Turning video off or on clears the retry budget and its block.
		s.recovery = videoRecovery{}
		s.videoError = ""
	}
	if s.link != linkOpen {
		return
	}
	// A service turned off ends here first. The sender ends its shells and
	// transfers when it gets the session.config below; transfers are also
	// cancelled one by one.
	if previous.TerminalEnabled && !settings.TerminalEnabled {
		s.endTerminals(terminalDisabled, false)
	}
	if previous.FilesEnabled && !settings.FilesEnabled {
		s.endTransfers(FileFailed, true)
		if s.link != linkOpen {
			return
		}
	}
	if previous.TerminalEnabled != settings.TerminalEnabled ||
		previous.FilesEnabled != settings.FilesEnabled ||
		previous.CaptureDisplay != settings.CaptureDisplay {
		if !s.sendSessionConfig() {
			return
		}
	}
	if previous.ClipboardEnabled != settings.ClipboardEnabled && s.clipboardRegistered {
		if !s.sendClipboardConfig() {
			return
		}
	}
	s.videoApplySettings(&previous)
	if s.link != linkOpen {
		return
	}
	s.notifyState()
}

// Settings returns the settings in force.
func (s *Session) Settings() Settings { return s.settings }

// SetFingerprint sets the KVM key's fingerprint, for the settings page.
func (s *Session) SetFingerprint(text string) { s.fingerprint = text }

// Open reports whether a session is open.
func (s *Session) Open() bool { return s.link == linkOpen }

func truncate(text string, n int) string {
	if len(text) <= n {
		return text
	}
	cut := n
	for cut > 0 && text[cut]&0xc0 == 0x80 {
		cut--
	}
	return text[:cut]
}

// State is the session as the browser sees it.
type State struct {
	Connected       bool
	ApprovalPending bool
	Fingerprint     string
	SenderInfo      bool
	SenderVersion   string
	Displays        []string
	Clipboard       bool
	ClipboardFlags  uint32
	ClipboardOS     TargetOS
	VideoRegistered bool
	VideoCodecFlags uint32
	Source          VideoSource
	VideoError      string
	Terminal        bool
	FileReceive     bool
	UpdateInstall   bool
	UpdatePackage   string
	Update          Update
	StreamWidth     uint32
	StreamHeight    uint32
	StreamFPS       uint32
	StreamCodec     Codec
	StreamStarted   bool
}

// State returns what the browser shows.
func (s *Session) State() State {
	connected := s.link == linkOpen
	st := State{
		Connected:       connected,
		ApprovalPending: s.approvalPending,
		Fingerprint:     s.fingerprint,
		ClipboardFlags:  s.clipboardFlags,
		ClipboardOS:     s.clipboardOS,
		Source:          s.videoSource,
		VideoError:      s.videoError,
		Terminal:        s.TerminalAvailable(),
		FileReceive:     s.FileAvailable(),
		UpdateInstall:   s.UpdateAvailable(),
		UpdatePackage:   s.updatePackage,
		Update:          s.update,
		StreamWidth:     s.videoWidth,
		StreamHeight:    s.videoHeight,
		StreamFPS:       s.videoFPS,
		StreamCodec:     s.videoCodec,
		StreamStarted:   s.videoStreamStarted,
	}
	st.Clipboard = s.ClipboardAvailable() && s.clipboardFlags&(ClipboardFlagRead|ClipboardFlagWrite) != 0
	st.VideoRegistered = connected && s.videoRegistered
	if st.VideoRegistered {
		st.VideoCodecFlags = s.videoCodecFlags
	}
	st.SenderInfo = connected && s.senderInfoValid
	if st.SenderInfo {
		st.SenderVersion = s.senderVersion
		st.Displays = append([]string(nil), s.displays...)
	}
	return st
}

// String describes the session's handover for logs.
func (s *Session) String() string {
	return fmt.Sprintf("link=%d parse=%d source=%s restoring=%t disable_pending=%t sender_enabled=%t config_enabled=%t registered=%t switches=%d blocked=%t",
		s.link, s.parse, s.videoSource, s.videoRestoring, s.videoDisablePending, s.videoSenderEnabled,
		s.videoConfigEnabled, s.videoRegistered, len(s.recovery.startsMS), s.recovery.blocked)
}
