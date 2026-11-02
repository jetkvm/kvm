package usbgadget

import (
	"os"
	"testing"
)

func TestRelativeWheelPreservesButtons(t *testing.T) {
	u := inputTestGadget(t)
	r, w := reportPipe(t)
	u.relMouseHidFile = w
	for _, a := range []struct {
		run  func() error
		want []byte
	}{
		{func() error { return u.RelMouseReport(10, -5, 1) }, []byte{1, 10, 251, 0, 0}},
		{func() error { return u.RelMouseWheelReport(1, -1) }, []byte{1, 0, 0, 1, 255}},
		{func() error { return u.RelMouseReport(-127, 127, 255) }, []byte{255, 129, 127, 0, 0}},
		{func() error { return u.RelMouseWheelReport(-1, 1) }, []byte{255, 0, 0, 255, 1}},
		{func() error { return u.RelMouseReport(0, 0, 0) }, []byte{0, 0, 0, 0, 0}},
		{func() error { return u.RelMouseWheelReport(1, 0) }, []byte{0, 0, 0, 1, 0}},
	} {
		if e := a.run(); e != nil {
			t.Fatal(e)
		}
		readReport(t, r, a.want)
	}
}
func TestRelativeFailedWritePreservesButtons(t *testing.T) {
	u := inputTestGadget(t)
	r, w := reportPipe(t)
	u.relMouseHidFile = w
	if e := u.RelMouseReport(0, 0, 1); e != nil {
		t.Fatal(e)
	}
	readReport(t, r, []byte{1, 0, 0, 0, 0})
	w.Close()
	if e := u.RelMouseReport(0, 0, 2); e == nil {
		t.Fatal("closed endpoint accepted report")
	}
	nr, nw := reportPipe(t)
	u.hidOpenFile = func(string, int, os.FileMode) (*os.File, error) { return nw, nil }
	if e := u.RelMouseWheelReport(1, 0); e != nil {
		t.Fatal(e)
	}
	readReport(t, nr, []byte{1, 0, 0, 1, 0})
}
func TestRelativeRebindAndAdoptionClearButtons(t *testing.T) {
	for _, adopt := range []bool{false, true} {
		t.Run(map[bool]string{false: "rebind", true: "adoption"}[adopt], func(t *testing.T) {
			u := inputTestGadget(t)
			r, w := reportPipe(t)
			u.relMouseHidFile = w
			u.enabledDevices.Keyboard = false
			if e := u.RelMouseReport(0, 0, 1); e != nil {
				t.Fatal(e)
			}
			readReport(t, r, []byte{1, 0, 0, 0, 0})
			u.hidOpenFile = func(string, int, os.FileMode) (*os.File, error) { return nil, os.ErrNotExist }
			if adopt {
				u.adoptLiveGadget()
				readReport(t, r, []byte{0, 0, 0, 0, 0})
			} else {
				if e := u.rebindUsbWith(func() error { u.ResetHIDFiles(); u.resetHidHandover(); return nil }); e != nil {
					t.Fatal(e)
				}
				r, w = reportPipe(t)
				u.relMouseHidFile = w
			}
			if e := u.RelMouseWheelReport(1, 0); e != nil {
				t.Fatal(e)
			}
			readReport(t, r, []byte{0, 0, 0, 1, 0})
		})
	}
}
