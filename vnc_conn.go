package kvm

import (
	"errors"
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

	c.conn.LockWrite()
	defer c.conn.UnlockWrite()

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

		c.l.Trace().Uint16("w", w).Uint16("h", h).Int("size", len(payload)).Uint32("flags", flags).Bool("primed", c.primed).Msg("emitting OpenH264 rect")
		if err := c.conn.WriteOpenH264Rect(
			rfb.Rect{X: 0, Y: 0, W: w, H: h, Encoding: rfb.EncodingOpenH264},
			flags, payload,
		); err != nil {
			return err
		}
	} else if !hasH264 {
		pixels := rfb.PlaceholderImage(int(w), int(h))
		c.l.Trace().Uint16("w", w).Uint16("h", h).Msg("emitting Raw placeholder rect (client did not advertise OpenH264)")
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
func (c *vncConn) handleKeyEvent(m rfb.KeyEventMessage) {
	hid, ok := rfb.HIDFromKeysym(m.Keysym)
	if !ok {
		c.l.Debug().Uint32("keysym", m.Keysym).Msg("unmapped keysym, dropping")
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

	// Strip wheel bits from the button mask so they don't end up as
	// real button presses on the HID side.
	const wheelMask = rfb.PointerButtonUp | rfb.PointerButtonDown |
		rfb.PointerButtonLeftWh | rfb.PointerButtonRightW
	hidButtons := m.ButtonMask & ^wheelMask

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
