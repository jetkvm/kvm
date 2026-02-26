package diagnostics

import (
	"os"
	"path/filepath"
)

// LogInputDevices logs input device information.
func (d *Diagnostics) LogInputDevices() {
	d.logger.Info().Msg("--- Input Devices ---")

	// List /dev/input/
	d.runCmdLog("ls /dev/input", "ls", "-la", "/dev/input/")

	// /proc/bus/input/devices
	d.readFileLog("/proc/bus/input/devices", "/proc/bus/input/devices")
}

// LogI2CInfo logs I2C bus scans and touchscreen/HDMI sysfs info.
func (d *Diagnostics) LogI2CInfo() {
	d.logger.Info().Msg("--- I2C Info ---")

	// I2C bus 3 scan (touchscreen)
	d.runCmdLog("i2cdetect bus 3", "i2cdetect", "-y", "3")

	// I2C bus 4 scan (HDMI)
	d.runCmdLog("i2cdetect bus 4", "i2cdetect", "-y", "4")

	// FT5x06 raw reads at common addresses
	for _, addr := range []string{"0x15", "0x38", "0x39"} {
		d.runCmdLog("i2cget "+addr, "i2cget", "-y", "3", addr, "0xa3")
	}

	// Touchscreen sysfs
	d.listDirLog("I2C devices (3-00*)", "/sys/bus/i2c/devices")

	// Check specific touchscreen addresses
	for _, addr := range []string{"3-0015", "3-0038"} {
		devPath := filepath.Join("/sys/bus/i2c/devices", addr)
		if _, err := os.Stat(devPath); err == nil {
			d.listDirLog("sysfs "+addr, devPath)
			d.readFileLog(addr+" name", filepath.Join(devPath, "name"))
		}
	}

	// HDMI capture chip sleep_mode
	sleepModePath := "/sys/devices/platform/ff470000.i2c/i2c-4/4-000f/sleep_mode"
	d.readFileLog("HDMI sleep_mode", sleepModePath)
}

// LogDeviceFiles checks core device files exist.
func (d *Diagnostics) LogDeviceFiles() {
	d.logger.Info().Msg("--- Device Files ---")

	deviceFiles := []string{
		"/dev/fb0",
		"/dev/video0",
		"/dev/v4l-subdev2",
		"/dev/input/event0",
		"/dev/input/event1",
	}

	for _, path := range deviceFiles {
		d.checkFileLog(filepath.Base(path), path)
	}
}
