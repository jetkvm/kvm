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

func TestTouchscreenHidDevicePathFollowsEnabledHidFunctions(t *testing.T) {
	tests := []struct {
		name    string
		devices Devices
		want    string
	}{
		{
			name: "all earlier hid functions enabled",
			devices: Devices{
				Keyboard:      true,
				AbsoluteMouse: true,
				RelativeMouse: true,
				Touchscreen:   true,
			},
			want: "/dev/hidg3",
		},
		{
			name: "keyboard and touchscreen only",
			devices: Devices{
				Keyboard:    true,
				Touchscreen: true,
			},
			want: "/dev/hidg1",
		},
		{
			name: "touchscreen only",
			devices: Devices{
				Touchscreen: true,
			},
			want: "/dev/hidg0",
		},
		{
			name: "keyboard absolute mouse and touchscreen",
			devices: Devices{
				Keyboard:      true,
				AbsoluteMouse: true,
				Touchscreen:   true,
			},
			want: "/dev/hidg2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &UsbGadget{enabledDevices: tt.devices}

			if got := u.touchscreenHidDevicePath(); got != tt.want {
				t.Fatalf("touchscreenHidDevicePath() = %q, want %q", got, tt.want)
			}
		})
	}
}
