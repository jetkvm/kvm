package usbgadget

import "testing"

// controllerINEndpoints is the number of IN endpoints of the RV1106 USB
// controller, besides the control endpoint.
const controllerINEndpoints = 9

func externalTestGadget(devices Devices) (*UsbGadget, *ExternalFunction) {
	f := &ExternalFunction{Key: "external", Instance: "external", Order: 5000}
	configMap := map[string]gadgetConfigItem{f.Key: f.configItem()}
	for key, item := range defaultGadgetConfig {
		configMap[key] = item
	}
	return &UsbGadget{enabledDevices: devices, configMap: configMap}, f
}

// The endpoint counts of the functions match the device: with the default
// devices a function after them gets IN endpoint 6 (0x86 in lsusb on the
// host), and with the serial console also, IN endpoint 8.
func TestINEndpointsBeforeExternalFunction(t *testing.T) {
	u, f := externalTestGadget(defaultUsbGadgetDevices)
	if got := u.inEndpointsBefore(f.Order) + 1; got != 6 {
		t.Fatalf("default devices: first IN endpoint %d, want 6", got)
	}
	devices := defaultUsbGadgetDevices
	devices.SerialConsole = true
	u, f = externalTestGadget(devices)
	if got := u.inEndpointsBefore(f.Order) + 1; got != 8 {
		t.Fatalf("with the serial console: first IN endpoint %d, want 8", got)
	}
}

// For each set of enabled devices, the external function's own IN endpoint
// (after the spare one, if any) is an endpoint that works, and the
// controller has it.
func TestExternalFunctionINEndpointIsReliable(t *testing.T) {
	for set := 0; set < 1<<6; set++ {
		devices := Devices{
			AbsoluteMouse: set&1 != 0,
			RelativeMouse: set&2 != 0,
			Keyboard:      set&4 != 0,
			MassStorage:   set&8 != 0,
			SerialConsole: set&16 != 0,
			Audio:         set&32 != 0,
		}
		u, f := externalTestGadget(devices)
		endpoint := u.inEndpointsBefore(f.Order) + 1
		if u.externalNeedsSpareIN(f) {
			endpoint++
		}
		if unreliableINEndpoint(endpoint) || endpoint > controllerINEndpoints {
			t.Errorf("%+v: the external function's IN endpoint is %d", devices, endpoint)
		}
	}
}
