package usbgadget

// ncmConfig declares the CDC-NCM (Ethernet over USB) function.
// host_addr / dev_addr are placeholders overridden at runtime from
// customConfig.NcmHostMAC and customConfig.NcmDevMAC in loadGadgetConfig.
var ncmConfig = gadgetConfigItem{
	order:      5000,
	device:     "ncm.usb0",
	path:       []string{"functions", "ncm.usb0"},
	configPath: []string{"ncm.usb0"},
	attrs: gadgetAttributes{
		"host_addr": "02:00:00:00:00:01",
		"dev_addr":  "02:00:00:00:00:02",
	},
}
