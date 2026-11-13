package osservices

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
)

// Ported from the Mini's tests/test_os_services_session.c: the reset
// exchange, the handshake, the byte stream and the session service.

func TestResetHandshakeAndSignature(t *testing.T) {
	h := newHarness(t, hooksServices, defaultSettings())
	if p := h.phase(); p != PhaseResetting {
		t.Fatalf("phase before the first RESET: %d", p)
	}
	token := h.openSession()
	phase, again := h.usbStatus(h.s.USBStatus())
	if phase != PhaseActive || again != token {
		t.Fatalf("STATUS of the open session: phase %d token %x", phase, again)
	}
	if h.s.Counters.SessionsOpened != 1 {
		t.Fatalf("SessionsOpened %d", h.s.Counters.SessionsOpened)
	}
	// A second RESET makes a new token and ends the session.
	next := h.resetAndAdmit()
	if next == token || h.s.Open() || h.f.resets != 2 {
		t.Fatalf("second RESET: token %x (was %x), open %v, resets %d", next, token, h.s.Open(), h.f.resets)
	}
}

// Stale bytes before the hello: the tail of a frame of the old session, a
// hello with the old token, half an envelope. Fed in every chunk size from
// 1 to 70, the KVM finds exactly the new hello and stays aligned for the
// messages after it.
func TestHelloSearchAcrossChunks(t *testing.T) {
	for chunk := 1; chunk <= 70; chunk++ {
		h := newHarness(t, hooksServices, defaultSettings())
		oldToken := h.resetAndAdmit()
		token := h.resetAndAdmit()
		var stream []byte
		for i := 0; i < 1234; i++ {
			stream = append(stream, byte(i*31))
		}
		stream = append(stream, wireMsg(svcSession, kHello, makeHello(5, 0, oldToken, 9))...)
		frameHead := wireMsg(svcVideo, kVideoFrame)
		wle.PutUint32(frameHead[4:], 100000)
		stream = append(stream, frameHead[:5]...)
		hello := makeHello(5, 0, token, byte(chunk))
		stream = append(stream, wireMsg(svcSession, kHello, hello)...)
		stream = append(stream, wireMsg(svcSession, kPing)...)
		stream = append(stream, wireMsg(svcSession, kInfo, []byte(`{"version":"0.5.0","displays":["HDMI-A-1"]}`))...)
		h.feedChunked(stream, chunk)
		if !h.s.Open() {
			t.Fatalf("chunk %d: no session", chunk)
		}
		h.checkReady(h.expect(svcSession, kReady), hello, token)
		st := h.s.State()
		if !st.SenderInfo || st.SenderVersion != "0.5.0" || len(st.Displays) != 1 || st.Displays[0] != "HDMI-A-1" {
			t.Fatalf("chunk %d: sender info %+v", chunk, st)
		}
		if h.s.Counters.MessagesIn != 2 {
			t.Fatalf("chunk %d: MessagesIn %d", chunk, h.s.Counters.MessagesIn)
		}
	}
}

// Random stale bytes before the hello: random data, pieces of the new
// hello, hellos with an old token and envelopes that are no hello. In random
// chunkings the KVM finds exactly the new hello and discards exactly the
// bytes before it.
func TestHelloSearchRandomized(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 0))
	for round := 0; round < 300; round++ {
		h := newHarness(t, hooksServices, defaultSettings())
		old := h.resetAndAdmit()
		token := h.resetAndAdmit()
		hello := makeHello(5, 0, token, byte(round))
		message := wireMsg(svcSession, kHello, hello)
		var junk []byte
		for i := rng.IntN(8); i > 0; i-- {
			switch rng.IntN(5) {
			case 0:
				junk = append(junk, randomBytes(rng, rng.IntN(200))...)
			case 1:
				// A piece of the hello's envelope and token that breaks off.
				n := rng.IntN(specEnvelope + 4 + 16)
				junk = append(junk, message[:n]...)
				junk = append(junk, message[n]^0xff)
			case 2:
				junk = append(junk, wireMsg(svcSession, kHello, makeHello(5, 0, old, byte(i)))...)
			case 3:
				// The token behind an envelope of another size or kind.
				bad := wireMsg(svcSession, kHello, hello, []byte{0})
				if rng.IntN(2) == 0 {
					bad = wireMsg(svcSession, kPing, hello)
				}
				junk = append(junk, bad...)
			default:
				junk = append(junk, message[specEnvelope:]...)
			}
		}
		stream := cat(junk, message, wireMsg(svcSession, kPing))
		h.feedChunked(stream, 1+rng.IntN(200))
		if !h.s.Open() {
			t.Fatalf("round %d: no session (%q)", round, h.f.endReason)
		}
		h.checkReady(h.expect(svcSession, kReady), hello, token)
		if h.s.Counters.BytesDiscarded != uint64(len(junk)) || h.s.Counters.MessagesIn != 1 {
			t.Fatalf("round %d: discarded %d of %d junk bytes, %d messages", round, h.s.Counters.BytesDiscarded, len(junk),
				h.s.Counters.MessagesIn)
		}
	}
}

func TestHelloSearchGivesUpAfter8MiB(t *testing.T) {
	h := newHarness(t, hooksServices, defaultSettings())
	h.resetAndAdmit()
	junk := make([]byte, 65536)
	total := 0
	for h.s.Receive(junk) == len(junk) {
		total += len(junk)
		if h.phase() != PhaseReady {
			break
		}
		if total > HelloSearchLimit+len(junk) {
			t.Fatalf("searched %d bytes", total)
		}
	}
	if h.phase() != PhaseResetting || total < HelloSearchLimit {
		t.Fatalf("after %d bytes: phase %d", total, h.phase())
	}
	if !strings.Contains(h.f.endReason, "8 MiB") {
		t.Fatalf("end reason %q", h.f.endReason)
	}
	// A stopped session reads nothing more until RESET.
	if n := h.s.Receive(junk[:10]); n != 0 {
		t.Fatalf("Receive after the end took %d bytes", n)
	}
}

func TestWrongMajorEndsSession(t *testing.T) {
	h := newHarness(t, hooksServices, defaultSettings())
	token := h.resetAndAdmit()
	h.send(svcSession, kHello, makeHello(4, 0, token, 1))
	if h.s.Open() {
		t.Fatalf("a session for major version 4")
	}
	h.expectNone()
}

func TestNewerMinorSelectsOurs(t *testing.T) {
	h := newHarness(t, hooksServices, defaultSettings())
	token := h.resetAndAdmit()
	hello := makeHello(5, 7, token, 1)
	h.send(svcSession, kHello, hello)
	m := h.expect(svcSession, kReady)
	if minor := wle.Uint16(m.payload[2:]); minor != 0 {
		t.Fatalf("session.ready minor %d", minor)
	}
	// The signature covers the hello as sent, minor 7 included.
	if !verifyReady(m.payload, hello) {
		t.Fatalf("signature over the minor-7 hello does not verify")
	}
}

func TestTimers(t *testing.T) {
	h := newHarness(t, hooksServices, defaultSettings())
	h.resetAndAdmit()
	h.f.now += handshakeTimeoutMS
	h.tick()
	if h.phase() != PhaseReady {
		t.Fatalf("admission ended at exactly 15 s")
	}
	h.f.now++
	h.tick()
	if h.phase() != PhaseResetting {
		t.Fatalf("admission still open after 15 s")
	}

	h = newHarness(t, hooksServices, defaultSettings())
	h.openSession()
	for i := 0; i < 10; i++ {
		h.f.now += 14000
		h.tick()
		if !h.s.Open() {
			t.Fatalf("ping %d: session ended", i)
		}
		h.ping()
	}
	// An unknown message also counts as life.
	h.f.now += 14000
	h.send(77, 1, []byte("xyz"))
	h.f.now += 14000
	h.tick()
	if !h.s.Open() {
		t.Fatalf("an unknown message did not count as life")
	}
	h.f.now += 1001
	h.tick()
	if h.s.Open() || !strings.Contains(h.f.endReason, "15 seconds") {
		t.Fatalf("after 15 s of silence: open %v, reason %q", h.s.Open(), h.f.endReason)
	}
}

func TestSessionErrors(t *testing.T) {
	envelope := func(service, kind uint16, size uint32) []byte {
		e := wireMsg(service, kind)
		wle.PutUint32(e[4:], size)
		return e
	}
	cases := []struct {
		name   string
		stream []byte
		open   bool
	}{
		{"second hello", nil, false},
		{"ping with a payload", wireMsg(svcSession, kPing, []byte("x")), false},
		{"close reason 1", wireMsg(svcSession, kClose, []byte{1}), false},
		{"close reason 4", wireMsg(svcSession, kClose, []byte{4}), false},
		{"close of 2 bytes", wireMsg(svcSession, kClose, []byte{0, 0}), false},
		// A payload over its limit ends the session at the envelope.
		{"control payload over 64 KiB", envelope(9, 9, specControlMax+1), false},
		// video.frame may carry 8 MiB; one byte more ends the session.
		{"video.frame of 8 MiB", envelope(svcVideo, kVideoFrame, specFrameMax), true},
		{"video.frame over 8 MiB", envelope(svcVideo, kVideoFrame, specFrameMax+1), false},
		{"other video kind of 8 MiB", envelope(svcVideo, kVideoStatus, specFrameMax), false},
		{"version over 64 bytes", wireMsg(svcSession, kInfo, []byte(fmt.Sprintf(`{"version":"%065d","displays":[]}`, 1))), false},
		{"info that is no object", wireMsg(svcSession, kInfo, []byte("[1]")), false},
		{"info that is no JSON", wireMsg(svcSession, kInfo, []byte("{")), false},
		{"65 displays", wireMsg(svcSession, kInfo, []byte(displaysJSON(65, 2))), false},
		{"display of 257 bytes", wireMsg(svcSession, kInfo, []byte(`{"displays":["`+strings.Repeat("d", 257)+`"]}`)), false},
		{"version that is no string", wireMsg(svcSession, kInfo, []byte(`{"version":5}`)), false},
		// A KVM-to-sender kind from the sender.
		{"session.config", wireMsg(svcSession, kConfig, []byte("{}")), false},
		{"session.ready", wireMsg(svcSession, kReady, make([]byte, specReadyLen)), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, hooksServices, defaultSettings())
			token := h.openSession()
			stream := c.stream
			if stream == nil {
				stream = wireMsg(svcSession, kHello, makeHello(5, 0, token, 2))
			}
			// The KVM never reads past the end of a session.
			read := cat(stream, wireMsg(svcSession, kPing))
			used := h.s.Receive(read)
			if want := len(stream) + len(read[len(stream):])*boolInt(c.open); used != want {
				t.Fatalf("Receive took %d of %d bytes, want %d", used, len(read), want)
			}
			if h.s.Open() != c.open {
				t.Fatalf("open %v, want %v (reason %q)", h.s.Open(), c.open, h.f.endReason)
			}
			if !c.open {
				if h.f.ended != 1 || h.s.LastEndReason != h.f.endReason {
					t.Fatalf("SessionEnded %d times, reason %q, LastEndReason %q", h.f.ended, h.f.endReason, h.s.LastEndReason)
				}
				// The KVM stopped reading.
				if n := h.s.Receive(wireMsg(svcSession, kPing)); n != 0 {
					t.Fatalf("Receive after the end took %d bytes", n)
				}
			}
		})
	}

	// A write that cannot be queued ends the session.
	h := newHarness(t, hooksServices, defaultSettings())
	h.openSession()
	h.f.failWrites = true
	h.registerClipboard(3, byte(OSLinux))
	if h.s.Open() || h.f.endReason != "the IN endpoint did not drain" {
		t.Fatalf("failed write: open %v, reason %q", h.s.Open(), h.f.endReason)
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func displaysJSON(count, nameLength int) string {
	var b strings.Builder
	b.WriteString(`{"displays":[`)
	for i := 0; i < count; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"%s"`, strings.Repeat(string(rune('a'+i%26)), nameLength))
	}
	b.WriteString(`]}`)
	return b.String()
}

func TestUnknownMessagesKeepAlignment(t *testing.T) {
	h := newHarness(t, hooksServices, defaultSettings())
	h.openSession()
	big := bytes.Repeat([]byte{0x5a}, 80000)
	stream := cat(
		wireMsg(42, 1, big[:specControlMax]),
		wireMsg(svcClipboard, 99, big[:100]),
		wireMsg(svcSession, 0),
		// A frame while video is not shown: discarded whole.
		wireMsg(svcVideo, kVideoFrame, big[:70000]),
		wireMsg(svcSession, kInfo, []byte(`{"version":"9.9","displays":["A","B"]}`)),
	)
	h.feedChunked(stream, 4093)
	if !h.s.Open() {
		t.Fatalf("session ended: %q", h.f.endReason)
	}
	if h.s.Counters.MessagesSkipped != 4 {
		t.Fatalf("MessagesSkipped %d", h.s.Counters.MessagesSkipped)
	}
	st := h.s.State()
	if st.SenderVersion != "9.9" || len(st.Displays) != 2 {
		t.Fatalf("sender info %+v", st)
	}
	if h.s.Counters.BytesIn != uint64(len(stream)+specEnvelope+specHelloLen) {
		t.Fatalf("BytesIn %d, want %d", h.s.Counters.BytesIn, len(stream)+specEnvelope+specHelloLen)
	}
}

// session.info changes what the browser sees only when it differs; a list
// that shrinks and grows again still counts as a change.
func TestSessionInfoReportsChanges(t *testing.T) {
	h := newHarness(t, hooksServices, defaultSettings())
	h.openSession()
	h.ping()
	steps := []struct {
		json    string
		changes bool
	}{
		{`{"version":"1.0","displays":["A","B"]}`, true},
		{`{"version":"1.0","displays":["A","B"]}`, false},
		{`{"version":"1.0","displays":["A","C"]}`, true},
		{`{"version":"1.0","displays":["A"]}`, true},
		{`{"version":"1.0","displays":["A","C"]}`, true},
		{`{"version":"1.0","displays":["AC"]}`, true},
		{`{"version":"1.1","displays":["AC"]}`, true},
		{`{"displays":["AC"]}`, true},
		{`{}`, true},
		{`{"version":null,"displays":null}`, false},
		// Unknown members are ignored.
		{`{"build":"x","displays":[]}`, false},
	}
	for i, step := range steps {
		before := h.f.stateChanges
		h.send(svcSession, kInfo, []byte(step.json))
		if !h.s.Open() {
			t.Fatalf("step %d ended the session: %q", i, h.f.endReason)
		}
		if (h.f.stateChanges != before) != step.changes {
			t.Fatalf("step %d (%s): state changed %v, want %v", i, step.json, h.f.stateChanges != before, step.changes)
		}
	}
	if st := h.s.State(); st.SenderVersion != "" || len(st.Displays) != 0 || !st.SenderInfo {
		t.Fatalf("after {}: %+v", st)
	}

	// The largest list the protocol allows: 64 names of 256 bytes.
	var b strings.Builder
	b.WriteString(`{"version":"0.4.0","displays":[`)
	for d := 0; d < 64; d++ {
		if d > 0 {
			b.WriteByte(',')
		} else {
			b.WriteByte(' ')
		}
		b.WriteByte('"')
		for c := 0; c < 256; c++ {
			b.WriteByte(byte('a' + (d+c)%26))
		}
		b.WriteByte('"')
	}
	b.WriteString("]}")
	info := []byte(b.String())
	before := h.f.stateChanges
	h.send(svcSession, kInfo, info)
	st := h.s.State()
	if !h.s.Open() || h.f.stateChanges != before+1 || len(st.Displays) != 64 {
		t.Fatalf("largest info: open %v, %d displays", h.s.Open(), len(st.Displays))
	}
	last := st.Displays[63]
	if len(last) != 256 || last[0] != byte('a'+63%26) {
		t.Fatalf("last display %q", last)
	}
	before = h.f.stateChanges
	h.send(svcSession, kInfo, info)
	if h.f.stateChanges != before {
		t.Fatalf("the same info changed the state")
	}
	// The last byte of the last name.
	info[len(info)-4] = '#'
	h.send(svcSession, kInfo, info)
	if h.f.stateChanges != before+1 || h.s.State().Displays[63][255] != '#' {
		t.Fatalf("the last byte of the last name changed nothing")
	}
}

func TestApprovalPending(t *testing.T) {
	h := newHarness(t, hooksServices, defaultSettings())
	h.openSession()
	h.send(svcSession, kClose, []byte{3})
	if h.s.Open() || !h.s.State().ApprovalPending {
		t.Fatalf("not approved: open %v, pending %v", h.s.Open(), h.s.State().ApprovalPending)
	}
	if h.f.endReason != "the sender's user has not approved this KVM" {
		t.Fatalf("end reason %q", h.f.endReason)
	}
	// The sender retries; it is still not approved.
	h.openSession()
	h.send(svcSession, kClose, []byte{3})
	if !h.s.State().ApprovalPending {
		t.Fatalf("pending cleared by a second refusal")
	}
	// Approved now: the first message that is not a close clears it.
	h.openSession()
	if !h.s.State().ApprovalPending {
		t.Fatalf("pending cleared by the handshake alone")
	}
	changes := h.f.stateChanges
	h.ping()
	if h.s.State().ApprovalPending || h.f.stateChanges == changes {
		t.Fatalf("pending after the first ping")
	}
	// The other close reasons end the session without it.
	for reason, text := range map[byte]string{0: "the sender closed the session", 2: "the sender closed the session: protocol error"} {
		h.openSession()
		h.send(svcSession, kClose, []byte{reason})
		if h.s.Open() || h.s.State().ApprovalPending || h.f.endReason != text {
			t.Fatalf("close %d: open %v, pending %v, reason %q", reason, h.s.Open(), h.s.State().ApprovalPending, h.f.endReason)
		}
	}
}

func TestOtherRegistrations(t *testing.T) {
	h := newHarness(t, hooksServices, defaultSettings())
	h.openSession()
	h.registerService(svcTerminal, 1)
	h.registerService(svcFile, 1)
	h.send(svcUpdate, kUpdateCaps, le32(1), []byte("linux-x86_64-appimage"))
	st := h.s.State()
	if !h.s.terminalShell || !h.s.fileReceive || !st.UpdateInstall || st.UpdatePackage != "linux-x86_64-appimage" {
		t.Fatalf("registrations: shell %v receive %v %+v", h.s.terminalShell, h.s.fileReceive, st)
	}
	h.registerService(svcTerminal, 3)
	if h.s.terminalShell || !h.s.Open() {
		t.Fatalf("invalid terminal.capabilities: shell %v open %v", h.s.terminalShell, h.s.Open())
	}
	// None of these is answered (Service registration).
	h.expectNone()
}

// A STATUS before the first RESET has a non-zero token: the protocol has
// no all-zero token.
func TestStatusBeforeResetHasAToken(t *testing.T) {
	h := newHarness(t, hooksServices, defaultSettings())
	phase, token := h.usbStatus(h.s.USBStatus())
	if phase != PhaseResetting || token == ([16]byte{}) {
		t.Fatalf("phase %d token %x", phase, token)
	}
}

func TestReconfigurationEndsSession(t *testing.T) {
	h := newHarness(t, hooksServices, defaultSettings())
	token := h.openSession()
	h.registerClipboard(1, byte(OSLinux))
	if id, _ := h.s.ClipboardRequest(nil, false); id == 0 {
		t.Fatalf("no clipboard request")
	}
	h.s.USBReconfigured(false)
	if h.s.Open() || len(h.f.clipboard) != 1 || h.f.clipboard[0].answered {
		t.Fatalf("after reconfiguration: open %v, clipboard %+v", h.s.Open(), h.f.clipboard)
	}
	phase, now := h.usbStatus(h.s.USBStatus())
	// The sender reads a new non-zero token as superseded.
	if phase != PhaseResetting || now == ([16]byte{}) || now == token {
		t.Fatalf("STATUS: phase %d token %x (was %x)", phase, now, token)
	}
	// Bytes after the end are not read, and no hello can use the new token
	// before the next RESET.
	if n := h.s.Receive(wireMsg(svcSession, kPing)); n != 0 {
		t.Fatalf("Receive after the end took %d bytes", n)
	}
	h.s.TransportDrained()
	if h.phase() != PhaseResetting {
		t.Fatalf("admitted without a RESET")
	}
	h.s.SetFaulted(true)
	if h.phase() != PhaseFaulted {
		t.Fatalf("phase %d after SetFaulted", h.phase())
	}
	// Faulted: a RESET answers, but no session is admitted until it
	// recovers.
	if phase, _ := h.usbStatus(h.s.USBReset()); phase != PhaseFaulted {
		t.Fatalf("RESET while faulted answered phase %d", phase)
	}
	h.s.TransportDrained()
	if h.phase() != PhaseFaulted || h.s.Reading() {
		t.Fatalf("admitted while faulted")
	}
	h.s.SetFaulted(false)
	h.openSession()
}

// The device transport answers RESET itself and hands the token over; a
// hello must carry exactly that token, and End covers a stuck IN endpoint.
func TestResetWithTokenAndEnd(t *testing.T) {
	h := newHarness(t, hooksServices, defaultSettings())
	var token [16]byte
	for i := range token {
		token[i] = byte(0xa0 + i)
	}
	phase, got := h.usbStatus(h.s.USBResetWithToken(token))
	if phase != PhaseResetting || got != token || h.f.resets != 1 {
		t.Fatalf("RESET: phase %d token %x resets %d", phase, got, h.f.resets)
	}
	h.s.TransportDrained()
	other := token
	other[15] ^= 1
	h.send(svcSession, kHello, makeHello(5, 0, other, 1))
	if h.s.Open() {
		t.Fatalf("a hello with another token opened the session")
	}
	hello := makeHello(5, 0, token, 1)
	h.send(svcSession, kHello, hello)
	if !h.s.Open() {
		t.Fatalf("the hello with the token did not open the session")
	}
	h.checkReady(h.expect(svcSession, kReady), hello, token)

	h.s.End("the IN endpoint did not drain")
	if h.s.Open() || h.f.ended != 1 || h.f.endReason != "the IN endpoint did not drain" {
		t.Fatalf("End: open %v, ended %d, reason %q", h.s.Open(), h.f.ended, h.f.endReason)
	}
	if h.phase() != PhaseResetting {
		t.Fatalf("phase %d after End", h.phase())
	}
	// A second end reports nothing.
	h.s.End("again")
	if h.f.ended != 1 || h.s.Counters.SessionsEnded != 1 {
		t.Fatalf("a second End reported: ended %d, SessionsEnded %d", h.f.ended, h.s.Counters.SessionsEnded)
	}
}

// The KVM's key or signature can be unavailable: the session ends instead of
// sending a session.ready the sender cannot verify.
func TestHandshakeWithoutAKey(t *testing.T) {
	for _, c := range []struct {
		name   string
		reason string
		p      func(f *fakePlatform) Platform
	}{
		{"no key", "the KVM key is unavailable", func(f *fakePlatform) Platform { return noKeyPlatform{f} }},
		{"no signature", "the KVM could not sign session.ready", func(f *fakePlatform) Platform { return noSignPlatform{f} }},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakePlatform(t)
			h := &harness{t: t, f: f}
			h.s = New(c.p(f), defaultSettings())
			token := h.resetAndAdmit()
			h.send(svcSession, kHello, makeHello(5, 0, token, 1))
			if h.s.Open() || h.f.endReason != c.reason {
				t.Fatalf("open %v, reason %q", h.s.Open(), h.f.endReason)
			}
			h.expectNone()
		})
	}
}

type noKeyPlatform struct{ *fakePlatform }

func (noKeyPlatform) PublicKey() ([]byte, bool) { return nil, false }

type noSignPlatform struct{ *fakePlatform }

func (noSignPlatform) Sign([]byte) ([]byte, bool) { return nil, false }

// The optional hooks are optional: a platform with none of them keeps its
// session, ignores stray messages for IDs it never handed out, offers no
// terminal or files, and answers video with enabled 0.
func TestPlatformWithoutOptionalHooks(t *testing.T) {
	settings := defaultSettings()
	settings.FilesEnabled = true
	h := newHarness(t, hooksPlain, settings)
	h.openSession()
	h.registerService(svcTerminal, 1)
	h.registerService(svcFile, 1)
	if h.s.TerminalAvailable() || h.s.FileAvailable() {
		t.Fatalf("terminal %v files %v without hooks", h.s.TerminalAvailable(), h.s.FileAvailable())
	}
	if id, refused := h.s.TerminalOpen(24, 80); id != 0 || refused == "" {
		t.Fatalf("TerminalOpen: %d %q", id, refused)
	}
	if dropID, _, refused := h.s.FileDrop([]FileEntry{{"a", 1}}); dropID != 0 || refused == "" {
		t.Fatalf("FileDrop: %d %q", dropID, refused)
	}
	// Stray messages for IDs never handed out are ignored.
	h.send(svcTerminal, kTermOutput, le32(1), []byte("x"))
	h.send(svcTerminal, kTermExited, le32(1), []byte{0})
	h.send(svcFile, kFileReady, le32(2), le32(1))
	h.send(svcFile, kFileDone, le32(2), []byte{1, 0, 0, 0})
	if !h.s.Open() || h.s.Counters.ServiceFaults != 0 {
		t.Fatalf("open %v faults %d", h.s.Open(), h.s.Counters.ServiceFaults)
	}
	h.expectNone()
	// Video registers, and stays on hardware capture.
	h.registerVideo(3840, 2160, 60)
	if m := h.expect(svcVideo, kVideoConfig); m.payload[0] != 0 {
		t.Fatalf("video.config % x", m.payload)
	}
	if st := h.s.State(); !st.VideoRegistered || st.Source != SourcePhysical {
		t.Fatalf("video %+v", st)
	}
	// The clipboard needs no optional hook.
	h.registerClipboard(3, byte(OSLinux))
	h.expectClipboardConfig(1)
	if id, _ := h.s.ClipboardRequest(nil, false); id == 0 {
		t.Fatalf("no clipboard request")
	}
	h.s.End("done")
	if h.s.Open() || len(h.f.clipboard) != 1 {
		t.Fatalf("end: open %v clipboard %+v", h.s.Open(), h.f.clipboard)
	}
}

// Every write that cannot be queued ends the session: the handshake's, a
// request's and a settings change's.
func TestWritesThatCannotBeQueued(t *testing.T) {
	h := newHarness(t, hooksServices, defaultSettings())
	token := h.resetAndAdmit()
	h.f.failWrites = true
	h.send(svcSession, kHello, makeHello(5, 0, token, 1))
	if h.s.Open() || h.f.endReason != "the IN endpoint did not drain" || h.s.Counters.SessionsOpened != 0 {
		t.Fatalf("session.ready not written: open %v reason %q", h.s.Open(), h.f.endReason)
	}
	// session.config after a written session.ready.
	h = newHarness(t, hooksServices, defaultSettings())
	token = h.resetAndAdmit()
	h.f.outCapacity = specEnvelope + specReadyLen
	h.send(svcSession, kHello, makeHello(5, 0, token, 1))
	if h.s.Open() || h.s.Counters.SessionsOpened != 0 {
		t.Fatalf("session.config not written: open %v", h.s.Open())
	}

	h = newHarness(t, hooksServices, defaultSettings())
	h.openSession()
	h.registerClipboard(3, byte(OSLinux))
	h.f.failWrites = true
	if id, status := h.s.ClipboardRequest(nil, false); id != 0 || status != ClipboardFailed || h.s.Open() {
		t.Fatalf("clipboard.get not written: %d %v open %v", id, status, h.s.Open())
	}

	for _, change := range []func(*Settings){
		func(s *Settings) { s.TerminalEnabled = false },
		func(s *Settings) { s.ClipboardEnabled = false },
	} {
		h = newHarness(t, hooksServices, defaultSettings())
		h.openSession()
		h.registerClipboard(3, byte(OSLinux))
		h.f.failWrites = true
		settings := defaultSettings()
		change(&settings)
		changes := h.f.stateChanges
		h.s.ApplySettings(settings)
		if h.s.Open() || h.s.Settings() != settings || h.f.stateChanges != changes+1 {
			t.Fatalf("settings not written: open %v", h.s.Open())
		}
	}

	h, _ = beginUpdate(t, 64)
	h.s.UpdateCancel(h.s.Update().UpdateID, "")
	h.f.failWrites = true
	if id, refused := h.s.UpdateBegin(64, testSHA, ""); id != 0 || refused != "the IN endpoint did not drain" || h.s.Open() {
		t.Fatalf("update.begin not written: %d %q open %v", id, refused, h.s.Open())
	}
}
