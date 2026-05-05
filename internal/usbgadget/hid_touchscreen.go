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
		"report_length":   "7",
		"no_out_endpoint": "1",
		"wakeup_on_write": "1",
	},
	reportDesc: touchscreenReportDesc,
}

// One-contact HID multitouch digitizer for Android targets.
//
// Android treats this descriptor as a direct touchscreen rather than a mouse,
// which avoids cursor/IME behavior and routes gestures through the same input
// path as physical touch.
//
// Report layout, 7 bytes:
//
//	byte 0: bit0 Tip Switch, bit1 In Range, bits2-7 padding
//	byte 1: Contact Identifier
//	byte 2-3: X, little-endian, 0..32767
//	byte 4-5: Y, little-endian, 0..32767
//	byte 6: Contact Count
var touchscreenReportDesc = []byte{
	0x05, 0x0D, // Usage Page (Digitizers)
	0x09, 0x04, // Usage (Touch Screen)
	0xA1, 0x01, // Collection (Application)

	0x09, 0x22, //   Usage (Finger)
	0xA1, 0x02, //   Collection (Logical)

	0x09, 0x42, //     Usage (Tip Switch)
	0x09, 0x32, //     Usage (In Range)
	0x15, 0x00, //     Logical Minimum (0)
	0x25, 0x01, //     Logical Maximum (1)
	0x75, 0x01, //     Report Size (1)
	0x95, 0x02, //     Report Count (2)
	0x81, 0x02, //     Input (Data,Var,Abs)

	0x75, 0x01, //     Report Size (1)
	0x95, 0x06, //     Report Count (6)
	0x81, 0x03, //     Input (Const,Var,Abs) padding

	0x09, 0x51, //     Usage (Contact Identifier)
	0x15, 0x00, //     Logical Minimum (0)
	0x25, 0x0F, //     Logical Maximum (15)
	0x75, 0x08, //     Report Size (8)
	0x95, 0x01, //     Report Count (1)
	0x81, 0x02, //     Input (Data,Var,Abs)

	0x05, 0x01, //     Usage Page (Generic Desktop)
	0x09, 0x30, //     Usage (X)
	0x09, 0x31, //     Usage (Y)
	0x16, 0x00, 0x00, // Logical Minimum (0)
	0x26, 0xFF, 0x7F, // Logical Maximum (32767)
	0x36, 0x00, 0x00, // Physical Minimum (0)
	0x46, 0xFF, 0x7F, // Physical Maximum (32767)
	0x75, 0x10, //     Report Size (16)
	0x95, 0x02, //     Report Count (2)
	0x81, 0x02, //     Input (Data,Var,Abs)

	0xC0, //   End Collection

	0x05, 0x0D, // Usage Page (Digitizers)
	0x09, 0x54, // Usage (Contact Count)
	0x15, 0x00, // Logical Minimum (0)
	0x25, 0x01, // Logical Maximum (1)
	0x75, 0x08, // Report Size (8)
	0x95, 0x01, // Report Count (1)
	0x81, 0x02, // Input (Data,Var,Abs)

	0xC0, // End Collection
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
		u.logWithSuppression("touchscreenWriteHidFile", 100, u.log, err, "failed to write to hidg3")
		u.touchscreenHidFile.Close()
		u.touchscreenHidFile = nil
		return err
	}
	u.resetLogSuppressionCounter("touchscreenWriteHidFile")
	return nil
}

func (u *UsbGadget) HasTouchscreen() bool {
	return u.enabledDevices.Touchscreen
}

func (u *UsbGadget) TouchscreenReport(x int, y int, touching bool) error {
	if !u.enabledDevices.Touchscreen {
		return nil
	}

	u.touchscreenHidLock.Lock()
	defer u.touchscreenHidLock.Unlock()

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
	contactCount := byte(0)
	if touching {
		flags = 0x03
		contactCount = 0x01
	}

	err := u.touchscreenWriteHidFile([]byte{
		flags,
		0x00,
		byte(x),
		byte(x >> 8),
		byte(y),
		byte(y >> 8),
		contactCount,
	})
	if err != nil {
		return err
	}

	u.resetUserInputTime()
	return nil
}
