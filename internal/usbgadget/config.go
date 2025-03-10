package usbgadget

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
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
		u.log.Trace("using default gadget config")
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

func mountConfigFS() error {
	_, err := os.Stat(gadgetPath)
	// TODO: check if it's mounted properly
	if err == nil {
		return nil
	}

	if os.IsNotExist(err) {
		err = exec.Command("mount", "-t", "configfs", "none", configFSPath).Run()
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
		u.log.Error("no udc found, skipping USB stack init")
		return nil
	}

	u.udc = udcs[0]
	_, err := os.Stat(u.kvmGadgetPath)
	if err == nil {
		u.log.Info("usb gadget already exists")
	}

	if err := mountConfigFS(); err != nil {
		u.log.Errorf("failed to mount configfs: %v, usb stack might not function properly", err)
	}

	if err := os.MkdirAll(u.configC1Path, 0755); err != nil {
		u.log.Errorf("failed to create config path: %v", err)
	}

	if err := u.writeGadgetConfig(); err != nil {
		u.log.Errorf("failed to start gadget: %v", err)
	}

	return nil
}

func (u *UsbGadget) UpdateGadgetConfig() error {
	u.configLock.Lock()
	defer u.configLock.Unlock()

	u.loadGadgetConfig()

	if err := u.writeGadgetConfig(); err != nil {
		u.log.Errorf("failed to update gadget: %v", err)
	}

	return nil
}

func (u *UsbGadget) getOrderedConfigItems() orderedGadgetConfigItems {
	items := make([]gadgetConfigItemWithKey, 0)
	for key, item := range u.configMap {
		items = append(items, gadgetConfigItemWithKey{key, item})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].item.order < items[j].item.order
	})

	return items
}

func (u *UsbGadget) writeGadgetConfig() error {
	// create kvm gadget path
	err := os.MkdirAll(u.kvmGadgetPath, 0755)
	if err != nil {
		return err
	}

	u.log.Tracef("writing gadget config")
	for _, val := range u.getOrderedConfigItems() {
		key := val.key
		item := val.item

		// check if the item is enabled in the config
		if !u.isGadgetConfigItemEnabled(key) {
			u.log.Tracef("disabling gadget config: %s", key)
			err = u.disableGadgetItemConfig(item)
			if err != nil {
				return err
			}
			continue
		}
		u.log.Tracef("writing gadget config: %s", key)
		err = u.writeGadgetItemConfig(item)
		if err != nil {
			return err
		}
	}

	if err = u.writeUDC(); err != nil {
		u.log.Errorf("failed to write UDC: %v", err)
		return err
	}

	if err = u.rebindUsb(true); err != nil {
		u.log.Infof("failed to rebind usb: %v", err)
	}

	return nil
}

func (u *UsbGadget) disableGadgetItemConfig(item gadgetConfigItem) error {
	// remove symlink if exists
	if item.configPath == nil {
		return nil
	}

	configPath := joinPath(u.configC1Path, item.configPath)

	if _, err := os.Lstat(configPath); os.IsNotExist(err) {
		u.log.Tracef("symlink %s does not exist", item.configPath)
		return nil
	}

	if err := os.Remove(configPath); err != nil {
		return fmt.Errorf("failed to remove symlink %s: %w", item.configPath, err)
	}

	return nil
}

func (u *UsbGadget) writeGadgetItemConfig(item gadgetConfigItem) error {
	// create directory for the item
	gadgetItemPath := joinPath(u.kvmGadgetPath, item.path)
	err := os.MkdirAll(gadgetItemPath, 0755)
	if err != nil {
		return fmt.Errorf("failed to create path %s: %w", gadgetItemPath, err)
	}

	if len(item.attrs) > 0 {
		// write attributes for the item
		err = u.writeGadgetAttrs(gadgetItemPath, item.attrs)
		if err != nil {
			return fmt.Errorf("failed to write attributes for %s: %w", gadgetItemPath, err)
		}
	}

	// write report descriptor if available
	if item.reportDesc != nil {
		err = u.writeIfDifferent(path.Join(gadgetItemPath, "report_desc"), item.reportDesc, 0644)
		if err != nil {
			return err
		}
	}

	// create config directory if configAttrs are set
	if len(item.configAttrs) > 0 {
		configItemPath := joinPath(u.configC1Path, item.configPath)
		err = os.MkdirAll(configItemPath, 0755)
		if err != nil {
			return fmt.Errorf("failed to create path %s: %w", configItemPath, err)
		}

		err = u.writeGadgetAttrs(configItemPath, item.configAttrs)
		if err != nil {
			return fmt.Errorf("failed to write config attributes for %s: %w", configItemPath, err)
		}
	}

	// create symlink if configPath is set
	if item.configPath != nil && item.configAttrs == nil {
		configPath := joinPath(u.configC1Path, item.configPath)
		u.log.Tracef("Creating symlink from %s to %s", configPath, gadgetItemPath)
		if err := ensureSymlink(configPath, gadgetItemPath); err != nil {
			return err
		}
	}

	return nil
}

func (u *UsbGadget) writeGadgetAttrs(basePath string, attrs gadgetAttributes) error {
	for key, val := range attrs {
		filePath := filepath.Join(basePath, key)
		err := u.writeIfDifferent(filePath, []byte(val), 0644)
		if err != nil {
			return fmt.Errorf("failed to write to %s: %w", filePath, err)
		}
	}
	return nil
}
