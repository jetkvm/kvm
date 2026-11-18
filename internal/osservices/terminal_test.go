package osservices

import (
	"bytes"
	"testing"
)

// Ported from test_os_services_session.c: the terminal service. The
// sender's side reads the KVM's messages field by field and answers with its
// own encodings.

func (h *harness) senderTerminalStatus(kind uint16, id uint32, status byte) {
	h.t.Helper()
	h.send(svcTerminal, kind, le32(id), []byte{status})
}

func (h *harness) senderOutput(id uint32, data string) {
	h.t.Helper()
	h.send(svcTerminal, kTermOutput, le32(id), []byte(data))
}

// checkTerminalSize reads terminal.open or terminal.resize: session_id
// u32, rows u16, cols u16.
func (h *harness) checkTerminalSize(m sentMessage, kind uint16, id uint32, rows, cols uint16) {
	h.t.Helper()
	if m.service != svcTerminal || m.kind != kind || len(m.payload) != 8 ||
		wle.Uint32(m.payload) != id || wle.Uint16(m.payload[4:]) != rows || wle.Uint16(m.payload[6:]) != cols {
		h.t.Fatalf("got kind %d % x, want kind %d for %d at %dx%d", m.kind, m.payload, kind, id, rows, cols)
	}
}

// browserOpensTerminal: the browser's channel opens; the sender sees
// terminal.open with size 0 by 0, then the browser's size.
func (h *harness) browserOpensTerminal(rows, cols uint32) uint32 {
	h.t.Helper()
	id, refused := h.s.TerminalOpen(rows, cols)
	if refused != "" || id == 0 {
		h.t.Fatalf("TerminalOpen: %d %q", id, refused)
	}
	h.checkTerminalSize(h.expect(svcTerminal, kTermOpen), kTermOpen, id, 0, 0)
	if rows != 0 && cols != 0 {
		h.checkTerminalSize(h.expect(svcTerminal, kTermResize), kTermResize, id, uint16(rows), uint16(cols))
	}
	h.expectNone()
	return id
}

func (h *harness) openRunningTerminal() uint32 {
	h.t.Helper()
	id := h.browserOpensTerminal(24, 80)
	h.senderTerminalStatus(kTermOpened, id, 0)
	return id
}

func (h *harness) expectTerminalClosed(index int, id uint32, reason string, exited bool) {
	h.t.Helper()
	closed := h.f.closed()
	if len(closed) <= index {
		h.t.Fatalf("%d TerminalClosed calls, want call %d for %d", len(closed), index, id)
	}
	if e := closed[index]; e.id != id || e.text != reason || e.exited != exited {
		h.t.Fatalf("TerminalClosed %d: %d %q exited %v, want %d %q exited %v", index, e.id, e.text, e.exited, id, reason, exited)
	}
}

func (h *harness) expectTerminalCloseSent(id uint32) {
	h.t.Helper()
	m := h.expect(svcTerminal, kTermClose)
	if len(m.payload) != 4 || wle.Uint32(m.payload) != id {
		h.t.Fatalf("terminal.close % x, want %d", m.payload, id)
	}
}

func TestTerminalOfferFollowsRegistrationAndSettings(t *testing.T) {
	// No session, then a session without the shell registered.
	h := newHarness(t, hooksServices, defaultSettings())
	if h.s.TerminalAvailable() {
		t.Fatalf("a terminal without a session")
	}
	if id, refused := h.s.TerminalOpen(24, 80); id != 0 || refused != "no OS Services terminal is connected" {
		t.Fatalf("TerminalOpen without a session: %d %q", id, refused)
	}
	h.openSession()
	if h.s.TerminalAvailable() {
		t.Fatalf("a terminal before registration")
	}
	if id, refused := h.s.TerminalOpen(0, 0); id != 0 || refused == "" {
		t.Fatalf("TerminalOpen before registration: %d %q", id, refused)
	}
	h.expectNone()
	changes := h.f.stateChanges
	h.registerService(svcTerminal, 1)
	if !h.s.TerminalAvailable() || !h.s.State().Terminal || h.f.stateChanges == changes {
		t.Fatalf("registration: available %v, state changed %v", h.s.TerminalAvailable(), h.f.stateChanges != changes)
	}

	// Turning the setting off withdraws the offer and tells the sender;
	// turning it on tells the sender again, which registers again.
	settings := defaultSettings()
	settings.TerminalEnabled = false
	changes = h.f.stateChanges
	h.s.ApplySettings(settings)
	h.checkConfig(h.expect(svcSession, kConfig), false, false, "")
	h.expectNone()
	if h.s.TerminalAvailable() || h.f.stateChanges == changes {
		t.Fatalf("setting off: available %v", h.s.TerminalAvailable())
	}
	if _, refused := h.s.TerminalOpen(0, 0); refused != "no OS Services terminal is connected" {
		t.Fatalf("TerminalOpen with the setting off: %q", refused)
	}
	h.expectNone()
	settings.TerminalEnabled = true
	h.s.ApplySettings(settings)
	h.checkConfig(h.expect(svcSession, kConfig), true, false, "")
	if !h.s.TerminalAvailable() {
		t.Fatalf("setting on: not available")
	}

	// A withdrawal, and a sender that registers again on its own.
	changes = h.f.stateChanges
	h.registerService(svcTerminal, 0)
	if h.s.TerminalAvailable() || h.f.stateChanges == changes || !h.s.Open() {
		t.Fatalf("withdrawal: available %v", h.s.TerminalAvailable())
	}
	h.registerService(svcTerminal, 1)
	if !h.s.TerminalAvailable() {
		t.Fatalf("registered again: not available")
	}
	h.expectNone()

	// A platform that takes no output offers no terminal.
	h = newHarness(t, hooksNoTerminal, settings)
	h.openSession()
	h.registerService(svcTerminal, 1)
	if !h.s.terminalShell || h.s.TerminalAvailable() || h.s.State().Terminal {
		t.Fatalf("no terminal hooks: shell %v available %v", h.s.terminalShell, h.s.TerminalAvailable())
	}
	if id, refused := h.s.TerminalOpen(0, 0); id != 0 || refused == "" {
		t.Fatalf("TerminalOpen without hooks: %d %q", id, refused)
	}
	h.expectNone()
}

func TestTerminalRoundTrip(t *testing.T) {
	h := openServices(t, true, false)
	id := h.browserOpensTerminal(40, 132)
	h.senderTerminalStatus(kTermOpened, id, 0)
	if len(h.f.closed()) != 0 {
		t.Fatalf("closed after opened OK")
	}

	// A keystroke goes out at once, as one terminal.input.
	if !h.s.TerminalInput(id, []byte("ls\r")) {
		t.Fatalf("TerminalInput refused")
	}
	m := h.expect(svcTerminal, kTermInput)
	if !bytes.Equal(m.payload, cat(le32(id), []byte("ls\r"))) {
		t.Fatalf("terminal.input % x", m.payload)
	}
	// Nothing to send is not a message.
	if !h.s.TerminalInput(id, nil) {
		t.Fatalf("empty TerminalInput refused")
	}
	h.expectNone()

	// Output reaches the browser in order, byte for byte.
	h.senderOutput(id, "file1  file2\r\n")
	h.senderOutput(id, "$ ")
	if string(h.f.termOutput) != "file1  file2\r\n$ " || h.f.termOutputID != id {
		t.Fatalf("output %q for %d", h.f.termOutput, h.f.termOutputID)
	}

	// Resizes within 1 to 65535; anything else sends nothing.
	if !h.s.TerminalResize(id, 50, 200) {
		t.Fatalf("resize refused")
	}
	h.checkTerminalSize(h.expect(svcTerminal, kTermResize), kTermResize, id, 50, 200)
	if !h.s.TerminalResize(id, 65535, 65535) {
		t.Fatalf("largest resize refused")
	}
	h.checkTerminalSize(h.expect(svcTerminal, kTermResize), kTermResize, id, 65535, 65535)
	for _, size := range [][3]uint32{{id, 0, 80}, {id, 24, 0}, {id, 65536, 80}, {id, 24, 65536}, {id + 1, 24, 80}} {
		if h.s.TerminalResize(size[0], size[1], size[2]) {
			t.Fatalf("resize %v accepted", size)
		}
	}
	h.expectNone()

	// Credit returns what the browser used.
	if !h.s.TerminalCredit(id, 16384) {
		t.Fatalf("credit refused")
	}
	m = h.expect(svcTerminal, kTermCredit)
	if !bytes.Equal(m.payload, cat(le32(id), le32(16384))) {
		t.Fatalf("terminal.credit % x", m.payload)
	}
	if h.s.TerminalCredit(id+1, 1) {
		t.Fatalf("credit for an unknown terminal")
	}

	// The shell exits by itself: the one closing line the browser shows as
	// a success.
	h.senderTerminalStatus(kTermExited, id, 0)
	if len(h.f.closed()) != 1 {
		t.Fatalf("%d closings", len(h.f.closed()))
	}
	h.expectTerminalClosed(0, id, "The shell exited", true)
	// terminal.exited is the last message: nothing is sent, and late
	// messages for the ID are ignored.
	h.expectNone()
	if h.s.TerminalInput(id, []byte("x")) || h.s.TerminalCredit(id, 1) || h.s.TerminalResize(id, 1, 1) {
		t.Fatalf("an ended terminal took input")
	}
	h.senderOutput(id, "late")
	h.senderTerminalStatus(kTermExited, id, 2)
	if len(h.f.termOutput) != 16 || len(h.f.closed()) != 1 || h.s.Counters.ServiceFaults != 0 {
		t.Fatalf("late messages: output %q, %d closings, %d faults", h.f.termOutput, len(h.f.closed()), h.s.Counters.ServiceFaults)
	}
	h.expectNone()

	// Opened without a size: only terminal.open, which selects 24 by 80.
	second := h.browserOpensTerminal(0, 0)
	// One of the two sizes zero: no resize either.
	third := h.browserOpensTerminal(24, 0)
	if second == id || third == second || third == id {
		t.Fatalf("IDs used again: %d %d %d", id, second, third)
	}
}

func TestTerminalCloseAndExitReasons(t *testing.T) {
	h := openServices(t, true, false)
	// The browser closes: terminal.close, and no report back.
	first := h.openRunningTerminal()
	h.expectNone()
	h.s.TerminalClose(first)
	h.expectTerminalCloseSent(first)
	if len(h.f.closed()) != 0 {
		t.Fatalf("TerminalClosed after the browser's close")
	}
	// The sender's answer arrives after the session ended here.
	h.senderTerminalStatus(kTermExited, first, 1)
	if len(h.f.closed()) != 0 {
		t.Fatalf("the sender's answer was reported")
	}
	h.s.TerminalClose(first)
	h.expectNone()

	// The sender loses the shell, or ends it after a withdrawal.
	lost := h.openRunningTerminal()
	closed := h.openRunningTerminal()
	if lost == first || closed == lost {
		t.Fatalf("IDs used again")
	}
	h.senderTerminalStatus(kTermExited, lost, 2)
	h.senderTerminalStatus(kTermExited, closed, 1)
	h.expectTerminalClosed(0, lost, "The target computer lost its shell", false)
	h.expectTerminalClosed(1, closed, "", false)
	h.expectNone()

	// Refusals end the terminal without a close: only OK creates one.
	lines := []string{
		"Terminal unavailable: the target computer allows no remote terminal",
		"Terminal unavailable: the target computer already runs its maximum number of terminals",
		"Terminal unavailable: the target computer could not start a shell",
	}
	for i, line := range lines {
		id := h.browserOpensTerminal(24, 80)
		h.senderTerminalStatus(kTermOpened, id, byte(1+i))
		h.expectTerminalClosed(2+i, id, line, false)
		h.expectNone()
	}
}

func TestTerminalOpenTimeoutAndLimit(t *testing.T) {
	h := openServices(t, true, false)
	slow := h.browserOpensTerminal(24, 80)
	h.f.now += 5000 - 1
	h.tick()
	h.expectNone()
	if len(h.f.closed()) != 0 {
		t.Fatalf("closed before 5 s")
	}
	h.f.now++
	h.tick()
	// The answer may still be on its way with a shell started: close it.
	h.expectTerminalCloseSent(slow)
	h.expectTerminalClosed(0, slow, "Terminal unavailable: the target computer did not answer", false)
	h.senderTerminalStatus(kTermOpened, slow, 0)
	h.senderOutput(slow, "x")
	if h.f.termOutputCalls != 0 {
		t.Fatalf("output of a timed-out terminal")
	}
	h.tick()
	h.expectNone()
	if len(h.f.closed()) != 1 {
		t.Fatalf("%d closings", len(h.f.closed()))
	}

	// An answered terminal has no deadline.
	var ids [4]uint32
	for i := range ids {
		ids[i] = h.openRunningTerminal()
	}
	h.f.now += 60000
	for range ids {
		h.ping()
		h.tick()
	}
	if len(h.f.closed()) != 1 {
		t.Fatalf("an answered terminal timed out")
	}
	// A fifth terminal is refused here, with the sender's own busy line.
	if id, refused := h.s.TerminalOpen(24, 80); id != 0 ||
		refused != "Terminal unavailable: the target computer already runs its maximum number of terminals" {
		t.Fatalf("fifth terminal: %d %q", id, refused)
	}
	h.expectNone()
	h.s.TerminalClose(ids[2])
	h.expectTerminalCloseSent(ids[2])
	fifth := h.browserOpensTerminal(24, 80)
	for _, id := range ids {
		if fifth == id {
			t.Fatalf("ID %d used again", id)
		}
	}
}

func TestTerminalDataLimits(t *testing.T) {
	h := openServices(t, true, false)
	id := h.openRunningTerminal()
	// 65532 bytes of keystrokes fill one payload of exactly 64 KiB; one
	// byte more needs a second message.
	keys := make([]byte, 2*specTerminalData+5)
	for i := range keys {
		keys[i] = byte(i*7 + 1)
	}
	if !h.s.TerminalInput(id, keys[:specTerminalData]) {
		t.Fatalf("largest input refused")
	}
	m := h.expect(svcTerminal, kTermInput)
	if len(m.payload) != specControlMax || !bytes.Equal(m.payload[4:], keys[:specTerminalData]) {
		t.Fatalf("largest input: %d bytes", len(m.payload))
	}
	h.expectNone()
	if !h.s.TerminalInput(id, keys) {
		t.Fatalf("input across messages refused")
	}
	received := 0
	for i := 0; i < 3; i++ {
		m = h.expect(svcTerminal, kTermInput)
		if len(m.payload) <= 4 || len(m.payload) > specControlMax || wle.Uint32(m.payload) != id ||
			!bytes.Equal(m.payload[4:], keys[received:received+len(m.payload)-4]) {
			t.Fatalf("input message %d of %d bytes", i, len(m.payload))
		}
		received += len(m.payload) - 4
	}
	if received != len(keys) {
		t.Fatalf("%d of %d bytes", received, len(keys))
	}
	h.expectNone()
	// One byte under and one byte over a full message.
	h.s.TerminalInput(id, keys[:specTerminalData-1])
	if m = h.expect(svcTerminal, kTermInput); len(m.payload) != specControlMax-1 {
		t.Fatalf("input of 65531 bytes: %d", len(m.payload))
	}
	h.expectNone()
	h.s.TerminalInput(id, keys[:specTerminalData+1])
	if m = h.expect(svcTerminal, kTermInput); len(m.payload) != specControlMax {
		t.Fatalf("first of two: %d", len(m.payload))
	}
	if m = h.expect(svcTerminal, kTermInput); len(m.payload) != 5 || m.payload[4] != keys[specTerminalData] {
		t.Fatalf("second of two: % x", m.payload)
	}
	h.expectNone()

	// Output of the largest size is delivered whole.
	h.senderOutput(id, string(bytes.Repeat([]byte{'o'}, specTerminalData)))
	if len(h.f.termOutput) != specTerminalData || h.s.Counters.ServiceFaults != 0 {
		t.Fatalf("largest output: %d bytes", len(h.f.termOutput))
	}
	// A payload_size one over 64 KiB ends the session at the envelope.
	envelope := wireMsg(svcTerminal, kTermOutput)
	wle.PutUint32(envelope[4:], specControlMax+1)
	h.feed(envelope)
	if h.s.Open() {
		t.Fatalf("a payload over 64 KiB was read")
	}
	h.expectTerminalClosed(0, id, "OS Services disconnected", false)
}

// Each invalid sender message faults the terminal service alone: every
// terminal ends here and on the sender, the session and the clipboard stay,
// and the service's messages count only after a new registration.
func TestTerminalInvalidMessagesFaultTheService(t *testing.T) {
	cases := []struct {
		kind    uint16
		payload []byte
		reason  string
	}{
		{kTermOpened, []byte{1, 0, 0, 0, 0, 0}, "invalid terminal.opened"},
		{kTermOpened, []byte{1, 0, 0, 0, 4}, "invalid terminal.opened"},
		{kTermOpened, []byte{0, 0, 0, 0, 0}, "invalid terminal.opened"},
		{kTermOutput, []byte{1, 0, 0, 0}, "invalid terminal.output"},
		{kTermOutput, []byte{0, 0, 0, 0, 'x'}, "invalid terminal.output"},
		{kTermExited, []byte{1, 0, 0, 0, 3}, "invalid terminal.exited"},
		{kTermExited, []byte{1, 0, 0}, "invalid terminal.exited"},
		{kTermCaps, []byte{2, 0, 0, 0}, "invalid capabilities"},
		{kTermCaps, []byte{1, 0, 0, 0, 0}, "invalid capabilities"},
		{kTermOpen, []byte{1, 0, 0, 0, 0, 0, 0, 0}, "a KVM-to-sender terminal message from the sender"},
		{kTermInput, []byte{1, 0, 0, 0, 'x'}, "a KVM-to-sender terminal message from the sender"},
		{kTermResize, []byte{1, 0, 0, 0, 24, 0, 80, 0}, "a KVM-to-sender terminal message from the sender"},
		{kTermClose, []byte{1, 0, 0, 0}, "a KVM-to-sender terminal message from the sender"},
		{kTermCredit, []byte{1, 0, 0, 0, 0, 1, 0, 0}, "a KVM-to-sender terminal message from the sender"},
	}
	for _, c := range cases {
		t.Run(c.reason, func(t *testing.T) {
			h := openServices(t, true, false)
			h.registerClipboard(1, byte(OSLinux))
			h.expectClipboardConfig(1)
			a := h.openRunningTerminal()
			b := h.browserOpensTerminal(24, 80)
			h.send(svcTerminal, c.kind, c.payload)
			if !h.s.Open() || h.s.Counters.ServiceFaults != 1 || h.s.TerminalAvailable() || !h.s.ClipboardAvailable() {
				t.Fatalf("open %v faults %d terminal %v clipboard %v", h.s.Open(), h.s.Counters.ServiceFaults,
					h.s.TerminalAvailable(), h.s.ClipboardAvailable())
			}
			line := "Terminal faulted: " + c.reason
			if len(h.f.closed()) != 2 {
				t.Fatalf("%d closings", len(h.f.closed()))
			}
			h.expectTerminalClosed(0, a, line, false)
			h.expectTerminalClosed(1, b, line, false)
			h.expectTerminalCloseSent(a)
			h.expectTerminalCloseSent(b)
			h.expectNone()
			// Ignored until it registers again, even a valid message.
			h.senderOutput(a, "x")
			h.send(svcTerminal, kTermOpened, c.payload)
			if h.s.Counters.ServiceFaults != 1 || h.f.termOutputCalls != 0 {
				t.Fatalf("a faulted service's message counted")
			}
			h.registerService(svcTerminal, 1)
			if !h.s.TerminalAvailable() {
				t.Fatalf("registered again: not available")
			}
			again := h.openRunningTerminal()
			h.senderOutput(again, "ok")
			if string(h.f.termOutput) != "ok" {
				t.Fatalf("output %q", h.f.termOutput)
			}
		})
	}
}

func TestTerminalOutputWaitsForTheBrowser(t *testing.T) {
	h := openServices(t, true, false)
	a := h.openRunningTerminal()
	b := h.openRunningTerminal()
	// One OUT read with output for both terminals and a ping.
	first := len(wireMsg(svcTerminal, kTermOutput, le32(a), []byte("slow")))
	read := cat(wireMsg(svcTerminal, kTermOutput, le32(a), []byte("slow")),
		wireMsg(svcTerminal, kTermOutput, le32(b), []byte("fine")),
		wireMsg(svcSession, kPing))

	// The browser's channel is full: the session takes the first message
	// and nothing after it, and offers it again on each tick.
	h.f.backedUp = true
	if n := h.s.Receive(read); n != first || !h.s.Holding() {
		t.Fatalf("Receive took %d, holding %v", n, h.s.Holding())
	}
	if n := h.s.Receive(read[first:]); n != 0 {
		t.Fatalf("Receive while holding took %d", n)
	}
	h.f.now += 2000 - 1
	h.tick()
	if h.f.termOutputCalls != 3 || !h.s.Holding() {
		t.Fatalf("calls %d holding %v", h.f.termOutputCalls, h.s.Holding())
	}

	// It has room: the held output goes first, then the rest of the read.
	h.f.backedUp = false
	h.feed(read[first:])
	if h.s.Holding() || string(h.f.termOutput) != "slowfine" || len(h.f.closed()) != 0 {
		t.Fatalf("output %q holding %v", h.f.termOutput, h.s.Holding())
	}
	h.expectNone()

	// The last message of a read waits for the tick alone.
	h.f.backedUp = true
	h.feed(read[:first])
	if !h.s.Holding() {
		t.Fatalf("not holding")
	}
	h.f.backedUp = false
	h.tick()
	if h.s.Holding() || len(h.f.termOutput) != 12 {
		t.Fatalf("the tick did not deliver: %q", h.f.termOutput)
	}

	// A browser that takes nothing for 2 s: the terminal ends on both sides
	// and the session reads on.
	h.f.backedUp = true
	if n := h.s.Receive(read); n != first {
		t.Fatalf("Receive took %d", n)
	}
	h.f.now += 2000
	h.tick()
	if h.s.Holding() {
		t.Fatalf("still holding after 2 s")
	}
	h.expectTerminalCloseSent(a)
	h.expectTerminalClosed(0, a, "Terminal closed: the browser did not keep up", false)
	h.f.backedUp = false
	h.feed(read[first:])
	if len(h.f.termOutput) != 16 || !h.s.Open() {
		t.Fatalf("output %q open %v", h.f.termOutput, h.s.Open())
	}
	h.expectNone()
}

// A reply behind held output is read once the output goes, before any
// request it answers times out.
func TestRequestsWaitWhileOutputIsHeld(t *testing.T) {
	h := openServices(t, true, false)
	h.registerClipboard(1, byte(OSLinux))
	h.expectClipboardConfig(1)
	a := h.openRunningTerminal()
	get, _ := h.s.ClipboardRequest(nil, false)
	h.expect(svcClipboard, kClipGet)
	h.f.now += 100
	output := wireMsg(svcTerminal, kTermOutput, le32(a), []byte("slow"))
	read := cat(output, wireMsg(svcClipboard, kClipContent, le32(get), []byte{0, 0, 0, 0}, []byte("hi")))
	h.f.backedUp = true
	if n := h.s.Receive(read); n != len(output) {
		t.Fatalf("Receive took %d", n)
	}
	// The request's deadline passes while its answer waits behind the
	// output.
	h.f.now += 2000 - 1
	h.tick()
	h.f.backedUp = false
	h.tick()
	if h.s.Holding() {
		t.Fatalf("still holding")
	}
	h.feed(read[len(output):])
	h.tick()
	if len(h.f.clipboard) != 1 || !h.f.clipboard[0].answered || h.f.clipboard[0].status != ClipboardOK || h.f.clipboard[0].text != "hi" {
		t.Fatalf("clipboard %+v", h.f.clipboard)
	}
	if len(h.f.closed()) != 0 {
		t.Fatalf("a terminal closed")
	}
	h.expectNone()
}

func TestTerminalEndsWithBrowserSettingsAndSession(t *testing.T) {
	// A browser that cannot keep up: the terminal ends on both sides.
	h := openServices(t, true, false)
	slow := h.openRunningTerminal()
	other := h.openRunningTerminal()
	h.f.backedUp = true
	h.senderOutput(slow, "flood")
	h.f.now += 2000
	h.tick()
	h.expectTerminalCloseSent(slow)
	h.expectTerminalClosed(0, slow, "Terminal closed: the browser did not keep up", false)
	h.f.backedUp = false
	h.senderOutput(other, "fine")
	if string(h.f.termOutput) != "fine" {
		t.Fatalf("output %q", h.f.termOutput)
	}

	// The setting goes off: each terminal ends with its line, no close (the
	// sender ends its shells on session.config), then the config.
	third := h.openRunningTerminal()
	settings := defaultSettings()
	settings.TerminalEnabled = false
	h.s.ApplySettings(settings)
	closed := h.f.closed()
	if len(closed) != 3 {
		t.Fatalf("%d closings", len(closed))
	}
	// In slot order: the third terminal took the slow one's slot.
	ends := map[uint32]string{closed[1].id: closed[1].text, closed[2].id: closed[2].text}
	if ends[other] != "Terminal disabled" || ends[third] != "Terminal disabled" {
		t.Fatalf("closings %+v", closed)
	}
	h.checkConfig(h.expect(svcSession, kConfig), false, false, "")
	h.expectNone()
	h.senderTerminalStatus(kTermExited, other, 1)
	h.registerService(svcTerminal, 0)
	if len(h.f.closed()) != 3 {
		t.Fatalf("the sender's answers were reported")
	}
	h.expectNone()

	// The session ends: every terminal ends once, nothing is written.
	h = openServices(t, true, false)
	opening := h.browserOpensTerminal(24, 80)
	running := h.openRunningTerminal()
	h.s.End("the IN endpoint did not drain")
	if len(h.f.closed()) != 2 {
		t.Fatalf("%d closings", len(h.f.closed()))
	}
	h.expectTerminalClosed(0, opening, "OS Services disconnected", false)
	h.expectTerminalClosed(1, running, "OS Services disconnected", false)
	h.expectNone()
	h.s.End("again")
	h.tick()
	if len(h.f.closed()) != 2 || h.s.TerminalInput(running, []byte("x")) {
		t.Fatalf("a second end reported again")
	}

	// Keystrokes that cannot be queued end their terminal with its own line
	// and then the session, which ends the others.
	h = openServices(t, true, false)
	typing := h.openRunningTerminal()
	idle := h.openRunningTerminal()
	h.f.failWrites = true
	if h.s.TerminalInput(typing, []byte("rm -rf build\r")) || h.s.Open() {
		t.Fatalf("lost keystrokes: open %v", h.s.Open())
	}
	if len(h.f.closed()) != 2 {
		t.Fatalf("%d closings", len(h.f.closed()))
	}
	h.expectTerminalClosed(0, typing, "Terminal closed: keystrokes were not delivered", false)
	h.expectTerminalClosed(1, idle, "OS Services disconnected", false)

	// A withdrawal that finds a terminal still opening here.
	h = openServices(t, true, false)
	racing := h.browserOpensTerminal(24, 80)
	h.registerService(svcTerminal, 0)
	h.expectTerminalClosed(0, racing, "Terminal closed: the target computer withdrew its shell", false)
	h.senderTerminalStatus(kTermOpened, racing, 1)
	if len(h.f.closed()) != 1 {
		t.Fatalf("%d closings", len(h.f.closed()))
	}
	h.expectNone()

	// A resize or a close that cannot be queued ends the session.
	h = openServices(t, true, false)
	id := h.openRunningTerminal()
	h.f.failWrites = true
	if h.s.TerminalResize(id, 30, 100) || h.s.Open() || h.f.endReason != "the IN endpoint did not drain" {
		t.Fatalf("failed resize: open %v reason %q", h.s.Open(), h.f.endReason)
	}
	h.expectTerminalClosed(0, id, "OS Services disconnected", false)
	// A terminal.open that cannot be queued opens nothing.
	h = openServices(t, true, false)
	h.f.failWrites = true
	if id, refused := h.s.TerminalOpen(24, 80); id != 0 || refused != "Terminal unavailable: the IN endpoint did not drain" {
		t.Fatalf("failed open: %d %q", id, refused)
	}
	if len(h.f.closed()) != 0 {
		t.Fatalf("a terminal that never opened was reported")
	}
}

// Terminal IDs are never 0 and never used again in the session, also when
// the counter wraps.
func TestTerminalIDsSkipZero(t *testing.T) {
	h := openServices(t, true, false)
	h.s.nextTerminalID = 0xffffffff
	a := h.browserOpensTerminal(0, 0)
	b := h.browserOpensTerminal(0, 0)
	if a != 0xffffffff || b != 1 {
		t.Fatalf("IDs %d, %d", a, b)
	}
}
