package usbgadget

import (
	"encoding/json"
	"os"
	"sync"
)

// hidHandoverPath lives on tmpfs. A reboot clears it, and a reboot
// re-enumerates the gadget anyway, so nothing needs to survive one.
const hidHandoverPath = "/tmp/jetkvm-hid-handover.json"

// hidHandover is what a restarted process needs when it adopts the previous
// instance's gadget instead of re-enumerating it: the host's last LED report,
// which the host will not resend, and an absolute mouse press that never got
// its release.
type hidHandover struct {
	KeyboardLeds byte `json:"keyboard_leds"`
	AbsPressed   bool `json:"abs_pressed"`
	AbsX         int  `json:"abs_x"`
	AbsY         int  `json:"abs_y"`
}

var hidHandoverLock sync.Mutex

func updateHidHandover(fn func(h *hidHandover)) {
	hidHandoverLock.Lock()
	defer hidHandoverLock.Unlock()

	h, _ := readHidHandover()
	fn(&h)
	data, err := json.Marshal(h)
	if err != nil {
		return
	}
	tmp := hidHandoverPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return
	}
	_ = os.Rename(tmp, hidHandoverPath)
}

func readHidHandover() (hidHandover, bool) {
	var h hidHandover
	data, err := os.ReadFile(hidHandoverPath)
	if err != nil {
		return h, false
	}
	if err := json.Unmarshal(data, &h); err != nil {
		return hidHandover{}, false
	}
	return h, true
}

func loadHidHandover() (hidHandover, bool) {
	hidHandoverLock.Lock()
	defer hidHandoverLock.Unlock()
	return readHidHandover()
}

// resetHidHandover runs after a rebind. The host re-enumerated, which
// released every input and makes it resend its LED state, so nothing from
// before the rebind applies to the next adoption.
func (u *UsbGadget) resetHidHandover() {
	// Hold the mouse lock through the write, or a press reported in between
	// would be persisted and then overwritten.
	u.absMouseLock.Lock()
	defer u.absMouseLock.Unlock()
	u.absMousePressed = false
	updateHidHandover(func(h *hidHandover) { *h = hidHandover{} })
}

// adoptLiveGadget runs when Init kept the previous instance's gadget bound.
// The host saw no disconnect, so it still holds whatever inputs the previous
// process left pressed, and it will not resend its LED state.
func (u *UsbGadget) adoptLiveGadget() {
	h, ok := loadHidHandover()
	if ok {
		u.updateKeyboardState(h.KeyboardLeds)
	}

	if err := u.KeyboardReport(0, make([]byte, hidKeyBufferSize)); err != nil {
		u.log.Warn().Err(err).Msg("failed to release inherited keyboard state")
	}
	if err := u.RelMouseReport(0, 0, 0); err != nil {
		u.log.Warn().Err(err).Msg("failed to release inherited relative mouse buttons")
	}
	if ok && h.AbsPressed {
		// Seed the press state so the release below is a real edge and clears
		// the handover entry; otherwise the next adoption would replay it.
		// The position is where the button went down: a drag in progress at
		// the restart snaps back there, which keeps the hot path free of a
		// file write per mouse move.
		u.absMouseLock.Lock()
		u.absMousePressed = true
		u.absMouseLock.Unlock()
		if err := u.AbsMouseReport(h.AbsX, h.AbsY, 0); err != nil {
			u.log.Warn().Err(err).Msg("failed to release inherited absolute mouse buttons")
		}
	}
}
