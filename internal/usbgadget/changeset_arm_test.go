//go:build arm && linux

package usbgadget

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	usbConfig = &Config{
		VendorId:     "0x1d6b", //The Linux Foundation
		ProductId:    "0x0104", //Multifunction Composite Gadget
		SerialNumber: "",
		Manufacturer: "JetKVM",
		Product:      "USB Emulation Device",
		strictMode:   true,
	}
	usbDevices = &Devices{
		AbsoluteMouse: true,
		RelativeMouse: true,
		Keyboard:      true,
		MassStorage:   true,
	}
	usbGadgetName = "jetkvm"
	usbGadget     *UsbGadget
)

var oldAbsoluteMouseCombinedReportDesc = []byte{
	0x05, 0x01, // Usage Page (Generic Desktop Ctrls)
	0x09, 0x02, // Usage (Mouse)
	0xA1, 0x01, // Collection (Application)

	// Report ID 1: Absolute Mouse Movement
	0x85, 0x01, //     Report ID (1)
	0x09, 0x01, //     Usage (Pointer)
	0xA1, 0x00, //     Collection (Physical)
	0x05, 0x09, //         Usage Page (Button)
	0x19, 0x01, //         Usage Minimum (0x01)
	0x29, 0x03, //         Usage Maximum (0x03)
	0x15, 0x00, //         Logical Minimum (0)
	0x25, 0x01, //         Logical Maximum (1)
	0x75, 0x01, //         Report Size (1)
	0x95, 0x03, //         Report Count (3)
	0x81, 0x02, //         Input (Data, Var, Abs)
	0x95, 0x01, //         Report Count (1)
	0x75, 0x05, //         Report Size (5)
	0x81, 0x03, //         Input (Cnst, Var, Abs)
	0x05, 0x01, //         Usage Page (Generic Desktop Ctrls)
	0x09, 0x30, //         Usage (X)
	0x09, 0x31, //         Usage (Y)
	0x16, 0x00, 0x00, //         Logical Minimum (0)
	0x26, 0xFF, 0x7F, //         Logical Maximum (32767)
	0x36, 0x00, 0x00, //         Physical Minimum (0)
	0x46, 0xFF, 0x7F, //         Physical Maximum (32767)
	0x75, 0x10, //         Report Size (16)
	0x95, 0x02, //         Report Count (2)
	0x81, 0x02, //         Input (Data, Var, Abs)
	0xC0, //     End Collection

	// Report ID 2: Relative Wheel Movement
	0x85, 0x02, //     Report ID (2)
	0x09, 0x38, //     Usage (Wheel)
	0x15, 0x81, //     Logical Minimum (-127)
	0x25, 0x7F, //     Logical Maximum (127)
	0x75, 0x08, //     Report Size (8)
	0x95, 0x01, //     Report Count (1)
	0x81, 0x06, //     Input (Data, Var, Rel)

	0xC0, // End Collection
}

func TestUsbGadgetInit(t *testing.T) {
	assert := assert.New(t)
	usbGadget = NewUsbGadget(usbGadgetName, usbDevices, usbConfig, nil)

	assert.NotNil(usbGadget)
}

func TestUsbGadgetStrictModeInitFail(t *testing.T) {
	usbConfig.strictMode = true
	u := NewUsbGadget("test", usbDevices, usbConfig, nil)
	assert.Nil(t, u, "should be nil")
}

func TestUsbGadgetUDCNotBoundAfterReportDescrChanged(t *testing.T) {
	assert := assert.New(t)
	usbGadget = NewUsbGadget(usbGadgetName, usbDevices, usbConfig, nil)
	assert.NotNil(usbGadget)

	// release the usb gadget and create a new one
	usbGadget = nil

	altGadgetConfig := defaultGadgetConfig

	oldAbsoluteMouseConfig := altGadgetConfig["absolute_mouse"]
	oldAbsoluteMouseConfig.reportDesc = oldAbsoluteMouseCombinedReportDesc
	altGadgetConfig["absolute_mouse"] = oldAbsoluteMouseConfig

	usbGadget = newUsbGadget(usbGadgetName, altGadgetConfig, usbDevices, usbConfig, nil)
	assert.NotNil(usbGadget)

	udcs := getUdcs()
	assert.Equal(1, len(udcs), "should be only one UDC")
	// check if the UDC is bound
	udc := udcs[0]
	assert.NotNil(udc, "UDC should exist")

	udcStr, err := os.ReadFile("/sys/kernel/config/usb_gadget/jetkvm/UDC")
	assert.Nil(err, "usb_gadget/UDC should exist")
	assert.Equal(strings.TrimSpace(udc), strings.TrimSpace(string(udcStr)), "UDC should be the same")
}
