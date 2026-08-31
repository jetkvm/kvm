package usbgadget

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestIsVbusPresent(t *testing.T) {
	origPath := extconClassPath
	t.Cleanup(func() { extconClassPath = origPath })

	t.Run("vbus present", func(t *testing.T) {
		root := t.TempDir()
		writeExtconFixture(t, root, "extcon0", "ff3e0000.usb2-phy", "USB=1\nUSB-HOST=0\nSDP=1\n")
		extconClassPath = root

		present, known := (&UsbGadget{}).IsVbusPresent()
		if !known || !present {
			t.Fatalf("IsVbusPresent() = (%v, %v), want (true, true)", present, known)
		}
	})

	t.Run("vbus absent", func(t *testing.T) {
		root := t.TempDir()
		writeExtconFixture(t, root, "extcon0", "ff3e0000.usb2-phy", "USB=0\nUSB-HOST=0\nSDP=0\n")
		extconClassPath = root

		present, known := (&UsbGadget{}).IsVbusPresent()
		if !known || present {
			t.Fatalf("IsVbusPresent() = (%v, %v), want (false, true)", present, known)
		}
	})

	t.Run("no extcon class", func(t *testing.T) {
		extconClassPath = filepath.Join(t.TempDir(), "does-not-exist")

		present, known := (&UsbGadget{}).IsVbusPresent()
		if known || present {
			t.Fatalf("IsVbusPresent() = (%v, %v), want (false, false)", present, known)
		}
	})

	t.Run("unrelated extcon devices are ignored", func(t *testing.T) {
		root := t.TempDir()
		writeExtconFixture(t, root, "extcon0", "hdmi-connector", "HDMI=1\n")
		extconClassPath = root

		present, known := (&UsbGadget{}).IsVbusPresent()
		if known || present {
			t.Fatalf("IsVbusPresent() = (%v, %v), want (false, false)", present, known)
		}
	})

	t.Run("USB-HOST line does not shadow USB line", func(t *testing.T) {
		root := t.TempDir()
		writeExtconFixture(t, root, "extcon0", "ff3e0000.usb2-phy", "USB-HOST=0\nUSB=1\n")
		extconClassPath = root

		present, known := (&UsbGadget{}).IsVbusPresent()
		if !known || !present {
			t.Fatalf("IsVbusPresent() = (%v, %v), want (true, true)", present, known)
		}
	})
}
