package osservices

import (
	"bytes"
	"testing"
)

// Ported from test_os_services_session.c: the clipboard service.

// answerContent is the sender's clipboard.content.
func (h *harness) answerContent(id uint32, status, truncated byte, text string) {
	h.t.Helper()
	h.send(svcClipboard, kClipContent, le32(id), []byte{status, truncated, 0, 0}, []byte(text))
}

func (h *harness) answerStatus(id uint32, status byte) {
	h.t.Helper()
	h.send(svcClipboard, kClipStatus, le32(id), []byte{status})
}

func TestClipboard(t *testing.T) {
	h := newHarness(t, hooksServices, defaultSettings())
	h.openSession()
	if id, status := h.s.ClipboardRequest(nil, false); id != 0 || status != ClipboardUnsupported {
		t.Fatalf("request before registration: %d %v", id, status)
	}

	h.registerClipboard(3, byte(OSLinux))
	h.expectClipboardConfig(1)
	st := h.s.State()
	if !h.s.ClipboardAvailable() || !st.Clipboard || st.ClipboardOS != OSLinux || st.ClipboardFlags != 3 {
		t.Fatalf("after registration: %+v", st)
	}

	// get: the request on the wire, then the answer.
	get, status := h.s.ClipboardRequest(nil, false)
	if get == 0 || status != ClipboardOK {
		t.Fatalf("get: %d %v", get, status)
	}
	m := h.expect(svcClipboard, kClipGet)
	if len(m.payload) != 4 || wle.Uint32(m.payload) != get {
		t.Fatalf("clipboard.get % x", m.payload)
	}
	h.answerContent(get, 0, 0, "héllo, wörld")
	if len(h.f.clipboard) != 1 {
		t.Fatalf("clipboard events %+v", h.f.clipboard)
	}
	if e := h.f.clipboard[0]; e.requestID != get || !e.answered || e.status != ClipboardOK || e.text != "héllo, wörld" || e.truncated {
		t.Fatalf("answer %+v", e)
	}
	// A repeated answer for the finished request is ignored.
	h.answerContent(get, 0, 0, "again")
	if len(h.f.clipboard) != 1 {
		t.Fatalf("a repeated answer was reported")
	}

	// set, with the largest text the wire allows.
	text := bytes.Repeat([]byte{'a'}, specClipboardText)
	set, _ := h.s.ClipboardRequest(text, true)
	if set == 0 || set == get {
		t.Fatalf("set ID %d (get %d)", set, get)
	}
	m = h.expect(svcClipboard, kClipSet)
	if len(m.payload) != 4+len(text) || wle.Uint32(m.payload) != set || !bytes.Equal(m.payload[4:], text) {
		t.Fatalf("clipboard.set of %d bytes", len(m.payload))
	}
	h.answerStatus(set, 0)
	if len(h.f.clipboard) != 2 || !h.f.clipboard[1].answered || h.f.clipboard[1].requestID != set {
		t.Fatalf("set answer %+v", h.f.clipboard)
	}
	// One byte over the limit, or invalid UTF-8, never reaches the wire.
	if id, status := h.s.ClipboardRequest(bytes.Repeat([]byte{'b'}, specClipboardText+1), true); id != 0 || status != ClipboardFailed {
		t.Fatalf("too long: %d %v", id, status)
	}
	if id, _ := h.s.ClipboardRequest([]byte{0xc0, 0x80}, true); id != 0 {
		t.Fatalf("invalid UTF-8 was sent")
	}
	h.expectNone()

	// The KVM abandons a request after 2 seconds; a late answer is ignored.
	slow, _ := h.s.ClipboardRequest(nil, false)
	h.f.now += 2000 - 1
	h.tick()
	if len(h.f.clipboard) != 2 {
		t.Fatalf("abandoned before 2 s")
	}
	h.f.now++
	h.tick()
	if len(h.f.clipboard) != 3 {
		t.Fatalf("not abandoned after 2 s")
	}
	if e := h.f.clipboard[2]; e.requestID != slow || e.answered || e.status != ClipboardFailed {
		t.Fatalf("timeout %+v", e)
	}
	h.answerContent(slow, 0, 0, "late")
	if len(h.f.clipboard) != 3 || !h.s.ClipboardAvailable() || h.s.Counters.ServiceFaults != 0 {
		t.Fatalf("late answer: %d events, faults %d", len(h.f.clipboard), h.s.Counters.ServiceFaults)
	}

	// At most four requests are open.
	h.f.clearOutput()
	var open [4]uint32
	for i := range open {
		open[i], _ = h.s.ClipboardRequest(nil, false)
		if open[i] == 0 {
			t.Fatalf("request %d refused", i)
		}
	}
	if id, status := h.s.ClipboardRequest(nil, false); id != 0 || status != ClipboardFailed {
		t.Fatalf("fifth request: %d %v", id, status)
	}
	// Answers in any order.
	h.answerContent(open[2], 2, 0, "")
	if e := h.f.clipboard[3]; e.requestID != open[2] || e.status != ClipboardEmpty || !e.answered {
		t.Fatalf("answer out of order %+v", e)
	}
	// Withdrawal fails every open request, and the sender hears the
	// setting again.
	h.f.clearOutput()
	h.registerClipboard(0, byte(OSLinux))
	if len(h.f.clipboard) != 7 || h.s.ClipboardAvailable() {
		t.Fatalf("withdrawal: %d events, available %v", len(h.f.clipboard), h.s.ClipboardAvailable())
	}
	for _, e := range h.f.clipboard[4:] {
		if e.answered || e.requestID == open[2] {
			t.Fatalf("withdrawal event %+v", e)
		}
	}
	h.expectClipboardConfig(1)
	h.expectNone()
}

// Every status the protocol lists reaches the platform as sent.
func TestClipboardStatuses(t *testing.T) {
	h := newHarness(t, hooksServices, defaultSettings())
	h.openSession()
	h.registerClipboard(3, byte(OSWindows))
	h.expectClipboardConfig(1)
	for status := byte(0); status <= 4; status++ {
		get, _ := h.s.ClipboardRequest(nil, false)
		text := ""
		if status == 0 {
			text = "x"
		}
		h.answerContent(get, status, 0, text)
		set, _ := h.s.ClipboardRequest([]byte("y"), true)
		h.answerStatus(set, status)
	}
	if len(h.f.clipboard) != 10 || h.s.Counters.ServiceFaults != 0 {
		t.Fatalf("%d events, %d faults", len(h.f.clipboard), h.s.Counters.ServiceFaults)
	}
	for i, e := range h.f.clipboard {
		if e.status != ClipboardStatus(i/2) || !e.answered {
			t.Fatalf("event %d: %+v", i, e)
		}
	}
}

func TestClipboardFaultsAndSettings(t *testing.T) {
	h := newHarness(t, hooksServices, defaultSettings())
	h.openSession()
	h.registerClipboard(1, byte(OSMacOS))
	h.expectClipboardConfig(1)
	// Read access only: a set is unsupported.
	if id, status := h.s.ClipboardRequest([]byte("x"), true); id != 0 || status != ClipboardUnsupported {
		t.Fatalf("set without write access: %d %v", id, status)
	}
	get, _ := h.s.ClipboardRequest(nil, false)
	h.f.clearOutput()
	// Text with a non-OK status is invalid: the clipboard service faults,
	// the session stays.
	h.answerContent(get, 2, 0, "x")
	if !h.s.Open() || h.s.ClipboardAvailable() || h.s.Counters.ServiceFaults != 1 {
		t.Fatalf("fault: open %v available %v faults %d", h.s.Open(), h.s.ClipboardAvailable(), h.s.Counters.ServiceFaults)
	}
	if len(h.f.clipboard) != 1 || h.f.clipboard[0].answered {
		t.Fatalf("the open request did not fail: %+v", h.f.clipboard)
	}
	// Until it registers again its messages are ignored.
	h.answerContent(get, 0, 0, "y")
	if len(h.f.clipboard) != 1 || h.s.Counters.ServiceFaults != 1 {
		t.Fatalf("a faulted service's message counted")
	}
	h.registerClipboard(3, byte(OSWindows))
	h.expectClipboardConfig(1)
	if !h.s.ClipboardAvailable() {
		t.Fatalf("not available after registering again")
	}

	// Invalid capabilities fault the service.
	h.registerClipboard(4, byte(OSWindows))
	if h.s.ClipboardAvailable() {
		t.Fatalf("flags 4 accepted")
	}
	h.registerClipboard(1, 4)
	if h.s.ClipboardAvailable() {
		t.Fatalf("os 4 accepted")
	}
	h.send(svcClipboard, kClipCaps, le32(1), []byte{1, 0, 1, 0})
	if h.s.ClipboardAvailable() {
		t.Fatalf("a reserved byte accepted")
	}
	h.registerClipboard(1, byte(OSWindows))
	h.expectClipboardConfig(1)

	// Sharing off: the sender is told, and no request is sent.
	settings := defaultSettings()
	settings.ClipboardEnabled = false
	h.f.clearOutput()
	h.s.ApplySettings(settings)
	h.expectClipboardConfig(0)
	h.expectNone()
	if id, status := h.s.ClipboardRequest(nil, false); id != 0 || status != ClipboardUnsupported {
		t.Fatalf("request with sharing off: %d %v", id, status)
	}
	if h.s.State().Clipboard {
		t.Fatalf("the browser is offered the clipboard with sharing off")
	}
	// A new registration is answered with the current setting.
	h.registerClipboard(1, byte(OSWindows))
	h.expectClipboardConfig(0)

	// The session policy goes out when it changes, and only then.
	settings.TerminalEnabled = false
	settings.CaptureDisplay = `DP-2 "left"`
	h.s.ApplySettings(settings)
	h.checkConfig(h.expect(svcSession, kConfig), false, false, `DP-2 "left"`)
	h.s.ApplySettings(settings)
	h.expectNone()
}

// Each invalid sender message faults the clipboard service alone.
func TestClipboardInvalidMessagesFaultTheService(t *testing.T) {
	cases := []struct {
		name    string
		kind    uint16
		payload func(get, set uint32) []byte
	}{
		{"caps of 7 bytes", kClipCaps, func(_, _ uint32) []byte { return []byte{1, 0, 0, 0, 3, 0, 0} }},
		{"content of 7 bytes", kClipContent, func(get, _ uint32) []byte { return cat(le32(get), []byte{0, 0, 0}) }},
		{"content status 5", kClipContent, func(get, _ uint32) []byte { return cat(le32(get), []byte{5, 0, 0, 0}) }},
		{"content truncated 2", kClipContent, func(get, _ uint32) []byte { return cat(le32(get), []byte{0, 2, 0, 0}) }},
		{"content reserved byte", kClipContent, func(get, _ uint32) []byte { return cat(le32(get), []byte{0, 0, 0, 1}) }},
		{"content invalid UTF-8", kClipContent, func(get, _ uint32) []byte { return cat(le32(get), []byte{0, 0, 0, 0, 0xff}) }},
		{"content for a set", kClipContent, func(_, set uint32) []byte { return cat(le32(set), []byte{2, 0, 0, 0}) }},
		{"status of 6 bytes", kClipStatus, func(_, set uint32) []byte { return cat(le32(set), []byte{0, 0}) }},
		{"status 5", kClipStatus, func(_, set uint32) []byte { return cat(le32(set), []byte{5}) }},
		{"status for a get", kClipStatus, func(get, _ uint32) []byte { return cat(le32(get), []byte{0}) }},
		{"get from the sender", kClipGet, func(get, _ uint32) []byte { return le32(get) }},
		{"set from the sender", kClipSet, func(get, _ uint32) []byte { return le32(get) }},
		{"config from the sender", kClipConfig, func(_, _ uint32) []byte { return []byte{1} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := openServices(t, true, false)
			h.registerClipboard(3, byte(OSLinux))
			h.expectClipboardConfig(1)
			terminal := h.openRunningTerminal()
			get, _ := h.s.ClipboardRequest(nil, false)
			set, _ := h.s.ClipboardRequest([]byte("x"), true)
			h.f.clearOutput()
			h.send(svcClipboard, c.kind, c.payload(get, set))
			if !h.s.Open() || h.s.Counters.ServiceFaults != 1 || h.s.ClipboardAvailable() {
				t.Fatalf("open %v faults %d available %v", h.s.Open(), h.s.Counters.ServiceFaults, h.s.ClipboardAvailable())
			}
			if len(h.f.clipboard) != 2 || h.f.clipboard[0].answered || h.f.clipboard[1].answered {
				t.Fatalf("open requests did not fail: %+v", h.f.clipboard)
			}
			// The terminal is untouched.
			h.senderOutput(terminal, "ok")
			if string(h.f.termOutput) != "ok" || len(h.f.closed()) != 0 {
				t.Fatalf("the terminal was touched")
			}
			h.expectNone()
		})
	}
}

// Request IDs are never 0, also when the counter wraps.
func TestClipboardRequestIDsSkipZero(t *testing.T) {
	h := newHarness(t, hooksServices, defaultSettings())
	h.openSession()
	h.registerClipboard(1, byte(OSLinux))
	h.expectClipboardConfig(1)
	h.s.nextClipboardRequest = 0xffffffff
	a, _ := h.s.ClipboardRequest(nil, false)
	b, _ := h.s.ClipboardRequest(nil, false)
	if a != 0xffffffff || b != 1 {
		t.Fatalf("IDs %d, %d", a, b)
	}
	if wle.Uint32(h.expect(svcClipboard, kClipGet).payload) != a || wle.Uint32(h.expect(svcClipboard, kClipGet).payload) != b {
		t.Fatalf("the wire carries other IDs")
	}
}
