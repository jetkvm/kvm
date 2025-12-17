package usbgadget

import (
	"fmt"
	"os"
	"path"
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

func rebindUsb(udc string, ignoreUnbindError bool) error {
	err := os.WriteFile(path.Join(dwc3Path, "unbind"), []byte(udc), 0644)
	if err != nil && !ignoreUnbindError {
		return err
	}
	// Add 200ms delay after unbind to allow UVC video device cleanup before rebind.
	// Without this delay, the UVC gadget driver fails with "kobject already initialized"
	// because the video4linux device hasn't been fully unregistered yet.
	time.Sleep(200 * time.Millisecond)
	err = os.WriteFile(path.Join(dwc3Path, "bind"), []byte(udc), 0644)
	if err != nil {
		return err
	}
	return nil
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

// BindUDC binds the gadget to the UDC by writing the UDC name to the gadget's UDC file.
func (u *UsbGadget) BindUDC() error {
	udcFile := path.Join(u.kvmGadgetPath, "UDC")
	err := os.WriteFile(udcFile, []byte(u.udc), 0644)
	if err != nil {
		return fmt.Errorf("error binding gadget to UDC: %w", err)
	}
	return nil
}

// UnbindUDC unbinds the gadget from the UDC by writing empty string to the gadget's UDC file.
func (u *UsbGadget) UnbindUDC() error {
	udcFile := path.Join(u.kvmGadgetPath, "UDC")
	err := os.WriteFile(udcFile, []byte(""), 0644)
	if err != nil {
		return fmt.Errorf("error unbinding gadget from UDC: %w", err)
	}
	return nil
}

// RebindDwc3Driver rebinds the dwc3 platform driver (heavy operation - use sparingly).
func (u *UsbGadget) RebindDwc3Driver(ignoreUnbindError bool) error {
	u.log.Info().Str("udc", u.udc).Msg("rebinding dwc3 platform driver")
	return rebindUsb(u.udc, ignoreUnbindError)
}

// removeConfigSymlinks removes all function symlinks from configs/c.1/
// This is needed during reconfiguration to prevent stale state from causing
// binding failures (especially with UVC which can fail with -19 ENODEV).
func (u *UsbGadget) removeConfigSymlinks() {
	entries, err := os.ReadDir(u.configC1Path)
	if err != nil {
		u.log.Debug().Err(err).Msg("failed to read config c.1 directory")
		return
	}

	for _, entry := range entries {
		// Only remove symlinks (function links like hid.usb0, uvc.usb0, etc.)
		if entry.Type()&os.ModeSymlink != 0 {
			symlinkPath := path.Join(u.configC1Path, entry.Name())
			if err := os.Remove(symlinkPath); err != nil {
				u.log.Debug().Err(err).Str("symlink", entry.Name()).Msg("failed to remove config symlink")
			} else {
				u.log.Debug().Str("symlink", entry.Name()).Msg("removed config symlink")
			}
		}
	}
}
