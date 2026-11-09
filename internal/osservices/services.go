package osservices

import (
	"fmt"
	"math"
	"strings"
)

// --- Clipboard service ------------------------------------------------------

func (s *Session) findClipboardRequest(id uint32) *clipboardRequest {
	for i := range s.clipboardRequests {
		if s.clipboardRequests[i].inUse && s.clipboardRequests[i].requestID == id {
			return &s.clipboardRequests[i]
		}
	}
	return nil
}

func (s *Session) finishClipboardRequest(r *clipboardRequest, status ClipboardStatus, text []byte, truncated bool) {
	id := r.requestID
	r.inUse = false
	s.p.ClipboardDone(id, status, true, text, truncated)
}

func (s *Session) handleClipboard(payload []byte) {
	length := len(payload)
	switch s.kind {
	case clipboardCapabilities:
		if length != 8 {
			s.faultService(ServiceClipboard, "invalid clipboard.capabilities length")
			return
		}
		flags := le.Uint32(payload)
		if flags&^uint32(ClipboardFlagRead|ClipboardFlagWrite) != 0 || payload[4] > byte(OSLinux) ||
			payload[5] != 0 || payload[6] != 0 || payload[7] != 0 {
			s.faultService(ServiceClipboard, "invalid clipboard.capabilities")
			return
		}
		s.serviceFaulted[ServiceClipboard] = false
		s.clipboardFlags = flags
		s.clipboardOS = TargetOS(payload[4])
		s.clipboardRegistered = flags != 0
		if flags == 0 {
			s.failClipboardRequests(ClipboardUnsupported)
		}
		if s.sendClipboardConfig() {
			s.notifyState()
		}
	case clipboardContent:
		if length < 8 || payload[4] > byte(ClipboardFailed) || payload[5] > 1 || payload[6] != 0 || payload[7] != 0 ||
			length-8 > ClipboardMaxText || (payload[4] != byte(ClipboardOK) && length != 8) ||
			!UTF8Valid(payload[8:]) {
			s.faultService(ServiceClipboard, "invalid clipboard.content")
			return
		}
		r := s.findClipboardRequest(le.Uint32(payload))
		if r == nil {
			return // abandoned after 2 seconds; a late answer is ignored
		}
		if r.kind != clipboardGet {
			s.faultService(ServiceClipboard, "clipboard.content answers a clipboard.set")
			return
		}
		s.finishClipboardRequest(r, ClipboardStatus(payload[4]), payload[8:], payload[5] == 1)
	case clipboardStatus:
		if length != 5 || payload[4] > byte(ClipboardFailed) {
			s.faultService(ServiceClipboard, "invalid clipboard.status")
			return
		}
		r := s.findClipboardRequest(le.Uint32(payload))
		if r == nil {
			return
		}
		if r.kind != clipboardSet {
			s.faultService(ServiceClipboard, "clipboard.status answers a clipboard.get")
			return
		}
		s.finishClipboardRequest(r, ClipboardStatus(payload[4]), nil, false)
	case clipboardGet, clipboardSet, clipboardConfig:
		s.faultService(ServiceClipboard, "a KVM-to-sender clipboard message from the sender")
	default:
		s.Counters.MessagesSkipped++
	}
}

// ClipboardAvailable: clipboard gestures reach the target: a sender with
// clipboard access is connected and sharing is on.
func (s *Session) ClipboardAvailable() bool {
	return s.link == linkOpen && s.clipboardRegistered && s.settings.ClipboardEnabled
}

// ClipboardRequest starts a clipboard.get (text nil) or clipboard.set
// request. It returns the request ID, or 0 when the request cannot start;
// the status then says why (Unsupported: no sender, no access or sharing
// off; Failed: too many open requests or the write failed).
func (s *Session) ClipboardRequest(text []byte, set bool) (uint32, ClipboardStatus) {
	needed := uint32(ClipboardFlagRead)
	if set {
		needed = ClipboardFlagWrite
	}
	if !s.ClipboardAvailable() || s.clipboardFlags&needed == 0 {
		return 0, ClipboardUnsupported
	}
	if set && (len(text) > ClipboardMaxText || !UTF8Valid(text)) {
		return 0, ClipboardFailed
	}
	var slot *clipboardRequest
	for i := range s.clipboardRequests {
		if !s.clipboardRequests[i].inUse {
			slot = &s.clipboardRequests[i]
			break
		}
	}
	if slot == nil {
		return 0, ClipboardFailed
	}
	id := s.nextClipboardRequest
	s.nextClipboardRequest++
	if id == 0 {
		id = s.nextClipboardRequest
		s.nextClipboardRequest++
	}
	request := le.AppendUint32(nil, id)
	kind := uint16(clipboardGet)
	if set {
		kind = clipboardSet
	}
	if !s.writeMessage(ServiceClipboard, kind, request, text) {
		s.endSession(inEndpointStuck)
		return 0, ClipboardFailed
	}
	*slot = clipboardRequest{
		requestID:  id,
		deadlineMS: s.nowMS() + ClipboardTimeoutMS,
		kind:       kind,
		inUse:      true,
	}
	return id, ClipboardOK
}

// --- Terminal service -------------------------------------------------------

func (s *Session) findTerminal(id uint32) *terminal {
	if id == 0 {
		return nil
	}
	for i := range s.terminals {
		if s.terminals[i].state != terminalFree && s.terminals[i].id == id {
			return &s.terminals[i]
		}
	}
	return nil
}

func (s *Session) sendTerminalClose(id uint32) bool {
	return s.sendMessage(ServiceTerminal, terminalClose, le.AppendUint32(nil, id))
}

// endTerminal ends one terminal session on the KVM's side and reports it.
// The slot is freed first, so neither the report nor a close whose write
// fails (which ends the session) can end it twice. closeSender sends
// terminal.close.
func (s *Session) endTerminal(t *terminal, reason string, exited, closeSender bool) {
	id := t.id
	*t = terminal{}
	if s.terminal != nil {
		s.terminal.TerminalClosed(id, reason, exited)
	}
	if closeSender && s.link == linkOpen {
		s.sendTerminalClose(id)
	}
}

func (s *Session) endTerminals(reason string, closeSender bool) {
	for i := range s.terminals {
		if s.terminals[i].state != terminalFree {
			s.endTerminal(&s.terminals[i], reason, false, closeSender)
		}
	}
}

// parseFlagCapabilities reads terminal.capabilities and file.capabilities:
// a u32 of flags with one defined bit; 0 withdraws.
func (s *Session) parseFlagCapabilities(service uint16, payload []byte, defined uint32, registered *bool) bool {
	if len(payload) != 4 || le.Uint32(payload)&^defined != 0 {
		s.faultService(service, "invalid capabilities")
		return false
	}
	s.serviceFaulted[service] = false
	*registered = le.Uint32(payload) != 0
	s.notifyState()
	return true
}

func terminalRefusal(status byte) string {
	switch status {
	case terminalOpenedUnsupported:
		return terminalNotOpened + "the target computer allows no remote terminal"
	case terminalOpenedBusy:
		return terminalBusy
	default:
		return terminalNotOpened + "the target computer could not start a shell"
	}
}

// terminalMessageValid checks terminal.opened, terminal.output and
// terminal.exited: a non-zero session_id, then a status byte or the data.
func terminalMessageValid(kind uint16, payload []byte) bool {
	if len(payload) < 4 || le.Uint32(payload) == 0 {
		return false
	}
	switch kind {
	case terminalOpened:
		return len(payload) == terminalStatusLen && payload[4] <= terminalOpenedFailed
	case terminalOutput:
		return len(payload) > 4
	default:
		return len(payload) == terminalStatusLen && payload[4] <= terminalExitFailed
	}
}

// deliverHeld offers the held terminal.output to the browser again. It
// returns false while its channel stays backed up; past
// terminalOutputHoldMS the terminal ends instead, since shell output cannot
// be dropped without corrupting the screen.
func (s *Session) deliverHeld() bool {
	id := le.Uint32(s.payload)
	t := s.findTerminal(id)
	taken := t == nil || s.terminal.TerminalOutput(id, s.payload[4:s.payloadSize])
	if !taken && s.nowMS()-s.heldMS < terminalOutputHoldMS {
		return false
	}
	s.parse = parseHeader
	if !taken {
		s.endTerminal(t, terminalSlowBrowser, false, true)
	}
	return true
}

func (s *Session) handleTerminal(payload []byte) {
	switch s.kind {
	case terminalCapabilities:
		if s.parseFlagCapabilities(ServiceTerminal, payload, terminalFlagShell, &s.terminalShell) && !s.terminalShell {
			// The sender ends its shells before it withdraws; what is left
			// here never opened there.
			s.endTerminals(terminalWithdrawn, false)
		}
		return
	case terminalOpened, terminalOutput, terminalExited:
	case terminalOpen, terminalInput, terminalResize, terminalClose, terminalCredit:
		s.faultService(ServiceTerminal, "a KVM-to-sender terminal message from the sender")
		return
	default:
		s.Counters.MessagesSkipped++
		return
	}
	if !terminalMessageValid(s.kind, payload) {
		switch s.kind {
		case terminalOpened:
			s.faultService(ServiceTerminal, "invalid terminal.opened")
		case terminalOutput:
			s.faultService(ServiceTerminal, "invalid terminal.output")
		default:
			s.faultService(ServiceTerminal, "invalid terminal.exited")
		}
		return
	}
	t := s.findTerminal(le.Uint32(payload))
	if t == nil {
		// It ended while the message was in transit (Terminal service).
		return
	}
	switch s.kind {
	case terminalOpened:
		if payload[4] == terminalOpenedOK {
			t.state = terminalRunning
		} else {
			// Only OK creates a terminal session: nothing to close there.
			s.endTerminal(t, terminalRefusal(payload[4]), false, false)
		}
	case terminalOutput:
		// A terminal opens only on a platform that takes output. The
		// payload stays in s.payload while it is held.
		if s.terminal != nil {
			s.parse = parseHeld
			s.heldMS = s.nowMS()
			s.deliverHeld()
		}
	default:
		reason := ""
		switch payload[4] {
		case terminalExitExited:
			reason = terminalShellExited
		case terminalExitFailed:
			reason = terminalShellLost
		}
		s.endTerminal(t, reason, payload[4] == terminalExitExited, false)
	}
}

// TerminalAvailable: a shell is offered: a sender with a shell is connected,
// the terminal setting is on and the platform takes output.
func (s *Session) TerminalAvailable() bool {
	return s.link == linkOpen && s.terminalShell && s.settings.TerminalEnabled && s.terminal != nil
}

func terminalSizeValid(rows, cols uint32) bool {
	return rows != 0 && cols != 0 && rows <= math.MaxUint16 && cols <= math.MaxUint16
}

func terminalSize(id, rows, cols uint32) []byte {
	p := le.AppendUint32(make([]byte, 0, terminalSizeLen), id)
	p = le.AppendUint16(p, uint16(rows))
	return le.AppendUint16(p, uint16(cols))
}

// TerminalOpen opens a terminal session: terminal.open with size 0 by 0, then
// terminal.resize when rows and cols are both non-zero. It returns the ID, or
// the line to show the browser when no terminal can open. The sender has 5
// seconds to answer; a refusal or a timeout arrives as TerminalClosed.
func (s *Session) TerminalOpen(rows, cols uint32) (uint32, string) {
	if !s.TerminalAvailable() {
		return 0, terminalUnavailable
	}
	var slot *terminal
	for i := range s.terminals {
		if s.terminals[i].state == terminalFree {
			slot = &s.terminals[i]
			break
		}
	}
	if slot == nil {
		return 0, terminalBusy
	}
	// Never 0, and never again in this protocol session.
	id := s.nextTerminalID
	s.nextTerminalID++
	if id == 0 {
		id = s.nextTerminalID
		s.nextTerminalID++
	}
	// The size is the browser's to report; the shell starts at its default
	// and is resized at once (Terminal service).
	if !s.sendMessage(ServiceTerminal, terminalOpen, terminalSize(id, 0, 0)) {
		return 0, terminalNotSent
	}
	if terminalSizeValid(rows, cols) {
		if !s.sendMessage(ServiceTerminal, terminalResize, terminalSize(id, rows, cols)) {
			return 0, terminalNotSent
		}
	}
	*slot = terminal{id: id, state: terminalOpening, deadlineMS: s.nowMS() + terminalOpenTimeoutMS}
	return id, ""
}

// TerminalInput sends keystrokes for the shell, in terminal.input messages
// of at most TerminalMaxData bytes. It returns false when the terminal is
// not open, or when a write failed: a gap would change what the shell runs,
// so the terminal closes and the session ends.
func (s *Session) TerminalInput(id uint32, data []byte) bool {
	t := s.findTerminal(id)
	if t == nil || s.link != linkOpen {
		return false
	}
	head := le.AppendUint32(nil, id)
	for offset := 0; offset < len(data); {
		n := min(len(data)-offset, TerminalMaxData)
		if !s.writeMessage(ServiceTerminal, terminalInput, head, data[offset:offset+n]) {
			s.endTerminal(t, TerminalInputLost, false, false)
			s.endSession(inEndpointStuck)
			return false
		}
		offset += n
	}
	return true
}

// TerminalResize sends the browser's terminal size: rows and cols 1 to
// 65535.
func (s *Session) TerminalResize(id, rows, cols uint32) bool {
	if s.findTerminal(id) == nil || s.link != linkOpen || !terminalSizeValid(rows, cols) {
		return false
	}
	return s.sendMessage(ServiceTerminal, terminalResize, terminalSize(id, rows, cols))
}

// TerminalCredit: the browser took bytes more of the terminal's output.
func (s *Session) TerminalCredit(id, bytes uint32) bool {
	if s.findTerminal(id) == nil || s.link != linkOpen {
		return false
	}
	p := le.AppendUint32(make([]byte, 0, terminalCreditLen), id)
	return s.sendMessage(ServiceTerminal, terminalCredit, le.AppendUint32(p, bytes))
}

// TerminalClose: the browser closed its terminal. terminal.close goes to the
// sender, and no TerminalClosed follows.
func (s *Session) TerminalClose(id uint32) {
	t := s.findTerminal(id)
	if t == nil {
		return
	}
	*t = terminal{}
	if s.link == linkOpen {
		s.sendTerminalClose(id)
	}
}

// --- File service -----------------------------------------------------------

func (d *dropState) transferID(index int) uint32 {
	return d.dropID + 1 + uint32(index)
}

// findTransfer returns the index of a transfer of the drop that has not
// ended. dropID 0 accepts the current drop, as the sender's messages name no
// drop.
func (s *Session) findTransfer(dropID, transferID uint32) (int, bool) {
	d := &s.drop
	if d.dropID == 0 || (dropID != 0 && dropID != d.dropID) || transferID <= d.dropID ||
		transferID-d.dropID > d.count {
		return 0, false
	}
	i := int(transferID - d.dropID - 1)
	if d.transfers[i].ended {
		return 0, false
	}
	return i, true
}

func (s *Session) forgetDrop() { s.drop = dropState{} }

// reportCredit tells the browser the credit it has not heard of yet.
func (s *Session) reportCredit(index int) {
	d := &s.drop
	t := &d.transfers[index]
	if !d.attached || t.ended || t.credit == t.creditReported {
		return
	}
	bytes := t.credit - t.creditReported
	t.creditReported = t.credit
	if s.files != nil {
		s.files.FileCredit(d.dropID, d.transferID(index), bytes)
	}
}

// reportDone tells the browser how a transfer ended; after the last one the
// drop is over.
func (s *Session) reportDone(index int, path string) {
	d := &s.drop
	t := &d.transfers[index]
	t.reported = true
	d.reported++
	dropID := d.dropID
	transferID := d.transferID(index)
	status := t.status
	last := d.reported == d.count
	if last {
		s.forgetDrop()
	}
	if s.files != nil {
		s.files.FileDone(dropID, transferID, status, path, last)
	}
}

// endTransfer ends one transfer on the KVM's side, reports it when the
// drop's channel is attached, and with cancelSender sends file.cancel.
func (s *Session) endTransfer(index int, status FileStatus, path string, cancelSender bool) {
	d := &s.drop
	t := &d.transfers[index]
	transferID := d.transferID(index)
	t.ended = true
	t.status = status
	d.ended++
	if d.ended == d.count {
		// An unattached drop keeps its outcomes for the channel this long.
		d.progressMS = s.nowMS()
	}
	if d.attached {
		s.reportDone(index, path)
	}
	if cancelSender && s.link == linkOpen {
		s.sendMessage(ServiceFile, fileCancel, le.AppendUint32(nil, transferID))
	}
}

// endTransfers ends every transfer of the drop that is still open.
func (s *Session) endTransfers(status FileStatus, cancelSender bool) {
	dropID := s.drop.dropID
	for i := 0; dropID != 0 && s.drop.dropID == dropID && i < int(s.drop.count); i++ {
		if !s.drop.transfers[i].ended {
			s.endTransfer(i, status, "", cancelSender)
		}
	}
}

func (s *Session) handleFileCredit(payload []byte) {
	ready := s.kind == fileReady
	if len(payload) != fileCreditLen {
		if ready {
			s.faultService(ServiceFile, "invalid file.ready")
		} else {
			s.faultService(ServiceFile, "invalid file.credit")
		}
		return
	}
	index, ok := s.findTransfer(0, le.Uint32(payload))
	if !ok {
		return // ended here while the message was in transit
	}
	t := &s.drop.transfers[index]
	if ready && t.ready {
		s.faultService(ServiceFile, "a second file.ready")
		return
	}
	if !ready && !t.ready {
		s.faultService(ServiceFile, "file.credit before file.ready")
		return
	}
	t.ready = true
	// Cumulative; saturate rather than wrap.
	bytes := uint64(le.Uint32(payload[4:]))
	if t.credit > math.MaxUint64-bytes {
		t.credit = math.MaxUint64
	} else {
		t.credit += bytes
	}
	s.drop.progressMS = s.nowMS()
	s.reportCredit(index)
}

func (s *Session) handleFileDone(payload []byte) {
	length := len(payload)
	if length < fileDoneFixedLen || payload[4] > byte(FileCancelled) || payload[5] != 0 || payload[6] != 0 ||
		payload[7] != 0 || (payload[4] != byte(FileOK) && length != fileDoneFixedLen) ||
		!UTF8Valid(payload[fileDoneFixedLen:]) {
		s.faultService(ServiceFile, "invalid file.done")
		return
	}
	index, ok := s.findTransfer(0, le.Uint32(payload))
	if !ok {
		return
	}
	t := &s.drop.transfers[index]
	if payload[4] == byte(FileOK) && t.sent != t.size {
		// OK means the sender wrote every byte, and the KVM has not sent
		// them all.
		s.faultService(ServiceFile, "file.done OK before every byte was sent")
		return
	}
	s.endTransfer(index, FileStatus(payload[4]), string(payload[fileDoneFixedLen:]), false)
}

func (s *Session) handleFile(payload []byte) {
	switch s.kind {
	case fileCapabilities:
		if s.parseFlagCapabilities(ServiceFile, payload, fileFlagReceive, &s.fileReceive) && !s.fileReceive {
			// A withdrawal fails every open transfer (File service).
			s.endTransfers(FileFailed, true)
		}
	case fileReady, fileCredit:
		s.handleFileCredit(payload)
	case fileDone:
		s.handleFileDone(payload)
	case fileDrop, fileChunk, fileCancel:
		s.faultService(ServiceFile, "a KVM-to-sender file message from the sender")
	default:
		s.Counters.MessagesSkipped++
	}
}

// FileAvailable: files can be received: a sender that receives files is
// connected, the files setting is on and the platform relays credit and
// outcomes.
func (s *Session) FileAvailable() bool {
	return s.link == linkOpen && s.fileReceive && s.settings.FilesEnabled && s.files != nil
}

// quoteName quotes a name for an error message: quotes, backslashes, control
// characters and the bytes of invalid UTF-8 escaped, cut at a character.
func quoteName(name string) string {
	quoted := fmt.Sprintf("%q", name)
	if len(quoted) > 70 {
		cut := 64
		for cut > 0 && quoted[cut]&0xc0 == 0x80 {
			cut--
		}
		quoted = quoted[:cut] + `..."`
	}
	return quoted
}

// FileDrop offers files to the sender in one file.drop. It returns the drop
// ID and the transfer IDs, or the reason to show the browser (then nothing
// was sent). One drop is open at a time.
func (s *Session) FileDrop(files []FileEntry) (uint32, []uint32, string) {
	if !s.FileAvailable() {
		return 0, nil, fileUnavailable
	}
	// One drop at a time: the sender writes files one after another. A drop
	// whose outcomes wait for its channel does not count.
	if s.drop.dropID != 0 && s.drop.ended < s.drop.count {
		return 0, nil, fileBusy
	}
	if len(files) == 0 {
		return 0, nil, fileNoFiles
	}
	if len(files) > FileDropMax {
		return 0, nil, fmt.Sprintf("too many files for one drop: %d (at most %d)", len(files), FileDropMax)
	}
	payloadLength := fileDropHeaderLen
	for _, f := range files {
		if !FileNameValid([]byte(f.Name)) {
			return 0, nil, "invalid file name " + quoteName(f.Name)
		}
		payloadLength += fileDropEntryLen + min(len(f.Name), MaxControlPayload+1)
	}
	if payloadLength > MaxControlPayload {
		return 0, nil, fmt.Sprintf("too many file names for one drop: %d bytes", payloadLength)
	}
	// The drop's ID, then one per file, none 0 and none used before.
	first := s.nextFileID
	if first == 0 || first > math.MaxUint32-uint32(len(files))-1 {
		first = 1
	}
	s.nextFileID = first + uint32(len(files)) + 1
	payload := make([]byte, 0, payloadLength)
	payload = le.AppendUint32(payload, uint32(len(files)))
	for i, f := range files {
		payload = le.AppendUint32(payload, first+1+uint32(i))
		payload = le.AppendUint64(payload, f.Size)
		payload = le.AppendUint16(payload, uint16(len(f.Name)))
		payload = append(payload, f.Name...)
	}
	if !s.sendMessage(ServiceFile, fileDrop, payload) {
		return 0, nil, "send drop: " + inEndpointStuck
	}
	// A finished drop whose channel never opened is replaced.
	s.forgetDrop()
	now := s.nowMS()
	s.drop = dropState{
		dropID:           first,
		count:            uint32(len(files)),
		progressMS:       now,
		attachDeadlineMS: now + fileAttachTimeoutMS,
		transfers:        make([]fileTransfer, len(files)),
	}
	ids := make([]uint32, len(files))
	for i, f := range files {
		s.drop.transfers[i].size = f.Size
		ids[i] = first + 1 + uint32(i)
	}
	return first, ids, ""
}

// FileAttach: the drop's channel is open. Credit and outcomes held so far are
// delivered now, and the rest as they happen. It returns false for a drop
// that is unknown or already attached; the glue then closes the channel.
func (s *Session) FileAttach(dropID uint32) bool {
	d := &s.drop
	if dropID == 0 || d.dropID != dropID || d.attached {
		return false
	}
	d.attached = true
	if d.ended < d.count {
		d.progressMS = s.nowMS()
	}
	for i := 0; s.drop.dropID == dropID && i < int(s.drop.count); i++ {
		t := &s.drop.transfers[i]
		if !t.ended {
			s.reportCredit(i)
		} else if !t.reported {
			s.reportDone(i, "")
		}
	}
	return true
}

// FileData relays bytes of a transfer from the drop's channel at once, as
// file.chunk messages of at most ChunkMaxData bytes each. It returns true
// when they went to the sender. Bytes beyond the transfer's credit or size
// fail the whole drop; a failed write ends the session.
func (s *Session) FileData(dropID, transferID uint32, data []byte) bool {
	if dropID == 0 || len(data) == 0 || s.link != linkOpen {
		return false
	}
	index, ok := s.findTransfer(dropID, transferID)
	if !ok {
		return false
	}
	t := &s.drop.transfers[index]
	length := uint64(len(data))
	if length > t.credit-t.sent || length > t.size-t.sent {
		// A browser that sends beyond its credit loses the drop.
		s.endTransfers(FileFailed, true)
		return false
	}
	offset := t.sent
	t.sent += length
	s.drop.progressMS = s.nowMS()
	head := make([]byte, chunkHeaderLen)
	le.PutUint32(head, transferID)
	for done := 0; done < len(data); {
		n := min(len(data)-done, ChunkMaxData)
		le.PutUint64(head[4:], offset+uint64(done))
		if !s.writeMessage(ServiceFile, fileChunk, head, data[done:done+n]) {
			s.endSession(inEndpointStuck)
			return false
		}
		done += n
	}
	return true
}

// FileCancel: the browser stopped a transfer. file.cancel goes to the sender
// and FileDone Cancelled follows.
func (s *Session) FileCancel(dropID, transferID uint32) bool {
	if dropID == 0 {
		return false
	}
	index, ok := s.findTransfer(dropID, transferID)
	if !ok {
		return false
	}
	s.endTransfer(index, FileCancelled, "", true)
	return true
}

// FileChannelClosed: the drop's channel closed. Each transfer still open is
// cancelled on the sender, and the drop is forgotten without further
// callbacks.
func (s *Session) FileChannelClosed(dropID uint32) {
	if dropID == 0 || s.drop.dropID != dropID {
		return
	}
	// Nobody is left to tell.
	s.drop.attached = false
	s.endTransfers(FileCancelled, true)
	if s.drop.dropID == dropID {
		s.forgetDrop()
	}
}

// FileEndUnattached: a channel for the drop was refused or closed before it
// was attached. Each transfer still open ends with status (file.cancel to
// the sender, no FileDone) and the drop is forgotten. It returns the drop's
// count of transfers, or 0 when it did nothing.
func (s *Session) FileEndUnattached(dropID uint32, status FileStatus) uint32 {
	if dropID == 0 || s.drop.dropID != dropID || s.drop.attached {
		return 0
	}
	count := s.drop.count
	s.endTransfers(status, true)
	if s.drop.dropID == dropID {
		s.forgetDrop()
	}
	return count
}

// FileSize is the size of a transfer of the drop.
func (s *Session) FileSize(dropID, transferID uint32) (uint64, bool) {
	d := &s.drop
	if dropID == 0 || d.dropID != dropID || transferID <= dropID || transferID-dropID > d.count {
		return 0, false
	}
	return d.transfers[transferID-dropID-1].size, true
}

// FileFail: the drop's channel lost bytes the browser sent. Each transfer
// still open fails.
func (s *Session) FileFail(dropID uint32) {
	if dropID == 0 || s.drop.dropID != dropID {
		return
	}
	s.endTransfers(FileFailed, true)
}

// --- Update service ---------------------------------------------------------

func (s *Session) updateOpen(id uint32) bool {
	return id != 0 && s.update.UpdateID == id && !s.update.Ended
}

// endUpdate ends the open update on the KVM's side; cancelSender also sends
// update.cancel.
func (s *Session) endUpdate(status uint8, message string, cancelSender bool) {
	u := &s.update
	if !s.updateOpen(u.UpdateID) {
		return
	}
	u.Ended = true
	u.Status = status
	u.Message = message
	if cancelSender && s.link == linkOpen {
		s.sendMessage(ServiceUpdate, updateCancel, le.AppendUint32(nil, u.UpdateID))
	}
}

func (s *Session) handleUpdateCapabilities(payload []byte) {
	length := len(payload)
	if length < 4 || le.Uint32(payload)&^uint32(updateFlagInstall) != 0 || length-4 > UpdatePackageMax ||
		!UTF8Valid(payload[4:]) || ((le.Uint32(payload) != 0) != (length > 4)) {
		s.faultService(ServiceUpdate, "invalid update.capabilities")
		return
	}
	s.serviceFaulted[ServiceUpdate] = false
	s.updateInstall = le.Uint32(payload) != 0
	s.updatePackage = string(payload[4:])
	if !s.updateInstall {
		s.endUpdate(UpdateFailed, "the sender withdrew the update service", true)
	}
	s.notifyState()
}

func (s *Session) handleUpdateCredit(payload []byte) {
	ready := s.kind == updateReady
	if len(payload) != 8 {
		if ready {
			s.faultService(ServiceUpdate, "invalid update.ready")
		} else {
			s.faultService(ServiceUpdate, "invalid update.credit")
		}
		return
	}
	u := &s.update
	if !s.updateOpen(le.Uint32(payload)) {
		return // ended here while the message was in transit
	}
	if ready == u.ready {
		if ready {
			s.faultService(ServiceUpdate, "a second update.ready")
		} else {
			s.faultService(ServiceUpdate, "update.credit before update.ready")
		}
		return
	}
	u.ready = true
	bytes := uint64(le.Uint32(payload[4:]))
	if u.credit > math.MaxUint64-bytes {
		u.credit = math.MaxUint64
	} else {
		u.credit += bytes
	}
	u.progressMS = s.nowMS()
}

func (s *Session) handleUpdateStatus(payload []byte) {
	length := len(payload)
	if length < 8 || payload[4] > UpdateCancelled || payload[5]|payload[6]|payload[7] != 0 ||
		(payload[4] != UpdateFailed && length != 8) || !UTF8Valid(payload[8:]) {
		s.faultService(ServiceUpdate, "invalid update.status")
		return
	}
	if !s.updateOpen(le.Uint32(payload)) {
		return
	}
	if payload[4] == UpdateInstalling && s.update.Sent != s.update.Size {
		s.faultService(ServiceUpdate, "update.status Installing before every byte was sent")
		return
	}
	message := truncate(string(payload[8:]), updateMessageMax)
	// What JSON escapes would grow the browser's event: shown as '?'.
	message = strings.Map(func(r rune) rune {
		if r < 0x20 || r == '"' || r == '\\' {
			return '?'
		}
		return r
	}, message)
	s.endUpdate(payload[4], message, false)
	// The sender exits to install the KVM's update; the next session
	// switches back, as after the KVM's own USB restart.
	s.videoSwitchBack = s.videoSwitchBack || payload[4] == UpdateInstalling
}

func (s *Session) handleUpdate(payload []byte) {
	switch s.kind {
	case updateCapabilities:
		s.handleUpdateCapabilities(payload)
	case updateReady, updateCredit:
		s.handleUpdateCredit(payload)
	case updateStatus:
		s.handleUpdateStatus(payload)
	case updateBegin, updateChunk, updateCancel:
		s.faultService(ServiceUpdate, "a KVM-to-sender update message from the sender")
	default:
		s.Counters.MessagesSkipped++
	}
}

// UpdateAvailable: a sender that installs packages is connected.
func (s *Session) UpdateAvailable() bool {
	return s.link == linkOpen && s.updateInstall
}

// UpdatePackage names the package the sender installs.
func (s *Session) UpdatePackage() string { return s.updatePackage }

// Update returns the update in progress, or the last one.
func (s *Session) Update() Update { return s.update }

// UpdateBegin sends update.begin. It returns the update ID, or why no update
// started. One update is open at a time.
func (s *Session) UpdateBegin(size uint64, sha256 [32]byte, version string) (uint32, string) {
	if !s.UpdateAvailable() {
		return 0, updateUnavailable
	}
	if s.updateOpen(s.update.UpdateID) {
		return 0, updateBusy
	}
	if size == 0 || len(version) > UpdateVersionMax {
		return 0, "invalid OS Services release"
	}
	s.nextUpdateID++
	if s.nextUpdateID == 0 {
		s.nextUpdateID = 1
	}
	s.update = Update{UpdateID: s.nextUpdateID, Size: size, progressMS: s.nowMS()}
	head := make([]byte, updateBeginFixedLen)
	le.PutUint32(head, s.update.UpdateID)
	le.PutUint64(head[4:], size)
	copy(head[12:], sha256[:])
	if !s.writeMessage(ServiceUpdate, updateBegin, head, []byte(version)) {
		s.endSession(inEndpointStuck)
		return 0, inEndpointStuck
	}
	return s.update.UpdateID, ""
}

// UpdateSend relays up to len(data) bytes of the update, at the next offset,
// in update.chunk messages as far as the sender's credit and the package
// size allow. It returns the bytes sent: 0 while there is no credit or once
// the update has ended.
func (s *Session) UpdateSend(id uint32, data []byte) int {
	u := &s.update
	done := 0
	for done < len(data) && s.updateOpen(id) && u.ready {
		limit := min(u.credit, u.Size)
		n := uint64(min(len(data)-done, ChunkMaxData))
		if limit-u.Sent < n {
			n = limit - u.Sent
		}
		if n == 0 {
			break
		}
		head := make([]byte, chunkHeaderLen)
		le.PutUint32(head, id)
		le.PutUint64(head[4:], u.Sent)
		if !s.writeMessage(ServiceUpdate, updateChunk, head, data[done:done+int(n)]) {
			s.endSession(inEndpointStuck)
			break
		}
		u.Sent += n
		u.progressMS = s.nowMS()
		done += int(n)
	}
	return done
}

// UpdateCancel ends the update with update.cancel and status Cancelled.
func (s *Session) UpdateCancel(id uint32, reason string) {
	if s.updateOpen(id) {
		s.endUpdate(UpdateCancelled, reason, true)
	}
}
