package diagnostics

// LogNetworking logs network interface state and configuration.
func (d *Diagnostics) LogNetworking() {
	d.logger.Info().Msg("--- Networking ---")

	// Interface state via ip addr
	d.runCmdLog("ip addr show eth0", "ip", "addr", "show", "eth0")

	// Routing table
	d.runCmdLog("ip route", "ip", "route")

	// DNS config
	d.readFileLog("resolv.conf", "/etc/resolv.conf")

	// Link state from sysfs
	d.readFileLog("eth0 operstate", "/sys/class/net/eth0/operstate")

	// MAC address from sysfs
	d.readFileLog("eth0 address", "/sys/class/net/eth0/address")
}
