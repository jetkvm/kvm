package usbgadget

import (
	"context"
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
	return rebindUsbWithTimeout(udc, ignoreUnbindError, 10*time.Second)
}

func rebindUsbWithTimeout(udc string, ignoreUnbindError bool, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Unbind with timeout
	err := writeFileWithTimeout(ctx, path.Join(dwc3Path, "unbind"), []byte(udc), 0644)
	if err != nil && !ignoreUnbindError {
		return fmt.Errorf("failed to unbind UDC: %w", err)
	}

	// Small delay to allow unbind to complete
	time.Sleep(100 * time.Millisecond)

	// Bind with timeout
	err = writeFileWithTimeout(ctx, path.Join(dwc3Path, "bind"), []byte(udc), 0644)
	if err != nil {
		return fmt.Errorf("failed to bind UDC: %w", err)
	}
	return nil
}

func writeFileWithTimeout(ctx context.Context, filename string, data []byte, perm os.FileMode) error {
	done := make(chan error, 1)
	go func() {
		done <- os.WriteFile(filename, data, perm)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("write operation timed out: %w", ctx.Err())
	}
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
