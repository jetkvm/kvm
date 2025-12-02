package usbgadget

import (
	"fmt"
	"os/exec"
)

type gadgetConfigItem struct {
	order       uint
	device      string
	path        []string
	attrs       gadgetAttributes
	configAttrs gadgetAttributes
	configPath  []string
	reportDesc  []byte
}

type gadgetAttributes map[string]string

type gadgetConfigItemWithKey struct {
	key  string
	item gadgetConfigItem
}

type orderedGadgetConfigItems []gadgetConfigItemWithKey

var defaultGadgetConfig = map[string]gadgetConfigItem{
	"base": {
		order: 0,
		attrs: gadgetAttributes{
			"bcdUSB":    "0x0200", // USB 2.0
			"idVendor":  "0x1d6b", // The Linux Foundation
			"idProduct": "0x0104", // Multifunction Composite Gadget
			"bcdDevice": "0x0100", // USB2
		},
		configAttrs: gadgetAttributes{
			"MaxPower": "250", // in unit of 2mA
		},
	},
	"base_info": {
		order:      1,
		path:       []string{"strings", "0x409"},
		configPath: []string{"strings", "0x409"},
		attrs: gadgetAttributes{
			"serialnumber": "",
			"manufacturer": "JetKVM",
			"product":      "JetKVM USB Emulation Device",
		},
		configAttrs: gadgetAttributes{
			"configuration": "Config 1: HID",
		},
	},
	// keyboard HID
	"keyboard": keyboardConfig,
	// mouse HID
	"absolute_mouse": absoluteMouseConfig,
	// relative mouse HID
	"relative_mouse": relativeMouseConfig,
	// mass storage
	"mass_storage_base": massStorageBaseConfig,
	"mass_storage_lun0": massStorageLun0Config,
}

func (enabledDevices *Devices) isGadgetConfigItemEnabled(itemKey string) bool {
	switch itemKey {
	case "absolute_mouse":
		return enabledDevices.AbsoluteMouse
	case "relative_mouse":
		return enabledDevices.RelativeMouse
	case "keyboard":
		return enabledDevices.Keyboard
	case "mass_storage_base":
		return enabledDevices.MassStorage
	case "mass_storage_lun0":
		return enabledDevices.MassStorage
	default:
		return true
	}
}

func (u *UsbGadget) loadGadgetConfig() {
	if u.customConfig.isEmpty {
		u.getUsbGadgetLoggingContext().Trace().Msg("using default gadget config")
		return
	}

	u.configMap["base"].attrs["idVendor"] = u.customConfig.VendorId
	u.configMap["base"].attrs["idProduct"] = u.customConfig.ProductId

	u.configMap["base_info"].attrs["serialnumber"] = u.customConfig.SerialNumber
	u.configMap["base_info"].attrs["manufacturer"] = u.customConfig.Manufacturer
	u.configMap["base_info"].attrs["product"] = u.customConfig.Product
}

func (u *UsbGadget) SetGadgetConfig(config *Config) {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	if config == nil {
		return // nothing to do
	}

	u.customConfig = *config
	u.loadGadgetConfig()
}

func (u *UsbGadget) SetGadgetDevices(devices *Devices) {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	if devices == nil {
		return // nothing to do
	}

	u.enabledDevices = *devices
}

// GetPath returns the path to the item.
func (u *UsbGadget) GetPath(itemKey string) (string, error) {
	item, ok := u.configMap[itemKey]
	if !ok {
		return "", fmt.Errorf("config item %s not found", itemKey)
	}
	return joinPath(u.kvmGadgetPath, item.path), nil
}

// OverrideGadgetConfig overrides the gadget config for the given item and attribute.
// It returns an error if the item is not found or the attribute is not found.
// It returns true if the attribute is overridden, false otherwise.
func (u *UsbGadget) OverrideGadgetConfig(itemKey string, itemAttr string, value string) (bool, error) {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	context := u.getUsbGadgetLoggingContext().Str("itemKey", itemKey).Str("itemAttr", itemAttr).Str("value", value)

	// get it as a pointer
	_, ok := u.configMap[itemKey]
	if !ok {
		err := fmt.Errorf("not found %s", itemKey)
		context.Err(err).Error().Msg("overriding gadget config")
		return false, err
	}

	if u.configMap[itemKey].attrs[itemAttr] == value {
		context.Trace().Msg("unchanged gadget config")
		return false, nil
	}

	u.configMap[itemKey].attrs[itemAttr] = value
	context.Info().Msg("overriding gadget config")

	return true, nil
}

func mountConfigFS(path string) error {
	err := exec.Command("mount", "-t", "configfs", "none", path).Run()
	if err != nil {
		return fmt.Errorf("failed to mount configfs: %w", err)
	}
	return nil
}

func (u *UsbGadget) Init() error {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	u.loadGadgetConfig()

	udcs := getUdcs()
	if len(udcs) < 1 {
		u.getUsbGadgetLoggingContext().Warn().Msg("no udc found, skipping USB stack init")
		return nil
	}

	u.udc = udcs[0]

	err := u.configureUsbGadget(false)
	if err != nil {
		u.getUsbGadgetLoggingContext().Err(err).Error().Msg("unable to initialize USB stack")
		if u.strictMode {
			return err
		}
	}

	return nil
}

func (u *UsbGadget) UpdateGadgetConfig() error {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	u.loadGadgetConfig()

	err := u.configureUsbGadget(true)
	if err != nil {
		u.getUsbGadgetLoggingContext().Err(err).Error().Msg("unable to update gadget config")
		if u.strictMode {
			return err
		}
	}

	return nil
}

func (u *UsbGadget) configureUsbGadget(resetUsb bool) error {
	return u.WithTransaction(func(u *UsbGadget, tx *UsbGadgetTransaction) error {
		tx.MountConfigFS()
		tx.CreateConfigPath(u.configC1Path)
		tx.WriteGadgetConfig(u.kvmGadgetPath, u.configC1Path, u.udc, u.getOrderedConfigItems(), &u.enabledDevices)
		if resetUsb {
			tx.RebindUsb(u.udc, true)
		}
		return nil
	})
}
