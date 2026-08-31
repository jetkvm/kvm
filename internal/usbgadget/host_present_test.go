package usbgadget

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtconHostState(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		wantPresent bool
		wantKnown   bool
	}{
		{
			name:        "no host (all zero)",
			payload:     "USB=0\nUSB-HOST=0\nUSB_VBUS_EN=0\nSDP=0\nCDP=0\nDCP=0\nSLOW-CHARGER=0\n",
			wantPresent: false,
			wantKnown:   true,
		},
		{
			name:        "real host (SDP set)",
			payload:     "USB=1\nUSB-HOST=1\nUSB_VBUS_EN=1\nSDP=1\nCDP=0\nDCP=0\nSLOW-CHARGER=0\n",
			wantPresent: true,
			wantKnown:   true,
		},
		{
			name:        "USB-HOST set but no SDP",
			payload:     "USB=1\nUSB-HOST=1\nUSB_VBUS_EN=1\nSDP=0\nCDP=0\nDCP=0\nSLOW-CHARGER=0\n",
			wantPresent: true,
			wantKnown:   true,
		},
		{
			name:        "charger only (DCP, VBUS on) -> not a host",
			payload:     "USB=1\nUSB-HOST=0\nUSB_VBUS_EN=1\nSDP=0\nCDP=0\nDCP=1\nSLOW-CHARGER=0\n",
			wantPresent: false,
			wantKnown:   true,
		},
		{
			name:        "charger only (SLOW-CHARGER) -> not a host",
			payload:     "USB=1\nUSB-HOST=0\nUSB_VBUS_EN=1\nSDP=0\nCDP=0\nDCP=0\nSLOW-CHARGER=1\n",
			wantPresent: false,
			wantKnown:   true,
		},
		{
			name:        "VBUS only, no host-capable/charger cable -> known no host",
			payload:     "USB=1\nUSB_VBUS_EN=1\n",
			wantPresent: false,
			wantKnown:   true,
		},
		{
			name:        "empty payload -> unknown (fall back)",
			payload:     "",
			wantPresent: false,
			wantKnown:   false,
		},
		{
			name:        "host-capable lines all zero -> known no host",
			payload:     "USB=0\nUSB-HOST=0\nSDP=0\nCDP=0\n",
			wantPresent: false,
			wantKnown:   true,
		},
		{
			name:        "malformed value ignored, no known cable -> unknown",
			payload:     "USB_VBUS_EN=notanumber\n",
			wantPresent: false,
			wantKnown:   false,
		},
		{
			name:        "recognizable host cable + malformed charger -> known host",
			payload:     "SDP=1\nDCP=notanumber\n",
			wantPresent: true,
			wantKnown:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			present, known := extconHostState(tt.payload)
			if present != tt.wantPresent || known != tt.wantKnown {
				t.Fatalf(
					"extconHostState(%q) = (%v, %v), want (%v, %v)",
					tt.payload, present, known, tt.wantPresent, tt.wantKnown,
				)
			}
		})
	}
}

func writeExtconFixture(t *testing.T, root string, device string, name string, state string) {
	t.Helper()
	dir := filepath.Join(root, device)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "name"), []byte(name+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state"), []byte(state), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestIsUsbHostPresent(t *testing.T) {
	origPath := extconClassPath
	t.Cleanup(func() { extconClassPath = origPath })

	t.Run("host present", func(t *testing.T) {
		root := t.TempDir()
		writeExtconFixture(t, root, "extcon0", "ff3e0000.usb2-phy", "USB=1\nUSB-HOST=0\nSDP=1\nDCP=0\n")
		extconClassPath = root

		present, known := (&UsbGadget{}).IsUsbHostPresent()
		if !known || !present {
			t.Fatalf("IsUsbHostPresent() = (%v, %v), want (true, true)", present, known)
		}
	})

	t.Run("host absent", func(t *testing.T) {
		root := t.TempDir()
		writeExtconFixture(t, root, "extcon0", "ff3e0000.usb2-phy", "USB=0\nUSB-HOST=0\nSDP=0\nDCP=0\n")
		extconClassPath = root

		present, known := (&UsbGadget{}).IsUsbHostPresent()
		if !known || present {
			t.Fatalf("IsUsbHostPresent() = (%v, %v), want (false, true)", present, known)
		}
	})

	t.Run("no extcon class", func(t *testing.T) {
		extconClassPath = filepath.Join(t.TempDir(), "does-not-exist")

		present, known := (&UsbGadget{}).IsUsbHostPresent()
		if known || present {
			t.Fatalf("IsUsbHostPresent() = (%v, %v), want (false, false)", present, known)
		}
	})

	t.Run("unrelated extcon devices are ignored", func(t *testing.T) {
		root := t.TempDir()
		writeExtconFixture(t, root, "extcon0", "hdmi-connector", "HDMI=1\n")
		extconClassPath = root

		present, known := (&UsbGadget{}).IsUsbHostPresent()
		if known || present {
			t.Fatalf("IsUsbHostPresent() = (%v, %v), want (false, false)", present, known)
		}
	})
}
