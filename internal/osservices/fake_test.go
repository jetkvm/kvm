package osservices

// The test side of the session: a fake Platform with every optional hook, a
// fake video platform, and the sender's encodings written from PROTOCOL.md.
// The sender side uses none of the package's encoders or wire constants, so
// that a wrong constant or layout in the package shows up as a test failure.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/json"
	"testing"
)

// Service IDs and kinds from the tables of PROTOCOL.md.
const (
	svcSession   = 0
	svcVideo     = 1
	svcClipboard = 2
	svcTerminal  = 3
	svcFile      = 4
	svcUpdate    = 5

	kHello  = 1
	kReady  = 2
	kPing   = 3
	kClose  = 4
	kConfig = 5
	kInfo   = 6

	kVideoCaps     = 1
	kVideoConfig   = 3
	kVideoStatus   = 4
	kVideoFrame    = 5
	kVideoKeyframe = 6

	kClipCaps    = 1
	kClipGet     = 2
	kClipContent = 3
	kClipSet     = 4
	kClipStatus  = 5
	kClipConfig  = 7

	kTermCaps   = 1
	kTermOpen   = 2
	kTermOpened = 3
	kTermInput  = 4
	kTermOutput = 5
	kTermResize = 6
	kTermClose  = 7
	kTermExited = 8
	kTermCredit = 9

	kFileCaps   = 1
	kFileDrop   = 2
	kFileReady  = 3
	kFileChunk  = 4
	kFileCredit = 5
	kFileDone   = 6
	kFileCancel = 7

	kUpdateCaps   = 1
	kUpdateBegin  = 2
	kUpdateReady  = 3
	kUpdateChunk  = 4
	kUpdateCredit = 5
	kUpdateStatus = 6
	kUpdateCancel = 7
)

// Field values and sizes from PROTOCOL.md.
const (
	specContext       = "IP-KVM OS Services session.ready v5"
	specHelloLen      = 52
	specReadyLen      = 116
	specReadySigned   = 52
	specEnvelope      = 8
	specControlMax    = 65536
	specFrameMax      = 8 * 1024 * 1024
	specClipboardText = 65528
	specTerminalData  = 65532
	specChunkData     = 65524
	specFrameHead     = 12

	flagKey  = 1
	flagSets = 2
)

var wle = binary.LittleEndian

// wireMsg frames one message as the sender does: the envelope, then the
// payload parts.
func wireMsg(service, kind uint16, parts ...[]byte) []byte {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, specEnvelope, specEnvelope+n)
	wle.PutUint16(out, service)
	wle.PutUint16(out[2:], kind)
	wle.PutUint32(out[4:], uint32(n))
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func le16(v uint16) []byte { return wle.AppendUint16(nil, v) }
func le32(v uint32) []byte { return wle.AppendUint32(nil, v) }
func le64(v uint64) []byte { return wle.AppendUint64(nil, v) }

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// sentMessage is one message the KVM wrote to the IN endpoint.
type sentMessage struct {
	service, kind uint16
	payload       []byte
}

type clipEvent struct {
	requestID uint32
	status    ClipboardStatus
	answered  bool
	text      string
	truncated bool
}

// hookEvent is one call of a terminal or file hook, in call order.
type hookEvent struct {
	kind     string // "output", "closed", "credit" or "done"
	id       uint32 // terminal ID or drop ID
	transfer uint32
	data     []byte // output
	text     string // closed reason or done path
	exited   bool
	status   FileStatus
	last     bool
	bytes    uint64
}

// fakePlatform is the C tests' fake_platform: a settable clock, a seeded
// xorshift64* random source, a fixed Ed25519 key and an IN endpoint that
// records every message. It implements Platform, TerminalPlatform and
// FilePlatform.
type fakePlatform struct {
	t      *testing.T
	now    int64
	random uint64
	key    ed25519.PrivateKey
	pub    ed25519.PublicKey

	out         []byte
	outRead     int
	outCapacity int
	failWrites  bool
	writes      int
	ended       int
	endReason   string
	resets      int
	// A RESET whose old transfers the transport has not drained yet.
	drainPending bool
	// noKeyLater's PublicKey fails.
	keyGone      bool
	stateChanges int
	clipboard    []clipEvent

	termOutput      []byte
	termOutputID    uint32
	termOutputCalls int
	backedUp        bool
	hooks           []hookEvent
}

func newFakePlatform(t *testing.T) *fakePlatform {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i*7 + 1)
	}
	key := ed25519.NewKeyFromSeed(seed)
	return &fakePlatform{
		t:           t,
		now:         1000000,
		random:      0x9e3779b97f4a7c15,
		key:         key,
		pub:         key.Public().(ed25519.PublicKey),
		outCapacity: 16 << 20,
	}
}

func (f *fakePlatform) NowMS() int64 { return f.now }

func (f *fakePlatform) FillRandom(b []byte) {
	for i := range b {
		f.random ^= f.random >> 12
		f.random ^= f.random << 25
		f.random ^= f.random >> 27
		b[i] = byte((f.random * 2685821657736338717) >> 56)
	}
}

func (f *fakePlatform) PublicKey() ([]byte, bool) {
	return append([]byte(nil), f.pub...), true
}

func (f *fakePlatform) Sign(message []byte) ([]byte, bool) {
	return ed25519.Sign(f.key, message), true
}

func (f *fakePlatform) Write(envelope, head, body []byte) bool {
	f.writes++
	if len(envelope) != specEnvelope {
		f.t.Errorf("Write: envelope of %d bytes", len(envelope))
	}
	if got := wle.Uint32(envelope[4:]); int(got) != len(head)+len(body) {
		f.t.Errorf("Write: payload_size %d for %d bytes", got, len(head)+len(body))
	}
	total := len(envelope) + len(head) + len(body)
	if f.failWrites || len(f.out)+total > f.outCapacity {
		return false
	}
	f.out = append(f.out, envelope...)
	f.out = append(f.out, head...)
	f.out = append(f.out, body...)
	return true
}

func (f *fakePlatform) SessionEnded(reason string) {
	f.ended++
	f.endReason = reason
}

func (f *fakePlatform) ResetTransport() {
	f.resets++
	f.drainPending = true
	// What was queued for the IN endpoint is gone with the old session.
	f.out = f.out[:0]
	f.outRead = 0
}

func (f *fakePlatform) StateChanged() { f.stateChanges++ }

func (f *fakePlatform) ClipboardDone(requestID uint32, status ClipboardStatus, answered bool, text []byte, truncated bool) {
	f.clipboard = append(f.clipboard, clipEvent{requestID, status, answered, string(text), truncated})
}

func (f *fakePlatform) TerminalOutput(terminalID uint32, data []byte) bool {
	if terminalID == 0 || len(data) == 0 {
		f.t.Errorf("TerminalOutput(%d, %d bytes)", terminalID, len(data))
	}
	f.termOutputCalls++
	if f.backedUp {
		return false
	}
	f.termOutput = append(f.termOutput, data...)
	f.termOutputID = terminalID
	f.hooks = append(f.hooks, hookEvent{kind: "output", id: terminalID, data: append([]byte(nil), data...)})
	return true
}

func (f *fakePlatform) TerminalClosed(terminalID uint32, reason string, exited bool) {
	f.hooks = append(f.hooks, hookEvent{kind: "closed", id: terminalID, text: reason, exited: exited})
}

func (f *fakePlatform) FileCredit(dropID, transferID uint32, bytes uint64) {
	if bytes == 0 {
		f.t.Errorf("FileCredit(%d, %d, 0)", dropID, transferID)
	}
	f.hooks = append(f.hooks, hookEvent{kind: "credit", id: dropID, transfer: transferID, bytes: bytes})
}

func (f *fakePlatform) FileDone(dropID, transferID uint32, status FileStatus, path string, last bool) {
	f.hooks = append(f.hooks, hookEvent{kind: "done", id: dropID, transfer: transferID, status: status, text: path, last: last})
}

// closed returns the TerminalClosed calls so far.
func (f *fakePlatform) closed() []hookEvent { return f.hooksOf("closed") }

// fileEvents returns the FileCredit and FileDone calls so far.
func (f *fakePlatform) fileEvents() []hookEvent {
	var out []hookEvent
	for _, e := range f.hooks {
		if e.kind == "credit" || e.kind == "done" {
			out = append(out, e)
		}
	}
	return out
}

func (f *fakePlatform) hooksOf(kind string) []hookEvent {
	var out []hookEvent
	for _, e := range f.hooks {
		if e.kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// next returns the next message the KVM wrote, or false.
func (f *fakePlatform) next() (sentMessage, bool) {
	rest := f.out[f.outRead:]
	if len(rest) < specEnvelope {
		if len(rest) != 0 {
			f.t.Fatalf("%d bytes of an envelope left on the IN endpoint", len(rest))
		}
		return sentMessage{}, false
	}
	size := int(wle.Uint32(rest[4:]))
	if len(rest) < specEnvelope+size {
		f.t.Fatalf("a message cut short on the IN endpoint")
	}
	m := sentMessage{
		service: wle.Uint16(rest),
		kind:    wle.Uint16(rest[2:]),
		payload: append([]byte(nil), rest[specEnvelope:specEnvelope+size]...),
	}
	f.outRead += specEnvelope + size
	return m, true
}

func (f *fakePlatform) clearOutput() {
	f.out = f.out[:0]
	f.outRead = 0
}

// The C tests' fake_video: a display that presents when the test says so, a
// single access-unit slot the test can keep busy, a keyframe request source
// that requeues what it gets back, and a network that ends an interval at
// each whole second.
type videoRecord struct {
	frame VideoFrame
	data  []byte // a copy of frame.Data
}

type keyframeDoneEvent struct {
	generation uint32
	delivered  bool
}

const fakeSlotBytes = 3 * 1024 * 1024

type fakeVideo struct {
	t     *testing.T
	clock *fakePlatform

	presents      int
	presentWidth  uint32
	presentHeight uint32
	serials       []uint32
	presentFails  bool
	restores      int

	streams       int
	streamWidth   uint32
	streamHeight  uint32
	streamFPS     uint32
	streamCodec   Codec
	streamRefused bool
	stopped       int

	slot          []byte
	capacity      int
	reserved      bool
	busy          bool
	noViewer      bool
	reserves      int
	cancels       int
	cancelWritten int
	frames        []videoRecord
	refuseCommit  bool
	gaps          int

	keyframePending uint32
	keyframeDone    []keyframeDoneEvent

	manual       bool
	stale        bool
	estimateKbps uint32
	sentKbps     uint32
	intervalMS   int64
	ceilingKbps  uint32
	usOffset     int64
}

func newFakeVideo(t *testing.T, clock *fakePlatform) *fakeVideo {
	return &fakeVideo{
		t:          t,
		clock:      clock,
		slot:       make([]byte, fakeSlotBytes),
		capacity:   fakeSlotBytes,
		intervalMS: clock.now,
	}
}

func (v *fakeVideo) NowUS() int64 { return v.clock.now*1000 + v.usOffset }

func (v *fakeVideo) DisplayPresent(width, height, serial uint32) bool {
	if serial == 0 {
		v.t.Errorf("DisplayPresent with serial 0")
	}
	v.presents++
	v.serials = append(v.serials, serial)
	v.presentWidth, v.presentHeight = width, height
	return !v.presentFails
}

func (v *fakeVideo) DisplayRestore() { v.restores++ }

func (v *fakeVideo) VideoStream(width, height, fps uint32, codec Codec) int {
	if v.reserved {
		v.t.Errorf("VideoStream with a slot reserved")
	}
	v.streams++
	v.streamWidth, v.streamHeight, v.streamFPS, v.streamCodec = width, height, fps, codec
	if v.streamRefused {
		return 0
	}
	return v.capacity
}

func (v *fakeVideo) VideoStopped() {
	if v.reserved {
		v.t.Errorf("VideoStopped with a slot reserved")
	}
	v.stopped++
}

func (v *fakeVideo) FrameReserve(length int) ([]byte, bool) {
	if v.reserved || length > v.capacity || length <= 0 {
		v.t.Errorf("FrameReserve(%d): reserved %v, capacity %d", length, v.reserved, v.capacity)
	}
	v.reserves++
	if v.busy {
		return nil, true
	}
	if v.noViewer {
		return nil, false
	}
	v.reserved = true
	return v.slot[:length], false
}

func (v *fakeVideo) FrameCommit(frame *VideoFrame) bool {
	if !v.reserved || len(frame.Data) > v.capacity {
		v.t.Errorf("FrameCommit of %d bytes: reserved %v", len(frame.Data), v.reserved)
	}
	if len(frame.Data) > 0 && &frame.Data[0] != &v.slot[0] {
		v.t.Errorf("FrameCommit: Data is not the reserved slot")
	}
	v.reserved = false
	record := videoRecord{frame: *frame, data: append([]byte(nil), frame.Data...)}
	record.frame.Data = nil
	v.frames = append(v.frames, record)
	return !v.refuseCommit
}

func (v *fakeVideo) FrameCancel(written int) {
	if !v.reserved || written > v.capacity {
		v.t.Errorf("FrameCancel(%d): reserved %v", written, v.reserved)
	}
	v.reserved = false
	v.cancels++
	v.cancelWritten = written
}

func (v *fakeVideo) VideoGap() { v.gaps++ }

func (v *fakeVideo) KeyframeWanted() uint32 {
	g := v.keyframePending
	v.keyframePending = 0
	return g
}

func (v *fakeVideo) KeyframeDone(generation uint32, delivered bool) {
	if generation == 0 {
		v.t.Errorf("KeyframeDone(0)")
	}
	v.keyframeDone = append(v.keyframeDone, keyframeDoneEvent{generation, delivered})
	if !delivered {
		v.keyframePending = generation
	}
}

func (v *fakeVideo) VideoNetwork(ceilingKbps uint32) VideoNetwork {
	now := v.clock.now
	v.ceilingKbps = ceilingKbps
	n := VideoNetwork{Adaptive: !v.manual}
	if now/1000 != v.intervalMS/1000 {
		v.intervalMS = now
		n.Interval = true
		n.EstimateKbps = v.estimateKbps
		if n.EstimateKbps == 0 {
			n.EstimateKbps = ceilingKbps
		}
		n.Cut = !v.stale
		n.SentKbps = v.sentKbps
	}
	return n
}

// lastKeyframeDone is the last KeyframeDone call.
func (v *fakeVideo) lastKeyframeDone() keyframeDoneEvent {
	v.t.Helper()
	if len(v.keyframeDone) == 0 {
		v.t.Fatalf("no KeyframeDone call")
	}
	return v.keyframeDone[len(v.keyframeDone)-1]
}

// hookSet selects the optional hooks the session gets: New finds them by
// type assertion, so each set is its own type.
type hookSet int

const (
	// Terminal and file hooks, no video: the C tests' fake_platform.
	hooksServices hookSet = iota
	// No optional hook at all.
	hooksPlain
	// File hooks, no terminal hooks.
	hooksNoTerminal
	// Terminal hooks, no file hooks.
	hooksNoFiles
	// Every hook: fake_platform with fake_video attached.
	hooksVideo
	// VideoPlatform without VideoGapPlatform, KeyframePlatform and
	// VideoNetworkPlatform.
	hooksVideoBare
)

func platformFor(set hookSet, f *fakePlatform, v *fakeVideo) Platform {
	switch set {
	case hooksPlain:
		return struct{ Platform }{f}
	case hooksNoTerminal:
		return struct {
			Platform
			FilePlatform
		}{f, f}
	case hooksNoFiles:
		return struct {
			Platform
			TerminalPlatform
		}{f, f}
	case hooksVideo:
		return struct {
			*fakePlatform
			*fakeVideo
		}{f, v}
	case hooksVideoBare:
		return struct {
			Platform
			TerminalPlatform
			FilePlatform
			VideoPlatform
		}{f, f, f, v}
	}
	return f
}

// defaultSettings are the C tests' default_settings.
func defaultSettings() Settings {
	return Settings{
		ClipboardEnabled: true,
		TerminalEnabled:  true,
		FilesEnabled:     false,
		VideoEnabled:     true,
		Width:            2560,
		Height:           1440,
		Layout:           LayoutExtend,
	}
}

func settings1080p() Settings {
	s := defaultSettings()
	s.Width, s.Height = 1920, 1080
	return s
}

// harness is one session on a fake platform, with the sender's side.
type harness struct {
	t *testing.T
	f *fakePlatform
	v *fakeVideo
	s *Session
	// videoFlags are the codec_flags of registerVideo.
	videoFlags uint32
	// captureUS is the capture time of sendFrameAfterWait's last frame.
	captureUS uint64
}

func newHarness(t *testing.T, set hookSet, settings Settings) *harness {
	t.Helper()
	f := newFakePlatform(t)
	h := &harness{t: t, f: f, videoFlags: 1, captureUS: 2000000}
	if set == hooksVideo || set == hooksVideoBare {
		h.v = newFakeVideo(t, f)
	}
	h.s = New(platformFor(set, f, h.v), settings)
	return h
}

func (h *harness) feed(data []byte) {
	h.t.Helper()
	if used := h.s.Receive(data); used != len(data) {
		h.t.Fatalf("Receive took %d of %d bytes", used, len(data))
	}
}

// feedChunked feeds in pieces of chunk bytes, as USB transfers split it.
func (h *harness) feedChunked(data []byte, chunk int) {
	h.t.Helper()
	for off := 0; off < len(data); off += chunk {
		h.feed(data[off:min(off+chunk, len(data))])
	}
}

func (h *harness) send(service, kind uint16, parts ...[]byte) {
	h.t.Helper()
	h.feed(wireMsg(service, kind, parts...))
}

func (h *harness) ping() {
	h.t.Helper()
	h.send(svcSession, kPing)
}

func (h *harness) tick() { h.s.Tick() }

// usbStatus checks the fixed fields of a RESET or STATUS answer.
func (h *harness) usbStatus(status [USBStatusLen]byte) (Phase, [16]byte) {
	h.t.Helper()
	if string(status[:4]) != "JKV5" || status[5] != 0 || status[6] != 0 || status[7] != 0 {
		h.t.Fatalf("status % x", status)
	}
	var token [16]byte
	copy(token[:], status[8:])
	return Phase(status[4]), token
}

func (h *harness) phase() Phase {
	h.t.Helper()
	p, _ := h.usbStatus(h.s.USBStatus())
	return p
}

// resetAndAdmit is the sender's reset exchange: RESET, STATUS while the
// old transfers drain, STATUS Ready.
func (h *harness) resetAndAdmit() [16]byte {
	h.t.Helper()
	phase, token := h.usbStatus(h.s.USBReset())
	if phase != 0 || token == ([16]byte{}) {
		h.t.Fatalf("RESET answered phase %d token %x", phase, token)
	}
	if p, again := h.usbStatus(h.s.USBStatus()); p != 0 || again != token {
		h.t.Fatalf("STATUS before the drain: phase %d token %x", p, again)
	}
	h.s.TransportDrained()
	if p, again := h.usbStatus(h.s.USBStatus()); p != 1 || again != token {
		h.t.Fatalf("STATUS after the drain: phase %d token %x", p, again)
	}
	return token
}

func makeHello(major, minor uint16, token [16]byte, nonceSeed byte) []byte {
	hello := cat(le16(major), le16(minor), token[:])
	for i := 0; i < 32; i++ {
		hello = append(hello, nonceSeed+byte(i*13))
	}
	return hello
}

// checkReady is what the sender does with session.ready (KVM approval).
func (h *harness) checkReady(m sentMessage, hello []byte, token [16]byte) {
	h.t.Helper()
	if m.service != svcSession || m.kind != kReady || len(m.payload) != specReadyLen {
		h.t.Fatalf("session.ready: service %d kind %d, %d bytes", m.service, m.kind, len(m.payload))
	}
	p := m.payload
	if wle.Uint16(p) != 5 || wle.Uint16(p[2:]) != 0 {
		h.t.Fatalf("session.ready version %d.%d", wle.Uint16(p), wle.Uint16(p[2:]))
	}
	if !bytes.Equal(p[4:20], token[:]) {
		h.t.Fatalf("session.ready token % x, want % x", p[4:20], token)
	}
	if !bytes.Equal(p[20:52], h.f.pub) {
		h.t.Fatalf("session.ready key is not the KVM's")
	}
	signed := cat([]byte(specContext), hello, p[:specReadySigned])
	if !ed25519.Verify(p[20:52], signed, p[specReadySigned:]) {
		h.t.Fatalf("session.ready signature does not verify")
	}
	// A signature for another hello (another nonce) must not verify.
	signed[len(specContext)+20] ^= 1
	if ed25519.Verify(p[20:52], signed, p[specReadySigned:]) {
		h.t.Fatalf("session.ready signature verifies for another nonce")
	}
}

// checkConfig reads session.config as the sender does.
func (h *harness) checkConfig(m sentMessage, terminal, files bool, display string) {
	h.t.Helper()
	if m.service != svcSession || m.kind != kConfig {
		h.t.Fatalf("got service %d kind %d, want session.config", m.service, m.kind)
	}
	var config map[string]any
	if err := json.Unmarshal(m.payload, &config); err != nil {
		h.t.Fatalf("session.config %q: %v", m.payload, err)
	}
	t, tok := config["terminal_enabled"].(bool)
	f, fok := config["files_enabled"].(bool)
	d, dok := config["capture_display"].(string)
	if !tok || !fok || !dok || t != terminal || f != files || d != display {
		h.t.Fatalf("session.config %s, want terminal %v files %v display %q", m.payload, terminal, files, display)
	}
	// The KVM leaves the display's mode to a sender that says it sets it.
	if mode, ok := config["display_mode_by_sender"].(bool); !ok || !mode {
		h.t.Fatalf("session.config %s without display_mode_by_sender true", m.payload)
	}
}

// verifyReady checks the session.ready signature over the hello.
func verifyReady(ready, hello []byte) bool {
	signed := cat([]byte(specContext), hello, ready[:specReadySigned])
	return ed25519.Verify(ready[20:52], signed, ready[specReadySigned:])
}

// openSession: reset, hello, ready and config; nothing else written.
func (h *harness) openSession() [16]byte {
	h.t.Helper()
	token := h.resetAndAdmit()
	hello := makeHello(5, 0, token, 3)
	h.send(svcSession, kHello, hello)
	if !h.s.Open() {
		h.t.Fatalf("no session after the hello")
	}
	h.checkReady(h.expect(svcSession, kReady), hello, token)
	settings := h.s.Settings()
	h.checkConfig(h.expect(svcSession, kConfig), settings.TerminalEnabled, settings.FilesEnabled, settings.CaptureDisplay)
	h.expectNone()
	return token
}

func (h *harness) next() (sentMessage, bool) { return h.f.next() }

func (h *harness) expect(service, kind uint16) sentMessage {
	h.t.Helper()
	m, ok := h.f.next()
	if !ok {
		h.t.Fatalf("no message, want service %d kind %d", service, kind)
	}
	if m.service != service || m.kind != kind {
		h.t.Fatalf("got service %d kind %d (% x), want service %d kind %d", m.service, m.kind, trim(m.payload), service, kind)
	}
	return m
}

func (h *harness) expectNone() {
	h.t.Helper()
	if m, ok := h.f.next(); ok {
		h.t.Fatalf("unexpected message: service %d kind %d (% x)", m.service, m.kind, trim(m.payload))
	}
}

func trim(p []byte) []byte {
	if len(p) > 32 {
		return p[:32]
	}
	return p
}

func (h *harness) registerService(service uint16, flags uint32) {
	h.t.Helper()
	h.send(service, 1, le32(flags))
}

func (h *harness) registerClipboard(flags uint32, os byte) {
	h.t.Helper()
	h.send(svcClipboard, kClipCaps, le32(flags), []byte{os, 0, 0, 0})
}

func (h *harness) expectClipboardConfig(enabled byte) {
	h.t.Helper()
	m := h.expect(svcClipboard, kClipConfig)
	if len(m.payload) != 1 || m.payload[0] != enabled {
		h.t.Fatalf("clipboard.config % x, want %d", m.payload, enabled)
	}
}

// openServices is an open session with the given policy, the shell and file
// reception registered, and nothing written since session.config.
func openServices(t *testing.T, terminal, files bool) *harness {
	t.Helper()
	settings := defaultSettings()
	settings.TerminalEnabled = terminal
	settings.FilesEnabled = files
	h := newHarness(t, hooksServices, settings)
	h.openSession()
	h.registerService(svcTerminal, 1)
	h.registerService(svcFile, 1)
	// Neither registration is answered (Service registration).
	h.expectNone()
	return h
}
