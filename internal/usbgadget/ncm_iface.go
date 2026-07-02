package usbgadget

import (
	"errors"
	"fmt"
	"os"

	"github.com/vishvananda/netlink"
)

const ncmInterfaceName = "usb0"

// bringUpNcmInterface brings usb0 up. IPv6 link-local (fe80::/10) is
// auto-assigned by the kernel from the dev_addr MAC via modified EUI-64.
// Returns nil (not an error) if the netdev doesn't exist yet — the kernel
// creates it asynchronously after UDC bind, so the caller may retry.
func (u *UsbGadget) bringUpNcmInterface() error {
	link, err := netlink.LinkByName(ncmInterfaceName)
	if err != nil {
		var lnf netlink.LinkNotFoundError
		if errors.As(err, &lnf) {
			return nil
		}
		return fmt.Errorf("lookup %s: %w", ncmInterfaceName, err)
	}

	// Guard against a sysctl override leaving IPv6 disabled on this interface;
	// the IPv6 link-local fe80:: is our only reachability path.
	_ = os.WriteFile("/proc/sys/net/ipv6/conf/"+ncmInterfaceName+"/disable_ipv6", []byte("0"), 0644)

	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("link up %s: %w", ncmInterfaceName, err)
	}

	// Install the host-isolation firewall before returning success. Fail closed:
	// if the firewall can't be installed, usb0 must not be exposed.
	if err := u.applyNcmFirewall(); err != nil {
		// Roll back the link so we don't leak an unfiltered interface.
		_ = netlink.LinkSetDown(link)
		return fmt.Errorf("apply NCM firewall: %w", err)
	}
	return nil
}

// tearDownNcmInterface removes the firewall and brings usb0 down before the
// gadget rebind drops the netdev. Both steps are best-effort.
func (u *UsbGadget) tearDownNcmInterface() {
	u.removeNcmFirewall()
	link, err := netlink.LinkByName(ncmInterfaceName)
	if err != nil {
		return
	}
	_ = netlink.LinkSetDown(link)
}
