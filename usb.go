package kvm

import (
	"errors"
	"fmt"
	gadget "github.com/openstadia/go-usb-gadget"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const configFSPath = "/sys/kernel/config"
const gadgetPath = "/sys/kernel/config/usb_gadget"
const kvmGadgetPath = "/sys/kernel/config/usb_gadget/jetkvm"
const configC1Path = "/sys/kernel/config/usb_gadget/jetkvm/configs/c.1"

func init() {
	_ = os.MkdirAll(imagesFolder, 0755)
	udcs := gadget.GetUdcs()
	if len(udcs) < 1 {
		usbLogger.Error("no udc found, skipping USB stack init")
		return
	}
	udc = udcs[0]
	_, err := os.Stat(kvmGadgetPath)
	if err == nil {
		logger.Info("usb gadget already exists, skipping usb gadget initialization")
		return
	}
	err = mountConfigFS()
	if err != nil {
		logger.Errorf("failed to mount configfs: %v, usb stack might not function properly", err)
	}
	err = writeGadgetConfig()
	if err != nil {
		logger.Errorf("failed to start gadget: %v", err)
	}

	//TODO: read hid reports(capslock, numlock, etc) from keyboardHidFile
}

func UpdateGadgetConfig() error {
	LoadConfig()
	gadgetAttrs := [][]string{
		{"idVendor", config.UsbConfig.UsbVendorId},
		{"idProduct", config.UsbConfig.UsbProductId},
	}
	err := writeGadgetAttrs(kvmGadgetPath, gadgetAttrs)
	if err != nil {
		return err
	}

	log.Printf("Successfully updated usb gadget attributes: %v", gadgetAttrs)

	strAttrs := [][]string{
		{"serialnumber", config.UsbConfig.UsbSerialNumber},
		{"manufacturer", config.UsbConfig.UsbManufacturer},
		{"product", config.UsbConfig.UsbName},
	}
	gadgetStringsPath := filepath.Join(kvmGadgetPath, "strings", "0x409")
	err = os.MkdirAll(gadgetStringsPath, 0755)
	if err != nil {
		return err
	}
	err = writeGadgetAttrs(gadgetStringsPath, strAttrs)
	if err != nil {
		return err
	}

	log.Printf("Successfully updated string attributes: %s", strAttrs)

	err = rebindUsb()
	if err != nil {
		return err
	}

	return nil
}

func mountConfigFS() error {
	logger.Infof("Checking for gadgetPath: %s ...", gadgetPath)
	_, err := os.Stat(gadgetPath)
	if os.IsNotExist(err) {
		logger.Infof("running mount command...")
		err = exec.Command("mount", "-t", "configfs", "none", configFSPath).Run()
		if err != nil {
			return fmt.Errorf("failed to mount configfs: %w", err)
		}
	} else {
		return fmt.Errorf("unable to access usb gadget path: %w", err)
	}
	logger.Infof("Successfully mounted usb gadget at %s", gadgetPath)
	return nil
}

func writeGadgetAttrs(basePath string, attrs [][]string) error {
	for _, item := range attrs {
		filePath := filepath.Join(basePath, item[0])
		err := os.WriteFile(filePath, []byte(item[1]), 0644)
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

	LoadConfig()
	gadgetAttrs := [][]string{
		{"bcdUSB", "0x0200"}, //USB 2.0
		{"idVendor", config.UsbConfig.UsbVendorId},
		{"idProduct", config.UsbConfig.UsbProductId},
		{"bcdDevice", "0100"},
	}
	err = writeGadgetAttrs(kvmGadgetPath, gadgetAttrs)
	if err != nil {
		return err
	}

	logger.Infof("Successfully wrote gadget attributes: %s", gadgetAttrs)
	gadgetStringsPath := filepath.Join(kvmGadgetPath, "strings", "0x409")
	err = os.MkdirAll(gadgetStringsPath, 0755)
	if err != nil {
		return err
	}

	strAttrs := [][]string{
		{"serialnumber", config.UsbConfig.UsbSerialNumber},
		{"manufacturer", config.UsbConfig.UsbManufacturer},
		{"product", config.UsbConfig.UsbName},
	}
	err = writeGadgetAttrs(gadgetStringsPath, strAttrs)
	if err != nil {
		return err
	}
	logger.Infof("Successfully wrote string attributes: %s", strAttrs)

	configC1StringsPath := path.Join(configC1Path, "strings", "0x409")
	err = os.MkdirAll(configC1StringsPath, 0755)
	if err != nil {
		return err
	}

	err = writeGadgetAttrs(configC1Path, [][]string{
		{"MaxPower", "250"}, //in unit of 2mA
	})
	if err != nil {
		return err
	}

	err = writeGadgetAttrs(configC1StringsPath, [][]string{
		{"configuration", "Config 1: HID"},
	})
	if err != nil {
		return err
	}

	//keyboard HID
	hid0Path := path.Join(kvmGadgetPath, "functions", "hid.usb0")
	err = os.MkdirAll(hid0Path, 0755)
	if err != nil {
		return err
	}
	err = writeGadgetAttrs(hid0Path, [][]string{
		{"protocol", "1"},
		{"subclass", "0"},
		{"report_length", "8"},
	})
	if err != nil {
		return err
	}

	err = os.WriteFile(path.Join(hid0Path, "report_desc"), KeyboardReportDesc, 0644)
	if err != nil {
		return err
	}

	//mouse HID
	hid1Path := path.Join(kvmGadgetPath, "functions", "hid.usb1")
	err = os.MkdirAll(hid1Path, 0755)
	if err != nil {
		return err
	}
	err = writeGadgetAttrs(hid1Path, [][]string{
		{"protocol", "2"},
		{"subclass", "0"},
		{"report_length", "6"},
	})
	if err != nil {
		return err
	}

	err = os.WriteFile(path.Join(hid1Path, "report_desc"), CombinedMouseReportDesc, 0644)
	if err != nil {
		return err
	}
	//mass storage
	massStoragePath := path.Join(kvmGadgetPath, "functions", "mass_storage.usb0")
	err = os.MkdirAll(massStoragePath, 0755)
	if err != nil {
		return err
	}

	err = writeGadgetAttrs(massStoragePath, [][]string{
		{"stall", "1"},
	})
	if err != nil {
		return err
	}
	lun0Path := path.Join(massStoragePath, "lun.0")
	err = os.MkdirAll(lun0Path, 0755)
	if err != nil {
		return err
	}
	err = writeGadgetAttrs(lun0Path, [][]string{
		{"cdrom", "1"},
		{"ro", "1"},
		{"removable", "1"},
		{"file", "\n"},
		{"inquiry_string", "JetKVM Virtual Media"},
	})
	if err != nil {
		return err
	}

	err = os.Symlink(hid0Path, path.Join(configC1Path, "hid.usb0"))
	if err != nil {
		return err
	}

	err = os.Symlink(hid1Path, path.Join(configC1Path, "hid.usb1"))
	if err != nil {
		return err
	}

	err = os.Symlink(massStoragePath, path.Join(configC1Path, "mass_storage.usb0"))
	if err != nil {
		return err
	}

	err = os.WriteFile(path.Join(kvmGadgetPath, "UDC"), []byte(udc), 0644)
	if err != nil {
		return err
	}

	return nil
}

func rebindUsb() error {
	unbindErr := unbindUsb()
	if unbindErr != nil {
		return unbindErr
	}
	bindErr := bindUsb()
	if bindErr != nil {
		return bindErr
	}
	return nil
}

func unbindUsb() error {
	err := os.WriteFile("/sys/bus/platform/drivers/dwc3/unbind", []byte(udc), 0644)
	if err != nil {
		return err
	}
	return nil
}

func bindUsb() error {
	err := os.WriteFile("/sys/bus/platform/drivers/dwc3/bind", []byte(udc), 0644)
	if err != nil {
		return err
	}
	return nil
}

var keyboardHidFile *os.File
var keyboardLock = sync.Mutex{}
var mouseHidFile *os.File
var mouseLock = sync.Mutex{}

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
var CombinedMouseReportDesc = []byte{
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
