package usbgadget

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fakeHidGadget(t *testing.T, devByFunction map[string]string) string {
	t.Helper()
	gadgetPath := t.TempDir()
	for function, dev := range devByFunction {
		dir := filepath.Join(gadgetPath, "functions", function)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "dev"), []byte(dev), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return gadgetPath
}

func recordHidOpens(u *UsbGadget) *[]string {
	opened := &[]string{}
	u.hidOpenFile = func(name string, _ int, _ os.FileMode) (*os.File, error) {
		*opened = append(*opened, name)
		return nil, os.ErrNotExist
	}
	return opened
}

func TestHidDevicePathFollowsFunctionMinor(t *testing.T) {
	u := &UsbGadget{configMap: defaultGadgetConfig}
	u.kvmGadgetPath = fakeHidGadget(t, map[string]string{
		"hid.usb0": "242:0\n",
		"hid.usb1": "not a device number\n",
		"hid.usb3": "242:2\n",
	})

	for item, want := range map[string]string{
		"keyboard": "/dev/hidg0",
		"wake_hid": "/dev/hidg2",
	} {
		if got, err := u.hidDevicePath(item); err != nil || got != want {
			t.Errorf("hidDevicePath(%q) = %q, %v, want %q", item, got, err, want)
		}
	}

	for name, item := range map[string]string{
		"malformed dev attribute": "absolute_mouse",
		"function not created":    "relative_mouse",
		"unknown item":            "no_such_item",
	} {
		if got, err := u.hidDevicePath(item); err == nil {
			t.Errorf("%s: hidDevicePath(%q) = %q, want an error", name, item, got)
		}
	}
}

func TestDisabledKeyboardWritesToNoHidNode(t *testing.T) {
	u := newTestGadgetWithKeyboard(t, nil)
	u.enabledDevices.Keyboard = false
	u.kbdAutoReleaseTimers = make(map[byte]*time.Timer)
	u.kvmGadgetPath = fakeHidGadget(t, map[string]string{
		"hid.usb1": "242:0\n",
		"hid.usb2": "242:1\n",
		"hid.usb3": "242:2\n",
	})
	opened := recordHidOpens(u)

	if err := u.OpenKeyboardHidFile(); err != nil {
		t.Errorf("OpenKeyboardHidFile: %v", err)
	}
	if err := u.KeyboardReport(1, []byte{4, 5, 6, 7, 0, 0}); err != nil {
		t.Errorf("KeyboardReport: %v", err)
	}
	if err := u.KeypressReport(4, true); err != nil {
		t.Errorf("KeypressReport: %v", err)
	}
	u.CancelAllAutoReleaseTimers()
	if len(*opened) != 0 {
		t.Errorf("opened %v for a disabled keyboard", *opened)
	}
}

func TestHidReportFailsWithoutItsFunction(t *testing.T) {
	reports := map[string]func(*UsbGadget) error{
		"keyboard":       func(u *UsbGadget) error { return u.KeyboardReport(1, []byte{4, 0, 0, 0, 0, 0}) },
		"wake":           func(u *UsbGadget) error { return u.WakeReport(true) },
		"absolute mouse": func(u *UsbGadget) error { return u.AbsMouseReport(1, 1, 0) },
		"relative mouse": func(u *UsbGadget) error { return u.RelMouseReport(1, 1, 0) },
	}
	for name, report := range reports {
		t.Run(name, func(t *testing.T) {
			u := newTestGadgetWithKeyboard(t, nil)
			u.kvmGadgetPath = t.TempDir()
			opened := recordHidOpens(u)

			if err := report(u); err == nil {
				t.Error("report succeeded without a HID function")
			}
			if len(*opened) != 0 {
				t.Errorf("opened %v without a HID function", *opened)
			}
		})
	}
}
