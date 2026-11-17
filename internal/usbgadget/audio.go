package usbgadget

var audioConfig = gadgetConfigItem{
	order:      2500,
	device:     "uac1.usb0",
	path:       []string{"functions", "uac1.usb0"},
	configPath: []string{"uac1.usb0"},
	attrs: gadgetAttributes{
		"c_chmask": "3",
		"c_srate":  "48000",
		"c_ssize":  "2",
		"p_chmask": "0",
		// We capture PCM without applying the gadget's ALSA mixer controls.
		// Do not advertise hardware volume/mute that we cannot implement;
		// hosts can apply software volume instead. Disable both directions:
		// f_uac1 allocates a status IN endpoint if either feature unit is
		// enabled, even when p_chmask is zero. Avoiding that endpoint also
		// keeps mass storage on EP5 IN, below the RV1106 FIFO-reset bug
		// affecting EP6 IN on existing system images (#1634).
		"c_mute_present":   "0",
		"c_volume_present": "0",
		"p_mute_present":   "0",
		"p_volume_present": "0",
	},
}
