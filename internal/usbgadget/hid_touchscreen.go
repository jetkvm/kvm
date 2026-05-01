package usbgadget

import (
	"fmt"
	"os"
)

var touchscreenConfig = gadgetConfigItem{
	order:      1003,
	device:     "hid.usb3",
	path:       []string{"functions", "hid.usb3"},
	configPath: []string{"hid.usb3"},
	attrs: gadgetAttributes{
		"protocol":        "0",
		"subclass":        "0",
		"report_length":   "5",
		"no_out_endpoint": "1",
		"wakeup_on_write": "1",
	},
	reportDesc: touchscreenReportDesc,
}

// Single-touch digitizer (Android-compatible baseline)
var touchscreenReportDesc = []byte{
	0x05, 0x0D,
	0x09, 0x04,
	0xA1, 0x01,

	0x09, 0x22,
	0xA1, 0x02,

	0x09, 0x42,
	0x15, 0x00,
	0x25, 0x01,
	0x75, 0x01,
	0x95, 0x01,
	0x81, 0x02,

	0x09, 0x32,
	0x75, 0x01,
	0x95, 0x01,
	0x81, 0x02,

	0x75, 0x01,
	0x95, 0x06,
	0x81, 0x03,

	0x05, 0x01,
	0x09, 0x30,
	0x09, 0x31,
	0x16, 0x00, 0x00,
	0x26, 0xFF, 0x7F,
	0x36, 0x00, 0x00,
	0x46, 0xFF, 0x7F,
	0x75, 0x10,
	0x95, 0x02,
	0x81, 0x02,

	0xC0,
	0xC0,
}

func (u *UsbGadget) touchscreenWriteHidFile(data []byte) error {
	if u.touchscreenHidFile == nil {
		var err error
		u.touchscreenHidFile, err = os.OpenFile("/dev/hidg3", os.O_RDWR, 0666)
		if err != nil {
			return fmt.Errorf("failed to open hidg3: %w", err)
		}
	}

	_, err := u.writeWithTimeout(u.touchscreenHidFile, data)
	if err != nil {
		u.touchscreenHidFile.Close()
		u.touchscreenHidFile = nil
		return err
	}
	return nil
}

func (u *UsbGadget) HasTouchscreen() bool {
	return u.enabledDevices.Touchscreen
}

func (u *UsbGadget) TouchscreenReport(x int, y int, touching bool) error {
	if !u.enabledDevices.Touchscreen {
		return nil
	}

	if x < 0 {
		x = 0
	} else if x > 32767 {
		x = 32767
	}

	if y < 0 {
		y = 0
	} else if y > 32767 {
		y = 32767
	}

	flags := byte(0)
	if touching {
		flags = 0x03
	}

	return u.touchscreenWriteHidFile([]byte{
		flags,
		byte(x),
		byte(x >> 8),
		byte(y),
		byte(y >> 8),
	})
}
