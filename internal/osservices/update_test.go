package osservices

import (
	"bytes"
	"strings"
	"testing"
)

// Ported from test_os_services_session.c: the update service.

const updatePackage = "linux-x86_64-appimage"

func (h *harness) registerUpdate() {
	h.t.Helper()
	h.send(svcUpdate, kUpdateCaps, le32(1), []byte(updatePackage))
}

func (h *harness) senderUpdateCredit(kind uint16, id, bytes uint32) {
	h.t.Helper()
	h.send(svcUpdate, kind, le32(id), le32(bytes))
}

func (h *harness) senderUpdateStatus(id uint32, status byte, message string) {
	h.t.Helper()
	h.send(svcUpdate, kUpdateStatus, le32(id), []byte{status, 0, 0, 0}, []byte(message))
}

var testSHA = [32]byte{1, 2, 3}

// beginUpdate is an open session whose sender installs packages, with
// update.begin sent for a package of size bytes.
func beginUpdate(t *testing.T, size uint64) (*harness, uint32) {
	t.Helper()
	h := openServices(t, false, false)
	if h.s.UpdateAvailable() {
		t.Fatalf("update available before registration")
	}
	if id, refused := h.s.UpdateBegin(size, testSHA, "0.4.3"); id != 0 || refused != "no OS Services sender that installs updates is connected" {
		t.Fatalf("UpdateBegin before registration: %d %q", id, refused)
	}
	h.registerUpdate()
	if !h.s.UpdateAvailable() || h.s.UpdatePackage() != updatePackage {
		t.Fatalf("after registration: available %v package %q", h.s.UpdateAvailable(), h.s.UpdatePackage())
	}
	id, refused := h.s.UpdateBegin(size, testSHA, "0.4.3")
	if id == 0 || refused != "" {
		t.Fatalf("UpdateBegin: %d %q", id, refused)
	}
	m := h.expect(svcUpdate, kUpdateBegin)
	p := m.payload
	if len(p) != 44+5 || wle.Uint32(p) != id || wle.Uint64(p[4:]) != size || !bytes.Equal(p[12:44], testSHA[:]) ||
		string(p[44:]) != "0.4.3" {
		t.Fatalf("update.begin % x", p)
	}
	return h, id
}

// readUpdateChunks reads the update.chunk messages the KVM wrote, checks
// their offsets from offset, and copies them into into.
func (h *harness) readUpdateChunks(id uint32, offset uint64, into []byte) uint64 {
	h.t.Helper()
	total := uint64(0)
	for {
		m, ok := h.next()
		if !ok {
			return total
		}
		if m.service != svcUpdate || m.kind != kUpdateChunk || len(m.payload) <= 12 || len(m.payload)-12 > specChunkData ||
			wle.Uint32(m.payload) != id || wle.Uint64(m.payload[4:]) != offset+total {
			h.t.Fatalf("update.chunk: service %d kind %d, %d bytes at %d (want %d)", m.service, m.kind,
				len(m.payload), wle.Uint64(m.payload[4:]), offset+total)
		}
		if into != nil {
			copy(into[offset+total:], m.payload[12:])
		}
		total += uint64(len(m.payload) - 12)
	}
}

func TestUpdateCreditAndInstalling(t *testing.T) {
	data := make([]byte, 160000)
	for i := range data {
		data[i] = byte(i * 7)
	}
	const size = 150000
	h, id := beginUpdate(t, size)
	if again, refused := h.s.UpdateBegin(1, [32]byte{}, "1"); again != 0 || refused != "an OS Services update is already in progress" {
		t.Fatalf("second UpdateBegin: %d %q", again, refused)
	}
	// Nothing goes before update.ready, then only within the credit.
	if n := h.s.UpdateSend(id, data[:size]); n != 0 {
		t.Fatalf("%d bytes before update.ready", n)
	}
	h.senderUpdateCredit(kUpdateReady, id, 100000)
	if n := h.s.UpdateSend(id, data[:size]); n != 100000 {
		t.Fatalf("%d bytes sent with 100000 of credit", n)
	}
	received := make([]byte, size)
	if n := h.readUpdateChunks(id, 0, received); n != 100000 {
		t.Fatalf("%d bytes on the wire", n)
	}
	// A stall: no credit, nothing sent, until the sender returns some.
	if n := h.s.UpdateSend(id, data[100000:size]); n != 0 {
		t.Fatalf("%d bytes without credit", n)
	}
	h.expectNone()
	h.senderUpdateCredit(kUpdateCredit, id, 1<<20)
	// Credit beyond the package does not allow bytes beyond it.
	if n := h.s.UpdateSend(id, data[100000:]); n != 50000 {
		t.Fatalf("%d bytes past the credit", n)
	}
	if n := h.readUpdateChunks(id, 100000, received); n != 50000 {
		t.Fatalf("%d bytes on the wire", n)
	}
	if !bytes.Equal(received, data[:size]) {
		t.Fatalf("the package changed on the wire")
	}
	if u := h.s.Update(); u.Sent != size || u.Size != size || u.UpdateID != id {
		t.Fatalf("Update %+v", u)
	}
	// Messages for another update are in transit, and ignored.
	h.senderUpdateStatus(id+1, 1, "old")
	h.senderUpdateCredit(kUpdateCredit, id+1, 5)
	if h.s.Update().Ended {
		t.Fatalf("a message for another update ended this one")
	}
	h.senderUpdateStatus(id, 0, "")
	if u := h.s.Update(); !u.Ended || u.Status != UpdateInstalling || h.s.State().Update.Status != UpdateInstalling {
		t.Fatalf("after Installing: %+v", u)
	}
	h.expectNone()
	if n := h.s.UpdateSend(id, data[:1]); n != 0 {
		t.Fatalf("bytes after the end")
	}

	// The next update gets a new ID.
	next, refused := h.s.UpdateBegin(1, [32]byte{}, "")
	if refused != "" || next == id {
		t.Fatalf("next update: %d %q", next, refused)
	}
	// update.begin refuses what the protocol does not allow.
	h.s.UpdateCancel(next, "")
	for _, c := range []struct {
		size    uint64
		version string
	}{{0, "1"}, {1, strings.Repeat("9", 65)}} {
		if id, refused := h.s.UpdateBegin(c.size, [32]byte{}, c.version); id != 0 || refused != "invalid OS Services release" {
			t.Fatalf("UpdateBegin(%d, %d bytes): %d %q", c.size, len(c.version), id, refused)
		}
	}
	if id, _ := h.s.UpdateBegin(1, [32]byte{}, strings.Repeat("9", 64)); id == 0 {
		t.Fatalf("a 64-byte version refused")
	}
}

func TestUpdateEnds(t *testing.T) {
	data := make([]byte, 64)
	// The browser's cancel.
	h, id := beginUpdate(t, uint64(len(data)))
	h.s.UpdateCancel(id, "stopped")
	if m := h.expect(svcUpdate, kUpdateCancel); len(m.payload) != 4 || wle.Uint32(m.payload) != id {
		t.Fatalf("update.cancel % x", m.payload)
	}
	if u := h.s.Update(); !u.Ended || u.Status != UpdateCancelled || u.Message != "stopped" {
		t.Fatalf("cancelled: %+v", u)
	}
	h.senderUpdateStatus(id, 2, "")
	h.s.UpdateCancel(id, "again")
	h.expectNone()

	// 30 seconds without progress on either side.
	h, id = beginUpdate(t, uint64(len(data)))
	h.f.now += 20000
	h.senderUpdateCredit(kUpdateReady, id, 32)
	h.f.now += 20000
	if n := h.s.UpdateSend(id, data); n != 32 {
		t.Fatalf("%d bytes", n)
	}
	h.readUpdateChunks(id, 0, nil)
	h.f.now += 30000 - 1
	h.ping()
	h.tick()
	if h.s.Update().Ended {
		t.Fatalf("ended before 30 s")
	}
	h.f.now++
	h.tick()
	h.expect(svcUpdate, kUpdateCancel)
	if u := h.s.Update(); !u.Ended || u.Status != UpdateFailed || u.Message != "the update made no progress for 30 seconds" {
		t.Fatalf("idle: %+v", u)
	}

	// The sender's failure, cut to 64 bytes on a character boundary.
	h, id = beginUpdate(t, uint64(len(data)))
	h.senderUpdateStatus(id, 1, strings.Repeat("x", 63)+"\xc3\xa9 and more")
	if u := h.s.Update(); u.Status != UpdateFailed || u.Message != strings.Repeat("x", 63) {
		t.Fatalf("failure: %+v", u)
	}
	h.expectNone()
	// What JSON would escape becomes '?', so the browser's event shows the
	// message at its own size.
	h, id = beginUpdate(t, uint64(len(data)))
	h.senderUpdateStatus(id, 1, "a\x01\"b\\c\n\xc3\xa9")
	if m := h.s.Update().Message; m != "a??b?c?\xc3\xa9" {
		t.Fatalf("message %q", m)
	}
	h.expectNone()

	// A withdrawal, and the session's end.
	h, _ = beginUpdate(t, uint64(len(data)))
	h.registerService(svcUpdate, 0)
	h.expect(svcUpdate, kUpdateCancel)
	if h.s.UpdateAvailable() || h.s.Update().Message != "the sender withdrew the update service" {
		t.Fatalf("withdrawal: available %v %+v", h.s.UpdateAvailable(), h.s.Update())
	}
	h, id = beginUpdate(t, uint64(len(data)))
	h.s.End("test")
	if u := h.s.Update(); !u.Ended || u.Message != "the OS Services session ended" {
		t.Fatalf("session end: %+v", u)
	}
	if n := h.s.UpdateSend(id, data[:1]); n != 0 {
		t.Fatalf("bytes after the end")
	}

	// A chunk that cannot be queued ends the session.
	h, id = beginUpdate(t, uint64(len(data)))
	h.senderUpdateCredit(kUpdateReady, id, 64)
	h.f.failWrites = true
	if n := h.s.UpdateSend(id, data); n != 0 || h.s.Open() {
		t.Fatalf("failed chunk: %d sent, open %v", n, h.s.Open())
	}
}

func TestUpdateInvalidMessagesFaultTheService(t *testing.T) {
	cases := []struct {
		name    string
		kind    uint16
		payload []byte
		ready   bool
	}{
		{"ready of 7 bytes", kUpdateReady, make([]byte, 7), false},
		{"credit before ready", kUpdateCredit, make([]byte, 8), false},
		{"ready twice", kUpdateReady, make([]byte, 8), true},
		{"status 3", kUpdateStatus, []byte{0, 0, 0, 0, 3, 0, 0, 0}, false},
		{"status reserved byte", kUpdateStatus, []byte{0, 0, 0, 0, 1, 0, 1, 0}, false},
		{"cancelled with a message", kUpdateStatus, []byte{0, 0, 0, 0, 2, 0, 0, 0, 'x'}, false},
		{"failed with invalid UTF-8", kUpdateStatus, []byte{0, 0, 0, 0, 1, 0, 0, 0, 0xff}, false},
		// Installing before every byte was sent.
		{"installing early", kUpdateStatus, []byte{0, 0, 0, 0, 0, 0, 0, 0}, true},
		{"chunk from the sender", kUpdateChunk, make([]byte, 12), false},
		{"begin from the sender", kUpdateBegin, make([]byte, 44), false},
		{"cancel from the sender", kUpdateCancel, make([]byte, 4), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, id := beginUpdate(t, 64)
			if c.ready {
				h.senderUpdateCredit(kUpdateReady, id, 8)
			}
			payload := append([]byte(nil), c.payload...)
			wle.PutUint32(payload, id)
			h.send(svcUpdate, c.kind, payload)
			h.expect(svcUpdate, kUpdateCancel)
			h.expectNone()
			if !h.s.Update().Ended || !h.s.Open() || !h.s.serviceFaulted[ServiceUpdate] || h.s.UpdateAvailable() {
				t.Fatalf("ended %v open %v available %v", h.s.Update().Ended, h.s.Open(), h.s.UpdateAvailable())
			}
			// Registered again, it works again.
			h.registerUpdate()
			if next, refused := h.s.UpdateBegin(64, testSHA, ""); next == 0 || next == id || refused != "" {
				t.Fatalf("after the fault: %d %q", next, refused)
			}
		})
	}
}

// update.capabilities: flags and package together, at most 64 bytes of
// UTF-8.
func TestUpdateCapabilities(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		valid   bool
	}{
		{"install with a package", cat(le32(1), []byte(updatePackage)), true},
		{"withdrawal", le32(0), true},
		{"package of 64 bytes", cat(le32(1), bytes.Repeat([]byte{'p'}, 64)), true},
		{"package of 65 bytes", cat(le32(1), bytes.Repeat([]byte{'p'}, 65)), false},
		{"install without a package", le32(1), false},
		{"a package without install", cat(le32(0), []byte("x")), false},
		{"unknown flag", cat(le32(3), []byte("x")), false},
		{"package of invalid UTF-8", cat(le32(1), []byte{0xff}), false},
		{"3 bytes", []byte{1, 0, 0}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := openServices(t, false, false)
			changes := h.f.stateChanges
			h.send(svcUpdate, kUpdateCaps, c.payload)
			faulted := h.s.Counters.ServiceFaults == 1
			if faulted == c.valid || !h.s.Open() || h.f.stateChanges == changes {
				t.Fatalf("faults %d, state changed %v", h.s.Counters.ServiceFaults, h.f.stateChanges != changes)
			}
			if c.valid && h.s.UpdateAvailable() != (wle.Uint32(c.payload) == 1) {
				t.Fatalf("available %v", h.s.UpdateAvailable())
			}
			h.expectNone()
		})
	}
}
