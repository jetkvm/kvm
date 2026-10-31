package usbgadget

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHidDevicePathFollowsFunctionMinor(t *testing.T) {
	gadgetPath := t.TempDir()
	u := &UsbGadget{kvmGadgetPath: gadgetPath, configMap: defaultGadgetConfig}

	// With the absolute mouse disabled, the relative mouse and the wake
	// device move down a number.
	minors := map[string]string{"hid.usb0": "242:0\n", "hid.usb2": "242:1\n", "hid.usb3": "242:2\n"}
	for function, dev := range minors {
		dir := filepath.Join(gadgetPath, "functions", function)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "dev"), []byte(dev), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		item, fallback, want string
	}{
		{"keyboard", "/dev/hidg0", "/dev/hidg0"},
		{"relative_mouse", "/dev/hidg2", "/dev/hidg1"},
		{"wake_hid", "/dev/hidg3", "/dev/hidg2"},
		// no dev attribute: the function does not exist
		{"absolute_mouse", "/dev/hidg1", "/dev/hidg1"},
		{"no_such_item", "/dev/hidg9", "/dev/hidg9"},
	}
	for _, tt := range tests {
		if got := u.hidDevicePath(tt.item, tt.fallback); got != tt.want {
			t.Errorf("hidDevicePath(%q) = %q, want %q", tt.item, got, tt.want)
		}
	}
}
