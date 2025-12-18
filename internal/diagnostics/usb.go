package diagnostics

import (
	"os"
	"path/filepath"
	"strings"
)

const gadgetPath = "/sys/kernel/config/usb_gadget/jetkvm"

// LogUSBGadget logs comprehensive USB gadget state, controller info, and HID devices.
func (d *Diagnostics) LogUSBGadget() {
	d.logger.Info().Msg("--- USB Gadget ---")

	// === DWC3 Controller Info ===
	d.runShellLog("DWC3 uevent", "cat /sys/bus/platform/drivers/dwc3/*/uevent 2>&1")
	d.runShellLog("DWC3 driver dir", "ls -la /sys/bus/platform/drivers/dwc3/ 2>&1")
	d.runShellLog("DWC3 runtime status", "cat /sys/bus/platform/drivers/dwc3/*/power/runtime_status 2>&1")

	// === USB PHY Status ===
	d.runShellLog("USB PHY devices", "find /sys/devices -name '*phy*' 2>/dev/null | grep -i usb || echo 'none found'")
	d.runShellLog("USB PHY uevent", "cat /sys/devices/platform/*/phy/*/uevent 2>&1 || echo 'no phy uevent'")

	// === USB OTG/Role Status ===
	d.runShellLog("USB role", "cat /sys/class/usb_role/*/role 2>&1 || echo 'no usb_role'")
	d.runShellLog("OTG devices", "find /sys -name '*otg*' 2>/dev/null | head -20 || echo 'none found'")

	// === Detailed UDC Info ===
	d.runShellLog("UDC device uevent", "cat /sys/class/udc/*/device/uevent 2>&1")
	d.runShellLog("UDC uevent", "cat /sys/class/udc/*/uevent 2>&1")
	d.runShellLog("UDC maximum speed", "cat /sys/class/udc/*/maximum_speed 2>&1")
	d.runShellLog("UDC current speed", "cat /sys/class/udc/*/current_speed 2>&1")

	// List UDCs from platform
	d.listDirLog("UDC list", "/sys/devices/platform/usbdrd")

	// Find the UDC name and read its state
	files, err := os.ReadDir("/sys/devices/platform/usbdrd")
	if err == nil {
		for _, file := range files {
			if file.IsDir() && strings.HasSuffix(file.Name(), ".usb") {
				udcName := file.Name()
				statePath := filepath.Join("/sys/class/udc", udcName, "state")
				d.readFileLog("UDC state ("+udcName+")", statePath)
			}
		}
	}

	// === USB Power Management ===
	d.runShellLog("USB power control", "cat /sys/devices/platform/*/usb*/power/control 2>&1 || echo 'no power control'")
	d.runShellLog("USB power runtime status", "cat /sys/devices/platform/*/usb*/power/runtime_status 2>&1 || echo 'no runtime status'")

	// === USB Gadget Detailed Config ===
	d.readFileLog("gadget UDC binding", filepath.Join(gadgetPath, "UDC"))
	d.readFileLog("gadget bDeviceClass", filepath.Join(gadgetPath, "bDeviceClass"))
	d.readFileLog("gadget bDeviceProtocol", filepath.Join(gadgetPath, "bDeviceProtocol"))
	d.readFileLog("gadget bDeviceSubClass", filepath.Join(gadgetPath, "bDeviceSubClass"))
	d.readFileLog("gadget bMaxPacketSize0", filepath.Join(gadgetPath, "bMaxPacketSize0"))
	d.readFileLog("gadget idVendor", filepath.Join(gadgetPath, "idVendor"))
	d.readFileLog("gadget idProduct", filepath.Join(gadgetPath, "idProduct"))

	// Gadget config directory
	d.listDirLog("gadget config", gadgetPath)

	// === USB Gadget Functions Detail ===
	d.runShellLog("gadget functions", "ls -la "+gadgetPath+"/functions/*/ 2>&1 || echo 'no functions'")
	d.listDirLog("gadget functions list", filepath.Join(gadgetPath, "functions"))

	// HID function details
	hidFunctions := []string{"hid.usb0", "hid.usb1", "hid.usb2"}
	for _, hid := range hidFunctions {
		funcPath := filepath.Join(gadgetPath, "functions", hid)
		if _, err := os.Stat(funcPath); err == nil {
			d.readFileLog("HID "+hid+" protocol", filepath.Join(funcPath, "protocol"))
			d.readFileLog("HID "+hid+" subclass", filepath.Join(funcPath, "subclass"))
			d.readFileLog("HID "+hid+" report_length", filepath.Join(funcPath, "report_length"))
		}
	}

	// Config links
	d.runShellLog("gadget config c.1", "ls -la "+gadgetPath+"/configs/c.1/ 2>&1")
	d.runShellLog("gadget config links", "readlink "+gadgetPath+"/configs/c.1/* 2>&1 || echo 'no links'")

	// === Module/Driver Info ===
	d.runShellLog("USB modules", "lsmod 2>&1 | grep -iE 'usb|dwc3|gadget' || echo 'no usb modules'")

	// === USB Interrupts and IO Memory ===
	d.runShellLog("USB interrupts", "cat /proc/interrupts 2>&1 | grep -iE 'usb|dwc3' || echo 'no usb interrupts'")
	d.runShellLog("USB iomem", "cat /proc/iomem 2>&1 | grep -iE 'usb|dwc3' || echo 'no usb iomem'")

	// === Extcon (USB Connector) Status ===
	d.runShellLog("extcon list", "ls -la /sys/class/extcon/ 2>&1 || echo 'no extcon'")
	d.runShellLog("extcon state", "cat /sys/class/extcon/*/state 2>&1 || echo 'no extcon state'")
	d.runShellLog("extcon name", "cat /sys/class/extcon/*/name 2>&1 || echo 'no extcon name'")

	// === USB Character Devices ===
	hidDevices := []string{"/dev/hidg0", "/dev/hidg1", "/dev/hidg2"}
	for _, path := range hidDevices {
		d.checkFileLog(filepath.Base(path), path)
	}
	d.runShellLog("hidraw devices", "ls -la /dev/hidraw* 2>&1 || echo 'no hidraw devices'")

	// === Mass Storage (if present) ===
	massStoragePath := filepath.Join(gadgetPath, "functions/mass_storage.usb0/lun.0")
	if _, err := os.Stat(massStoragePath); err == nil {
		d.readFileLog("mass_storage file", filepath.Join(massStoragePath, "file"))
		d.readFileLog("mass_storage ro", filepath.Join(massStoragePath, "ro"))
		d.readFileLog("mass_storage removable", filepath.Join(massStoragePath, "removable"))
		d.readFileLog("mass_storage cdrom", filepath.Join(massStoragePath, "cdrom"))
	}
}
