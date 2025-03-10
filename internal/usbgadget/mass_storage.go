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
		"cdrom":          "1",
		"ro":             "1",
		"removable":      "1",
		"file":           "\n",
		"inquiry_string": "JetKVM Virtual Media",
	},
}
