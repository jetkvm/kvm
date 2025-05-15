package kvm

import (
	"fmt"
	"os/exec"

	"github.com/jetkvm/kvm/internal/usbgadget"
)

func initUsbEthernet(gadget *usbgadget.UsbGadget) error {
	if !gadget.UsbEthernetEnabled() {
		return nil
	}

	iface := gadget.UsbEthernetDevice()
	ipv4addr := networkState.UsbNetworkConfig().IPv4Addr

	scopedLogger := usbLogger.With().Str("iface", iface).Str("ipv4addr", ipv4addr).Logger()
	scopedLogger.Info().Msg("enabling USB Ethernet")

	if err := exec.Command("ip", "addr", "add", ipv4addr, "dev", iface).Run(); err != nil {
		return fmt.Errorf("failed to add ip addr: %w", err)
	}

	if err := exec.Command("ip", "link", "set", "dev", iface, "up").Run(); err != nil {
		return fmt.Errorf("failed to set ip link: %w", err)
	}

	return nil
}
