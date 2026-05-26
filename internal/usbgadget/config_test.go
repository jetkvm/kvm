package usbgadget

import "testing"

func TestAudioGadgetConfigFollowsEnabledDevice(t *testing.T) {
	u := &UsbGadget{enabledDevices: Devices{Audio: false}}
	if u.isGadgetConfigItemEnabled("audio") {
		t.Fatal("audio gadget should be disabled when audio device is disabled")
	}

	u.enabledDevices.Audio = true
	if !u.isGadgetConfigItemEnabled("audio") {
		t.Fatal("audio gadget should be enabled when audio device is enabled")
	}
}

func TestBaseGadgetConfigItemsAlwaysEnabled(t *testing.T) {
	u := &UsbGadget{}
	for _, item := range []string{"base", "base_info", "wake_hid"} {
		if !u.isGadgetConfigItemEnabled(item) {
			t.Fatalf("%s should always be enabled", item)
		}
	}
}
