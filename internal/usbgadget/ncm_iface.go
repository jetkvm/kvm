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
	return nil
}

// tearDownNcmInterface brings usb0 down before the gadget rebind drops the
// netdev. Silent no-op if the interface is already gone.
func (u *UsbGadget) tearDownNcmInterface() {
	link, err := netlink.LinkByName(ncmInterfaceName)
	if err != nil {
		return
	}
	_ = netlink.LinkSetDown(link)
}
