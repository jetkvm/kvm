package diagnostics

// LogKernelInfo logs kernel modules, GPIO state, and filtered dmesg.
func (d *Diagnostics) LogKernelInfo() {
	d.logger.Info().Msg("--- Kernel Info ---")

	// Loaded modules (filtered for relevant ones)
	d.runShellLog("lsmod (filtered)", "lsmod | grep -iE 'edt|ft5|touch|input|evdev|usb|hid|dwc' || true")

	// GPIO state
	d.runShellLog("GPIO state", "cat /sys/kernel/debug/gpio 2>/dev/null | head -50 || true")

	// Touchscreen-related dmesg
	d.runShellLog("dmesg touchscreen", "dmesg | grep -iE 'ft5|edt|touch|3-00|input:' | tail -30 || true")

	// I2C errors in dmesg
	d.runShellLog("dmesg I2C errors", "dmesg | grep -iE 'i2c.*error|i2c.*fail|probe.*fail|NACK|timeout' | tail -30 || true")
}

// LogDmesgTail logs the last 200 lines of dmesg.
func (d *Diagnostics) LogDmesgTail() {
	d.logger.Info().Msg("--- dmesg tail ---")
	d.runShellLog("dmesg tail", "dmesg | tail -200")
}
