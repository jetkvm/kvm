package usbgadget

// var ethernetConfig = gadgetConfigItem{
// 	order:      3100,
// 	device:     "ecm.usb0",
// 	path:       []string{"functions", "ecm.usb0"},
// 	configPath: []string{"ecm.usb0"},
// 	attrs: gadgetAttributes{
// 		"host_addr": "12:34:56:78:90:AB",
// 		"dev_addr":  "12:34:56:78:90:AC",
// 	},
// }

var rndisConfig = gadgetConfigItem{
	// https://stackoverflow.com/questions/12154087/rndis-composite-device-cannot-start
	// it has to be the first or second function, so give it a high priority here :-(
	order:      990,
	device:     "rndis.usb0",
	path:       []string{"functions", "rndis.usb0"},
	configPath: []string{"rndis.usb0"},
	// osDescAttrs: gadgetAttributes{
	// 	"use":           "1",
	// 	"b_vendor_code": "0xcd",
	// 	"qw_sign":       "MSFT100",
	// },
	// featureDescAttrs: gadgetAttributes{
	// 	"compatible_id":    "RNDIS",
	// 	"subcompatible_id": "5162001",
	// },
}
