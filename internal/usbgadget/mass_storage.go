package usbgadget

import (
	"cmp"
	"os"
	"path"
	"strings"
)

var massStorageBaseConfig = gadgetConfigItem{
	order:      3000,
	device:     "mass_storage.usb0",
	path:       []string{"functions", "mass_storage.usb0"},
	configPath: []string{"mass_storage.usb0"},
	attrs: gadgetAttributes{
		// Never halt the bulk endpoints. Every halt call in f_mass_storage is
		// gated by this flag, and on this dwc3 the halt races the function's
		// disable path on disconnect and oopses in the kernel (#1360). With
		// stalls off the function pads with a zero-length packet instead.
		"stall": "0",
	},
}

func (u *UsbGadget) lunFilePath() (string, error) {
	lunPath, err := u.GetPath("mass_storage_lun0")
	if err != nil {
		return "", err
	}
	return path.Join(lunPath, "file"), nil
}

func (u *UsbGadget) GetMassStorageImage() (string, error) {
	filePath, err := u.lunFilePath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (u *UsbGadget) setMassStorageImageLocked(imagePath string) error {
	filePath, err := u.lunFilePath()
	if err != nil {
		return err
	}
	imagePath = cmp.Or(imagePath, "\n")
	if err := os.WriteFile(filePath, []byte(imagePath), 0644); err != nil {
		return err
	}
	u.configMap["mass_storage_lun0"].attrs["file"] = imagePath
	return nil
}

func (u *UsbGadget) SetMassStorageImage(imagePath string) error {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	return u.setMassStorageImageLocked(imagePath)
}

func (u *UsbGadget) forceEjectLocked() error {
	if softDisconnect(u.udc) == nil {
		defer func() {
			_ = softConnect(u.udc)
		}()
	}
	return u.setMassStorageImageLocked("\n")
}

func (u *UsbGadget) ForceEjectMassStorageImage() error {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	return u.forceEjectLocked()
}

func (u *UsbGadget) syncMassStorageImageFromKernel() {
	img, err := u.GetMassStorageImage()
	if err != nil || img == "" {
		return
	}
	if strings.HasPrefix(img, "/dev/nbd") {
		if err := u.forceEjectLocked(); err != nil {
			u.log.Warn().Err(err).Str("image", img).Msg("failed to eject stale nbd-backed image at init")
		}
		return
	}
	u.configMap["mass_storage_lun0"].attrs["file"] = img
	u.log.Info().Str("image", img).Msg("adopted already-mounted mass storage image from kernel")
}

var massStorageLun0Config = gadgetConfigItem{
	order: 3001,
	path:  []string{"functions", "mass_storage.usb0", "lun.0"},
	attrs: gadgetAttributes{
		"cdrom":     "1",
		"ro":        "1",
		"removable": "1",
		"file":      "\n",
		// the additional whitespace is intentional to avoid the "JetKVM V irtual Media" string
		// https://github.com/jetkvm/rv1106-system/blob/778133a1c153041e73f7de86c9c434a2753ea65d/sysdrv/source/uboot/u-boot/drivers/usb/gadget/f_mass_storage.c#L2556
		// Vendor (8 chars), product (16 chars)
		"inquiry_string": "JetKVM  Virtual Media",
	},
}
