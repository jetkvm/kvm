package kvm

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jetkvm/kvm/internal/osservices"
	"github.com/pion/webrtc/v4"
)

// The browser's OS Services data channels, as on the JetKVM Mini:
//
//   - "guest-terminal": one terminal session. Binary messages are
//     keystrokes, a text message {"rows":R,"cols":C} resizes, and the
//     channel's close closes the terminal. Output goes to the browser as
//     binary messages; a terminal that ends sends "\r\n[reason]\r\n" as
//     text, then the channel closes.
//   - "guestfile_<drop>": one file drop. Binary messages are a 4-byte
//     little-endian transfer ID and file bytes; a text message
//     {"type":"cancel","transfer":N} stops a transfer. The KVM sends
//     {"type":"credit","transfer":N,"bytes":B} and
//     {"type":"done","transfer":N,"status":"ok|failed|cancelled","path":P}.

const (
	// guestChannelMessageMax is the largest message to the browser.
	guestChannelMessageMax = 16 * 1024
	// guestTerminalBuffered: past this much output buffered in the browser's
	// channel, the session holds terminal output (and the USB read).
	guestTerminalBuffered = 512 * 1024
	// guestTerminalCreditStep: credit goes back to the sender in steps of at
	// least this much.
	guestTerminalCreditStep = 16 * 1024
	// guestFileCloseDelay: a drop's channel closes this long after its last
	// outcome, so the browser reads it.
	guestFileCloseDelay = 500 * time.Millisecond
)

type guestTerminal struct {
	id      uint32
	channel *webrtc.DataChannel
	// Output sent to the browser and not yet credited to the sender.
	pendingCredit uint32
}

type guestDrop struct {
	id      uint32
	channel *webrtc.DataChannel
}

// handleGuestTerminalChannel serves a "guest-terminal" channel.
func handleGuestTerminalChannel(d *webrtc.DataChannel) {
	s := osServices
	if s == nil {
		_ = d.Close()
		return
	}
	var terminal *guestTerminal
	d.OnOpen(func() {
		var refusal string
		s.do(func(session *osservices.Session) {
			id, reason := session.TerminalOpen(0, 0)
			if id == 0 {
				refusal = reason
				return
			}
			terminal = &guestTerminal{id: id, channel: d}
			s.terminals[id] = terminal
		})
		if refusal != "" {
			closeGuestTerminalChannel(d, refusal)
		}
	})
	d.OnMessage(func(msg webrtc.DataChannelMessage) {
		s.do(func(session *osservices.Session) {
			if terminal == nil || s.terminals[terminal.id] != terminal {
				return
			}
			if !msg.IsString {
				session.TerminalInput(terminal.id, msg.Data)
				return
			}
			var size struct {
				Rows uint32 `json:"rows"`
				Cols uint32 `json:"cols"`
			}
			if err := json.Unmarshal(msg.Data, &size); err == nil {
				session.TerminalResize(terminal.id, size.Rows, size.Cols)
			}
		})
		s.link.Wake()
	})
	d.OnClose(func() {
		s.do(func(session *osservices.Session) {
			if terminal != nil && s.terminals[terminal.id] == terminal {
				delete(s.terminals, terminal.id)
				session.TerminalClose(terminal.id)
			}
		})
	})
}

func closeGuestTerminalChannel(d *webrtc.DataChannel, reason string) {
	go func() {
		if reason != "" {
			_ = d.SendText("\r\n[" + reason + "]\r\n")
		}
		// The browser reads the line before the close.
		time.Sleep(100 * time.Millisecond)
		_ = d.Close()
	}()
}

// TerminalOutput sends shell output to the terminal's channel. It holds the
// output while the channel is backed up.
func (s *osServicesService) TerminalOutput(id uint32, data []byte) bool {
	t, ok := s.terminals[id]
	if !ok {
		return true
	}
	if t.channel.ReadyState() != webrtc.DataChannelStateOpen {
		return true
	}
	if t.channel.BufferedAmount() > guestTerminalBuffered {
		return false
	}
	for offset := 0; offset < len(data); offset += guestChannelMessageMax {
		end := min(offset+guestChannelMessageMax, len(data))
		if err := t.channel.Send(data[offset:end]); err != nil {
			return true
		}
	}
	t.pendingCredit += uint32(len(data))
	return true
}

// flushTerminalCredits returns the output the browser took to the sender;
// called with the session lock held.
func (s *osServicesService) flushTerminalCredits() {
	for id, t := range s.terminals {
		if t.pendingCredit >= guestTerminalCreditStep && t.channel.BufferedAmount() < guestTerminalBuffered {
			s.session.TerminalCredit(id, t.pendingCredit)
			t.pendingCredit = 0
		}
	}
}

func (s *osServicesService) TerminalClosed(id uint32, reason string, _ bool) {
	t, ok := s.terminals[id]
	if !ok {
		return
	}
	delete(s.terminals, id)
	closeGuestTerminalChannel(t.channel, reason)
}

// --- File drops -----------------------------------------------------------------

type guestFileDropResult struct {
	Channel   string   `json:"channel"`
	Transfers []uint32 `json:"transfers"`
}

const invalidGuestDrop = "drop.files must be an array of {name: string, size: whole number}"

func rpcGuestFileDrop(drop map[string]any) (guestFileDropResult, error) {
	s := osServices
	if s == nil {
		return guestFileDropResult{}, errors.New("no OS Services file transfer is connected")
	}
	list, ok := drop["files"].([]any)
	if !ok {
		return guestFileDropResult{}, errors.New(invalidGuestDrop)
	}
	files := make([]osservices.FileEntry, 0, len(list))
	for _, item := range list {
		file, ok := item.(map[string]any)
		if !ok {
			return guestFileDropResult{}, errors.New(invalidGuestDrop)
		}
		name, ok := file["name"].(string)
		size, isNumber := file["size"].(float64)
		if !ok || !isNumber || size < 0 || size > 1<<53 || size != math.Trunc(size) {
			return guestFileDropResult{}, errors.New(invalidGuestDrop)
		}
		files = append(files, osservices.FileEntry{Name: name, Size: uint64(size)})
	}
	var result guestFileDropResult
	var refusal string
	s.do(func(session *osservices.Session) {
		dropID, transfers, reason := session.FileDrop(files)
		if dropID == 0 {
			refusal = reason
			return
		}
		result = guestFileDropResult{
			Channel:   osservices.FileChannelPrefix + strconv.FormatUint(uint64(dropID), 10),
			Transfers: transfers,
		}
	})
	if refusal != "" {
		return guestFileDropResult{}, errors.New(refusal)
	}
	return result, nil
}

// handleGuestFileChannel serves a "guestfile_<drop>" channel.
func handleGuestFileChannel(d *webrtc.DataChannel) {
	s := osServices
	id, err := strconv.ParseUint(strings.TrimPrefix(d.Label(), osservices.FileChannelPrefix), 10, 32)
	if s == nil || err != nil || id == 0 {
		_ = d.Close()
		return
	}
	drop := &guestDrop{id: uint32(id), channel: d}
	attached := false
	d.OnOpen(func() {
		s.do(func(session *osservices.Session) {
			s.drops[drop.id] = drop
			attached = session.FileAttach(drop.id)
			if !attached {
				delete(s.drops, drop.id)
				session.FileEndUnattached(drop.id, osservices.FileFailed)
			}
		})
		if !attached {
			_ = d.Close()
		}
	})
	d.OnMessage(func(msg webrtc.DataChannelMessage) {
		s.do(func(session *osservices.Session) {
			if s.drops[drop.id] != drop {
				return
			}
			if !msg.IsString {
				if len(msg.Data) < 4 {
					session.FileFail(drop.id)
					return
				}
				transfer := binary.LittleEndian.Uint32(msg.Data)
				session.FileData(drop.id, transfer, msg.Data[4:])
				return
			}
			var cancel struct {
				Type     string `json:"type"`
				Transfer uint32 `json:"transfer"`
			}
			if json.Unmarshal(msg.Data, &cancel) == nil && cancel.Type == "cancel" {
				session.FileCancel(drop.id, cancel.Transfer)
			}
		})
	})
	d.OnClose(func() {
		s.do(func(session *osservices.Session) {
			if s.drops[drop.id] == drop {
				delete(s.drops, drop.id)
				session.FileChannelClosed(drop.id)
			}
		})
	})
}

func (s *osServicesService) FileCredit(dropID, transferID uint32, bytes uint64) {
	if drop, ok := s.drops[dropID]; ok {
		_ = drop.channel.SendText(fmt.Sprintf(`{"type":"credit","transfer":%d,"bytes":%d}`, transferID, bytes))
	}
}

func (s *osServicesService) FileDone(dropID, transferID uint32, status osservices.FileStatus, path string, last bool) {
	drop, ok := s.drops[dropID]
	if !ok {
		return
	}
	message := map[string]any{"type": "done", "transfer": transferID, "status": status.String()}
	if path != "" {
		message["path"] = path
	}
	if text, err := json.Marshal(message); err == nil {
		_ = drop.channel.SendText(string(text))
	}
	if last {
		delete(s.drops, dropID)
		go func() {
			time.Sleep(guestFileCloseDelay)
			_ = drop.channel.Close()
		}()
	}
}
