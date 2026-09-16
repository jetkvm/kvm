package usbgadget

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
)

func TestAbsMousePositionTracksLastReport(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	logger := zerolog.Nop()
	u := &UsbGadget{
		log:                   &logger,
		logSuppressionCounter: make(map[string]int),
		absMouseHidFile:       w,
		enabledDevices:        Devices{AbsoluteMouse: true},
	}

	if x, y, known := u.GetAbsMousePosition(); x != 0 || y != 0 || known {
		t.Fatalf("initial GetAbsMousePosition = (%d, %d, %v), want (0, 0, false)", x, y, known)
	}

	if err := u.AbsMouseReport(1234, 5678, 0); err != nil {
		t.Fatalf("AbsMouseReport: %v", err)
	}
	if x, y, known := u.GetAbsMousePosition(); x != 1234 || y != 5678 || !known {
		t.Fatalf("GetAbsMousePosition = (%d, %d, %v), want (1234, 5678, true)", x, y, known)
	}

	// Position tracking must not depend on a button-state change.
	if err := u.AbsMouseReport(42, 99, 0); err != nil {
		t.Fatalf("AbsMouseReport: %v", err)
	}
	if x, y, known := u.GetAbsMousePosition(); x != 42 || y != 99 || !known {
		t.Fatalf("GetAbsMousePosition = (%d, %d, %v), want (42, 99, true)", x, y, known)
	}
}

func TestAbsMousePositionDisabledDeviceNoop(t *testing.T) {
	logger := zerolog.Nop()
	u := &UsbGadget{
		log:                   &logger,
		logSuppressionCounter: make(map[string]int),
		enabledDevices:        Devices{AbsoluteMouse: false},
	}

	if err := u.AbsMouseReport(1234, 5678, 0); err != nil {
		t.Fatalf("AbsMouseReport with disabled device: %v", err)
	}
	if x, y, known := u.GetAbsMousePosition(); x != 0 || y != 0 || known {
		t.Fatalf("GetAbsMousePosition = (%d, %d, %v), want (0, 0, false) since the device is disabled", x, y, known)
	}
}
