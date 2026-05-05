package usbgadget

import (
	"encoding/json"
	"testing"
)

func TestDevicesUnmarshalDefaultsMissingTouchscreen(t *testing.T) {
	var devices Devices

	if err := json.Unmarshal([]byte(`{
		"absolute_mouse": true,
		"relative_mouse": true,
		"keyboard": true,
		"mass_storage": true,
		"serial_console": false
	}`), &devices); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if !devices.Touchscreen {
		t.Fatalf("Touchscreen = false, want true when field is missing")
	}
}

func TestDevicesUnmarshalPreservesExplicitTouchscreenFalse(t *testing.T) {
	var devices Devices

	if err := json.Unmarshal([]byte(`{
		"absolute_mouse": true,
		"relative_mouse": true,
		"keyboard": true,
		"touchscreen": false,
		"mass_storage": true,
		"serial_console": false
	}`), &devices); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if devices.Touchscreen {
		t.Fatalf("Touchscreen = true, want false when field is explicit")
	}
}
