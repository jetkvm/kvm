package usbgadget

var massStorageBaseConfig = gadgetConfigItem{
	order:      3000,
	device:     "mass_storage.usb0",
	path:       []string{"functions", "mass_storage.usb0"},
	configPath: []string{"mass_storage.usb0"},
	attrs: gadgetAttributes{
		"stall": "1",
	},
}

var massStorageLun0Config = gadgetConfigItem{
	order: 3001,
	path:  []string{"functions", "mass_storage.usb0", "lun.0"},
	attrs: gadgetAttributes{
		"cdrom":     "1",
		"ro":        "1",
		"removable": "1",
		"file":      "\n",
		// the additional whitespace is intentional to avoid the "JetKVM V irtual Media" string
		// https://github.com/jetkvm/rv1106-system/blob/778133a1c153041e73f7de86c9c434a2753ea65d/sysdrv/source/uboot/u-boot/drivers/usb/gadget/f_mass_storage.c#L2556
		// Vendor (8 chars), product (16 chars)
		"inquiry_string": "JetKVM  Virtual Media",
	},
}
