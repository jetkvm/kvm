package osservices

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// Ported from test_os_services_session.c: the file service.

// checkDrop reads file.drop as the protocol lays it out: file_count, then
// per file transfer_id, size, name_length and name, and nothing after.
func (h *harness) checkDrop(files []FileEntry, ids []uint32) {
	h.t.Helper()
	m := h.expect(svcFile, kFileDrop)
	p := m.payload
	if len(p) < 4 || int(wle.Uint32(p)) != len(files) {
		h.t.Fatalf("file.drop count % x", trim(p))
	}
	o := 4
	seen := map[uint32]bool{}
	for i, f := range files {
		if o+14 > len(p) {
			h.t.Fatalf("file.drop entry %d cut short", i)
		}
		id := wle.Uint32(p[o:])
		if id == 0 || id != ids[i] || seen[id] {
			h.t.Fatalf("file.drop entry %d: transfer_id %d, want %d", i, id, ids[i])
		}
		seen[id] = true
		nameLength := int(wle.Uint16(p[o+12:]))
		if wle.Uint64(p[o+4:]) != f.Size || nameLength != len(f.Name) || o+14+nameLength > len(p) ||
			string(p[o+14:o+14+nameLength]) != f.Name {
			h.t.Fatalf("file.drop entry %d does not match %q", i, f.Name)
		}
		o += 14 + nameLength
	}
	if o != len(p) {
		h.t.Fatalf("%d bytes after the last file.drop entry", len(p)-o)
	}
}

func (h *harness) browserDrops(files []FileEntry) (uint32, []uint32) {
	h.t.Helper()
	dropID, ids, refused := h.s.FileDrop(files)
	if refused != "" || dropID == 0 || len(ids) != len(files) {
		h.t.Fatalf("drop refused: %q", refused)
	}
	h.checkDrop(files, ids)
	for _, id := range ids {
		if id == dropID {
			h.t.Fatalf("a transfer ID equals the drop ID")
		}
	}
	h.expectNone()
	return dropID, ids
}

func (h *harness) senderCredit(kind uint16, id, bytes uint32) {
	h.t.Helper()
	h.send(svcFile, kind, le32(id), le32(bytes))
}

func (h *harness) senderDone(id uint32, status byte, path string) {
	h.t.Helper()
	h.send(svcFile, kFileDone, le32(id), []byte{status, 0, 0, 0}, []byte(path))
}

func (h *harness) expectChunk(id uint32, offset uint64, data []byte) {
	h.t.Helper()
	m := h.expect(svcFile, kFileChunk)
	if len(m.payload) != 12+len(data) || wle.Uint32(m.payload) != id || wle.Uint64(m.payload[4:]) != offset ||
		!bytes.Equal(m.payload[12:], data) {
		h.t.Fatalf("file.chunk of %d bytes for %d at %d, want %d bytes for %d at %d",
			len(m.payload)-12, wle.Uint32(m.payload), wle.Uint64(m.payload[4:]), len(data), id, offset)
	}
}

func (h *harness) expectCancelSent(id uint32) {
	h.t.Helper()
	m := h.expect(svcFile, kFileCancel)
	if len(m.payload) != 4 || wle.Uint32(m.payload) != id {
		h.t.Fatalf("file.cancel % x, want %d", m.payload, id)
	}
}

func (h *harness) expectCreditEvent(index int, dropID, id uint32, bytes uint64) {
	h.t.Helper()
	events := h.f.fileEvents()
	if len(events) <= index {
		h.t.Fatalf("%d file events, want a credit at %d", len(events), index)
	}
	if e := events[index]; e.kind != "credit" || e.id != dropID || e.transfer != id || e.bytes != bytes {
		h.t.Fatalf("file event %d: %+v, want credit %d for %d/%d", index, e, bytes, dropID, id)
	}
}

func (h *harness) expectDoneEvent(index int, dropID, id uint32, status FileStatus, path string, last bool) {
	h.t.Helper()
	events := h.f.fileEvents()
	if len(events) <= index {
		h.t.Fatalf("%d file events, want an outcome at %d", len(events), index)
	}
	if e := events[index]; e.kind != "done" || e.id != dropID || e.transfer != id || e.status != status ||
		e.text != path || e.last != last {
		h.t.Fatalf("file event %d: %+v, want done %v %q last %v for %d/%d", index, e, status, path, last, dropID, id)
	}
}

func entries(names ...string) []FileEntry {
	out := make([]FileEntry, len(names))
	for i, n := range names {
		out[i] = FileEntry{Name: n, Size: uint64(i)}
	}
	return out
}

func TestFileOfferFollowsRegistrationAndSettings(t *testing.T) {
	// The default policy has files off: registered or not, no drop.
	h := newHarness(t, hooksServices, defaultSettings())
	h.openSession()
	h.registerService(svcFile, 1)
	if !h.s.fileReceive || h.s.FileAvailable() || h.s.State().FileReceive {
		t.Fatalf("files off: receive %v available %v", h.s.fileReceive, h.s.FileAvailable())
	}
	if dropID, _, refused := h.s.FileDrop([]FileEntry{{"a.txt", 3}}); dropID != 0 || refused != "no OS Services file transfer is connected" {
		t.Fatalf("drop with files off: %d %q", dropID, refused)
	}
	h.expectNone()

	// Turned on: the sender hears it, and the offer follows.
	settings := defaultSettings()
	settings.FilesEnabled = true
	h.s.ApplySettings(settings)
	h.checkConfig(h.expect(svcSession, kConfig), true, true, "")
	if !h.s.FileAvailable() || !h.s.State().FileReceive {
		t.Fatalf("files on: not available")
	}
	// Withdrawn and registered again.
	h.registerService(svcFile, 0)
	if h.s.FileAvailable() {
		t.Fatalf("withdrawn: available")
	}
	h.registerService(svcFile, 1)
	if !h.s.FileAvailable() {
		t.Fatalf("registered again: not available")
	}
	h.expectNone()
	// Turned off again.
	settings.FilesEnabled = false
	h.s.ApplySettings(settings)
	h.checkConfig(h.expect(svcSession, kConfig), true, false, "")
	if h.s.FileAvailable() {
		t.Fatalf("files off again: available")
	}
	h.expectNone()

	// A platform that relays no outcomes offers no file reception.
	settings.FilesEnabled = true
	h = newHarness(t, hooksNoFiles, settings)
	h.openSession()
	h.registerService(svcFile, 1)
	if !h.s.fileReceive || h.s.FileAvailable() {
		t.Fatalf("no file hooks: receive %v available %v", h.s.fileReceive, h.s.FileAvailable())
	}
}

func TestFileDropLimits(t *testing.T) {
	h := openServices(t, false, true)
	// Names: one path component without a control character.
	invalid := []string{
		"", ".", "..", "a/b", "a\\b", "/", "tab\tname", "line\nbreak",
		"nul\x01", "del\x7f", "c1\xc2\x85", "c1\xc2\x9f", "bad\xff",
		"overlong\xc0\xae", "surrogate\xed\xa0\x80", "cut\xe2\x82",
	}
	for _, name := range invalid {
		dropID, ids, refused := h.s.FileDrop([]FileEntry{{name, 1}})
		if dropID != 0 || ids != nil || !strings.HasPrefix(refused, `invalid file name "`) {
			t.Fatalf("name %q: %d %q", name, dropID, refused)
		}
	}
	// The text names the file, escaped, so it stays one line of UTF-8.
	if _, _, refused := h.s.FileDrop([]FileEntry{{"a\nb\"\xff", 1}}); refused != `invalid file name "a\x0ab\"\xff"` {
		t.Fatalf("refusal %q", refused)
	}
	// A long name is cut at a character, inside the quotes.
	for _, name := range []string{strings.Repeat("é", 50) + "/", strings.Repeat("\x01", 50), strings.Repeat("\xff", 50)} {
		_, _, refused := h.s.FileDrop([]FileEntry{{name, 1}})
		quoted := strings.TrimPrefix(refused, "invalid file name ")
		if !strings.HasPrefix(quoted, `"`) || !strings.HasSuffix(quoted, `..."`) || len(quoted) > 70 || !UTF8Valid([]byte(quoted)) {
			t.Fatalf("long name %q: %q", name, refused)
		}
	}
	h.expectNone()
	if _, _, refused := h.s.FileDrop(nil); refused != "file.drop needs at least one file" {
		t.Fatalf("empty drop: %q", refused)
	}

	// Names that look odd but are one component.
	valid := entries("...", ".env", "a..b", "r\xc3\xa9sum\xc3\xa9.pdf", "a b", "\xf0\x9f\x93\x84", "x\xc2\xa0y")
	dropID, _ := h.browserDrops(valid)
	// One drop at a time.
	if _, _, refused := h.s.FileDrop(valid[:1]); refused != "a file transfer is already in progress" {
		t.Fatalf("second drop: %q", refused)
	}
	h.expectNone()
	h.s.FileChannelClosed(dropID)
	for i := range valid {
		h.expectCancelSent(dropID + 1 + uint32(i))
	}
	h.expectNone()

	// The largest drop the KVM takes, and one file more.
	files := make([]FileEntry, FileDropMax+1)
	for i := range files {
		files[i] = FileEntry{fmt.Sprintf("f%d", i), uint64(i)}
	}
	if _, _, refused := h.s.FileDrop(files); refused != "too many files for one drop: 257 (at most 256)" {
		t.Fatalf("257 files: %q", refused)
	}
	h.expectNone()
	dropID, _ = h.browserDrops(files[:FileDropMax])
	h.s.FileChannelClosed(dropID)
	h.f.clearOutput()

	// The file.drop payload: one name filling 64 KiB exactly, then one byte
	// more.
	longest := specControlMax - 4 - 14
	long := strings.Repeat("n", longest+1)
	if _, _, refused := h.s.FileDrop([]FileEntry{{long, 1}}); refused != "too many file names for one drop: 65537 bytes" {
		t.Fatalf("name over the payload: %q", refused)
	}
	h.expectNone()
	dropID, _ = h.browserDrops([]FileEntry{{long[:longest], 1}})
	h.s.FileChannelClosed(dropID)
	h.f.clearOutput()
	// Spread over two files, the same limit.
	two := []FileEntry{{long[:30000], 0}, {long[:longest-30000-14+1], ^uint64(0)}}
	if _, _, refused := h.s.FileDrop(two); refused == "" {
		t.Fatalf("two names over the payload accepted")
	}
	two[1].Name = two[1].Name[1:]
	h.browserDrops(two)
	h.expectNone()
}

func TestFileTransferAndCredit(t *testing.T) {
	h := openServices(t, false, true)
	data := make([]byte, 300000)
	for i := range data {
		data[i] = byte(i*31 + 7)
	}
	files := []FileEntry{{"big.bin", 200000}, {"small.txt", 5}}
	dropID, ids := h.browserDrops(files)
	// Before the channel opens, the sender's credit waits.
	h.senderCredit(kFileReady, ids[0], 131072)
	if len(h.f.fileEvents()) != 0 {
		t.Fatalf("credit before the channel opened")
	}
	if h.s.FileAttach(dropID+1000) || !h.s.FileAttach(dropID) || h.s.FileAttach(dropID) {
		t.Fatalf("FileAttach accepts only the drop, once")
	}
	h.expectCreditEvent(0, dropID, ids[0], 131072)
	if size, ok := h.s.FileSize(dropID, ids[0]); !ok || size != 200000 {
		t.Fatalf("FileSize %d %v", size, ok)
	}
	if _, ok := h.s.FileSize(dropID, ids[1]+1); ok {
		t.Fatalf("FileSize of no transfer")
	}

	// Bytes within the credit go out at once, at the next offset.
	if !h.s.FileData(dropID, ids[0], data[:32768]) {
		t.Fatalf("FileData refused")
	}
	h.expectChunk(ids[0], 0, data[:32768])
	if !h.s.FileData(dropID, ids[0], data[32768:131072]) {
		t.Fatalf("FileData refused")
	}
	// 98304 bytes: one chunk of 65524 and the rest.
	h.expectChunk(ids[0], 32768, data[32768:32768+specChunkData])
	h.expectChunk(ids[0], 32768+specChunkData, data[32768+specChunkData:131072])
	h.expectNone()
	// IDs that name no transfer of the drop.
	if h.s.FileData(dropID, 0, data[:1]) || h.s.FileData(dropID, ids[0], nil) ||
		h.s.FileData(dropID+1, ids[0], data[:1]) || h.s.FileData(dropID, ids[1]+1, data[:1]) || h.s.FileData(0, ids[0], data[:1]) {
		t.Fatalf("FileData for no transfer")
	}
	h.expectNone()
	// The sender returns credit; it reaches the browser one to one, and the
	// file's last byte may be sent.
	h.senderCredit(kFileCredit, ids[0], 65536)
	h.senderCredit(kFileCredit, ids[0], 4096)
	h.expectCreditEvent(1, dropID, ids[0], 65536)
	h.expectCreditEvent(2, dropID, ids[0], 4096)
	if !h.s.FileData(dropID, ids[0], data[131072:200000]) {
		t.Fatalf("FileData refused")
	}
	h.expectChunk(ids[0], 131072, data[131072:131072+specChunkData])
	h.expectChunk(ids[0], 131072+specChunkData, data[131072+specChunkData:200000])
	h.senderDone(ids[0], 0, "/home/u/Downloads/big.bin")
	h.expectDoneEvent(3, dropID, ids[0], FileOK, "/home/u/Downloads/big.bin", false)
	// Credit for a transfer that ended is not passed on.
	h.senderCredit(kFileCredit, ids[0], 1)
	if len(h.f.fileEvents()) != 4 {
		t.Fatalf("credit after the outcome was passed on")
	}

	// The second file: its size bounds the bytes whatever the credit.
	h.senderCredit(kFileReady, ids[1], 524288)
	h.expectCreditEvent(4, dropID, ids[1], 524288)
	if h.s.FileData(dropID, ids[1], data[:6]) {
		t.Fatalf("bytes past the size accepted")
	}
	// That failed the drop: both sides learn of it exactly once.
	h.expectCancelSent(ids[1])
	h.expectNone()
	if len(h.f.fileEvents()) != 6 {
		t.Fatalf("%d file events", len(h.f.fileEvents()))
	}
	h.expectDoneEvent(5, dropID, ids[1], FileFailed, "", true)
	if h.s.FileData(dropID, ids[1], data[:5]) || h.s.FileCancel(dropID, ids[1]) {
		t.Fatalf("a failed transfer took data or a cancel")
	}
	h.senderDone(ids[1], 2, "")
	if len(h.f.fileEvents()) != 6 || h.s.Counters.ServiceFaults != 0 || h.s.FileAttach(dropID) {
		t.Fatalf("the sender's answer was reported")
	}

	// The next drop takes new IDs.
	next, nextIDs := h.browserDrops(files[1:])
	if next <= ids[1] || nextIDs[0] <= next {
		t.Fatalf("next drop %d %v after %v", next, nextIDs, ids)
	}
}

// Browser data is cut into file.chunk messages of at most 65524 bytes: a
// payload of at most 64 KiB with the 12-byte chunk header.
func TestFileChunkBoundaries(t *testing.T) {
	h := openServices(t, false, true)
	const max = specChunkData
	data := make([]byte, 3*max)
	for i := range data {
		data[i] = byte(i*13 + 5)
	}
	dropID, ids := h.browserDrops([]FileEntry{{"cuts.bin", uint64(len(data))}})
	id := ids[0]
	h.s.FileAttach(dropID)
	h.senderCredit(kFileReady, id, uint32(len(data)))
	// One under, exactly, and one over the largest chunk.
	h.s.FileData(dropID, id, data[:max-1])
	h.expectChunk(id, 0, data[:max-1])
	h.expectNone()
	h.s.FileData(dropID, id, data[max-1:2*max-1])
	h.expectChunk(id, max-1, data[max-1:2*max-1])
	h.expectNone()
	h.s.FileData(dropID, id, data[2*max-1:])
	h.expectChunk(id, 2*max-1, data[2*max-1:3*max-1])
	h.expectChunk(id, 3*max-1, data[3*max-1:])
	h.expectNone()
	h.senderDone(id, 0, "/h/cuts.bin")
	h.expectDoneEvent(1, dropID, id, FileOK, "/h/cuts.bin", true)
}

func TestFileCreditBoundaryAndOverrun(t *testing.T) {
	data := bytes.Repeat([]byte{'d'}, 4096)
	files := []FileEntry{{"a", 10000}, {"b", 10}, {"c", 10}}
	h := openServices(t, false, true)
	dropID, ids := h.browserDrops(files)
	h.s.FileAttach(dropID)
	// No credit before file.ready: a single byte is beyond it.
	if h.s.FileData(dropID, ids[0], data[:1]) {
		t.Fatalf("a byte without credit accepted")
	}
	for i := range ids {
		h.expectCancelSent(ids[i])
		h.expectDoneEvent(i, dropID, ids[i], FileFailed, "", i == 2)
	}
	h.expectNone()

	h = openServices(t, false, true)
	second, ids := h.browserDrops(files)
	h.s.FileAttach(second)
	h.senderCredit(kFileReady, ids[0], 4096)
	// Exactly the credit is fine; one byte more is not.
	if !h.s.FileData(second, ids[0], data[:4000]) || !h.s.FileData(second, ids[0], data[:96]) {
		t.Fatalf("data within the credit refused")
	}
	h.expectChunk(ids[0], 0, data[:4000])
	h.expectChunk(ids[0], 4000, data[:96])
	h.senderCredit(kFileCredit, ids[0], 100)
	if h.s.FileData(second, ids[0], data[:101]) {
		t.Fatalf("data past the credit accepted")
	}
	h.expectCancelSent(ids[0])
	h.expectCancelSent(ids[1])
	h.expectCancelSent(ids[2])
	h.expectNone()
	if len(h.f.fileEvents()) != 5 {
		t.Fatalf("%d file events", len(h.f.fileEvents()))
	}
	h.expectDoneEvent(2, second, ids[0], FileFailed, "", false)
	h.expectDoneEvent(4, second, ids[2], FileFailed, "", true)
}

func TestFileZeroByteFilesAndHeldOutcomes(t *testing.T) {
	h := openServices(t, false, true)
	files := []FileEntry{{"empty1", 0}, {"empty2", 0}, {"gone", 7}}
	dropID, ids := h.browserDrops(files)
	// An empty file is complete as created: file.ready, then file.done.
	h.senderCredit(kFileReady, ids[0], 524288)
	h.senderDone(ids[0], 0, "/home/u/empty1")
	h.senderCredit(kFileReady, ids[1], 524288)
	h.senderDone(ids[1], 0, "/home/u/empty2")
	// The sender could not create the third.
	h.senderDone(ids[2], 1, "")
	if len(h.f.fileEvents()) != 0 {
		t.Fatalf("outcomes before the channel opened")
	}
	h.expectNone()
	// The drop is over; its outcomes wait for the channel.
	if !h.s.FileAttach(dropID) || len(h.f.fileEvents()) != 3 {
		t.Fatalf("attach: %d events", len(h.f.fileEvents()))
	}
	h.expectDoneEvent(0, dropID, ids[0], FileOK, "", false)
	h.expectDoneEvent(1, dropID, ids[1], FileOK, "", false)
	h.expectDoneEvent(2, dropID, ids[2], FileFailed, "", true)
	if h.s.FileAttach(dropID) {
		t.Fatalf("a finished drop attached again")
	}

	// Attached first: each empty file's outcome comes as it happens, with
	// its path.
	live, one := h.browserDrops(files[:1])
	h.s.FileAttach(live)
	h.senderCredit(kFileReady, one[0], 524288)
	h.expectCreditEvent(3, live, one[0], 524288)
	h.senderDone(one[0], 0, "/home/u/empty1 (2)")
	h.expectDoneEvent(4, live, one[0], FileOK, "/home/u/empty1 (2)", true)
	h.expectNone()

	// A finished drop whose channel never opens: a new drop replaces it, and
	// after 30 seconds it is forgotten anyway.
	lonely, one := h.browserDrops(files[:1])
	h.senderDone(one[0], 1, "")
	replacing, again := h.browserDrops(files[:1])
	if h.s.FileAttach(lonely) {
		t.Fatalf("a replaced drop attached")
	}
	h.senderDone(again[0], 1, "")
	h.f.now += 30000
	h.ping()
	h.tick()
	if h.s.FileAttach(replacing) || len(h.f.fileEvents()) != 5 {
		t.Fatalf("a drop kept past 30 s")
	}
	h.expectNone()
}

func TestFileCancels(t *testing.T) {
	h := openServices(t, false, true)
	data := make([]byte, 64)
	files := []FileEntry{{"a", 64}, {"b", 64}, {"c", 64}}
	dropID, ids := h.browserDrops(files)
	h.s.FileAttach(dropID)
	h.senderCredit(kFileReady, ids[0], 524288)
	h.s.FileData(dropID, ids[0], data[:10])
	h.expectChunk(ids[0], 0, data[:10])
	// The browser stops the active file and a queued one.
	if !h.s.FileCancel(dropID, ids[0]) {
		t.Fatalf("cancel refused")
	}
	h.expectCancelSent(ids[0])
	if !h.s.FileCancel(dropID, ids[2]) {
		t.Fatalf("cancel refused")
	}
	h.expectCancelSent(ids[2])
	if h.s.FileCancel(dropID, ids[2]) || h.s.FileCancel(dropID+7, ids[1]) || h.s.FileCancel(0, ids[1]) ||
		h.s.FileData(dropID, ids[0], data[:1]) {
		t.Fatalf("a cancelled or unknown transfer was accepted")
	}
	h.expectNone()
	h.expectCreditEvent(0, dropID, ids[0], 524288)
	h.expectDoneEvent(1, dropID, ids[0], FileCancelled, "", false)
	h.expectDoneEvent(2, dropID, ids[2], FileCancelled, "", false)
	// The sender's own answers to those cancels are not reported again.
	h.senderDone(ids[0], 2, "")
	h.senderDone(ids[2], 2, "")
	if len(h.f.fileEvents()) != 3 {
		t.Fatalf("the sender's answers were reported")
	}
	// The sender cancels the last one itself (reception turned off there).
	h.senderCredit(kFileReady, ids[1], 524288)
	h.senderDone(ids[1], 2, "")
	h.expectCreditEvent(3, dropID, ids[1], 524288)
	h.expectDoneEvent(4, dropID, ids[1], FileCancelled, "", true)
	h.expectNone()

	// The channel closes mid-drop: everything open is cancelled on the
	// sender, and nobody is told.
	closing, ids := h.browserDrops(files)
	h.s.FileAttach(closing)
	h.senderCredit(kFileReady, ids[0], 1024)
	h.senderDone(ids[0], 1, "")
	before := len(h.f.fileEvents())
	h.s.FileChannelClosed(closing)
	h.expectCancelSent(ids[1])
	h.expectCancelSent(ids[2])
	h.expectNone()
	if len(h.f.fileEvents()) != before || h.s.FileAttach(closing) {
		t.Fatalf("a closed channel heard of its transfers")
	}
	h.s.FileChannelClosed(closing)
	h.expectNone()
}

func TestFileIdleTimeout(t *testing.T) {
	h := openServices(t, false, true)
	data := make([]byte, 16)
	files := []FileEntry{{"a", 16}, {"b", 16}}
	dropID, ids := h.browserDrops(files)
	h.s.FileAttach(dropID)
	// Progress on either side restarts the 30 seconds.
	h.f.now += 20000
	h.senderCredit(kFileReady, ids[0], 16)
	h.f.now += 20000
	h.s.FileData(dropID, ids[0], data[:8])
	h.expectChunk(ids[0], 0, data[:8])
	h.f.now += 30000 - 1
	h.ping()
	h.tick()
	h.expectNone()
	if len(h.f.fileEvents()) != 1 {
		t.Fatalf("timed out before 30 s")
	}
	h.f.now++
	h.tick()
	h.expectCancelSent(ids[0])
	h.expectCancelSent(ids[1])
	h.expectNone()
	h.expectDoneEvent(1, dropID, ids[0], FileFailed, "", false)
	h.expectDoneEvent(2, dropID, ids[1], FileFailed, "", true)
	h.tick()
	if len(h.f.fileEvents()) != 3 {
		t.Fatalf("a second timeout")
	}

	// A drop whose channel never opens fails sooner, even while the sender
	// makes progress, and no longer refuses the next drop; its outcomes then
	// wait 30 seconds for the channel.
	unattached, ids := h.browserDrops(files)
	h.f.now += 10000 - 1
	h.senderCredit(kFileReady, ids[0], 16)
	h.tick()
	h.expectNone()
	if _, _, refused := h.s.FileDrop(files); refused == "" {
		t.Fatalf("a second drop while the first is open")
	}
	h.f.now++
	h.ping()
	h.tick()
	h.expectCancelSent(ids[0])
	h.expectCancelSent(ids[1])
	h.expectNone()
	if len(h.f.fileEvents()) != 3 {
		t.Fatalf("outcomes without a channel")
	}
	h.f.now += 30000 - 1
	h.ping()
	h.tick()
	if !h.s.FileAttach(unattached) {
		t.Fatalf("the outcomes did not wait")
	}
	h.expectDoneEvent(3, unattached, ids[0], FileFailed, "", false)
	h.expectDoneEvent(4, unattached, ids[1], FileFailed, "", true)
	h.expectNone()
}

// A browser that leaves between the drop and its channel: the next drop is
// accepted once the attach timeout has failed the first, and an attached
// drop never hits that timeout.
func TestFileAttachTimeoutFreesNextDrop(t *testing.T) {
	h := openServices(t, false, true)
	files := []FileEntry{{"a", 16}}
	_, ids := h.browserDrops(files)
	h.f.now += 10000
	h.ping()
	h.tick()
	h.expectCancelSent(ids[0])
	next, _ := h.browserDrops(files)
	if !h.s.FileAttach(next) {
		t.Fatalf("attach refused")
	}
	h.f.now += 10000
	h.tick()
	h.expectNone()
}

// Each invalid sender message faults the file service alone: the drop fails
// on both sides, the session and the terminal stay, and the service's
// messages count only after a new registration.
func TestFileInvalidMessagesFaultTheService(t *testing.T) {
	cases := []struct {
		name       string
		kind       uint16
		payload    []byte
		afterReady bool
	}{
		{"ready of 7 bytes", kFileReady, make([]byte, 7), false},
		{"ready of 9 bytes", kFileReady, make([]byte, 9), false},
		{"credit of 4 bytes", kFileCredit, make([]byte, 4), true},
		{"done of 7 bytes", kFileDone, make([]byte, 7), false},
		{"done status 3", kFileDone, []byte{0, 0, 0, 0, 3, 0, 0, 0}, false},
		{"done reserved byte", kFileDone, []byte{0, 0, 0, 0, 1, 0, 1, 0}, false},
		{"done path of invalid UTF-8", kFileDone, []byte{0, 0, 0, 0, 0, 0, 0, 0, 0xff}, false},
		{"failed with a path", kFileDone, []byte{0, 0, 0, 0, 1, 0, 0, 0, '/', 'x'}, false},
		{"cancelled with a path", kFileDone, []byte{0, 0, 0, 0, 2, 0, 0, 0, '/', 'x'}, false},
		// OK while the KVM has not sent the bytes.
		{"OK before the bytes", kFileDone, []byte{0, 0, 0, 0, 0, 0, 0, 0, '/', 'x'}, true},
		// Credit before file.ready, and file.ready twice.
		{"credit before ready", kFileCredit, []byte{0, 0, 0, 0, 1, 0, 0, 0}, false},
		{"ready twice", kFileReady, []byte{0, 0, 0, 0, 1, 0, 0, 0}, true},
		{"caps flags 3", kFileCaps, []byte{3, 0, 0, 0}, false},
		{"caps of 3 bytes", kFileCaps, []byte{1, 0, 0}, false},
		{"drop from the sender", kFileDrop, []byte{1, 0, 0, 0}, false},
		{"chunk from the sender", kFileChunk, make([]byte, 12), false},
		{"cancel from the sender", kFileCancel, make([]byte, 4), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := openServices(t, true, true)
			terminal := h.openRunningTerminal()
			files := []FileEntry{{"a", 5}, {"b", 5}}
			dropID, ids := h.browserDrops(files)
			h.s.FileAttach(dropID)
			if c.afterReady {
				h.senderCredit(kFileReady, ids[0], 1)
				if len(h.f.fileEvents()) != 1 {
					t.Fatalf("no credit event")
				}
			}
			before := len(h.f.fileEvents())
			payload := append([]byte(nil), c.payload...)
			if c.kind != kFileCaps && c.kind != kFileDrop && len(payload) >= 4 {
				wle.PutUint32(payload, ids[0])
			}
			changes := h.f.stateChanges
			h.send(svcFile, c.kind, payload)
			if !h.s.Open() || h.s.Counters.ServiceFaults != 1 || h.s.FileAvailable() || h.f.stateChanges == changes ||
				!h.s.TerminalAvailable() {
				t.Fatalf("open %v faults %d files %v terminal %v", h.s.Open(), h.s.Counters.ServiceFaults,
					h.s.FileAvailable(), h.s.TerminalAvailable())
			}
			h.expectCancelSent(ids[0])
			h.expectCancelSent(ids[1])
			h.expectNone()
			if len(h.f.fileEvents()) != before+2 {
				t.Fatalf("%d file events", len(h.f.fileEvents()))
			}
			h.expectDoneEvent(before, dropID, ids[0], FileFailed, "", false)
			h.expectDoneEvent(before+1, dropID, ids[1], FileFailed, "", true)
			// Ignored until it registers again.
			h.senderCredit(kFileReady, ids[0], 1)
			h.registerService(svcFile, 1)
			if !h.s.FileAvailable() || h.s.Counters.ServiceFaults != 1 {
				t.Fatalf("registered again: available %v faults %d", h.s.FileAvailable(), h.s.Counters.ServiceFaults)
			}
			h.senderOutput(terminal, "still here")
			if string(h.f.termOutput) != "still here" {
				t.Fatalf("output %q", h.f.termOutput)
			}
			h.browserDrops(files[:1])
		})
	}
}

func TestFileEndsWithSettingsWithdrawalAndSession(t *testing.T) {
	files := []FileEntry{{"a", 5}, {"b", 5}}
	data := []byte{1, 2, 3, 4, 5}

	// The setting goes off: transfers fail and are cancelled, then the
	// sender hears the policy.
	h := openServices(t, false, true)
	dropID, ids := h.browserDrops(files)
	h.s.FileAttach(dropID)
	h.senderCredit(kFileReady, ids[0], 5)
	h.s.FileData(dropID, ids[0], data)
	h.expectChunk(ids[0], 0, data)
	h.senderDone(ids[0], 0, "/h/a")
	h.expectDoneEvent(1, dropID, ids[0], FileOK, "/h/a", false)
	h.s.ApplySettings(defaultSettings())
	h.expectCancelSent(ids[1])
	h.checkConfig(h.expect(svcSession, kConfig), true, false, "")
	h.expectNone()
	if len(h.f.fileEvents()) != 3 {
		t.Fatalf("%d file events", len(h.f.fileEvents()))
	}
	h.expectDoneEvent(2, dropID, ids[1], FileFailed, "", true)

	// The sender withdraws mid-transfer.
	h = openServices(t, false, true)
	dropID, ids = h.browserDrops(files)
	h.s.FileAttach(dropID)
	h.senderCredit(kFileReady, ids[0], 5)
	h.s.FileData(dropID, ids[0], data[:2])
	h.expectChunk(ids[0], 0, data[:2])
	h.registerService(svcFile, 0)
	h.expectCancelSent(ids[0])
	h.expectCancelSent(ids[1])
	h.expectNone()
	h.expectDoneEvent(1, dropID, ids[0], FileFailed, "", false)
	h.expectDoneEvent(2, dropID, ids[1], FileFailed, "", true)
	if h.s.Counters.ServiceFaults != 0 {
		t.Fatalf("a withdrawal counted as a fault")
	}

	// The session ends mid-transfer: every open transfer fails exactly once,
	// the finished one is not reported again, nothing is written.
	h = openServices(t, false, true)
	three := []FileEntry{{"a", 0}, {"b", 5}, {"c", 5}}
	dropID, ids = h.browserDrops(three)
	h.s.FileAttach(dropID)
	h.senderCredit(kFileReady, ids[0], 524288)
	h.senderDone(ids[0], 0, "/h/a")
	h.senderCredit(kFileReady, ids[1], 524288)
	h.s.FileData(dropID, ids[1], data[:3])
	h.expectChunk(ids[1], 0, data[:3])
	before := len(h.f.fileEvents())
	h.s.End("the IN endpoint did not drain")
	h.expectNone()
	if len(h.f.fileEvents()) != before+2 {
		t.Fatalf("%d file events", len(h.f.fileEvents()))
	}
	h.expectDoneEvent(before, dropID, ids[1], FileFailed, "", false)
	h.expectDoneEvent(before+1, dropID, ids[2], FileFailed, "", true)
	h.s.End("again")
	h.s.FileChannelClosed(dropID)
	if h.s.FileData(dropID, ids[1], data[:1]) || len(h.f.fileEvents()) != before+2 {
		t.Fatalf("an ended drop took data or reported again")
	}

	// Ended before its channel opened: the failures wait for it.
	h = openServices(t, false, true)
	dropID, ids = h.browserDrops(files)
	h.s.USBReset()
	if len(h.f.fileEvents()) != 0 {
		t.Fatalf("outcomes before the channel opened")
	}
	if !h.s.FileAttach(dropID) || len(h.f.fileEvents()) != 2 {
		t.Fatalf("attach after the end: %d events", len(h.f.fileEvents()))
	}
	h.expectDoneEvent(0, dropID, ids[0], FileFailed, "", false)
	h.expectDoneEvent(1, dropID, ids[1], FileFailed, "", true)

	// A chunk that cannot be queued ends the session.
	h = openServices(t, false, true)
	dropID, ids = h.browserDrops(files)
	h.s.FileAttach(dropID)
	h.senderCredit(kFileReady, ids[0], 5)
	h.f.failWrites = true
	if h.s.FileData(dropID, ids[0], data) || h.s.Open() {
		t.Fatalf("a failed chunk: open %v", h.s.Open())
	}
	h.expectDoneEvent(1, dropID, ids[0], FileFailed, "", false)
	h.expectDoneEvent(2, dropID, ids[1], FileFailed, "", true)
	if len(h.f.fileEvents()) != 3 {
		t.Fatalf("%d file events", len(h.f.fileEvents()))
	}

	// A file.drop that cannot be queued starts no drop.
	h = openServices(t, false, true)
	h.f.failWrites = true
	if dropID, _, refused := h.s.FileDrop(files); dropID != 0 || refused != "send drop: the IN endpoint did not drain" {
		t.Fatalf("failed drop: %d %q", dropID, refused)
	}
}

// FileEndUnattached and FileFail: a channel that was refused, and a channel
// that lost bytes.
func TestFileEndUnattachedAndFail(t *testing.T) {
	h := openServices(t, false, true)
	files := []FileEntry{{"a", 5}, {"b", 5}}
	dropID, ids := h.browserDrops(files)
	h.senderCredit(kFileReady, ids[0], 5)
	if h.s.FileEndUnattached(0, FileCancelled) != 0 || h.s.FileEndUnattached(dropID+1, FileCancelled) != 0 {
		t.Fatalf("FileEndUnattached for no drop")
	}
	if n := h.s.FileEndUnattached(dropID, FileCancelled); n != 2 {
		t.Fatalf("FileEndUnattached: %d", n)
	}
	h.expectCancelSent(ids[0])
	h.expectCancelSent(ids[1])
	h.expectNone()
	if len(h.f.fileEvents()) != 0 || h.s.FileAttach(dropID) {
		t.Fatalf("an ended unattached drop reported or attached")
	}

	// Attached: FileEndUnattached does nothing; FileFail fails the rest.
	dropID, ids = h.browserDrops(files)
	h.s.FileAttach(dropID)
	if h.s.FileEndUnattached(dropID, FileCancelled) != 0 {
		t.Fatalf("FileEndUnattached of an attached drop")
	}
	h.senderCredit(kFileReady, ids[0], 5)
	h.s.FileData(dropID, ids[0], []byte("12345"))
	h.expectChunk(ids[0], 0, []byte("12345"))
	h.senderDone(ids[0], 0, "/h/a")
	h.s.FileFail(dropID + 1)
	h.expectNone()
	h.s.FileFail(dropID)
	h.expectCancelSent(ids[1])
	h.expectNone()
	h.expectDoneEvent(2, dropID, ids[1], FileFailed, "", true)
}

// IDs stay non-zero and unique when the counter nears its end: the drop
// starts again at 1.
func TestFileIDsWrap(t *testing.T) {
	h := openServices(t, false, true)
	h.s.nextFileID = 0xffffffff - 2
	files := []FileEntry{{"a", 1}, {"b", 1}}
	dropID, ids := h.browserDrops(files)
	if dropID != 1 || ids[0] != 2 || ids[1] != 3 {
		t.Fatalf("drop %d %v", dropID, ids)
	}
}

// A 10 MiB file under the sender's credit window (512 KiB offered, returned
// as it writes in 128 KiB steps), with the browser sending 32 KiB messages.
// The relay adds no messages, bytes or round trips: one chunk per browser
// message, 20 bytes of framing per chunk, a full window per round, and
// never a byte beyond the credit.
func TestFileThroughputUnderTheCreditWindow(t *testing.T) {
	const (
		window = 512 * 1024
		step   = 128 * 1024
		frame  = 32 * 1024
		size   = 10 * 1024 * 1024
	)
	h := openServices(t, false, true)
	dropID, ids := h.browserDrops([]FileEntry{{"large.iso", size}})
	id := ids[0]
	h.s.FileAttach(dropID)
	bytesOut := h.s.Counters.BytesOut

	// Sender state.
	granted, received, unreturned := uint64(window), uint64(0), uint64(0)
	chunks, creditsSent := 0, 1
	h.senderCredit(kFileReady, id, window)
	// Browser state.
	var browserCredit, browserSent uint64
	eventsRead, rounds := 0, 0
	buf := make([]byte, frame)
	for done := false; !done; {
		if rounds++; rounds > 64 {
			t.Fatalf("no end after 64 rounds")
		}
		events := h.f.fileEvents()
		for ; eventsRead < len(events); eventsRead++ {
			if e := events[eventsRead]; e.kind == "done" {
				if e.status != FileOK || !e.last {
					t.Fatalf("outcome %+v", e)
				}
				done = true
			} else {
				browserCredit += e.bytes
			}
		}
		if done {
			break
		}
		for browserCredit > 0 && browserSent < size {
			n := min(browserCredit, frame, size-browserSent)
			for i := range buf[:n] {
				buf[i] = byte(browserSent / frame)
			}
			if !h.s.FileData(dropID, id, buf[:n]) {
				t.Fatalf("FileData refused at %d", browserSent)
			}
			browserCredit -= n
			browserSent += n
		}
		for {
			m, ok := h.next()
			if !ok {
				break
			}
			if m.service != svcFile || m.kind != kFileChunk {
				t.Fatalf("service %d kind %d", m.service, m.kind)
			}
			offset := wle.Uint64(m.payload[4:])
			length := uint64(len(m.payload) - 12)
			if wle.Uint32(m.payload) != id || offset != received || offset+length > granted || offset+length > size ||
				m.payload[12] != byte(offset/frame) {
				t.Fatalf("chunk at %d of %d bytes (received %d, granted %d)", offset, length, received, granted)
			}
			received += length
			unreturned += length
			chunks++
			if received == size {
				h.senderDone(id, 0, "/h/large.iso")
			} else if unreturned >= step {
				h.senderCredit(kFileCredit, id, uint32(unreturned))
				granted += unreturned
				unreturned = 0
				creditsSent++
			}
		}
		h.f.clearOutput()
	}
	if received != size || browserSent != size {
		t.Fatalf("received %d, sent %d", received, browserSent)
	}
	// One chunk per browser message, and no other message.
	if chunks != size/frame {
		t.Fatalf("%d chunks", chunks)
	}
	wire := h.s.Counters.BytesOut - bytesOut
	if wire != size+uint64(chunks)*(specEnvelope+12) {
		t.Fatalf("%d bytes on the wire", wire)
	}
	// Every credit reached the browser, one event per sender message.
	if len(h.f.fileEvents()) != creditsSent+1 {
		t.Fatalf("%d events for %d credits", len(h.f.fileEvents()), creditsSent)
	}
	// A full window moves per round: 20 rounds of 512 KiB, then the
	// outcome.
	if rounds != size/window+1 {
		t.Fatalf("%d rounds", rounds)
	}
}
