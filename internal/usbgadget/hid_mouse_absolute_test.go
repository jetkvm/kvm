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

	if x, y := u.GetAbsMousePosition(); x != 0 || y != 0 {
		t.Fatalf("initial GetAbsMousePosition = (%d, %d), want (0, 0)", x, y)
	}

	if err := u.AbsMouseReport(1234, 5678, 0); err != nil {
		t.Fatalf("AbsMouseReport: %v", err)
	}
	if x, y := u.GetAbsMousePosition(); x != 1234 || y != 5678 {
		t.Fatalf("GetAbsMousePosition = (%d, %d), want (1234, 5678)", x, y)
	}

	// Position tracking must not depend on a button-state change.
	if err := u.AbsMouseReport(42, 99, 0); err != nil {
		t.Fatalf("AbsMouseReport: %v", err)
	}
	if x, y := u.GetAbsMousePosition(); x != 42 || y != 99 {
		t.Fatalf("GetAbsMousePosition = (%d, %d), want (42, 99)", x, y)
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
	if x, y := u.GetAbsMousePosition(); x != 0 || y != 0 {
		t.Fatalf("GetAbsMousePosition = (%d, %d), want (0, 0) since the device is disabled", x, y)
	}
}
