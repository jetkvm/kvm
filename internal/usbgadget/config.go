package usbgadget

import (
	"fmt"
	"os"
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
			"idProduct": "0104",   // Multifunction Composite Gadget
			"bcdDevice": "0100",
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

func (u *UsbGadget) isGadgetConfigItemEnabled(itemKey string) bool {
	switch itemKey {
	case "absolute_mouse":
		return u.enabledDevices.AbsoluteMouse
	case "relative_mouse":
		return u.enabledDevices.RelativeMouse
	case "keyboard":
		return u.enabledDevices.Keyboard
	case "mass_storage_base":
		return u.enabledDevices.MassStorage
	case "mass_storage_lun0":
		return u.enabledDevices.MassStorage
	default:
		return true
	}
}

func (u *UsbGadget) loadGadgetConfig() {
	if u.customConfig.isEmpty {
		u.log.Trace().Msg("using default gadget config")
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

// GetConfigPath returns the path to the config item.
func (u *UsbGadget) GetConfigPath(itemKey string) (string, error) {
	item, ok := u.configMap[itemKey]
	if !ok {
		return "", fmt.Errorf("config item %s not found", itemKey)
	}
	return joinPath(u.kvmGadgetPath, item.configPath), nil
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
func (u *UsbGadget) OverrideGadgetConfig(itemKey string, itemAttr string, value string) (error, bool) {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	// get it as a pointer
	_, ok := u.configMap[itemKey]
	if !ok {
		return fmt.Errorf("config item %s not found", itemKey), false
	}

	if u.configMap[itemKey].attrs[itemAttr] == value {
		return nil, false
	}

	u.configMap[itemKey].attrs[itemAttr] = value
	u.log.Info().Str("itemKey", itemKey).Str("itemAttr", itemAttr).Str("value", value).Msg("overriding gadget config")

	return nil, true
}

func mountConfigFS(path string) error {
	_, err := os.Stat(path)
	// TODO: check if it's mounted properly
	if err == nil {
		return nil
	}

	if os.IsNotExist(err) {
		err = exec.Command("mount", "-t", "configfs", "none", path).Run()
		if err != nil {
			return fmt.Errorf("failed to mount configfs: %w", err)
		}
	} else {
		return fmt.Errorf("unable to access usb gadget path: %w", err)
	}
	return nil
}

func (u *UsbGadget) Init() error {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	u.loadGadgetConfig()

	udcs := getUdcs()
	if len(udcs) < 1 {
		return u.logWarn("no udc found, skipping USB stack init", nil)
	}

	u.udc = udcs[0]

	err := u.WithTransaction(func() error {
		u.tx.MountConfigFS()
		u.tx.CreateConfigPath()
		u.tx.WriteGadgetConfig()
		return nil
	})
	if err != nil {
		return u.logError("unable to initialize USB stack", err)
	}

	return nil
}

func (u *UsbGadget) UpdateGadgetConfig() error {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	u.loadGadgetConfig()

	err := u.WithTransaction(func() error {
		u.tx.WriteGadgetConfig()
		return nil
	})
	if err != nil {
		return u.logError("unable to update gadget config", err)
	}

	return nil
}
