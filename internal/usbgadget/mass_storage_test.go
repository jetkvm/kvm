package usbgadget

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
)

func TestSetMassStorageBackingFileIsPreservedInReconfigurationPlan(t *testing.T) {
	gadgetPath := t.TempDir()
	functionPath := filepath.Join(gadgetPath, "functions", "mass_storage.usb0", "lun.0")
	if err := os.MkdirAll(functionPath, 0755); err != nil {
		t.Fatalf("create mass storage function path: %v", err)
	}

	lunConfig := massStorageLun0Config
	lunConfig.attrs = make(gadgetAttributes, len(massStorageLun0Config.attrs))
	for key, value := range massStorageLun0Config.attrs {
		lunConfig.attrs[key] = value
	}

	logger := zerolog.Nop()
	gadget := &UsbGadget{
		kvmGadgetPath: gadgetPath,
		configC1Path:  filepath.Join(t.TempDir(), "configs", "c.1"),
		configMap: map[string]gadgetConfigItem{
			"mass_storage_lun0": lunConfig,
		},
		enabledDevices: Devices{MassStorage: true},
		log:            &logger,
	}
	imagePath := "/userdata/jetkvm/images/talos.raw"

	if err := gadget.SetMassStorageBackingFile(imagePath); err != nil {
		t.Fatalf("set mass storage backing file: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(functionPath, "file"))
	if err != nil {
		t.Fatalf("read live mass storage backing file: %v", err)
	}
	if string(got) != imagePath {
		t.Fatalf("live mass storage backing file = %q, want %q", got, imagePath)
	}

	if err := gadget.newUsbGadgetTransaction(true); err != nil {
		t.Fatalf("create gadget transaction: %v", err)
	}
	t.Cleanup(func() {
		gadget.tx = nil
	})
	gadget.tx.WriteGadgetConfig()

	backingFilePath := filepath.Join(functionPath, "file")
	for _, change := range gadget.tx.c.Changes {
		if change.Path != backingFilePath {
			continue
		}
		if string(change.ExpectedContent) != imagePath {
			t.Fatalf("planned mass storage backing file = %q, want %q", change.ExpectedContent, imagePath)
		}
		return
	}

	t.Fatalf("reconfiguration plan does not contain mass storage backing file %q", backingFilePath)
}
