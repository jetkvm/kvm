//go:build arm && linux

package kvm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jetkvm/kvm/internal/usbgadget"
)

func TestMountedMassStorageImageSurvivesGadgetReconfiguration(t *testing.T) {
	const gadgetPath = "/sys/kernel/config/usb_gadget/jetkvm"
	backingFilePath := filepath.Join(gadgetPath, "functions", "mass_storage.usb0", "lun.0", "file")

	originalGadget := gadget
	var testGadget *usbgadget.UsbGadget
	var originalBackingFile []byte
	var imagePath string
	t.Cleanup(func() {
		if testGadget != nil {
			gadget = testGadget
			restoredBackingFile := string(originalBackingFile)
			if strings.TrimSpace(restoredBackingFile) == "" {
				restoredBackingFile = "\n"
			}
			if err := setMassStorageImage(restoredBackingFile); err != nil {
				t.Errorf("restore mass storage backing file: %v", err)
			}
			if err := testGadget.Close(); err != nil {
				t.Errorf("close USB gadget: %v", err)
			}
		} else if err := os.WriteFile(backingFilePath, originalBackingFile, 0644); err != nil {
			t.Errorf("restore mass storage backing file: %v", err)
		}
		gadget = originalGadget

		restoredBackingFile, err := os.ReadFile(backingFilePath)
		if err != nil {
			t.Errorf("read restored mass storage backing file: %v", err)
		} else if strings.TrimSpace(string(restoredBackingFile)) != strings.TrimSpace(string(originalBackingFile)) {
			t.Errorf("restored mass storage backing file = %q, want %q", restoredBackingFile, originalBackingFile)
		}
		if imagePath != "" {
			if err := os.Remove(imagePath); err != nil && !os.IsNotExist(err) {
				t.Errorf("remove temporary image: %v", err)
			}
		}
	})

	devices := &usbgadget.Devices{
		AbsoluteMouse: true,
		RelativeMouse: true,
		Keyboard:      true,
		MassStorage:   true,
	}
	config := &usbgadget.Config{
		VendorId:     "0x1d6b",
		ProductId:    "0x0104",
		Manufacturer: "JetKVM",
		Product:      "USB Emulation Device",
	}
	testGadget = usbgadget.NewUsbGadget("jetkvm", devices, config, nil)
	if testGadget == nil {
		t.Fatal("initialize USB gadget")
	}
	gadget = testGadget
	originalBackingFile, err := os.ReadFile(backingFilePath)
	if err != nil {
		t.Fatalf("read original mass storage backing file: %v", err)
	}

	image, err := os.CreateTemp("", "jetkvm-mass-storage-*.img")
	if err != nil {
		t.Fatalf("create temporary image: %v", err)
	}
	imagePath = image.Name()
	if err := image.Truncate(1024 * 1024); err != nil {
		image.Close()
		t.Fatalf("size temporary image: %v", err)
	}
	if err := image.Close(); err != nil {
		t.Fatalf("close temporary image: %v", err)
	}

	if err := setMassStorageImage(imagePath); err != nil {
		t.Fatalf("set mass storage image: %v", err)
	}
	if err := gadget.UpdateGadgetConfig(); err != nil {
		t.Fatalf("reconfigure USB gadget: %v", err)
	}

	got, err := os.ReadFile(backingFilePath)
	if err != nil {
		t.Fatalf("read mass storage backing file after reconfiguration: %v", err)
	}
	if strings.TrimSpace(string(got)) != imagePath {
		t.Fatalf("mass storage backing file after reconfiguration = %q, want %q", got, imagePath)
	}
}
