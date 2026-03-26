package usbgadget

var serialConsoleConfig = gadgetConfigItem{
	order:      4000,
	device:     "acm.usb0",
	path:       []string{"functions", "acm.usb0"},
	configPath: []string{"acm.usb0"},
}
