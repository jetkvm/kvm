package usbgadget

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

func getUdcs() []string {
	var udcs []string

	files, err := os.ReadDir("/sys/devices/platform/usbdrd")
	if err != nil {
		return nil
	}

	for _, file := range files {
		if !file.IsDir() || !strings.HasSuffix(file.Name(), ".usb") {
			continue
		}
		udcs = append(udcs, file.Name())
	}

	return udcs
}

// hidgDevicePath is the chardev used to verify HID function health after rebind.
const hidgDevicePath = "/dev/hidg0"

func rebindUsb(udc string, ignoreUnbindError bool) error {
	err := os.WriteFile(path.Join(dwc3Path, "unbind"), []byte(udc), 0644)
	if err != nil && !ignoreUnbindError {
		return err
	}
	err = os.WriteFile(path.Join(dwc3Path, "bind"), []byte(udc), 0644)
	if err != nil {
		return err
	}

	// The DWC3 controller on the RV1106 has a race condition where rapid
	// unbind→bind can leave HID chardevs (e.g. /dev/hidg0) permanently
	// returning ENXIO even though the sysfs entry and device node exist.
	// Verify the chardev is functional; if not, rebind once more with a
	// brief pause to allow the kernel to finish cleanup.
	if !isHidgChardevHealthy() {
		_ = os.WriteFile(path.Join(dwc3Path, "unbind"), []byte(udc), 0644)
		time.Sleep(100 * time.Millisecond)
		if err := os.WriteFile(path.Join(dwc3Path, "bind"), []byte(udc), 0644); err != nil {
			return fmt.Errorf("retry bind after hidg verification failed: %w", err)
		}
	}

	return nil
}

func isHidgChardevHealthy() bool {
	f, err := os.OpenFile(hidgDevicePath, os.O_RDWR, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

func (u *UsbGadget) rebindUsb(ignoreUnbindError bool) error {
	u.log.Info().Str("udc", u.udc).Msg("rebinding USB gadget to UDC")
	return rebindUsb(u.udc, ignoreUnbindError)
}

// RebindUsb rebinds the USB gadget to the UDC.
func (u *UsbGadget) RebindUsb(ignoreUnbindError bool) error {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	return u.rebindUsb(ignoreUnbindError)
}

// GetUsbState returns the current state of the USB gadget
func (u *UsbGadget) GetUsbState() (state string) {
	stateFile := path.Join("/sys/class/udc", u.udc, "state")
	stateBytes, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "not attached"
		} else {
			u.log.Trace().Err(err).Msg("failed to read usb state")
		}
		return "unknown"
	}
	return strings.TrimSpace(string(stateBytes))
}

// HostPresent reports whether a USB data host is physically connected to the
// port, based on the USB PHY extcon cable state. The UDC state reads
// "not attached" both when no host is plugged in (the supported idle state,
// #1540) and when a host is present but failed to enumerate (e.g. powered via
// the ATX/DC extension, #128); only the extcon cable state tells them apart.
func (u *UsbGadget) HostPresent() bool {
	stateBytes, err := os.ReadFile("/sys/class/extcon/extcon0/state")
	if err == nil {
		if hostPresent, known := extconHostState(string(stateBytes)); known {
			return hostPresent
		}
	}

	// No usable extcon state: any link state other than "not attached" implies
	// a host.
	state := u.GetUsbState()
	return state != USBStateNotAttached && state != USBStateUnknown
}

// extconHostState classifies an extcon `state` payload as host-present/absent.
// known is false when the payload had no recognizable cable state (empty, or a
// kernel that names cables differently), so the caller can fall back to the UDC
// state instead of wrongly concluding "no host".
func extconHostState(payload string) (hostPresent bool, known bool) {
	var sawHostCable, sawNonDataCable bool
	for _, line := range strings.Split(payload, "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		enabled, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil {
			continue
		}
		switch key {
		case "USB-HOST", "SDP", "CDP":
			sawHostCable = true
			if enabled != 0 {
				return true, true
			}
		case "DCP", "SLOW-CHARGER", "USB_VBUS_EN":
			if enabled != 0 {
				sawNonDataCable = true
			}
		}
	}
	// Recognizable cable state present: a host-capable line set to 0 or a
	// charger-only line means no data host.
	if sawHostCable || sawNonDataCable {
		return false, true
	}
	// No recognizable cable state at all -> unknown.
	return false, false
}

// IsUDCBound checks if the UDC state is bound.
func (u *UsbGadget) IsUDCBound() (bool, error) {
	udcFilePath := path.Join(dwc3Path, u.udc)
	_, err := os.Stat(udcFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("error checking USB emulation state: %w", err)
	}
	return true, nil
}

// BindUDC binds the gadget to the UDC.
func (u *UsbGadget) BindUDC() error {
	err := os.WriteFile(path.Join(dwc3Path, "bind"), []byte(u.udc), 0644)
	if err != nil {
		return fmt.Errorf("error binding UDC: %w", err)
	}
	return nil
}

// UnbindUDC unbinds the gadget from the UDC.
func (u *UsbGadget) UnbindUDC() error {
	err := os.WriteFile(path.Join(dwc3Path, "unbind"), []byte(u.udc), 0644)
	if err != nil {
		return fmt.Errorf("error unbinding UDC: %w", err)
	}
	return nil
}
