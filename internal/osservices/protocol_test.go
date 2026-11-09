package osservices

// A sender written from PROTOCOL.md against the session, ported from the
// Mini's tests/test_os_services_protocol.py. Nothing on the sender's side
// comes from the package: the encodings follow the specification, the
// session.ready signature is checked with crypto/ed25519 over bytes the
// sender assembles, and the fingerprint is computed with crypto/sha256. The
// randomized parts send valid and invalid messages in random USB chunkings
// and check the invariants the specification states.
//
// guestSender adds the terminal and file services of the sender, and
// browserModel drives the KVM's side of them (the browser's terminal
// channel and file drops) and checks what the KVM reports back.
//
// videoSender adds the video service: it applies video.config, reports
// video.status, and streams H.264 access units. The test plays the KVM's
// display and slot through fakeVideo.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// The sender's credit window (File service).
const protoWindow = 512 * 1024

// The lines the browser shows; they are the KVM's, not the protocol's.
const (
	lineExited   = "The shell exited"
	lineLost     = "The target computer lost its shell"
	lineDisabled = "Terminal disabled"
	lineBusy     = "Terminal unavailable: the target computer already runs its maximum number of terminals"
	lineNoAnswer = "Terminal unavailable: the target computer did not answer"
	lineSlow     = "Terminal closed: the browser did not keep up"
	noTerminal   = "no OS Services terminal is connected"
	noFiles      = "no OS Services file transfer is connected"
	dropBusy     = "a file transfer is already in progress"
	// How long shell output waits for the browser.
	terminalHoldMS = 2000
)

// Values from the tables of the Terminal and File services.
const (
	doneOK, doneFailed, doneCancelled       = 0, 1, 2
	exitExited, exitClosed, exitFailed      = 0, 1, 2
	openedOK, openedUnsupported, openedBusy = 0, 1, 2
)

// kvmEnd is the KVM end of the USB function, as the sender sees it.
type kvmEnd struct {
	t       *testing.T
	f       *fakePlatform
	v       *fakeVideo
	s       *Session
	pending []byte
	// The hook calls the browser has read.
	hooksRead int
}

func newKVMEnd(t *testing.T, seed uint64, video bool) *kvmEnd {
	t.Helper()
	f := newFakePlatform(t)
	f.now = 1000
	f.random = seed | 1
	keySeed := make([]byte, ed25519.SeedSize)
	for i := range keySeed {
		keySeed[i] = byte(i)
	}
	f.key = ed25519.NewKeyFromSeed(keySeed)
	f.pub = f.key.Public().(ed25519.PublicKey)
	k := &kvmEnd{t: t, f: f}
	set := hooksServices
	if video {
		k.v = newFakeVideo(t, f)
		set = hooksVideo
	}
	k.s = New(platformFor(set, f, k.v), Settings{
		ClipboardEnabled: true, TerminalEnabled: true, FilesEnabled: true, VideoEnabled: true,
		Width: 1920, Height: 1080, Layout: LayoutExtend,
	})
	return k
}

func (k *kvmEnd) advance(ms int64) {
	k.f.now += ms
	k.s.Tick()
}

// control answers the vendor requests; the transport finishes reaping the
// old transfers between two STATUS polls.
func (k *kvmEnd) control(request byte) [24]byte {
	if request == 0x40 {
		return k.s.USBReset()
	}
	status := k.s.USBStatus()
	if k.f.drainPending {
		k.f.drainPending = false
		k.s.TransportDrained()
	}
	return status
}

// write is bulk OUT in transfers of chunk bytes (0: one transfer). It
// returns the bytes the KVM consumed.
func (k *kvmEnd) write(data []byte, chunk int) int {
	step := chunk
	if step == 0 {
		step = max(len(data), 1)
	}
	used := 0
	for used < len(data) {
		part := data[used:min(used+step, len(data))]
		n := k.s.Receive(part)
		used += n
		if n < len(part) {
			break
		}
	}
	return used
}

// read is bulk IN: everything the KVM queued.
func (k *kvmEnd) read() []byte {
	k.pending = append(k.pending, k.f.out...)
	k.f.clearOutput()
	return k.pending
}

func (k *kvmEnd) messages() []sentMessage {
	data := k.read()
	var out []sentMessage
	for len(data) >= 8 {
		size := int(wle.Uint32(data[4:]))
		if len(data) < 8+size {
			break
		}
		out = append(out, sentMessage{wle.Uint16(data), wle.Uint16(data[2:]), append([]byte(nil), data[8:8+size]...)})
		data = data[8+size:]
	}
	k.pending = append(k.pending[:0], data...)
	return out
}

func (k *kvmEnd) isOpen() bool { return k.s.Open() }

// protoSender is the sender side: reset, handshake and approval.
type protoSender struct {
	t           *testing.T
	k           *kvmEnd
	rng         *rand.Rand
	approved    [][]byte
	fingerprint []byte
	config      map[string]any
}

func newProtoSender(t *testing.T, k *kvmEnd, seed uint64) *protoSender {
	return &protoSender{t: t, k: k, rng: rand.New(rand.NewPCG(seed, 7))}
}

func (s *protoSender) reset() [16]byte {
	s.t.Helper()
	status := s.k.control(0x40)
	if string(status[:4]) != "JKV5" || status[4] != 0 || status[5]|status[6]|status[7] != 0 {
		s.t.Fatalf("RESET answered % x", status)
	}
	var token [16]byte
	copy(token[:], status[8:])
	if token == ([16]byte{}) {
		s.t.Fatalf("RESET token is zero")
	}
	for i := 0; i < 100; i++ {
		status = s.k.control(0x41)
		if !bytes.Equal(status[8:], token[:]) {
			s.t.Fatalf("another RESET replaced this one")
		}
		if status[4] > 1 {
			s.t.Fatalf("STATUS phase %d", status[4])
		}
		if status[4] == 1 {
			return token
		}
	}
	s.t.Fatalf("STATUS never reached Ready")
	return token
}

func randomBytes(rng *rand.Rand, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Uint32())
	}
	return b
}

// negotiate resets and opens a session; nil when the KVM is not approved.
func (s *protoSender) negotiate(chunk int, stale []byte) map[string]any {
	s.t.Helper()
	token := s.reset()
	hello := cat(le16(5), le16(0), token[:], randomBytes(s.rng, 32))
	stream := cat(stale, wireMsg(svcSession, kHello, hello))
	if n := s.k.write(stream, chunk); n != len(stream) {
		s.t.Fatalf("the KVM took %d of %d bytes of the hello", n, len(stream))
	}
	// The sender searches the IN stream for session.ready with its token.
	data := s.k.read()
	if len(data) > 128*1024 {
		s.t.Fatalf("%d bytes before session.ready", len(data))
	}
	var ready []byte
	for len(data) >= 8 {
		size := int(wle.Uint32(data[4:]))
		payload := data[8:min(8+size, len(data))]
		service, kind := wle.Uint16(data), wle.Uint16(data[2:])
		data = data[min(8+size, len(data)):]
		if service == svcSession && kind == kReady && len(payload) >= 20 && bytes.Equal(payload[4:20], token[:]) {
			ready = payload
			break
		}
	}
	if ready == nil {
		s.t.Fatalf("no session.ready with the token")
	}
	s.k.pending = append([]byte(nil), data...)
	if len(ready) != 116 || wle.Uint16(ready) != 5 || wle.Uint16(ready[2:]) != 0 {
		s.t.Fatalf("session.ready of %d bytes, version %d.%d", len(ready), wle.Uint16(ready), wle.Uint16(ready[2:]))
	}
	publicKey := ready[20:52]
	if !ed25519.Verify(publicKey, cat([]byte(specContext), hello, ready[:52]), ready[52:116]) {
		s.t.Fatalf("session.ready signature is invalid")
	}
	digest := sha256.Sum256(publicKey)
	fingerprint := digest[:8]
	if s.approved != nil && !slices.ContainsFunc(s.approved, func(f []byte) bool { return bytes.Equal(f, fingerprint) }) {
		s.k.write(wireMsg(svcSession, kClose, []byte{3}), 0)
		return nil
	}
	got := s.k.messages()
	if len(got) == 0 || got[0].service != svcSession || got[0].kind != kConfig {
		s.t.Fatalf("no session.config after session.ready")
	}
	s.config = s.parseConfig(got[0].payload)
	s.fingerprint = fingerprint
	return s.config
}

func (s *protoSender) parseConfig(payload []byte) map[string]any {
	s.t.Helper()
	var config map[string]any
	if err := json.Unmarshal(payload, &config); err != nil {
		s.t.Fatalf("session.config %q: %v", payload, err)
	}
	for member, kind := range map[string]string{"terminal_enabled": "bool", "files_enabled": "bool", "capture_display": "string"} {
		v, ok := config[member]
		if !ok || fmt.Sprintf("%T", v) != kind {
			s.t.Fatalf("session.config %s: %s is not a %s", payload, member, kind)
		}
	}
	return config
}

func clipboardCaps(flags uint32, os byte) []byte {
	return wireMsg(svcClipboard, kClipCaps, le32(flags), []byte{os, 0, 0, 0})
}

// validFileName is the File service rule: one path component, not empty,
// not . or .., no / or \, no control character.
func validFileName(name []byte) bool {
	if !utf8.Valid(name) {
		return false
	}
	text := string(name)
	if text == "" || text == "." || text == ".." {
		return false
	}
	for _, r := range text {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func TestProtocolHandshakeInEveryChunking(t *testing.T) {
	k := newKVMEnd(t, 11, false)
	sender := newProtoSender(t, k, 1)
	for _, chunk := range []int{1, 2, 3, 7, 8, 9, 59, 60, 61, 512, 16384} {
		config := sender.negotiate(chunk, nil)
		if config["capture_display"] != "" || !k.isOpen() {
			t.Fatalf("chunk %d: %v open %v", chunk, config, k.isOpen())
		}
	}
}

func TestProtocolFingerprintIsSHA256PrefixOfTheKey(t *testing.T) {
	k := newKVMEnd(t, 12, false)
	sender := newProtoSender(t, k, 2)
	sender.negotiate(0, nil)
	digest := sha256.Sum256(k.f.pub)
	if !bytes.Equal(sender.fingerprint, digest[:8]) {
		t.Fatalf("fingerprint % x", sender.fingerprint)
	}
	var text []string
	for _, b := range sender.fingerprint {
		text = append(text, fmt.Sprintf("%02X", b))
	}
	if got := FormatFingerprint(digest[:]); got != strings.Join(text, ":") {
		t.Fatalf("FormatFingerprint %q", got)
	}
}

func TestProtocolStaleBytesOfTheLastSession(t *testing.T) {
	k := newKVMEnd(t, 13, false)
	sender := newProtoSender(t, k, 3)
	if sender.negotiate(0, nil) == nil {
		t.Fatalf("no session")
	}
	// A sender killed mid-frame leaves a frame head and its body in the OUT
	// endpoint; an old hello carries an old token.
	stale := wireMsg(svcVideo, kVideoFrame, randomBytes(sender.rng, 3000))[:2000]
	stale = append(stale, wireMsg(svcSession, kHello, le16(5), le16(0), randomBytes(sender.rng, 48))...)
	sender.negotiate(4096, stale)
	if !k.isOpen() {
		t.Fatalf("no session after stale bytes")
	}
}

func TestProtocolNotApproved(t *testing.T) {
	k := newKVMEnd(t, 14, false)
	sender := newProtoSender(t, k, 4)
	sender.approved = [][]byte{make([]byte, 8)}
	if sender.negotiate(0, nil) != nil || k.isOpen() || !k.s.State().ApprovalPending {
		t.Fatalf("not approved: open %v pending %v", k.isOpen(), k.s.State().ApprovalPending)
	}
	// The user approves the fingerprint; the next session clears it.
	newProtoSender(t, k, 5).negotiate(0, nil)
	k.write(wireMsg(svcSession, kInfo, []byte(`{"version":"1","displays":[]}`)), 0)
	if k.s.State().ApprovalPending {
		t.Fatalf("still pending")
	}
}

func TestProtocolLivenessWithPings(t *testing.T) {
	k := newKVMEnd(t, 15, false)
	newProtoSender(t, k, 6).negotiate(0, nil)
	for i := 0; i < 20; i++ {
		k.advance(2000)
		k.write(wireMsg(svcSession, kPing), 0)
	}
	if !k.isOpen() {
		t.Fatalf("pings did not keep the session")
	}
	k.advance(15001)
	if k.isOpen() {
		t.Fatalf("open after 15 s of silence")
	}
	// The KVM stopped reading: a write consumes nothing.
	if n := k.write(wireMsg(svcSession, kPing), 0); n != 0 {
		t.Fatalf("a write after the end took %d bytes", n)
	}
}

func TestProtocolClipboardRoundTrip(t *testing.T) {
	k := newKVMEnd(t, 16, false)
	newProtoSender(t, k, 7).negotiate(0, nil)
	k.write(clipboardCaps(3, 3), 0)
	if got := k.messages(); len(got) != 1 || !reflect.DeepEqual(got[0], sentMessage{svcClipboard, kClipConfig, []byte{1}}) {
		t.Fatalf("answer %+v", got)
	}
	text := []byte("Grüße 👋\n\ttabs")
	setID, _ := k.s.ClipboardRequest(text, true)
	getID, _ := k.s.ClipboardRequest(nil, false)
	if setID == 0 || getID == 0 {
		t.Fatalf("requests %d %d", setID, getID)
	}
	requests := k.messages()
	if len(requests) != 2 || !reflect.DeepEqual(requests[0], sentMessage{svcClipboard, kClipSet, cat(le32(setID), text)}) ||
		!reflect.DeepEqual(requests[1], sentMessage{svcClipboard, kClipGet, le32(getID)}) {
		t.Fatalf("requests %+v", requests)
	}
	// Answers in the other order.
	k.write(wireMsg(svcClipboard, kClipContent, le32(getID), []byte{0, 1, 0, 0}, text), 0)
	k.write(wireMsg(svcClipboard, kClipStatus, le32(setID), []byte{0}), 0)
	want := []clipEvent{{getID, ClipboardOK, true, string(text), true}, {setID, ClipboardOK, true, "", false}}
	if !reflect.DeepEqual(k.f.clipboard, want) {
		t.Fatalf("events %+v", k.f.clipboard)
	}
}

func TestProtocolSettingsChangesReachTheSender(t *testing.T) {
	k := newKVMEnd(t, 17, false)
	newProtoSender(t, k, 8).negotiate(0, nil)
	k.write(clipboardCaps(3, 3), 0)
	k.messages()
	settings := k.s.Settings()
	settings.ClipboardEnabled = false
	settings.FilesEnabled = false
	settings.CaptureDisplay = "HDMI-A-1"
	k.s.ApplySettings(settings)
	got := k.messages()
	var config map[string]any
	sawClipboard := false
	for _, m := range got {
		if m.service == svcSession && m.kind == kConfig {
			if err := json.Unmarshal(m.payload, &config); err != nil {
				t.Fatalf("session.config %q", m.payload)
			}
		}
		sawClipboard = sawClipboard || reflect.DeepEqual(m, sentMessage{svcClipboard, kClipConfig, []byte{0}})
	}
	want := map[string]any{"terminal_enabled": true, "files_enabled": false, "capture_display": "HDMI-A-1", "display_mode_by_sender": true}
	if !reflect.DeepEqual(config, want) || !sawClipboard {
		t.Fatalf("messages %+v", got)
	}
}

func TestProtocolRegistrationsAnsweredAsSpecified(t *testing.T) {
	k := newKVMEnd(t, 18, false)
	newProtoSender(t, k, 9).negotiate(0, nil)
	k.write(wireMsg(svcTerminal, kTermCaps, le32(1)), 0)
	k.write(wireMsg(svcFile, kFileCaps, le32(1)), 0)
	k.write(wireMsg(svcUpdate, kUpdateCaps, le32(1), []byte("linux-x86_64-appimage")), 0)
	if got := k.messages(); len(got) != 0 {
		t.Fatalf("registrations answered: %+v", got)
	}
	k.write(wireMsg(svcVideo, kVideoCaps, le32(1), le32(3840), le32(2160), le32(60)), 0)
	got := k.messages()
	if len(got) != 1 || got[0].service != svcVideo || got[0].kind != kVideoConfig || len(got[0].payload) != 20 {
		t.Fatalf("answer %+v", got)
	}
	p := got[0].payload
	width, height, fps := wle.Uint32(p[4:]), wle.Uint32(p[8:]), wle.Uint32(p[16:])
	if p[0] > 1 || p[1] > 1 || p[2] != 0 || p[3] != 0 || width < 1 || width > 3840 || height < 1 || height > 2160 || fps > 60 {
		t.Fatalf("video.config % x", p)
	}
	st := k.s.State()
	if st.ClipboardFlags != 0 || !st.VideoRegistered || !st.Terminal || !st.FileReceive || !st.UpdateInstall {
		t.Fatalf("registrations %+v", st)
	}
}

// Random mixes of valid, unknown and invalid messages in random chunkings.
// The KVM either keeps the session open, or ends it; it never reads past
// the end of a session.
func TestProtocolRandomizedStreams(t *testing.T) {
	rng := rand.New(rand.NewPCG(1234, 0))
	pick := func(choices ...int) int { return choices[rng.IntN(len(choices))] }
	randint := func(a, b int) int { return a + rng.IntN(b-a+1) }
	var endings, faults uint32
	for round := 0; round < 300; round++ {
		k := newKVMEnd(t, uint64(round+1), false)
		newProtoSender(t, k, uint64(round)).negotiate(pick(1, 5, 64, 512, 4096), nil)
		var stream []byte
		expectEnd := false
		for i := randint(1, 25); i > 0; i-- {
			choice := rng.Float64()
			switch {
			case choice < 0.25:
				stream = append(stream, wireMsg(svcSession, kPing)...)
			case choice < 0.4:
				stream = append(stream, wireMsg(uint16(pick(6, 7, 200, 65535)), uint16(randint(0, 9)), randomBytes(rng, randint(0, 300)))...)
			case choice < 0.55:
				displays := make([]string, randint(0, 64))
				for d := range displays {
					displays[d] = fmt.Sprintf("D%d", d)
				}
				info, _ := json.Marshal(map[string]any{"version": strings.Repeat("v", randint(0, 64)), "displays": displays})
				stream = append(stream, wireMsg(svcSession, kInfo, info)...)
			case choice < 0.65:
				stream = append(stream, clipboardCaps(uint32(randint(0, 3)), byte(randint(0, 3)))...)
			case choice < 0.75:
				// Invalid for a service other than the session: only that
				// service faults.
				stream = append(stream, wireMsg(uint16(pick(svcClipboard, svcTerminal, svcFile, svcVideo)), 1, randomBytes(rng, randint(0, 3)))...)
			case choice < 0.8:
				stream = append(stream, wireMsg(svcVideo, kVideoFrame, randomBytes(rng, randint(0, 70000)))...)
			case choice < 0.85:
				stream = append(stream, wireMsg(svcSession, uint16(pick(kHello, kReady, kConfig)), []byte("x"))...)
				expectEnd = true
			default:
				stream = append(stream, wireMsg(svcTerminal, uint16(randint(2, 8)), randomBytes(rng, randint(0, 64)))...)
			}
			if expectEnd {
				break
			}
		}
		used := k.write(stream, pick(1, 3, 64, 1000, 65536))
		if k.isOpen() == expectEnd || used != len(stream) {
			t.Fatalf("round %d: open %v (end expected %v), used %d of %d, reason %q", round, k.isOpen(), expectEnd,
				used, len(stream), k.f.endReason)
		}
		want := 8
		if expectEnd {
			want = 0
		}
		if n := k.write(wireMsg(svcSession, kPing), 0); n != want {
			t.Fatalf("round %d: a ping after the stream took %d bytes", round, n)
		}
		if expectEnd {
			endings++
		}
		faults += k.s.Counters.ServiceFaults
	}
	if endings == 0 || endings == 300 || faults == 0 {
		t.Fatalf("%d of 300 rounds ended the session, %d service faults", endings, faults)
	}
}

// --- The terminal and file services ----------------------------------------

type guestShell struct {
	rows, cols uint16
	input      []byte
}

type guestTransfer struct {
	id                         uint32
	size                       uint64
	name                       string
	credit, received, unreturn uint64
	data                       []byte
}

// guestSender is the sender's terminal and file services, from the OS
// Services protocol. It checks every KVM message as strictly as the
// specification allows: a message it does not allow fails the test, where
// a real sender would end the session. Shells echo their input. Files go
// to a model Downloads folder, one at a time in drop order, under a 512
// KiB credit window returned every step bytes.
type guestSender struct {
	*protoSender
	maxTerminals  int
	answerOpens   bool
	step          uint64
	echo          bool
	shell         bool
	receive       bool
	registrations map[uint16]int
	shells        map[uint32]*guestShell
	terminalIDs   map[uint32]bool
	outputSent    map[uint32][]byte
	transfers     map[uint32]*guestTransfer
	queue         []*guestTransfer
	active        *guestTransfer
	ended         map[uint32]byte
	downloads     map[string][]byte
}

func newGuestSender(p *protoSender) *guestSender {
	return &guestSender{
		protoSender:   p,
		maxTerminals:  4,
		answerOpens:   true,
		step:          128 * 1024,
		echo:          true,
		registrations: map[uint16]int{svcTerminal: 0, svcFile: 0},
		shells:        map[uint32]*guestShell{},
		terminalIDs:   map[uint32]bool{},
		outputSent:    map[uint32][]byte{},
		transfers:     map[uint32]*guestTransfer{},
		ended:         map[uint32]byte{},
		downloads:     map[string][]byte{},
	}
}

func (g *guestSender) assertion(ok bool, format string, args ...any) {
	g.t.Helper()
	if !ok {
		g.t.Fatalf("the sender would end the session: "+format, args...)
	}
}

func (g *guestSender) write(data []byte) {
	g.t.Helper()
	if !g.k.isOpen() {
		return // The KVM stopped reading; nothing arrives.
	}
	g.assertion(g.k.write(data, 0) == len(data), "the KVM stopped reading")
}

func (g *guestSender) start() map[string]any {
	g.t.Helper()
	config := g.negotiate(0, nil)
	g.applyConfig(config)
	g.pump()
	return config
}

// pump reads and answers what the KVM wrote until it writes nothing.
func (g *guestSender) pump() {
	g.t.Helper()
	for i := 0; i < 10000; i++ {
		got := g.k.messages()
		if len(got) == 0 {
			return
		}
		for _, m := range got {
			switch m.service {
			case svcSession:
				g.assertion(m.kind == kConfig, "session kind %d from the KVM", m.kind)
				g.applyConfig(g.parseConfig(m.payload))
			case svcTerminal:
				g.handleTerminal(m.kind, m.payload)
			case svcFile:
				g.handleFile(m.kind, m.payload)
			default:
				g.t.Fatalf("service %d: never registered", m.service)
			}
		}
	}
	g.t.Fatalf("the KVM never stops writing")
}

func (g *guestSender) register(service uint16, flags uint32) {
	g.t.Helper()
	g.registrations[service]++
	g.write(wireMsg(service, 1, le32(flags)))
}

// applyConfig: a flag that becomes false ends the service's activity, then
// withdraws it; one that becomes true registers it.
func (g *guestSender) applyConfig(config map[string]any) {
	g.t.Helper()
	g.config = config
	terminal, files := config["terminal_enabled"].(bool), config["files_enabled"].(bool)
	if terminal && !g.shell {
		g.shell = true
		g.register(svcTerminal, 1)
	} else if !terminal && g.shell {
		for _, id := range sortedKeys(g.shells) {
			g.exit(id, exitClosed)
		}
		g.shell = false
		g.register(svcTerminal, 0)
	}
	if files && !g.receive {
		g.receive = true
		g.register(svcFile, 1)
	} else if !files && g.receive {
		if g.active != nil {
			g.finish(g.active, doneCancelled)
		}
		for _, t := range g.queue {
			g.finish(t, doneCancelled)
		}
		g.queue = nil
		g.receive = false
		g.register(svcFile, 0)
	}
}

func sortedKeys[V any](m map[uint32]V) []uint32 {
	keys := make([]uint32, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func (g *guestSender) handleTerminal(kind uint16, p []byte) {
	g.t.Helper()
	switch kind {
	case kTermOpen:
		g.assertion(len(p) == 8, "terminal.open size")
		id, rows, cols := wle.Uint32(p), wle.Uint16(p[4:]), wle.Uint16(p[6:])
		g.assertion(id != 0, "terminal.open with session_id 0")
		g.assertion(!g.terminalIDs[id], "a session_id used again")
		g.assertion(rows == 0 && cols == 0, "the KVM opens with size 0 by 0")
		g.terminalIDs[id] = true
		g.outputSent[id] = nil
		if !g.answerOpens {
			return
		}
		status := byte(openedOK)
		switch {
		case !g.shell:
			status = openedUnsupported
		case len(g.shells) >= g.maxTerminals:
			status = openedBusy
		default:
			g.shells[id] = &guestShell{rows: 24, cols: 80}
		}
		g.write(wireMsg(svcTerminal, kTermOpened, le32(id), []byte{status}))
	case kTermInput:
		g.assertion(len(p) >= 5 && len(p) <= 4+specTerminalData, "terminal.input size")
		id := wle.Uint32(p)
		g.assertion(g.terminalIDs[id], "input for a session never opened")
		if shell := g.shells[id]; shell != nil {
			shell.input = append(shell.input, p[4:]...)
			if g.echo {
				g.output(id, p[4:])
			}
		}
	case kTermResize:
		g.assertion(len(p) == 8, "terminal.resize size")
		id, rows, cols := wle.Uint32(p), wle.Uint16(p[4:]), wle.Uint16(p[6:])
		g.assertion(rows != 0 && cols != 0, "terminal.resize to zero")
		g.assertion(g.terminalIDs[id], "resize of a session never opened")
		if shell := g.shells[id]; shell != nil {
			shell.rows, shell.cols = rows, cols
		}
	case kTermClose:
		g.assertion(len(p) == 4, "terminal.close size")
		id := wle.Uint32(p)
		g.assertion(g.terminalIDs[id], "close of a session never opened")
		if g.shells[id] != nil {
			g.exit(id, exitClosed)
		}
	case kTermCredit:
		g.assertion(len(p) == 8, "terminal.credit size")
		g.assertion(g.terminalIDs[wle.Uint32(p)], "credit for a session never opened")
	default:
		g.t.Fatalf("terminal kind %d from the KVM", kind)
	}
}

// output is what the shell prints, in messages of at most 65,532 bytes.
func (g *guestSender) output(id uint32, data []byte) {
	g.t.Helper()
	for off := 0; off < len(data); off += specTerminalData {
		piece := data[off:min(off+specTerminalData, len(data))]
		g.outputSent[id] = append(g.outputSent[id], piece...)
		g.write(wireMsg(svcTerminal, kTermOutput, le32(id), piece))
	}
}

func (g *guestSender) exit(id uint32, reason byte) {
	g.t.Helper()
	delete(g.shells, id)
	g.write(wireMsg(svcTerminal, kTermExited, le32(id), []byte{reason}))
}

func (g *guestSender) handleFile(kind uint16, p []byte) {
	g.t.Helper()
	switch kind {
	case kFileDrop:
		g.handleDrop(p)
	case kFileChunk:
		g.assertion(len(p) >= 12 && len(p) <= 12+specChunkData, "file.chunk size")
		id, offset, data := wle.Uint32(p), wle.Uint64(p[4:]), p[12:]
		// Not a rule of the specification, but the KVM never needs one.
		g.assertion(len(data) > 0, "an empty file.chunk")
		t := g.transfers[id]
		g.assertion(t != nil, "a chunk for an unknown transfer")
		if _, ended := g.ended[id]; ended {
			return // The sender ignores a chunk for a transfer that ended.
		}
		g.assertion(t == g.active, "a chunk before file.ready (no credit)")
		g.assertion(offset == t.received, "a chunk offset out of order")
		end := offset + uint64(len(data))
		g.assertion(end <= t.credit, "a chunk beyond the credit")
		g.assertion(end <= t.size, "a chunk beyond the file size")
		t.data = append(t.data, data...)
		t.received = end
		t.unreturn += uint64(len(data))
		if end == t.size {
			g.finish(t, doneOK)
			g.nextTransfer()
		} else if t.unreturn >= g.step {
			g.grant(t, t.unreturn)
			t.unreturn = 0
		}
	case kFileCancel:
		g.assertion(len(p) == 4, "file.cancel size")
		id := wle.Uint32(p)
		t := g.transfers[id]
		g.assertion(t != nil, "a cancel for an unknown transfer")
		if _, ended := g.ended[id]; ended {
			return
		}
		if t == g.active {
			g.finish(t, doneCancelled) // deletes the partial file
			g.nextTransfer()
		} else {
			g.queue = slices.DeleteFunc(g.queue, func(q *guestTransfer) bool { return q == t })
			g.finish(t, doneCancelled)
		}
	default:
		g.t.Fatalf("file kind %d from the KVM", kind)
	}
}

func (g *guestSender) handleDrop(p []byte) {
	g.t.Helper()
	g.assertion(len(p) >= 4, "file.drop size")
	count := int(wle.Uint32(p))
	g.assertion(count >= 1, "file.drop without files")
	o := 4
	var entries []*guestTransfer
	for i := 0; i < count; i++ {
		g.assertion(o+14 <= len(p), "a file.drop entry cut short")
		id, size, nameLength := wle.Uint32(p[o:]), wle.Uint64(p[o+4:]), int(wle.Uint16(p[o+12:]))
		o += 14
		g.assertion(o+nameLength <= len(p), "a file name cut short")
		name := p[o : o+nameLength]
		o += nameLength
		g.assertion(id != 0, "transfer_id 0")
		g.assertion(g.transfers[id] == nil && !slices.ContainsFunc(entries, func(e *guestTransfer) bool { return e.id == id }),
			"a transfer_id used again")
		g.assertion(validFileName(name), "file name %q", name)
		entries = append(entries, &guestTransfer{id: id, size: size, name: string(name)})
	}
	g.assertion(o == len(p), "bytes after the last file.drop entry")
	for _, e := range entries {
		g.transfers[e.id] = e
	}
	if !g.receive || !g.config["files_enabled"].(bool) {
		for _, e := range entries {
			g.finish(e, doneFailed)
		}
		return
	}
	g.queue = append(g.queue, entries...)
	g.nextTransfer()
}

// nextTransfer: one file at a time; file.ready when it is created, and for
// an empty file file.done at once.
func (g *guestSender) nextTransfer() {
	g.t.Helper()
	g.active = nil
	for len(g.queue) > 0 {
		t := g.queue[0]
		g.queue = g.queue[1:]
		t.credit = protoWindow
		g.write(wireMsg(svcFile, kFileReady, le32(t.id), le32(protoWindow)))
		if t.size > 0 {
			g.active = t
			return
		}
		g.finish(t, doneOK)
	}
}

func (g *guestSender) grant(t *guestTransfer, amount uint64) {
	g.t.Helper()
	t.credit += amount
	g.write(wireMsg(svcFile, kFileCredit, le32(t.id), le32(uint32(amount))))
}

func (g *guestSender) finish(t *guestTransfer, status byte) {
	g.t.Helper()
	var path []byte
	if status == doneOK {
		stem, extension := t.name, ""
		if dot := strings.LastIndexByte(t.name, '.'); dot >= 0 {
			stem, extension = t.name[:dot], t.name[dot:]
		}
		p := "/home/guest/Downloads/" + t.name
		for copies := 2; ; copies++ {
			if _, used := g.downloads[p]; !used {
				break
			}
			p = fmt.Sprintf("/home/guest/Downloads/%s (%d)%s", stem, copies, extension)
		}
		g.downloads[p] = append([]byte(nil), t.data...)
		path = []byte(p)
	}
	if t == g.active {
		g.active = nil
	}
	g.ended[t.id] = status
	g.write(wireMsg(svcFile, kFileDone, le32(t.id), []byte{status, 0, 0, 0}, path))
}

type closedLine struct {
	line   string
	exited bool
}

type doneOutcome struct {
	status FileStatus
	path   string
	last   bool
}

// browserModel is the browser's side of the KVM: terminal channels and file
// drops. poll collects what the KVM tells the browser and checks it: output
// only for open terminals, one end per terminal, credit and one outcome per
// transfer, last on a drop's final outcome.
type browserModel struct {
	t               *testing.T
	k               *kvmEnd
	opened          []uint32
	closedByBrowser map[uint32]bool
	output          map[uint32][]byte
	input           map[uint32][]byte
	closed          map[uint32]closedLine
	drops           map[uint32][]uint32
	dropOf          map[uint32]uint32
	size            map[uint32]uint64
	attached        map[uint32]bool
	channelClosed   map[uint32]bool
	credit          map[uint32]uint64
	sent            map[uint32]uint64
	done            map[uint32]doneOutcome
	order           []uint32
}

func newBrowserModel(t *testing.T, k *kvmEnd) *browserModel {
	return &browserModel{
		t: t, k: k,
		closedByBrowser: map[uint32]bool{}, output: map[uint32][]byte{}, input: map[uint32][]byte{},
		closed: map[uint32]closedLine{}, drops: map[uint32][]uint32{}, dropOf: map[uint32]uint32{},
		size: map[uint32]uint64{}, attached: map[uint32]bool{}, channelClosed: map[uint32]bool{},
		credit: map[uint32]uint64{}, sent: map[uint32]uint64{}, done: map[uint32]doneOutcome{},
	}
}

func (b *browserModel) openTerminal(rows, cols uint32) (uint32, string) {
	b.t.Helper()
	id, refused := b.k.s.TerminalOpen(rows, cols)
	if id != 0 {
		if refused != "" || slices.Contains(b.opened, id) {
			b.t.Fatalf("terminal %d: %q, or an ID used again", id, refused)
		}
		b.opened = append(b.opened, id)
		b.output[id] = nil
		b.input[id] = nil
	} else if refused == "" {
		b.t.Fatalf("no ID and no reason")
	}
	return id, refused
}

func (b *browserModel) typeKeys(id uint32, data []byte) bool {
	accepted := b.k.s.TerminalInput(id, data)
	if accepted {
		b.input[id] = append(b.input[id], data...)
	}
	return accepted
}

func (b *browserModel) closeTerminal(id uint32) {
	if _, ended := b.closed[id]; !ended {
		b.closedByBrowser[id] = true
	}
	b.k.s.TerminalClose(id)
}

type dropFile struct {
	name string
	size uint64
}

func (b *browserModel) drop(files []dropFile) (uint32, []uint32, string) {
	b.t.Helper()
	entries := make([]FileEntry, len(files))
	for i, f := range files {
		entries[i] = FileEntry{f.name, f.size}
	}
	dropID, ids, refused := b.k.s.FileDrop(entries)
	if dropID == 0 {
		if refused == "" {
			b.t.Fatalf("no drop and no reason")
		}
		return 0, nil, refused
	}
	if refused != "" || b.drops[dropID] != nil || len(ids) != len(files) {
		b.t.Fatalf("drop %d: %q %v", dropID, refused, ids)
	}
	for i, id := range ids {
		if _, used := b.dropOf[id]; id == 0 || used || slices.Contains(ids[:i], id) {
			b.t.Fatalf("transfer ID %d used again", id)
		}
		b.dropOf[id] = dropID
		b.size[id] = files[i].size
	}
	b.drops[dropID] = ids
	b.attach(dropID)
	return dropID, ids, ""
}

func (b *browserModel) attach(dropID uint32) bool {
	attached := b.k.s.FileAttach(dropID)
	if attached {
		b.attached[dropID] = true
	}
	return attached
}

func (b *browserModel) room(id uint32) uint64 { return b.credit[id] - b.sent[id] }

func (b *browserModel) send(id uint32, data []byte) bool {
	accepted := b.k.s.FileData(b.dropOf[id], id, data)
	if accepted {
		b.sent[id] += uint64(len(data))
	}
	return accepted
}

func (b *browserModel) cancel(id uint32) bool { return b.k.s.FileCancel(b.dropOf[id], id) }

func (b *browserModel) closeChannel(dropID uint32) {
	b.channelClosed[dropID] = true
	b.k.s.FileChannelClosed(dropID)
}

// poll takes what the KVM told the browser, checks it, and returns it.
func (b *browserModel) poll() []hookEvent {
	b.t.Helper()
	events := b.k.f.hooks[b.k.hooksRead:]
	b.k.hooksRead = len(b.k.f.hooks)
	for _, e := range events {
		switch e.kind {
		case "output":
			_, opened := b.output[e.id]
			_, ended := b.closed[e.id]
			if !opened || ended || b.closedByBrowser[e.id] || len(e.data) == 0 {
				b.t.Fatalf("output for terminal %d: opened %v ended %v closed by the browser %v", e.id, opened, ended, b.closedByBrowser[e.id])
			}
			b.output[e.id] = append(b.output[e.id], e.data...)
		case "closed":
			_, opened := b.output[e.id]
			_, ended := b.closed[e.id]
			if !opened || ended || b.closedByBrowser[e.id] {
				b.t.Fatalf("an end for terminal %d: opened %v ended %v closed by the browser %v", e.id, opened, ended, b.closedByBrowser[e.id])
			}
			b.closed[e.id] = closedLine{e.text, e.exited}
		case "credit":
			_, done := b.done[e.transfer]
			if b.dropOf[e.transfer] != e.id || !b.attached[e.id] || b.channelClosed[e.id] || done || e.bytes == 0 {
				b.t.Fatalf("credit %+v", e)
			}
			b.credit[e.transfer] += e.bytes
		case "done":
			_, done := b.done[e.transfer]
			if b.dropOf[e.transfer] != e.id || !b.attached[e.id] || b.channelClosed[e.id] || done || e.status > FileCancelled {
				b.t.Fatalf("outcome %+v", e)
			}
			if e.status != FileOK && e.text != "" {
				b.t.Fatalf("a path for a transfer that failed: %+v", e)
			}
			b.done[e.transfer] = doneOutcome{e.status, e.text, e.last}
			b.order = append(b.order, e.transfer)
			remaining := slices.ContainsFunc(b.drops[e.id], func(t uint32) bool { _, d := b.done[t]; return !d })
			if e.last == remaining {
				b.t.Fatalf("last on the wrong outcome: %+v", e)
			}
		default:
			b.t.Fatalf("event %+v", e)
		}
	}
	return events
}

func (b *browserModel) openTransfers() []uint32 {
	var out []uint32
	for _, dropID := range sortedKeys(b.drops) {
		if !b.attached[dropID] || b.channelClosed[dropID] {
			continue
		}
		for _, id := range b.drops[dropID] {
			if _, done := b.done[id]; !done {
				out = append(out, id)
			}
		}
	}
	return out
}

type serviceRun struct {
	t       *testing.T
	k       *kvmEnd
	sender  *guestSender
	browser *browserModel
}

func startServices(t *testing.T, seed uint64, configure func(g *guestSender)) *serviceRun {
	t.Helper()
	k := newKVMEnd(t, seed, false)
	g := newGuestSender(newProtoSender(t, k, seed))
	if configure != nil {
		configure(g)
	}
	r := &serviceRun{t: t, k: k, sender: g, browser: newBrowserModel(t, k)}
	config := g.start()
	if !config["terminal_enabled"].(bool) || !config["files_enabled"].(bool) || !k.s.TerminalAvailable() || !k.s.FileAvailable() {
		t.Fatalf("services not offered: %v", config)
	}
	return r
}

func (r *serviceRun) step() []hookEvent {
	r.t.Helper()
	r.sender.pump()
	return r.browser.poll()
}

// advance: time passes; the sender pings so the session stays.
func (r *serviceRun) advance(ms int64) {
	r.t.Helper()
	for ms > 0 {
		n := min(ms, 10000)
		r.k.write(wireMsg(svcSession, kPing), 0)
		r.k.advance(n)
		ms -= n
		r.step()
	}
}

// runDrop: the browser sends each file as its credit allows, frame by
// frame, until every transfer has its outcome.
func (r *serviceRun) runDrop(ids []uint32, contents [][]byte) {
	r.t.Helper()
	const frame = 32 * 1024
	for i := 0; i < 100000; i++ {
		r.step()
		all := true
		for _, id := range ids {
			if _, done := r.browser.done[id]; !done {
				all = false
			}
		}
		if all {
			return
		}
		for j, id := range ids {
			if _, done := r.browser.done[id]; done {
				continue
			}
			sent := r.browser.sent[id]
			n := min(r.browser.room(id), uint64(len(contents[j]))-sent, frame)
			if n > 0 && !r.browser.send(id, contents[j][sent:sent+n]) {
				r.t.Fatalf("FileData refused within the credit")
			}
		}
	}
	r.t.Fatalf("the drop stalled")
}

func TestServicesTerminalRoundTrip(t *testing.T) {
	r := startServices(t, 21, nil)
	var ids []uint32
	for _, size := range [][2]uint32{{24, 80}, {50, 200}, {0, 0}} {
		id, refused := r.browser.openTerminal(size[0], size[1])
		if refused != "" {
			t.Fatalf("open: %q", refused)
		}
		ids = append(ids, id)
	}
	r.step()
	if !reflect.DeepEqual(sortedKeys(r.sender.shells), sorted(ids)) {
		t.Fatalf("shells %v", sortedKeys(r.sender.shells))
	}
	// terminal.open came with 0 by 0 (checked by the sender), then the
	// browser's size; without one the shell keeps 24 by 80.
	for i, want := range [][2]uint16{{24, 80}, {50, 200}, {24, 80}} {
		if s := r.sender.shells[ids[i]]; s.rows != want[0] || s.cols != want[1] {
			t.Fatalf("shell %d at %dx%d", i, s.rows, s.cols)
		}
	}
	// Keystrokes, echoed by the shell; large input crosses messages.
	big := randomBytes(r.sender.rng, 150000)
	if !r.browser.typeKeys(ids[0], []byte("ls -la\r")) || !r.browser.typeKeys(ids[1], big) {
		t.Fatalf("input refused")
	}
	r.step()
	if !bytes.Equal(r.sender.shells[ids[1]].input, big) || string(r.browser.output[ids[0]]) != "ls -la\r" ||
		!bytes.Equal(r.browser.output[ids[1]], big) || len(r.browser.output[ids[2]]) != 0 {
		t.Fatalf("echo lost")
	}
	// Resizes that the protocol allows reach the shell; others do not.
	if !r.k.s.TerminalResize(ids[2], 30, 100) || r.k.s.TerminalResize(ids[2], 0, 100) || r.k.s.TerminalResize(ids[2], 30, 65536) {
		t.Fatalf("resize rules")
	}
	r.step()
	if s := r.sender.shells[ids[2]]; s.rows != 30 || s.cols != 100 {
		t.Fatalf("resize lost")
	}
	// The browser closes one: the shell ends, the KVM reports nothing.
	r.browser.closeTerminal(ids[0])
	r.step()
	if _, ok := r.sender.shells[ids[0]]; ok {
		t.Fatalf("the shell runs on")
	}
	if _, ok := r.browser.closed[ids[0]]; ok || r.browser.typeKeys(ids[0], []byte("x")) {
		t.Fatalf("a closed terminal")
	}
	// The shells end on the guest.
	r.sender.exit(ids[1], exitExited)
	r.sender.exit(ids[2], exitFailed)
	r.step()
	want := map[uint32]closedLine{ids[1]: {lineExited, true}, ids[2]: {lineLost, false}}
	if !reflect.DeepEqual(r.browser.closed, want) || r.k.s.Counters.ServiceFaults != 0 || !r.k.isOpen() {
		t.Fatalf("closed %v", r.browser.closed)
	}
}

func sorted(ids []uint32) []uint32 {
	out := slices.Clone(ids)
	slices.Sort(out)
	return out
}

func TestServicesTerminalLimitsRefusalsAndTimeout(t *testing.T) {
	r := startServices(t, 22, nil)
	var ids []uint32
	for i := 0; i < 4; i++ {
		id, _ := r.browser.openTerminal(24, 80)
		ids = append(ids, id)
	}
	r.step()
	if len(r.sender.shells) != 4 {
		t.Fatalf("%d shells", len(r.sender.shells))
	}
	// The KVM refuses a fifth itself; the sender hears nothing.
	if id, refused := r.browser.openTerminal(24, 80); id != 0 || refused != lineBusy {
		t.Fatalf("fifth: %d %q", id, refused)
	}
	r.step()
	if len(r.sender.terminalIDs) != 4 {
		t.Fatalf("the sender heard of the fifth")
	}
	r.browser.closeTerminal(ids[0])
	if id, _ := r.browser.openTerminal(24, 80); id == 0 {
		t.Fatalf("no terminal after a close")
	}
	r.step()
	if len(r.sender.shells) != 4 {
		t.Fatalf("%d shells", len(r.sender.shells))
	}

	// A sender busier than the KVM knows answers Busy.
	r = startServices(t, 23, func(g *guestSender) { g.maxTerminals = 1 })
	first, _ := r.browser.openTerminal(24, 80)
	second, _ := r.browser.openTerminal(24, 80)
	r.step()
	if !reflect.DeepEqual(sortedKeys(r.sender.shells), []uint32{first}) ||
		!reflect.DeepEqual(r.browser.closed, map[uint32]closedLine{second: {lineBusy, false}}) {
		t.Fatalf("busy sender: shells %v closed %v", sortedKeys(r.sender.shells), r.browser.closed)
	}

	// A sender that never answers: terminal.close after 5 seconds.
	r = startServices(t, 24, func(g *guestSender) { g.answerOpens = false })
	silent, _ := r.browser.openTerminal(24, 80)
	r.step()
	r.advance(4999)
	if len(r.browser.closed) != 0 {
		t.Fatalf("closed before 5 s")
	}
	r.advance(1)
	if !reflect.DeepEqual(r.browser.closed, map[uint32]closedLine{silent: {lineNoAnswer, false}}) || !r.k.isOpen() {
		t.Fatalf("closed %v", r.browser.closed)
	}
}

func TestServicesFileDropRoundTrip(t *testing.T) {
	r := startServices(t, 25, nil)
	rng := r.sender.rng
	files := []struct {
		name string
		data []byte
	}{
		{"empty.txt", nil},
		{"one.bin", []byte{1}},
		{"Grüße 👋.txt", randomBytes(rng, specChunkData)},
		{"edge", randomBytes(rng, specChunkData+1)},
		{"photo.jpg", randomBytes(rng, protoWindow+3)},
		{"same.txt", randomBytes(rng, 100)},
		{"same.txt", randomBytes(rng, 200)},
		{"large.iso", randomBytes(rng, 3*1024*1024+17)},
	}
	var drop []dropFile
	var contents [][]byte
	for _, f := range files {
		drop = append(drop, dropFile{f.name, uint64(len(f.data))})
		contents = append(contents, f.data)
	}
	_, ids, refused := r.browser.drop(drop)
	if refused != "" {
		t.Fatalf("drop: %q", refused)
	}
	r.runDrop(ids, contents)
	// Every file arrived whole at its path, in drop order.
	if !reflect.DeepEqual(r.browser.order, ids) {
		t.Fatalf("order %v, want %v", r.browser.order, ids)
	}
	paths := map[string]bool{}
	for i, id := range ids {
		d := r.browser.done[id]
		if d.status != FileOK || !bytes.Equal(r.sender.downloads[d.path], contents[i]) || paths[d.path] {
			t.Fatalf("file %d: %+v", i, d)
		}
		paths[d.path] = true
	}
	if p := r.browser.done[ids[6]].path; p != "/home/guest/Downloads/same (2).txt" {
		t.Fatalf("second same.txt at %q", p)
	}
	// The browser heard exactly the credit the sender granted.
	for _, id := range ids {
		if r.browser.credit[id] != r.sender.transfers[id].credit {
			t.Fatalf("transfer %d: browser credit %d, sender %d", id, r.browser.credit[id], r.sender.transfers[id].credit)
		}
	}
	// The next drop may start.
	if _, _, refused := r.browser.drop([]dropFile{{"next", 1}}); refused != "" || r.k.s.Counters.ServiceFaults != 0 {
		t.Fatalf("next drop: %q", refused)
	}
}

func TestServicesFileCancelsChannelCloseAndSessionEnd(t *testing.T) {
	r := startServices(t, 26, nil)
	const size = 300 * 1024
	contents := [][]byte{randomBytes(r.sender.rng, size), randomBytes(r.sender.rng, size), randomBytes(r.sender.rng, size)}
	dropID, ids, _ := r.browser.drop([]dropFile{{"f0", size}, {"f1", size}, {"f2", size}})
	r.step()
	// Some of the first file, then the browser stops it.
	if !r.browser.send(ids[0], contents[0][:100000]) || !r.browser.cancel(ids[0]) || r.browser.cancel(ids[0]) {
		t.Fatalf("send and cancel")
	}
	r.step()
	if r.browser.done[ids[0]].status != FileCancelled || r.sender.ended[ids[0]] != doneCancelled || len(r.sender.downloads) != 0 {
		t.Fatalf("cancel: %+v", r.browser.done[ids[0]])
	}
	// The sender moved on to the second file.
	if r.browser.credit[ids[1]] != protoWindow || !r.browser.send(ids[1], contents[1][:5000]) {
		t.Fatalf("second file: credit %d", r.browser.credit[ids[1]])
	}
	// The browser's channel closes: both remaining transfers end on the
	// sender, and nobody is left to tell.
	r.browser.closeChannel(dropID)
	if events := r.step(); len(events) != 0 {
		t.Fatalf("events after the channel closed: %+v", events)
	}
	for _, id := range ids {
		if r.sender.ended[id] != doneCancelled {
			t.Fatalf("transfer %d on the sender: %v", id, r.sender.ended)
		}
	}
	if _, ok := r.browser.done[ids[1]]; ok {
		t.Fatalf("an outcome after the channel closed")
	}

	// The session ends mid-transfer: each open transfer fails once.
	_, ids, _ = r.browser.drop([]dropFile{{"a", 10}, {"b", size}, {"c", 0}})
	r.step()
	if !r.browser.send(ids[0], []byte("0123456789")) {
		t.Fatalf("send refused")
	}
	r.step()
	if !r.browser.send(ids[1], contents[1][:1000]) {
		t.Fatalf("send refused")
	}
	r.k.s.End("the IN endpoint did not drain")
	r.browser.poll()
	for i, want := range []FileStatus{FileOK, FileFailed, FileFailed} {
		if r.browser.done[ids[i]].status != want {
			t.Fatalf("transfer %d: %+v", i, r.browser.done[ids[i]])
		}
	}
	if events := r.browser.poll(); len(events) != 0 || r.browser.send(ids[1], []byte("x")) || r.k.isOpen() {
		t.Fatalf("after the end: %+v", events)
	}
}

func TestServicesSettingsEndAndRestoreTheServices(t *testing.T) {
	r := startServices(t, 27, nil)
	t1, _ := r.browser.openTerminal(24, 80)
	t2, _ := r.browser.openTerminal(24, 80)
	_, ids, _ := r.browser.drop([]dropFile{{"a", 200000}, {"b", 10}})
	r.step()
	if !r.browser.send(ids[0], randomBytes(r.sender.rng, 50000)) {
		t.Fatalf("send refused")
	}
	settings := r.k.s.Settings()
	// The terminal setting goes off.
	settings.TerminalEnabled = false
	r.k.s.ApplySettings(settings)
	r.step()
	if !reflect.DeepEqual(r.browser.closed, map[uint32]closedLine{t1: {lineDisabled, false}, t2: {lineDisabled, false}}) ||
		len(r.sender.shells) != 0 || r.sender.shell || r.k.s.TerminalAvailable() {
		t.Fatalf("terminal off: closed %v", r.browser.closed)
	}
	if id, refused := r.browser.openTerminal(24, 80); id != 0 || refused != noTerminal {
		t.Fatalf("open with the terminal off: %d %q", id, refused)
	}
	// The transfer went on.
	if !r.browser.send(ids[0], randomBytes(r.sender.rng, 1000)) {
		t.Fatalf("the transfer stopped")
	}
	// File reception goes off: the browser learns both failed.
	settings.FilesEnabled = false
	r.k.s.ApplySettings(settings)
	r.step()
	for _, id := range ids {
		if r.browser.done[id].status != FileFailed || r.sender.ended[id] != doneCancelled {
			t.Fatalf("files off: %+v %v", r.browser.done[id], r.sender.ended)
		}
	}
	if r.k.s.FileAvailable() {
		t.Fatalf("files offered")
	}
	if _, _, refused := r.browser.drop([]dropFile{{"x", 1}}); refused != noFiles {
		t.Fatalf("drop with files off: %q", refused)
	}
	// Both back on: the sender registers again.
	settings.TerminalEnabled, settings.FilesEnabled = true, true
	r.k.s.ApplySettings(settings)
	r.step()
	if !r.k.s.TerminalAvailable() || !r.k.s.FileAvailable() {
		t.Fatalf("not offered again")
	}
	id, _ := r.browser.openTerminal(24, 80)
	_, ids, _ = r.browser.drop([]dropFile{{"again", 3}})
	r.step()
	if !r.browser.typeKeys(id, []byte("ok")) {
		t.Fatalf("input refused")
	}
	r.runDrop(ids, [][]byte{[]byte("abc")})
	if string(r.browser.output[id]) != "ok" || r.k.s.Counters.ServiceFaults != 0 {
		t.Fatalf("output %q faults %d", r.browser.output[id], r.k.s.Counters.ServiceFaults)
	}
}

func TestServicesInvalidSenderMessagesFaultOneService(t *testing.T) {
	r := startServices(t, 28, nil)
	terminal, _ := r.browser.openTerminal(24, 80)
	_, ids, _ := r.browser.drop([]dropFile{{"a", 100}})
	r.step()
	// terminal.output with session_id 0.
	r.sender.write(wireMsg(svcTerminal, kTermOutput, []byte{0, 0, 0, 0, 'x'}))
	r.step()
	if !reflect.DeepEqual(r.browser.closed, map[uint32]closedLine{terminal: {"Terminal faulted: invalid terminal.output", false}}) ||
		r.k.s.Counters.ServiceFaults != 1 || r.k.s.TerminalAvailable() {
		t.Fatalf("terminal fault: closed %v", r.browser.closed)
	}
	// The transfer is untouched.
	if !r.browser.send(ids[0], bytes.Repeat([]byte{'x'}, 50)) {
		t.Fatalf("send refused")
	}
	r.step()
	if _, done := r.browser.done[ids[0]]; done {
		t.Fatalf("the transfer ended")
	}
	// file.done with a reserved byte set.
	r.sender.write(wireMsg(svcFile, kFileDone, le32(ids[0]), []byte{1, 1, 0, 0}))
	r.step()
	if r.browser.done[ids[0]].status != FileFailed || r.k.s.Counters.ServiceFaults != 2 {
		t.Fatalf("file fault: %+v", r.browser.done[ids[0]])
	}
	// Both register again, and work.
	r.sender.register(svcTerminal, 1)
	r.sender.register(svcFile, 1)
	if !r.k.s.TerminalAvailable() || !r.k.s.FileAvailable() {
		t.Fatalf("not offered again")
	}
	again, _ := r.browser.openTerminal(24, 80)
	r.step()
	if !r.browser.typeKeys(again, []byte("hi")) {
		t.Fatalf("input refused")
	}
	r.step()
	if string(r.browser.output[again]) != "hi" || !r.k.isOpen() {
		t.Fatalf("output %q", r.browser.output[again])
	}
}

// Random browser, sender and clock actions. The sender checks every KVM
// message; the browser checks every report. After each step: output shown
// is a prefix of the shell's output, input delivered is a prefix of what
// the KVM took, the browser heard exactly the credit the sender granted for
// each open transfer, and the session stays. When the session ends, every
// terminal and every transfer has ended exactly once.
func TestServicesRandomizedTerminalAndFileTraffic(t *testing.T) {
	rounds := 100
	if testing.Short() {
		rounds = 10
	}
	rng := rand.New(rand.NewPCG(2468, 0))
	pick := func(choices ...int) int { return choices[rng.IntN(len(choices))] }
	randint := func(a, b int) int { return a + rng.IntN(b-a+1) }
	sizes := []uint64{0, 1, 1000, specChunkData, specChunkData + 1, 200000, protoWindow + 1}
	// What the rounds did, so that the invariants are not checked on
	// nothing.
	var shown, delivered, failed, ended, faultsSeen int
	for round := 0; round < rounds; round++ {
		step := uint64(pick(1, 4096, 128*1024, protoWindow))
		r := startServices(t, uint64(round)+100, func(g *guestSender) { g.step = step })
		browser, sender := r.browser, r.sender
		faults := uint32(0)
		faulted := map[uint16]bool{svcTerminal: false, svcFile: false}
		for action := 0; action < 120; action++ {
			registrations := map[uint16]int{svcTerminal: sender.registrations[svcTerminal], svcFile: sender.registrations[svcFile]}
			var live []uint32
			for _, id := range browser.opened {
				if _, ended := browser.closed[id]; !ended && !browser.closedByBrowser[id] {
					live = append(live, id)
				}
			}
			open := browser.openTransfers()
			x := rng.Float64()
			switch {
			case x < 0.10:
				id, refused := browser.openTerminal(uint32(pick(0, 1, 24, 65535, 65536)), uint32(pick(0, 1, 80, 65535)))
				if id == 0 && refused != noTerminal && refused != lineBusy {
					t.Fatalf("round %d: open refused with %q", round, refused)
				}
			case x < 0.22 && len(browser.opened) > 0:
				browser.typeKeys(browser.opened[rng.IntN(len(browser.opened))], randomBytes(rng, pick(1, 3, 100, specTerminalData+1, 70000)))
			case x < 0.26 && len(browser.opened) > 0:
				r.k.s.TerminalResize(browser.opened[rng.IntN(len(browser.opened))], uint32(randint(0, 300)), uint32(randint(0, 300)))
			case x < 0.30 && len(browser.opened) > 0:
				browser.closeTerminal(browser.opened[rng.IntN(len(browser.opened))])
			case x < 0.34 && len(sender.shells) > 0:
				shells := sortedKeys(sender.shells)
				sender.exit(shells[rng.IntN(len(shells))], byte(pick(exitExited, exitClosed, exitFailed)))
			case x < 0.42:
				// A shell, or a session_id the KVM never used.
				targets := append(sortedKeys(sender.shells), uint32(randint(1<<30, 1<<31-1)))
				sender.output(targets[rng.IntN(len(targets))], randomBytes(rng, pick(1, 50, 70000)))
			case x < 0.44 && len(sender.shells) > 0:
				r.k.f.backedUp = true
				shells := sortedKeys(sender.shells)
				slow := shells[rng.IntN(len(shells))]
				sender.output(slow, []byte("flood"))
				// The browser takes nothing while the output waits.
				r.advance(terminalHoldMS)
				r.k.f.backedUp = false
				browser.poll()
				if slices.Contains(live, slow) && browser.closed[slow] != (closedLine{lineSlow, false}) {
					t.Fatalf("round %d: slow terminal %d: %+v", round, slow, browser.closed[slow])
				}
			case x < 0.52:
				files := make([]dropFile, randint(1, 4))
				for i := range files {
					files[i] = dropFile{fmt.Sprintf("r%d-%d.bin", round, i), sizes[rng.IntN(len(sizes))]}
				}
				if dropID, _, refused := browser.drop(files); dropID == 0 && refused != noFiles && refused != dropBusy {
					t.Fatalf("round %d: drop refused with %q", round, refused)
				}
			case x < 0.68 && len(open) > 0:
				for _, id := range open {
					n := min(browser.room(id), browser.size[id]-browser.sent[id], uint64(pick(1, 32*1024, 100000)))
					if n > 0 && !browser.send(id, randomBytes(rng, int(n))) {
						t.Fatalf("round %d: FileData refused within the credit", round)
					}
				}
			case x < 0.70 && len(open) > 0:
				// A browser that sends past its credit, or past the file's
				// size, loses the drop.
				id := open[rng.IntN(len(open))]
				room, left := browser.room(id), browser.size[id]-browser.sent[id]
				n := room + 1
				if room > left && rng.Float64() < 0.5 {
					n = left + 1
				}
				if browser.send(id, randomBytes(rng, int(n))) {
					t.Fatalf("round %d: %d bytes past the credit or size accepted", round, n)
				}
			case x < 0.75 && len(open) > 0:
				if !browser.cancel(open[rng.IntN(len(open))]) {
					t.Fatalf("round %d: cancel refused", round)
				}
			case x < 0.78 && len(open) > 0:
				browser.closeChannel(browser.dropOf[open[rng.IntN(len(open))]])
			case x < 0.86:
				r.advance(int64(pick(100, 4999, 5000, 15000, 30000)))
			case x < 0.94:
				settings := r.k.s.Settings()
				settings.TerminalEnabled = rng.Float64() < 0.7
				settings.FilesEnabled = rng.Float64() < 0.7
				r.k.s.ApplySettings(settings)
			default:
				service := uint16(pick(svcTerminal, svcFile))
				invalid := map[uint16][][]byte{
					svcTerminal: {
						wireMsg(svcTerminal, kTermOutput, []byte{0, 0, 0, 0, 'x'}),
						wireMsg(svcTerminal, kTermOpened, []byte{1, 0, 0, 0, 9}),
						wireMsg(svcTerminal, kTermExited, []byte{1, 0, 0}),
						wireMsg(svcTerminal, kTermOpen, make([]byte, 8)),
					},
					svcFile: {
						wireMsg(svcFile, kFileReady, make([]byte, 7)),
						wireMsg(svcFile, kFileDone, []byte{1, 0, 0, 0, 5, 0, 0, 0}),
						wireMsg(svcFile, kFileDrop, []byte{1, 0, 0, 0}),
					},
				}[service]
				active := sender.active
				if service == svcFile && active != nil && active.received < active.size && rng.Float64() < 0.3 {
					// OK for a file the KVM has not sent in full.
					sender.finish(active, doneOK)
				} else {
					sender.write(invalid[rng.IntN(len(invalid))])
				}
				if !faulted[service] {
					faults++
					faulted[service] = true
				}
				flag := sender.shell
				if service == svcFile {
					flag = sender.receive
				}
				if flag && rng.Float64() < 0.8 {
					sender.register(service, 1)
				}
			}
			r.step()
			for _, service := range []uint16{svcTerminal, svcFile} {
				if sender.registrations[service] != registrations[service] {
					faulted[service] = false
				}
			}
			// The invariants.
			if !r.k.isOpen() || r.k.s.Counters.ServiceFaults != faults {
				t.Fatalf("round %d action %d: open %v faults %d, want %d (reason %q)", round, action, r.k.isOpen(),
					r.k.s.Counters.ServiceFaults, faults, r.k.f.endReason)
			}
			for id, shown := range browser.output {
				if sent := sender.outputSent[id]; !bytes.HasPrefix(sent, shown) {
					t.Fatalf("round %d: terminal %d shows output the shell did not send", round, id)
				}
			}
			for id, shell := range sender.shells {
				if !bytes.HasPrefix(browser.input[id], shell.input) {
					t.Fatalf("round %d: shell %d got input the browser did not type", round, id)
				}
			}
			for _, id := range browser.openTransfers() {
				if _, ended := sender.ended[id]; ended {
					continue
				}
				want := uint64(0)
				if tr := sender.transfers[id]; tr != nil {
					want = tr.credit
				}
				if browser.credit[id] != want {
					t.Fatalf("round %d: transfer %d: browser credit %d, sender %d", round, id, browser.credit[id], want)
				}
			}
			// What ended at the KVM ended at the sender too: the KVM
			// cancelled or closed it, or the sender ended it itself.
			for id, dropID := range browser.dropOf {
				_, done := browser.done[id]
				if _, ended := sender.ended[id]; (done || browser.channelClosed[dropID]) && !ended {
					t.Fatalf("round %d: transfer %d ended at the KVM only", round, id)
				}
			}
			for _, id := range browser.opened {
				_, ended := browser.closed[id]
				if _, running := sender.shells[id]; (ended || browser.closedByBrowser[id]) && running {
					t.Fatalf("round %d: terminal %d ended at the KVM only", round, id)
				}
			}
		}
		// The session ends: whatever was open ends now, exactly once.
		r.k.s.End("end of round")
		browser.poll()
		for _, id := range browser.opened {
			if _, ended := browser.closed[id]; !browser.closedByBrowser[id] && !ended {
				t.Fatalf("round %d: terminal %d did not end", round, id)
			}
		}
		if open := browser.openTransfers(); len(open) != 0 {
			t.Fatalf("round %d: transfers %v did not end", round, open)
		}
		if events := browser.poll(); len(events) != 0 {
			t.Fatalf("round %d: events after the end", round)
		}
		for _, out := range browser.output {
			shown += len(out)
		}
		for _, d := range browser.done {
			if d.status == FileOK {
				delivered++
			} else {
				failed++
			}
		}
		ended += len(browser.closed)
		faultsSeen += int(faults)
	}
	if shown == 0 || delivered == 0 || failed == 0 || ended == 0 || faultsSeen == 0 {
		t.Fatalf("output %d, files %d delivered and %d failed, %d terminals ended, %d faults", shown, delivered, failed, ended, faultsSeen)
	}
}

// --- The video service ------------------------------------------------------

var (
	protoSPS = []byte{0, 0, 0, 1, 0x67, 0x42, 0x00, 0x1f, 0xe9, 0x02}
	protoPPS = []byte{0, 0, 0, 1, 0x68, 0xce, 0x3c, 0x80}
)

type appliedVideo struct{ width, height, kbps, fps uint32 }

// videoSender is the sender's video service, from the OS Services
// protocol. It checks each video.config, applies it, reports video.status,
// and sends one H.264 access unit per frame: a keyframe with SPS and PPS
// first in each stream, and a keyframe after each request (with SPS and PPS
// only when setsOnRequest). A mute sender applies configs without a status.
type videoSender struct {
	*protoSender
	maxWidth, maxHeight, maxFPS, nativeFPS uint32
	setsOnRequest, mute                    bool
	applied                                *appliedVideo
	configs                                [][5]uint32
	requests                               int
	keyframeDue, setsDue                   bool
	captureUS                              uint64
}

func newVideoSender(p *protoSender) *videoSender {
	return &videoSender{protoSender: p, maxWidth: 3840, maxHeight: 2160, maxFPS: 60, nativeFPS: 60,
		setsOnRequest: true, captureUS: 7000000}
}

func (v *videoSender) register() {
	v.t.Helper()
	v.k.write(wireMsg(svcVideo, kVideoCaps, le32(1), le32(v.maxWidth), le32(v.maxHeight), le32(v.maxFPS)), 0)
	v.poll()
}

func (v *videoSender) ping() {
	v.t.Helper()
	if n := v.k.write(wireMsg(svcSession, kPing), 0); n != 8 {
		v.t.Fatalf("a ping took %d bytes", n)
	}
}

func (v *videoSender) poll() {
	v.t.Helper()
	for _, m := range v.k.messages() {
		if m.service != svcVideo {
			v.t.Fatalf("service %d kind %d", m.service, m.kind)
		}
		if m.kind == kVideoConfig {
			v.apply(m.payload)
			continue
		}
		if m.kind != kVideoKeyframe || len(m.payload) != 0 {
			v.t.Fatalf("video kind %d (% x)", m.kind, m.payload)
		}
		v.requests++
		v.keyframeDue = true
		v.setsDue = v.setsDue || v.setsOnRequest
	}
}

func (v *videoSender) apply(p []byte) {
	v.t.Helper()
	if len(p) != 20 {
		v.t.Fatalf("video.config of %d bytes", len(p))
	}
	enabled, layout, codec, reserved := p[0], p[1], p[2], p[3]
	width, height, kbps, fps := wle.Uint32(p[4:]), wle.Uint32(p[8:]), wle.Uint32(p[12:]), wle.Uint32(p[16:])
	if enabled > 1 || layout > 1 || codec != 0 || reserved != 0 || width == 0 || width > v.maxWidth ||
		height == 0 || height > v.maxHeight || fps == 0 || fps > v.maxFPS || kbps == 0 {
		v.t.Fatalf("video.config % x", p)
	}
	v.configs = append(v.configs, [5]uint32{uint32(enabled), width, height, kbps, fps})
	before := v.applied
	v.applied = nil
	if enabled == 1 {
		v.applied = &appliedVideo{width, height, kbps, fps}
	}
	same := (before == nil) == (v.applied == nil) && (before == nil || *before == *v.applied)
	if v.mute || same && enabled == 1 {
		return
	}
	reset := enabled == 1 && before == nil || before != nil && (before.width != width || before.height != height)
	v.status(reset, 0)
}

// status reports the applied state; a new stream starts with the next
// frame on reset.
func (v *videoSender) status(reset bool, errorCode uint32) {
	v.t.Helper()
	var body []byte
	if v.applied == nil {
		body = cat([]byte{0, 0, 0, 0}, make([]byte, 24), le32(errorCode))
	} else {
		r := byte(0)
		if reset {
			r = 1
		}
		a := v.applied
		body = cat([]byte{1, 1, 0, r}, le32(a.width), le32(a.height), le32(v.nativeFPS), le32(a.kbps), le32(a.kbps), le32(a.fps), le32(0))
		v.keyframeDue = v.keyframeDue || reset
		v.setsDue = v.setsDue || reset
	}
	v.k.write(wireMsg(svcVideo, kVideoStatus, body), 0)
}

// encodeFrame is the next video.frame and its access unit.
func (v *videoSender) encodeFrame(size int, gapUS uint64) ([]byte, []byte) {
	key, sets := v.keyframeDue, v.setsDue
	v.keyframeDue, v.setsDue = false, false
	if gapUS == 0 {
		gapUS = 1000000 / uint64(v.applied.fps)
	}
	v.captureUS += gapUS
	body := make([]byte, size)
	for i := range body {
		body[i] = byte(0x80 | ((v.captureUS + uint64(i)) & 0x7f))
	}
	var unit []byte
	if sets {
		unit = cat(protoSPS, protoPPS)
	}
	slice := byte(0x41)
	if key {
		slice = 0x65
	}
	unit = cat(unit, []byte{0, 0, 0, 1, slice}, body)
	flags := uint32(0)
	if key {
		flags |= flagKey
	}
	if sets {
		flags |= flagSets
	}
	return wireMsg(svcVideo, kVideoFrame, le64(v.captureUS), le32(flags), unit), unit
}

func (v *videoSender) frame(size int, gapUS uint64, chunk int) []byte {
	v.t.Helper()
	data, unit := v.encodeFrame(size, gapUS)
	if n := v.k.write(data, chunk); n != len(data) {
		v.t.Fatalf("the KVM took %d of %d bytes of a frame", n, len(data))
	}
	return unit
}

// wait: time passes at 100 ms ticks; a ping each second keeps the session.
func (v *videoSender) wait(ms int64) {
	v.t.Helper()
	for i := int64(0); i < ms/100; i++ {
		v.k.advance(100)
		if v.k.f.now%1000 == 0 {
			v.ping()
		}
		v.poll()
	}
}

type videoRun struct {
	t      *testing.T
	k      *kvmEnd
	sender *videoSender
}

func newVideoRun(t *testing.T, seed uint64) *videoRun {
	k := newKVMEnd(t, seed, true)
	return &videoRun{t: t, k: k, sender: newVideoSender(newProtoSender(t, k, seed))}
}

// show: the switch to service video, up to the first keyframe.
func (r *videoRun) show() {
	r.t.Helper()
	r.sender.negotiate(0, nil)
	r.sender.register()
	v := r.k.v
	if v.presents != 1 || v.presentWidth != 1920 || v.presentHeight != 1080 || r.k.s.Source() != SourceSwitchingToGuest {
		r.t.Fatalf("presents %d at %dx%d, source %v", v.presents, v.presentWidth, v.presentHeight, r.k.s.Source())
	}
	if len(r.sender.configs) != 0 {
		r.t.Fatalf("video.config before the display")
	}
	r.k.s.DisplayStatus(true, true)
	r.sender.poll()
	if !reflect.DeepEqual(r.sender.configs, [][5]uint32{{1, 1920, 1080, 8000, 60}}) || r.k.s.Source() != SourceGuest {
		r.t.Fatalf("configs %v source %v", r.sender.configs, r.k.s.Source())
	}
	if v.streams != 1 || v.streamWidth != 1920 || v.streamHeight != 1080 || v.streamFPS != 60 {
		r.t.Fatalf("stream %d %dx%d at %d", v.streams, v.streamWidth, v.streamHeight, v.streamFPS)
	}
	r.checkFrames([][]byte{r.sender.frame(2000, 0, 0)}, 0)
}

func (r *videoRun) checkFrames(units [][]byte, first int) {
	r.t.Helper()
	frames := r.k.v.frames
	if len(frames) != first+len(units) {
		r.t.Fatalf("%d frames, want %d", len(frames), first+len(units))
	}
	for i, unit := range units {
		f := frames[first+i]
		if !bytes.Equal(f.data, unit) || f.frame.Keyframe != (unit[4] == 0x67 || unit[4] == 0x65) {
			r.t.Fatalf("frame %d: %d bytes, keyframe %v", first+i, len(f.data), f.frame.Keyframe)
		}
	}
}

func TestVideoProtocolFramesInEveryChunkingWithCaptureSpacing(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 0))
	for _, chunk := range []int{1, 7, 12, 13, 4096, 65536} {
		r := newVideoRun(t, uint64(chunk))
		r.show()
		gaps := make([]uint64, 8)
		var units [][]byte
		for i := range gaps {
			gaps[i] = uint64(1 + rng.IntN(1000000))
			units = append(units, r.sender.frame(1+rng.IntN(20000), gaps[i], chunk))
		}
		r.checkFrames(units, 1)
		// pts follows the capture spacing (90 kHz, truncated).
		for i, gap := range gaps {
			diff := r.k.v.frames[i+1].frame.PTS90k - r.k.v.frames[i].frame.PTS90k - uint32(gap*9/100)
			if diff > 1 {
				t.Fatalf("chunk %d frame %d: pts off by %d", chunk, i, diff)
			}
		}
		if !r.k.isOpen() {
			t.Fatalf("chunk %d: session ended", chunk)
		}
	}
}

func TestVideoProtocolKeyframeRequestsAndParameterSets(t *testing.T) {
	r := newVideoRun(t, 31)
	r.show()
	r.sender.setsOnRequest = false
	r.k.v.keyframePending = 5
	r.sender.wait(3100)
	// At most one request per 600 ms while none is answered.
	if r.sender.requests != 6 {
		t.Fatalf("%d requests", r.sender.requests)
	}
	unit := r.sender.frame(2000, 0, 0)
	f := r.k.v.frames[1]
	if !f.frame.Keyframe || !f.frame.ParameterSetsAdded || !bytes.Equal(f.data, cat(protoSPS, protoPPS, unit)) ||
		f.frame.KeyframeGeneration != 5 || r.k.v.lastKeyframeDone() != (keyframeDoneEvent{5, true}) {
		t.Fatalf("answer %+v", f.frame)
	}
	r.sender.wait(3000)
	if r.sender.requests != 6 {
		t.Fatalf("%d requests after the answer", r.sender.requests)
	}
}

func TestVideoProtocolBackpressureThenDropAfterTheSlotWait(t *testing.T) {
	r := newVideoRun(t, 32)
	r.show()
	v := r.k.v
	v.busy = true
	data, unit := r.sender.encodeFrame(30000, 0)
	used := r.k.write(data, 0)
	// The envelope and the frame head; the bus NAKs the rest.
	if used != 8+12 || !r.k.s.FrameWaiting() {
		t.Fatalf("used %d waiting %v", used, r.k.s.FrameWaiting())
	}
	r.k.advance(100)
	if n := r.k.write(data[used:], 0); n != 0 {
		t.Fatalf("took %d while waiting", n)
	}
	v.busy = false
	if n := r.k.write(data[used:], 0); n != len(data)-used {
		t.Fatalf("took %d", n)
	}
	r.checkFrames([][]byte{unit}, 1)
	if c := r.k.s.VideoCounters; c.SlotWaits != 1 || c.SlotWaitMaxMS != 100 {
		t.Fatalf("counters %+v", c)
	}
	v.busy = true
	data, _ = r.sender.encodeFrame(30000, 0)
	used = r.k.write(data, 0)
	r.k.advance(999)
	if n := r.k.write(data[used:], 0); n != 0 {
		t.Fatalf("took %d before 1 s", n)
	}
	r.k.advance(1)
	if n := r.k.write(data[used:], 0); n != len(data)-used {
		t.Fatalf("took %d after 1 s", n)
	}
	if len(v.frames) != 2 || v.gaps != 1 || r.k.s.VideoCounters.FramesDropped != 1 {
		t.Fatalf("frames %d gaps %d", len(v.frames), v.gaps)
	}
	// The stream stays aligned.
	v.busy = false
	r.checkFrames([][]byte{r.sender.frame(2000, 0, 0)}, 2)
}

func TestVideoProtocolResizeStartsANewStream(t *testing.T) {
	r := newVideoRun(t, 33)
	r.show()
	settings := r.k.s.Settings()
	settings.Width, settings.Height = 1280, 720
	r.k.s.ApplySettings(settings)
	v := r.k.v
	if v.presents != 2 || v.presentWidth != 1280 || v.serials[0] == v.serials[1] {
		t.Fatalf("presents %d at %d", v.presents, v.presentWidth)
	}
	r.sender.poll()
	if len(r.sender.configs) != 1 {
		t.Fatalf("video.config before the display")
	}
	r.k.s.DisplayStatus(true, true)
	r.sender.poll()
	if last := r.sender.configs[len(r.sender.configs)-1]; last != [5]uint32{1, 1280, 720, 5000, 60} {
		t.Fatalf("config %v", last)
	}
	if v.streams != 2 || v.streamWidth != 1280 || v.streamFPS != 60 {
		t.Fatalf("stream %d at %d", v.streams, v.streamWidth)
	}
	var units [][]byte
	for i := 0; i < 3; i++ {
		units = append(units, r.sender.frame(2000, 0, 0))
	}
	r.checkFrames(units, 1)
	if v.frames[1].frame.Width != 1280 || r.k.s.VideoCounters.StreamResets != 2 {
		t.Fatalf("width %d resets %d", v.frames[1].frame.Width, r.k.s.VideoCounters.StreamResets)
	}
}

func TestVideoProtocolStreamResetNeedsAKeyframeWithParameterSets(t *testing.T) {
	r := newVideoRun(t, 34)
	r.show()
	r.sender.status(true, 0)
	r.sender.setsDue = false
	r.sender.frame(2000, 0, 0) // a keyframe without SPS and PPS
	if e := r.k.s.State().VideoError; e != "the first frame of a stream is not a keyframe with parameter sets" {
		t.Fatalf("error %q", e)
	}
	r.sender.poll()
	v := r.k.v
	if r.sender.configs[len(r.sender.configs)-1][0] != 0 || r.k.s.Source() != SourceSwitchingToPhysical || v.restores != 1 ||
		len(v.frames) != 1 {
		t.Fatalf("configs %v source %v", r.sender.configs, r.k.s.Source())
	}
	// New capabilities clear the fault.
	r.k.s.DisplayStatus(false, false)
	r.sender.register()
	if v.presents != 2 || r.k.s.State().VideoError != "" {
		t.Fatalf("presents %d error %q", v.presents, r.k.s.State().VideoError)
	}
}

func TestVideoProtocolNoStatusIn45Seconds(t *testing.T) {
	r := newVideoRun(t, 35)
	r.sender.mute = true
	r.sender.negotiate(0, nil)
	r.sender.register()
	r.k.s.DisplayStatus(true, true)
	r.sender.wait(44900)
	if r.k.s.Source() != SourceSwitchingToGuest {
		t.Fatalf("source %v", r.k.s.Source())
	}
	r.sender.wait(100)
	if r.k.s.Source() != SourceSwitchingToPhysical || r.k.v.restores != 1 {
		t.Fatalf("source %v restores %d", r.k.s.Source(), r.k.v.restores)
	}
	var enabled []uint32
	for _, c := range r.sender.configs {
		enabled = append(enabled, c[0])
	}
	if !reflect.DeepEqual(enabled, []uint32{1, 0}) || !strings.Contains(r.k.s.State().VideoError, "45 seconds") {
		t.Fatalf("configs %v error %q", enabled, r.k.s.State().VideoError)
	}
	r.k.s.DisplayStatus(false, false)
	if r.k.s.Source() != SourcePhysical {
		t.Fatalf("source %v", r.k.s.Source())
	}
}

func TestVideoProtocolDisableWaitsForTheStatusOr15Seconds(t *testing.T) {
	for _, mute := range []bool{false, true} {
		r := newVideoRun(t, 36)
		r.show()
		r.sender.mute = mute
		settings := r.k.s.Settings()
		settings.VideoEnabled = false
		r.k.s.ApplySettings(settings)
		r.sender.poll()
		want := 1
		if mute {
			want = 0
		}
		if r.sender.configs[len(r.sender.configs)-1][0] != 0 || r.k.v.restores != want {
			t.Fatalf("mute %v: restores %d", mute, r.k.v.restores)
		}
		r.sender.wait(14900)
		if r.k.v.restores != want {
			t.Fatalf("mute %v: restores %d before 15 s", mute, r.k.v.restores)
		}
		r.sender.wait(100)
		if r.k.v.restores != 1 {
			t.Fatalf("mute %v: restores %d after 15 s", mute, r.k.v.restores)
		}
	}
}

func TestVideoProtocolRetryBudgetAcrossSessions(t *testing.T) {
	r := newVideoRun(t, 37)
	for i := 0; i < 3; i++ {
		r.sender.negotiate(0, nil)
		r.sender.register()
		if r.k.v.presents != i+1 {
			t.Fatalf("switch %d: presents %d", i, r.k.v.presents)
		}
		r.k.advance(30000)
		r.k.s.DisplayStatus(false, false)
	}
	r.sender.negotiate(0, nil)
	r.sender.register()
	if r.k.v.presents != 3 || r.sender.configs[len(r.sender.configs)-1][0] != 0 ||
		!strings.Contains(r.k.s.State().VideoError, "too often") {
		t.Fatalf("fourth: presents %d error %q", r.k.v.presents, r.k.s.State().VideoError)
	}
	// Blocked past the window and into the next session.
	r.k.advance(120000)
	r.sender.negotiate(0, nil)
	r.sender.register()
	if r.k.v.presents != 3 {
		t.Fatalf("a switch while blocked")
	}
	settings := r.k.s.Settings()
	settings.VideoEnabled = false
	r.k.s.ApplySettings(settings)
	settings.VideoEnabled = true
	r.k.s.ApplySettings(settings)
	if r.k.v.presents != 4 {
		t.Fatalf("off and on: presents %d", r.k.v.presents)
	}
}
