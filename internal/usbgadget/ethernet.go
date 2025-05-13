package usbgadget

// Ethernet Control Model (ECM)
var ethernetEcmConfig = gadgetConfigItem{
	order:      4000,
	path:       []string{"functions", "ecm.usb0"},
	configPath: []string{"ecm.usb0"},
	attrs: gadgetAttributes{
		"host_addr": "", // MAC address of target host (randomly select)
		"dev_addr":  "", // MAC address of JetKVM (randomly select)
	},
}

// Ethernet Emulation Model (EEM)
var ethernetEemConfig = gadgetConfigItem{
	order:      4001,
	path:       []string{"functions", "eem.usb0"},
	configPath: []string{"eem.usb0"},
	attrs: gadgetAttributes{
		"host_addr": "", // MAC address of target host (randomly select)
		"dev_addr":  "", // MAC address of JetKVM (randomly select)
	},
}

// Network Control Model (NCM)
var ethernetNcmConfig = gadgetConfigItem{
	order:      4001,
	path:       []string{"functions", "ncm.usb0"},
	configPath: []string{"ncm.usb0"},
	attrs: gadgetAttributes{
		"host_addr": "", // MAC address of target host (randomly select)
		"dev_addr":  "", // MAC address of JetKVM (randomly select)
	},
}

// Remote Network Driver Interface Specification (RNDIS)
var ethernetRndisConfig = gadgetConfigItem{
	order:      4001,
	path:       []string{"functions", "rndis.usb0"},
	configPath: []string{"rndis.usb0"},
	attrs: gadgetAttributes{
		"host_addr": "", // MAC address of target host (randomly select)
		"dev_addr":  "", // MAC address of JetKVM (randomly select)
	},
}
