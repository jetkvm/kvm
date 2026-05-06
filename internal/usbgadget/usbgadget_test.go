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

func TestDevicesUnmarshalPreservesPartialConfig(t *testing.T) {
	var devices Devices

	if err := json.Unmarshal([]byte(`{
		"keyboard": true
	}`), &devices); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if !devices.Keyboard {
		t.Fatalf("Keyboard = false, want true")
	}
	if devices.AbsoluteMouse {
		t.Fatalf("AbsoluteMouse = true, want false when field is missing")
	}
	if devices.RelativeMouse {
		t.Fatalf("RelativeMouse = true, want false when field is missing")
	}
	if devices.Touchscreen {
		t.Fatalf("Touchscreen = true, want false for partial config")
	}
	if devices.MassStorage {
		t.Fatalf("MassStorage = true, want false when field is missing")
	}
}

func TestDevicesEnsureWheelCapablePointer(t *testing.T) {
	devices := Devices{
		Touchscreen: true,
	}

	devices.EnsureWheelCapablePointer()

	if !devices.AbsoluteMouse {
		t.Fatalf("AbsoluteMouse = false, want true for touchscreen wheel support")
	}
	if devices.RelativeMouse {
		t.Fatalf("RelativeMouse = true, want false")
	}
}

func TestDevicesEnsureWheelCapablePointerKeepsExistingMouse(t *testing.T) {
	tests := []struct {
		name    string
		devices Devices
		want    Devices
	}{
		{
			name: "absolute mouse",
			devices: Devices{
				AbsoluteMouse: true,
				Touchscreen:   true,
			},
			want: Devices{
				AbsoluteMouse: true,
				Touchscreen:   true,
			},
		},
		{
			name: "relative mouse",
			devices: Devices{
				RelativeMouse: true,
				Touchscreen:   true,
			},
			want: Devices{
				RelativeMouse: true,
				Touchscreen:   true,
			},
		},
		{
			name: "no touchscreen",
			devices: Devices{
				Keyboard: true,
			},
			want: Devices{
				Keyboard: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.devices.EnsureWheelCapablePointer()
			if tt.devices != tt.want {
				t.Fatalf("EnsureWheelCapablePointer() = %+v, want %+v", tt.devices, tt.want)
			}
		})
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

func TestAbsoluteMouseHidDevicePathFollowsEnabledHidFunctions(t *testing.T) {
	tests := []struct {
		name    string
		devices Devices
		want    string
	}{
		{
			name: "keyboard and absolute mouse",
			devices: Devices{
				Keyboard:      true,
				AbsoluteMouse: true,
			},
			want: "/dev/hidg1",
		},
		{
			name: "absolute mouse without keyboard",
			devices: Devices{
				AbsoluteMouse: true,
			},
			want: "/dev/hidg0",
		},
		{
			name: "touchscreen does not affect earlier absolute mouse path",
			devices: Devices{
				AbsoluteMouse: true,
				Touchscreen:   true,
			},
			want: "/dev/hidg0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &UsbGadget{enabledDevices: tt.devices}

			if got := u.absoluteMouseHidDevicePath(); got != tt.want {
				t.Fatalf("absoluteMouseHidDevicePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRelativeMouseHidDevicePathFollowsEnabledHidFunctions(t *testing.T) {
	tests := []struct {
		name    string
		devices Devices
		want    string
	}{
		{
			name: "keyboard absolute and relative mouse",
			devices: Devices{
				Keyboard:      true,
				AbsoluteMouse: true,
				RelativeMouse: true,
			},
			want: "/dev/hidg2",
		},
		{
			name: "keyboard and relative mouse",
			devices: Devices{
				Keyboard:      true,
				RelativeMouse: true,
			},
			want: "/dev/hidg1",
		},
		{
			name: "absolute and relative mouse",
			devices: Devices{
				AbsoluteMouse: true,
				RelativeMouse: true,
			},
			want: "/dev/hidg1",
		},
		{
			name: "relative mouse only",
			devices: Devices{
				RelativeMouse: true,
			},
			want: "/dev/hidg0",
		},
		{
			name: "touchscreen does not affect earlier relative mouse path",
			devices: Devices{
				RelativeMouse: true,
				Touchscreen:   true,
			},
			want: "/dev/hidg0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &UsbGadget{enabledDevices: tt.devices}

			if got := u.relativeMouseHidDevicePath(); got != tt.want {
				t.Fatalf("relativeMouseHidDevicePath() = %q, want %q", got, tt.want)
			}
		})
	}
}
