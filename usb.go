package kvm

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gadget "github.com/openstadia/go-usb-gadget"
)

const configFSPath = "/sys/kernel/config"
const gadgetPath = "/sys/kernel/config/usb_gadget"
const kvmGadgetPath = "/sys/kernel/config/usb_gadget/jetkvm"
const configC1Path = "/sys/kernel/config/usb_gadget/jetkvm/configs/c.1"

func (u *UsbDevicesConfig) isGadgetConfigItemEnabled(itemKey string) bool {
	switch itemKey {
	case "absolute_mouse":
		return u.AbsoluteMouse
	case "relative_mouse":
		return u.RelativeMouse
	case "keyboard":
		return u.Keyboard
	case "mass_storage_base":
		return u.MassStorage
	case "mass_storage_usb0":
		return u.MassStorage
	default:
		return true
	}
}

type gadgetConfigItem struct {
	device      string
	path        []string
	attrs       gadgetAttributes
	configAttrs gadgetAttributes
	configPath  string
	reportDesc  []byte
}

type gadgetAttributes map[string]string

var gadgetConfig = map[string]gadgetConfigItem{
	"base": {
		attrs: gadgetAttributes{
			"bcdUSB":    "0x0200", // USB 2.0
			"idVendor":  "0x1d6b", // The Linux Foundation
			"idProduct": "0104",   // Multifunction Composite Gadget¬
			"bcdDevice": "0100",
		},
		configAttrs: gadgetAttributes{
			"MaxPower": "250", // in unit of 2mA
		},
	},
	"base_info": {
		path: []string{"strings", "0x409"},
		attrs: gadgetAttributes{
			"serialnumber": GetDeviceID(),
			"manufacturer": "JetKVM",
			"product":      "JetKVM USB Emulation Device",
		},
		configAttrs: gadgetAttributes{
			"configuration": "Config 1: HID",
		},
	},
	// keyboard HID
	"keyboard": {
		device:     "hid.usb0",
		path:       []string{"functions", "hid.usb0"},
		configPath: path.Join(configC1Path, "hid.usb0"),
		attrs: gadgetAttributes{
			"protocol":      "1",
			"subclass":      "1",
			"report_length": "8",
		},
		reportDesc: KeyboardReportDesc,
	},
	// mouse HID
	"absolute_mouse": {
		device:     "hid.usb1",
		path:       []string{"functions", "hid.usb1"},
		configPath: path.Join(configC1Path, "hid.usb1"),
		attrs: gadgetAttributes{
			"protocol":      "2",
			"subclass":      "1",
			"report_length": "6",
		},
		reportDesc: CombinedAbsoluteMouseReportDesc,
	},
	// relative mouse HID
	"relative_mouse": {
		device:     "hid.usb2",
		path:       []string{"functions", "hid.usb2"},
		configPath: path.Join(configC1Path, "hid.usb2"),
		attrs: gadgetAttributes{
			"protocol":      "2",
			"subclass":      "1",
			"report_length": "4",
		},
		reportDesc: CombinedRelativeMouseReportDesc,
	},
	// mass storage
	"mass_storage_base": {
		device:     "mass_storage.usb0",
		path:       []string{"functions", "mass_storage.usb0"},
		configPath: path.Join(configC1Path, "mass_storage.usb0"),
		attrs: gadgetAttributes{
			"stall": "1",
		},
	},
	"mass_storage_usb0": {
		path: []string{"functions", "mass_storage.usb0", "lun.0"},
		attrs: gadgetAttributes{
			"cdrom":          "1",
			"ro":             "1",
			"removable":      "1",
			"file":           "\n",
			"inquiry_string": "JetKVM Virtual Media",
		},
	},
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

func writeIfDifferent(filePath string, content []byte, permMode os.FileMode) error {
	if _, err := os.Stat(filePath); err == nil {
		oldContent, err := os.ReadFile(filePath)
		if err == nil {
			if bytes.Equal(oldContent, content) {
				logger.Tracef("skipping writing to %s as it already has the correct content", filePath)
				return nil
			}

			if len(oldContent) == len(content)+1 &&
				bytes.Equal(oldContent[:len(content)], content) &&
				oldContent[len(content)] == 10 {
				logger.Tracef("skipping writing to %s as it already has the correct content", filePath)
				return nil
			}

			logger.Tracef("writing to %s as it has different content %v %v", filePath, oldContent, content)
		}
	}
	return os.WriteFile(filePath, content, permMode)
}

func disableGadgetItemConfig(item gadgetConfigItem) error {
	// remove symlink if exists
	if item.configPath != "" {
		if _, err := os.Lstat(item.configPath); os.IsNotExist(err) {
			logger.Tracef("symlink %s does not exist", item.configPath)
		} else {
			err := os.Remove(item.configPath)
			if err != nil {
				return fmt.Errorf("failed to remove symlink %s: %w", item.configPath, err)
			}
		}
	}

	return nil
}

func writeGadgetItemConfig(item gadgetConfigItem) error {
	// create directory for the item
	gadgetItemPathArr := append([]string{kvmGadgetPath}, item.path...)
	gadgetItemPath := filepath.Join(gadgetItemPathArr...)
	err := os.MkdirAll(gadgetItemPath, 0755)
	if err != nil {
		return fmt.Errorf("failed to create path %s: %w", gadgetItemPath, err)
	}

	if len(item.configAttrs) > 0 {
		configItemPathArr := append([]string{configC1Path}, item.path...)
		configItemPath := filepath.Join(configItemPathArr...)
		err = os.MkdirAll(configItemPath, 0755)
		if err != nil {
			return fmt.Errorf("failed to create path %s: %w", config, err)
		}

		err = writeGadgetAttrs(configItemPath, item.configAttrs)
		if err != nil {
			return fmt.Errorf("failed to write config attributes for %s: %w", configItemPath, err)
		}
	}

	if len(item.attrs) > 0 {
		// write attributes for the item
		err = writeGadgetAttrs(gadgetItemPath, item.attrs)
		if err != nil {
			return fmt.Errorf("failed to write attributes for %s: %w", gadgetItemPath, err)
		}
	}

	// write report descriptor if available
	if item.reportDesc != nil {
		err = writeIfDifferent(path.Join(gadgetItemPath, "report_desc"), item.reportDesc, 0644)
		if err != nil {
			return err
		}
	}

	// create symlink if configPath is set
	if item.configPath != "" {
		logger.Tracef("Creating symlink from %s to %s", item.configPath, gadgetItemPath)

		// check if the symlink already exists, if yes, check if it points to the correct path
		if _, err := os.Lstat(item.configPath); err == nil {
			linkPath, err := os.Readlink(item.configPath)
			if err != nil || linkPath != gadgetItemPath {
				err = os.Remove(item.configPath)
				if err != nil {
					return fmt.Errorf("failed to remove existing symlink %s: %w", item.configPath, err)
				}
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("failed to check if symlink exists: %w", err)
		}

		err = os.Symlink(gadgetItemPath, item.configPath)
		if err != nil {
			return fmt.Errorf("failed to create symlink from %s to %s: %w", item.configPath, gadgetItemPath, err)
		}
	}

	return nil
}

func init() {
	ensureConfigLoaded()

	_ = os.MkdirAll(imagesFolder, 0755)
	udcs := gadget.GetUdcs()
	if len(udcs) < 1 {
		usbLogger.Error("no udc found, skipping USB stack init")
		return
	}
	udc = udcs[0]
	_, err := os.Stat(kvmGadgetPath)
	if err == nil {
		logger.Info("usb gadget already exists")
	}
	err = mountConfigFS()
	if err != nil {
		logger.Errorf("failed to mount configfs: %v, usb stack might not function properly", err)
	}

	loadGadgetConfigFromUsbConfig()

	err = writeGadgetConfig()
	if err != nil {
		logger.Errorf("failed to start gadget: %v", err)
	}

	//TODO: read hid reports(capslock, numlock, etc) from keyboardHidFile
}

func loadGadgetConfigFromUsbConfig() {
	gadgetConfig["base"].attrs["idVendor"] = config.UsbConfig.VendorId
	gadgetConfig["base"].attrs["idProduct"] = config.UsbConfig.ProductId

	gadgetConfig["base_info"].attrs["serialnumber"] = config.UsbConfig.SerialNumber
	gadgetConfig["base_info"].attrs["manufacturer"] = config.UsbConfig.Manufacturer
	gadgetConfig["base_info"].attrs["product"] = config.UsbConfig.Product
}

func UpdateGadgetConfig() error {
	loadGadgetConfigFromUsbConfig()
	err := writeGadgetConfig()
	if err != nil {
		logger.Errorf("failed to update gadget: %v", err)
	}

	err = rebindUsb(false)
	if err != nil {
		return err
	}

	return nil
}

func writeGadgetAttrs(basePath string, attrs gadgetAttributes) error {
	for key, val := range attrs {
		filePath := filepath.Join(basePath, key)
		err := writeIfDifferent(filePath, []byte(val), 0644)
		if err != nil {
			return fmt.Errorf("failed to write to %s: %w", filePath, err)
		}
	}
	return nil
}

func writeGadgetConfig() error {
	if _, err := os.Stat(gadgetPath); os.IsNotExist(err) {
		return fmt.Errorf("USB gadget path does not exist: %s", gadgetPath)
	}

	err := os.MkdirAll(kvmGadgetPath, 0755)
	if err != nil {
		return err
	}

	logger.Tracef("writing gadget config")
	for key, item := range gadgetConfig {
		// check if the item is enabled in the config
		if !config.UsbDevices.isGadgetConfigItemEnabled(key) {
			logger.Tracef("disabling gadget config: %s", key)
			err = disableGadgetItemConfig(item)
			if err != nil {
				return err
			}
			continue
		}
		logger.Tracef("writing gadget config: %s", key)
		err = writeGadgetItemConfig(item)
		if err != nil {
			return err
		}
	}

	logger.Tracef("writing UDC")
	err = os.WriteFile(path.Join(kvmGadgetPath, "UDC"), []byte(udc), 0644)
	if err != nil {
		return err
	}

	err = rebindUsb(true)
	if err != nil {
		logger.Infof("failed to rebind usb: %v", err)
	}

	return nil
}

func rebindUsb(ignoreUnbindError bool) error {
	err := os.WriteFile("/sys/bus/platform/drivers/dwc3/unbind", []byte(udc), 0644)
	if err != nil && !ignoreUnbindError {
		return err
	}
	err = os.WriteFile("/sys/bus/platform/drivers/dwc3/bind", []byte(udc), 0644)
	if err != nil {
		return err
	}
	return nil
}

var (
	keyboardHidFile *os.File
	keyboardLock    = sync.Mutex{}
	mouseHidFile    *os.File
	mouseLock       = sync.Mutex{}
	relMouseHidFile *os.File
	relMouseLock    = sync.Mutex{}
)

func rpcKeyboardReport(modifier uint8, keys []uint8) error {
	keyboardLock.Lock()
	defer keyboardLock.Unlock()
	if keyboardHidFile == nil {
		var err error
		keyboardHidFile, err = os.OpenFile("/dev/hidg0", os.O_RDWR, 0666)
		if err != nil {
			return fmt.Errorf("failed to open hidg0: %w", err)
		}
	}
	if len(keys) > 6 {
		keys = keys[:6]
	}
	if len(keys) < 6 {
		keys = append(keys, make([]uint8, 6-len(keys))...)
	}
	_, err := keyboardHidFile.Write([]byte{modifier, 0, keys[0], keys[1], keys[2], keys[3], keys[4], keys[5]})
	if err != nil {
		keyboardHidFile.Close()
		keyboardHidFile = nil
		return err
	}
	resetUserInputTime()
	return err
}

func rpcAbsMouseReport(x, y int, buttons uint8) error {
	mouseLock.Lock()
	defer mouseLock.Unlock()
	if mouseHidFile == nil {
		var err error
		mouseHidFile, err = os.OpenFile("/dev/hidg1", os.O_RDWR, 0666)
		if err != nil {
			return fmt.Errorf("failed to open hidg1: %w", err)
		}
	}
	resetUserInputTime()
	_, err := mouseHidFile.Write([]byte{
		1,             // Report ID 1
		buttons,       // Buttons
		uint8(x),      // X Low Byte
		uint8(x >> 8), // X High Byte
		uint8(y),      // Y Low Byte
		uint8(y >> 8), // Y High Byte
	})
	if err != nil {
		mouseHidFile.Close()
		mouseHidFile = nil
		return err
	}
	return nil
}

var accumulatedWheelY float64 = 0

func rpcWheelReport(wheelY int8) error {
	if mouseHidFile == nil {
		return errors.New("hid not initialized")
	}

	// Accumulate the wheelY value
	accumulatedWheelY += float64(wheelY) / 8.0

	// Only send a report if the accumulated value is significant
	if abs(accumulatedWheelY) >= 1.0 {
		scaledWheelY := int8(accumulatedWheelY)

		_, err := mouseHidFile.Write([]byte{
			2,                  // Report ID 2
			byte(scaledWheelY), // Scaled Wheel Y (signed)
		})

		// Reset the accumulator, keeping any remainder
		accumulatedWheelY -= float64(scaledWheelY)

		resetUserInputTime()
		return err
	}

	return nil
}

// Helper function to get absolute value of float64
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func rpcRelMouseReport(mx, my int8, buttons uint8) error {
	relMouseLock.Lock()
	defer relMouseLock.Unlock()
	if relMouseHidFile == nil {
		var err error
		relMouseHidFile, err = os.OpenFile("/dev/hidg2", os.O_RDWR, 0666)
		if err != nil {
			return fmt.Errorf("failed to open hidg2: %w", err)
		}
	}
	resetUserInputTime()
	_, err := relMouseHidFile.Write([]byte{
		buttons,   // Buttons
		uint8(mx), // X
		uint8(my), // Y
		0,         // Wheel
	})
	if err != nil {
		logger.Errorf("failed to write to hidg2: %v", err)
		relMouseHidFile.Close()
		relMouseHidFile = nil
		return err
	}
	return nil
}

var usbState = "unknown"

func rpcGetUSBState() (state string) {
	stateBytes, err := os.ReadFile("/sys/class/udc/ffb00000.usb/state")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(stateBytes))
}

func triggerUSBStateUpdate() {
	go func() {
		if currentSession == nil {
			log.Println("No active RPC session, skipping update state update")
			return
		}
		writeJSONRPCEvent("usbState", usbState, currentSession)
	}()
}

var udc string

func init() {
	ensureConfigLoaded()

	go func() {
		for {
			newState := rpcGetUSBState()
			if newState != usbState {
				log.Printf("USB state changed from %s to %s", usbState, newState)
				usbState = newState
				requestDisplayUpdate()
				triggerUSBStateUpdate()
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
}

// Source: https://www.kernel.org/doc/Documentation/usb/gadget_hid.txt
var KeyboardReportDesc = []byte{
	0x05, 0x01, /* USAGE_PAGE (Generic Desktop)	          */
	0x09, 0x06, /* USAGE (Keyboard)                       */
	0xa1, 0x01, /* COLLECTION (Application)               */
	0x05, 0x07, /*   USAGE_PAGE (Keyboard)                */
	0x19, 0xe0, /*   USAGE_MINIMUM (Keyboard LeftControl) */
	0x29, 0xe7, /*   USAGE_MAXIMUM (Keyboard Right GUI)   */
	0x15, 0x00, /*   LOGICAL_MINIMUM (0)                  */
	0x25, 0x01, /*   LOGICAL_MAXIMUM (1)                  */
	0x75, 0x01, /*   REPORT_SIZE (1)                      */
	0x95, 0x08, /*   REPORT_COUNT (8)                     */
	0x81, 0x02, /*   INPUT (Data,Var,Abs)                 */
	0x95, 0x01, /*   REPORT_COUNT (1)                     */
	0x75, 0x08, /*   REPORT_SIZE (8)                      */
	0x81, 0x03, /*   INPUT (Cnst,Var,Abs)                 */
	0x95, 0x05, /*   REPORT_COUNT (5)                     */
	0x75, 0x01, /*   REPORT_SIZE (1)                      */
	0x05, 0x08, /*   USAGE_PAGE (LEDs)                    */
	0x19, 0x01, /*   USAGE_MINIMUM (Num Lock)             */
	0x29, 0x05, /*   USAGE_MAXIMUM (Kana)                 */
	0x91, 0x02, /*   OUTPUT (Data,Var,Abs)                */
	0x95, 0x01, /*   REPORT_COUNT (1)                     */
	0x75, 0x03, /*   REPORT_SIZE (3)                      */
	0x91, 0x03, /*   OUTPUT (Cnst,Var,Abs)                */
	0x95, 0x06, /*   REPORT_COUNT (6)                     */
	0x75, 0x08, /*   REPORT_SIZE (8)                      */
	0x15, 0x00, /*   LOGICAL_MINIMUM (0)                  */
	0x25, 0x65, /*   LOGICAL_MAXIMUM (101)                */
	0x05, 0x07, /*   USAGE_PAGE (Keyboard)                */
	0x19, 0x00, /*   USAGE_MINIMUM (Reserved)             */
	0x29, 0x65, /*   USAGE_MAXIMUM (Keyboard Application) */
	0x81, 0x00, /*   INPUT (Data,Ary,Abs)                 */
	0xc0, /* END_COLLECTION                         */
}

// Combined absolute and relative mouse report descriptor with report ID
var CombinedAbsoluteMouseReportDesc = []byte{
	0x05, 0x01, // Usage Page (Generic Desktop Ctrls)
	0x09, 0x02, // Usage (Mouse)
	0xA1, 0x01, // Collection (Application)

	// Report ID 1: Absolute Mouse Movement
	0x85, 0x01, //     Report ID (1)
	0x09, 0x01, //     Usage (Pointer)
	0xA1, 0x00, //     Collection (Physical)
	0x05, 0x09, //         Usage Page (Button)
	0x19, 0x01, //         Usage Minimum (0x01)
	0x29, 0x03, //         Usage Maximum (0x03)
	0x15, 0x00, //         Logical Minimum (0)
	0x25, 0x01, //         Logical Maximum (1)
	0x75, 0x01, //         Report Size (1)
	0x95, 0x03, //         Report Count (3)
	0x81, 0x02, //         Input (Data, Var, Abs)
	0x95, 0x01, //         Report Count (1)
	0x75, 0x05, //         Report Size (5)
	0x81, 0x03, //         Input (Cnst, Var, Abs)
	0x05, 0x01, //         Usage Page (Generic Desktop Ctrls)
	0x09, 0x30, //         Usage (X)
	0x09, 0x31, //         Usage (Y)
	0x16, 0x00, 0x00, //         Logical Minimum (0)
	0x26, 0xFF, 0x7F, //         Logical Maximum (32767)
	0x36, 0x00, 0x00, //         Physical Minimum (0)
	0x46, 0xFF, 0x7F, //         Physical Maximum (32767)
	0x75, 0x10, //         Report Size (16)
	0x95, 0x02, //         Report Count (2)
	0x81, 0x02, //         Input (Data, Var, Abs)
	0xC0, //     End Collection

	// Report ID 2: Relative Wheel Movement
	0x85, 0x02, //     Report ID (2)
	0x09, 0x38, //     Usage (Wheel)
	0x15, 0x81, //     Logical Minimum (-127)
	0x25, 0x7F, //     Logical Maximum (127)
	0x75, 0x08, //     Report Size (8)
	0x95, 0x01, //     Report Count (1)
	0x81, 0x06, //     Input (Data, Var, Rel)

	0xC0, // End Collection
}

var CombinedRelativeMouseReportDesc = []byte{
	// from: https://github.com/NicoHood/HID/blob/b16be57caef4295c6cd382a7e4c64db5073647f7/src/SingleReport/BootMouse.cpp#L26
	0x05, 0x01, // USAGE_PAGE (Generic Desktop)	  54
	0x09, 0x02, // USAGE (Mouse)
	0xa1, 0x01, // COLLECTION (Application)

	// Pointer and Physical are required by Apple Recovery
	0x09, 0x01, // USAGE (Pointer)
	0xa1, 0x00, // COLLECTION (Physical)

	// 8 Buttons
	0x05, 0x09, // USAGE_PAGE (Button)
	0x19, 0x01, // USAGE_MINIMUM (Button 1)
	0x29, 0x08, // USAGE_MAXIMUM (Button 8)
	0x15, 0x00, // LOGICAL_MINIMUM (0)
	0x25, 0x01, // LOGICAL_MAXIMUM (1)
	0x95, 0x08, // REPORT_COUNT (8)
	0x75, 0x01, // REPORT_SIZE (1)
	0x81, 0x02, // INPUT (Data,Var,Abs)

	// X, Y, Wheel
	0x05, 0x01, // USAGE_PAGE (Generic Desktop)
	0x09, 0x30, // USAGE (X)
	0x09, 0x31, // USAGE (Y)
	0x09, 0x38, // USAGE (Wheel)
	0x15, 0x81, // LOGICAL_MINIMUM (-127)
	0x25, 0x7f, // LOGICAL_MAXIMUM (127)
	0x75, 0x08, // REPORT_SIZE (8)
	0x95, 0x03, // REPORT_COUNT (3)
	0x81, 0x06, // INPUT (Data,Var,Rel)

	// End
	0xc0, //       End Collection (Physical)
	0xc0, //       End Collection
}
