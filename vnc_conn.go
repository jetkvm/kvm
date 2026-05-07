package kvm

import (
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/jetkvm/kvm/internal/rfb"
	"github.com/jetkvm/kvm/internal/sync"

	"github.com/rs/zerolog"
)

// vncConn holds the per-client state for an accepted VNC connection.
type vncConn struct {
	server  *VNCServer
	conn    *rfb.Conn
	netConn net.Conn
	l       *zerolog.Logger

	// Inbound frames pushed by the producer (cap 2; non-blocking
	// send drops on full).
	frames  chan vncFramePacket
	dropped atomic.Uint64

	// updateNeeded is set when the client has asked for a frame and
	// is waiting for one. The dispatcher selects on it together with
	// the frame channel; it only sends a rect when the client is
	// actually waiting.
	updateNeeded chan struct{}

	// writeQuit is closed by the read loop on disconnect to release
	// the dispatcher.
	writeQuit chan struct{}

	// stateMu guards width/height/needsResetCtx/encodings.
	stateMu         sync.Mutex
	width           uint16
	height          uint16
	needsResetCtx   bool // set on first frame, on resolution change, or after fallback
	hasOpenH264     bool // client advertised encoding 50
	resolutionDirty bool // pending DesktopSize update

	// cachedSPS / cachedPPS are the parameter-set NALs at connect
	// time. Sent as the first piece of the first encoding-50 rect so
	// the decoder is primed before the next IDR.
	cachedSPS []byte
	cachedPPS []byte
	primed    bool

	// Last-known mouse button mask, used to translate wheel-button
	// transitions into wheel reports.
	lastButtons uint8

	// waitingForIDR is true until an IDR slice arrives for this
	// client. Non-IDR frames are dropped during this window so the
	// decoder isn't fed a P-frame it can't decode.
	waitingForIDR bool
}

// markResolutionChanged is called by the producer when the capture
// resolution changes. The dispatcher will emit a DesktopSize update
// on its next opportunity.
func (c *vncConn) markResolutionChanged(w, h uint16) {
	c.stateMu.Lock()
	c.width = w
	c.height = h
	c.needsResetCtx = true
	c.resolutionDirty = true
	c.stateMu.Unlock()

	// Wake the dispatcher in case it was waiting for a frame: the
	// resolution-change must be visible even if frames pause.
	select {
	case c.updateNeeded <- struct{}{}:
	default:
	}
}

// readLoop parses inbound client messages and feeds HID input. Runs
// on the goroutine that called serveClient; on return, the
// connection is torn down by the caller.
func (c *vncConn) readLoop() {
	for {
		msg, err := c.conn.ReadClientMessage()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				c.l.Debug().Err(err).Msg("read loop ended")
			}
			return
		}

		switch m := msg.(type) {
		case rfb.SetEncodingsMessage:
			has := false
			for _, e := range m.Encodings {
				if e == rfb.EncodingOpenH264 {
					has = true
					break
				}
			}
			c.stateMu.Lock()
			c.hasOpenH264 = has
			c.stateMu.Unlock()
			c.l.Info().Bool("openH264", has).Interface("encodings", m.Encodings).Msg("client encodings")
			if !has {
				c.l.Warn().Msg("client did not advertise OpenH264 (encoding 50); falling back to placeholder")
			}

		case rfb.SetPixelFormatMessage:
			// Ignored: encoding-50 has its own pixel format, and we
			// always send the placeholder in DefaultPixelFormat (most
			// clients accept it). A future change can honour this for
			// non-H.264 clients.

		case rfb.FramebufferUpdateRequestMessage:
			// One outstanding "needs update" flag per client.
			select {
			case c.updateNeeded <- struct{}{}:
			default:
			}

		case rfb.KeyEventMessage:
			c.handleKeyEvent(m)

		case rfb.PointerEventMessage:
			c.handlePointerEvent(m)

		case rfb.ClientCutTextMessage:
			// Clipboard is out of scope for v1.
		}
	}
}

// dispatchLoop is the per-client writer goroutine. It waits for a
// FramebufferUpdateRequest from the client and ships one rect:
// a Raw placeholder for non-H.264 clients (immediately), or an
// OpenH264 rect for H.264 clients (after waiting for an IDR-bearing
// frame).
func (c *vncConn) dispatchLoop() {
	for {
		select {
		case <-c.writeQuit:
			return
		case <-c.updateNeeded:
		}

		c.stateMu.Lock()
		hasH264 := c.hasOpenH264
		c.stateMu.Unlock()

		if !hasH264 {
			// Send the placeholder immediately — there's nothing to
			// wait for, and the client gets infinite incremental
			// requests to keep alive.
			if err := c.writeUpdate(vncFramePacket{}); err != nil {
				c.l.Debug().Err(err).Msg("write update (placeholder)")
				_ = c.netConn.Close()
				return
			}
			continue
		}

		// H.264 client: wait for a frame (preferring IDR for the
		// first frame) or a resolution-change pulse.
		select {
		case <-c.writeQuit:
			return
		case pkt := <-c.frames:
			c.stateMu.Lock()
			drop := c.waitingForIDR && !pkt.hasIDR
			if pkt.hasIDR {
				c.waitingForIDR = false
			}
			c.stateMu.Unlock()
			if drop {
				c.l.Trace().Int("size", len(pkt.data)).Msg("dropping non-IDR frame while waiting for keyframe")
				select {
				case c.updateNeeded <- struct{}{}:
				default:
				}
				continue
			}
			if err := c.writeUpdate(pkt); err != nil {
				c.l.Debug().Err(err).Msg("write update")
				_ = c.netConn.Close()
				return
			}
		case <-time.After(2 * time.Second):
			c.stateMu.Lock()
			pending := c.resolutionDirty
			c.stateMu.Unlock()
			if pending {
				if err := c.writeUpdate(vncFramePacket{}); err != nil {
					c.l.Debug().Err(err).Msg("write update (resolution-only)")
					_ = c.netConn.Close()
					return
				}
				continue
			}
			// No frame arrived; re-arm and wait again.
			select {
			case c.updateNeeded <- struct{}{}:
			default:
			}
		}
	}
}

// writeUpdate emits one FramebufferUpdate covering whatever state is
// currently pending: resolution change first (DesktopSize rect),
// then either an OpenH264 H.264 rect or a Raw placeholder.
func (c *vncConn) writeUpdate(pkt vncFramePacket) error {
	c.stateMu.Lock()
	w, h := c.width, c.height
	dirty := c.resolutionDirty
	resetCtx := c.needsResetCtx
	hasH264 := c.hasOpenH264
	c.resolutionDirty = false
	c.needsResetCtx = false
	c.stateMu.Unlock()

	// Count rects up front so we can write the FramebufferUpdate
	// header before any rect bodies.
	rectCount := uint16(0)
	if dirty {
		rectCount++
	}
	hasFrame := len(pkt.data) > 0
	if hasH264 && hasFrame {
		rectCount++
	} else if !hasH264 {
		rectCount++ // placeholder Raw
	}
	if rectCount == 0 {
		return nil
	}

	// Single-writer assumption: only this dispatcher goroutine writes
	// to c.conn after the handshake completes (the read loop never
	// writes), so no locking is required.
	if err := c.conn.BeginFramebufferUpdate(rectCount); err != nil {
		return err
	}

	if dirty {
		if err := c.conn.WriteDesktopSizeRect(w, h); err != nil {
			return err
		}
	}

	if hasH264 && hasFrame {
		flags := uint32(0)
		if resetCtx {
			flags |= rfb.OpenH264FlagResetContext
		}

		var payload []byte
		if !c.primed && len(c.cachedSPS) > 0 && len(c.cachedPPS) > 0 {
			// Prepend cached SPS/PPS so the decoder has parameter
			// sets before any IDR slice it sees on this connection.
			payload = make(
				[]byte,
				0,
				len(c.cachedSPS)+len(c.cachedPPS)+len(pkt.data)+12,
			)
			payload = append(payload, []byte{0, 0, 0, 1}...)
			payload = append(payload, c.cachedSPS...)
			payload = append(payload, []byte{0, 0, 0, 1}...)
			payload = append(payload, c.cachedPPS...)
			payload = append(payload, pkt.data...)
			c.primed = true
		} else {
			payload = pkt.data
		}

		if err := c.conn.WriteOpenH264Rect(
			rfb.Rect{X: 0, Y: 0, W: w, H: h, Encoding: rfb.EncodingOpenH264},
			flags, payload,
		); err != nil {
			return err
		}
	} else if !hasH264 {
		pixels := rfb.PlaceholderImage(int(w), int(h))
		if err := c.conn.WriteRawRect(
			rfb.Rect{X: 0, Y: 0, W: w, H: h, Encoding: rfb.EncodingRaw},
			pixels,
		); err != nil {
			return err
		}
	}

	return c.conn.Flush()
}

// handleKeyEvent forwards a VNC KeyEvent to the USB HID gadget. The
// Linux/X11 keysym is translated to a USB HID Usage ID via the table
// in internal/rfb/keysym.go.
//
// macOS VNC clients (including TightVNC's Java viewer, "Chicken",
// Apple Screen Sharing, etc.) vary in how they map Cmd / Option /
// Caps_Lock to X11 keysyms. If a key on a macOS client doesn't reach
// the host as expected, run with JETKVM_LOG_TRACE=vnc and look for
// the "key event" trace line — it shows the raw keysym the client
// sent, which is the source of any misbehaviour we'd want to fix.
func (c *vncConn) handleKeyEvent(m rfb.KeyEventMessage) {
	hid, ok := rfb.HIDFromKeysym(m.Keysym)
	c.l.Trace().
		Str("keysym", fmt.Sprintf("0x%04x", m.Keysym)).
		Bool("down", m.Down).
		Bool("mapped", ok).
		Uint8("hid", hid).
		Msg("key event")
	if !ok {
		return
	}
	if err := rpcKeypressReport(hid, m.Down); err != nil {
		c.l.Warn().Err(err).Uint8("hid", hid).Msg("keypress report failed")
	}
}

// handlePointerEvent translates a VNC PointerEvent to an absolute
// HID mouse report. Wheel events appear as transient presses of
// buttons 4 (up) and 5 (down); we synthesise wheel reports from the
// rising edges and mask the wheel bits out of the regular report.
//
// Mouse buttons 4 and 5 (the back/forward side buttons on most
// Logitech-style mice) have no canonical RFB representation: the
// PointerEvent button-mask is 8 bits, of which RFC 6143 reserves
// bits 0..2 for primary buttons and 3..6 for wheel emulation. Some
// clients send the side buttons on bit 7 or via the Extended
// PointerEvent pseudo-encoding (not implemented here yet); whether
// they reach the server depends on the client. Run with
// JETKVM_LOG_TRACE=vnc and look at the "pointer event" trace to see
// the raw mask the client sent — that tells us which (if any) bit
// to wire up.
func (c *vncConn) handlePointerEvent(m rfb.PointerEventMessage) {
	c.stateMu.Lock()
	w, h := c.width, c.height
	c.stateMu.Unlock()

	const hidRange = 32767
	x := scaleCoord(int(m.X), int(w), hidRange)
	y := scaleCoord(int(m.Y), int(h), hidRange)

	// Detect wheel-button rising edges.
	prev := c.lastButtons
	c.lastButtons = m.ButtonMask
	if rising(prev, m.ButtonMask, rfb.PointerButtonUp) {
		_ = rpcWheelReport(1, 0)
	}
	if rising(prev, m.ButtonMask, rfb.PointerButtonDown) {
		_ = rpcWheelReport(-1, 0)
	}

	// Translate RFB's button-mask layout to USB HID's. RFB
	// (RFC 6143 §7.5.5) is bit 0=left, 1=middle, 2=right; USB HID
	// boot mouse is bit 0=left, 1=right, 2=middle. Wheel pseudo-bits
	// (3..6) were already turned into wheel reports above and are
	// stripped here.
	hidButtons := rfb.PointerMaskToHIDButtons(m.ButtonMask)

	c.l.Trace().
		Str("rfb_buttons", fmt.Sprintf("0x%02x", m.ButtonMask)).
		Uint16("x", m.X).Uint16("y", m.Y).
		Str("hid_buttons", fmt.Sprintf("0x%02x", hidButtons)).
		Msg("pointer event")

	if err := rpcAbsMouseReport(x, y, hidButtons); err != nil {
		c.l.Warn().Err(err).Msg("abs mouse report failed")
	}
}

// scaleCoord maps src in [0, srcMax-1] linearly to [0, dstMax].
func scaleCoord(src, srcMax, dstMax int) int {
	if srcMax <= 1 {
		return 0
	}
	v := src * dstMax / (srcMax - 1)
	if v < 0 {
		return 0
	}
	if v > dstMax {
		return dstMax
	}
	return v
}

// rising reports whether bit became set on the transition prev → cur.
func rising(prev, cur, bit uint8) bool {
	return (prev&bit) == 0 && (cur&bit) != 0
}
