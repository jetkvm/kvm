package usbgadget

import (
	"fmt"
	"os"
	"time"
)

var relativeMouseConfig = gadgetConfigItem{
	order:      1002,
	device:     "hid.usb2",
	path:       []string{"functions", "hid.usb2"},
	configPath: []string{"hid.usb2"},
	attrs: gadgetAttributes{
		"protocol":        "2",
		"subclass":        "1",
		"report_length":   "5",
		"no_out_endpoint": "1",
		"wakeup_on_write": "0",
	},
	reportDesc: relativeMouseCombinedReportDesc,
}

// from: https://github.com/NicoHood/HID/blob/b16be57caef4295c6cd382a7e4c64db5073647f7/src/SingleReport/BootMouse.cpp#L26
var relativeMouseCombinedReportDesc = []byte{
	0x05, 0x01, // USAGE_PAGE (Generic Desktop)	  54
	0x09, 0x02, // USAGE (Mouse)
	0xa1, 0x01, // COLLECTION (Application)

	// Pointer and Physical are required by Apple Recovery
	0x09, 0x01, // USAGE (Pointer)
	0xa1, 0x00, // COLLECTION (Physical)

	// 8 Buttons
	0x05, 0x09, // USAGE_PAGE (Button)
	0x19, 0x01, // USAGE_MINIMUM (Button 1)
	0x29, 0x08, // USAGE_MAXIMUM (Button 8)
	0x15, 0x00, // LOGICAL_MINIMUM (0)
	0x25, 0x01, // LOGICAL_MAXIMUM (1)
	0x95, 0x08, // REPORT_COUNT (8)
	0x75, 0x01, // REPORT_SIZE (1)
	0x81, 0x02, // INPUT (Data,Var,Abs)

	// X, Y, Wheel
	0x05, 0x01, // USAGE_PAGE (Generic Desktop)
	0x09, 0x30, // USAGE (X)
	0x09, 0x31, // USAGE (Y)
	0x09, 0x38, // USAGE (Wheel)
	0x15, 0x81, // LOGICAL_MINIMUM (-127)
	0x25, 0x7f, // LOGICAL_MAXIMUM (127)
	0x75, 0x08, // REPORT_SIZE (8)
	0x95, 0x03, // REPORT_COUNT (3)
	0x81, 0x06, // INPUT (Data,Var,Rel)

	// Horizontal Scroll
	0x05, 0x0C, //   USAGE_PAGE (Consumer)
	0x0A, 0x38, 0x02, // USAGE (AC Pan)
	0x15, 0x81, //   LOGICAL_MINIMUM (-127)
	0x25, 0x7f, //   LOGICAL_MAXIMUM (127)
	0x75, 0x08, //   REPORT_SIZE (8)
	0x95, 0x01, //   REPORT_COUNT (1)
	0x81, 0x06, //   INPUT (Data,Var,Rel)

	// End
	0xc0, //       End Collection (Physical)
	0xc0, //       End Collection
}

func (u *UsbGadget) relMouseWriteHidFile(data []byte) error {
	if u.relMouseHidFile == nil {
		var err error
		u.relMouseHidFile, err = u.openWithTimeout("/dev/hidg2", os.O_RDWR, 0666, 3*time.Second)
		if err != nil {
			return fmt.Errorf("failed to open hidg1: %w", err)
		}
	}

	_, err := u.writeWithTimeout(u.relMouseHidFile, data)
	if err != nil {
		u.logWithSuppression("relMouseWriteHidFile", 100, u.log, err, "failed to write to hidg2")
		u.relMouseHidFile.Close()
		u.relMouseHidFile = nil
		return err
	}
	u.resetLogSuppressionCounter("relMouseWriteHidFile")
	return nil
}

func (u *UsbGadget) HasRelativeMouse() bool {
	return u.enabledDevices.RelativeMouse
}

func (u *UsbGadget) RelMouseReport(mx int8, my int8, buttons uint8) error {
	u.hidLifecycle.RLock()
	defer u.hidLifecycle.RUnlock()

	if !u.enabledDevices.RelativeMouse {
		return nil
	}

	u.relMouseLock.Lock()
	defer u.relMouseLock.Unlock()

	return u.relMouseReportLocked(mx, my, buttons)
}

// relMouseReportLocked writes a relative mouse report and tracks the last
// reported button mask. Callers must hold relMouseLock.
func (u *UsbGadget) relMouseReportLocked(mx int8, my int8, buttons uint8) error {
	err := u.relMouseWriteHidFile([]byte{
		buttons,  // Buttons
		byte(mx), // X
		byte(my), // Y
		0,        // Wheel
		0,        // AC Pan (Horizontal Scroll)
	})
	if err != nil {
		return err
	}

	u.lastRelButtons = buttons
	u.resetUserInputTime()
	return nil
}

// JiggleRelMouse nudges the cursor with a relative out-and-back move: +N,+N
// then -N,-N, with N a random magnitude in [minMagnitude, maxMagnitude] on
// each axis. Net displacement is zero, so unlike an absolute nudge there's
// no position to anchor and nothing that can go stale relative to the host's
// real cursor. Both writes happen under a single lock acquisition, so a real
// report from a live session can't land between them, and it reuses the
// last reported button mask so it never releases a button held mid-drag.
func (u *UsbGadget) JiggleRelMouse(minMagnitude, maxMagnitude int) error {
	u.hidLifecycle.RLock()
	defer u.hidLifecycle.RUnlock()

	if !u.enabledDevices.RelativeMouse {
		return nil
	}

	u.relMouseLock.Lock()
	defer u.relMouseLock.Unlock()

	dx := randomSignedOffset(minMagnitude, maxMagnitude)
	dy := randomSignedOffset(minMagnitude, maxMagnitude)
	buttons := u.lastRelButtons

	if err := u.relMouseReportLocked(int8(dx), int8(dy), buttons); err != nil {
		return err
	}
	return u.relMouseReportLocked(int8(-dx), int8(-dy), buttons)
}

func (u *UsbGadget) RelMouseWheelReport(wheelY int8, wheelX int8) error {
	u.hidLifecycle.RLock()
	defer u.hidLifecycle.RUnlock()

	if !u.enabledDevices.RelativeMouse {
		return nil
	}

	u.relMouseLock.Lock()
	defer u.relMouseLock.Unlock()

	if wheelY == 0 && wheelX == 0 {
		return nil
	}

	err := u.relMouseWriteHidFile([]byte{
		0,            // Buttons (none)
		0,            // X
		0,            // Y
		byte(wheelY), // Wheel (signed)
		byte(wheelX), // AC Pan (signed)
	})

	u.resetUserInputTime()
	return err
}
