package usbgadget

var audioConfig = gadgetConfigItem{
	order:      2500,
	device:     "uac1.usb0",
	path:       []string{"functions", "uac1.usb0"},
	configPath: []string{"uac1.usb0"},
	attrs: gadgetAttributes{
		"c_chmask": "0x3",
		"c_srate":  "48000",
		"c_ssize":  "2",
		"p_chmask": "0",
	},
}
