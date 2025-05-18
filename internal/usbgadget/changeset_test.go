package usbgadget

import (
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

func TestUsbGadgetInit(t *testing.T) {
	assert := assert.New(t)
	usbGadget = NewUsbGadget(usbGadgetName, usbDevices, usbConfig, nil)

	assert.NotNil(usbGadget)
}

func TestUsbGadgetStrictModeInitFail(t *testing.T) {
	usbConfig.strictMode = true
	u := NewUsbGadget("test", usbDevices, usbConfig, nil)
	assert.NotNil(t, u, "should be nil")
}
