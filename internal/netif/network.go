package netif

import (
	"fmt"

	"github.com/vishvananda/netlink"
)

func ensureInterfaceIsUp(iface *netlink.Link) error {
	if (*iface).Attrs().OperState == netlink.OperUp {
		return nil
	}

	if err := netlink.LinkSetUp(*iface); err != nil {
		return fmt.Errorf("failed to set interface up: %w", err)
	}

	return nil
}
