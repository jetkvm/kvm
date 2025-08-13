package dhclient

import (
	"log"
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/dhcpv6/nclient6"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// isIPv6LinkReady returns true if the interface has a link-local address
// which is not tentative.
func isIPv6LinkReady(l netlink.Link) (bool, error) {
	addrs, err := netlink.AddrList(l, netlink.FAMILY_V6)
	if err != nil {
		return false, err
	}
	for _, addr := range addrs {
		if addr.IP.IsLinkLocalUnicast() && (addr.Flags&unix.IFA_F_TENTATIVE == 0) {
			if addr.Flags&unix.IFA_F_DADFAILED != 0 {
				log.Printf("DADFAILED for %v, continuing anyhow", addr.IP)
			}
			return true, nil
		}
	}
	return false, nil
}

// isIPv6RouteReady returns true if serverAddr is reachable.
func isIPv6RouteReady(serverAddr net.IP) waitForCondition {
	return func(l netlink.Link) (bool, error) {
		if serverAddr.IsMulticast() {
			return true, nil
		}

		routes, err := netlink.RouteList(l, netlink.FAMILY_V6)
		if err != nil {
			return false, err
		}
		for _, route := range routes {
			if route.LinkIndex != l.Attrs().Index {
				continue
			}
			// Default route.
			if route.Dst == nil {
				return true, nil
			}
			if route.Dst.Contains(serverAddr) {
				return true, nil
			}
		}
		return false, nil
	}
}

func (c *Client) requestLease6(iface netlink.Link) (*Lease, error) {
	ifname := iface.Attrs().Name
	l := c.l.With().Str("interface", ifname).Logger()

	clientPort := dhcpv6.DefaultClientPort
	if c.cfg.V6ClientPort != nil {
		clientPort = *c.cfg.V6ClientPort
	}

	// For ipv6, we cannot bind to the port until Duplicate Address
	// Detection (DAD) is complete which is indicated by the link being no
	// longer marked as "tentative". This usually takes about a second.

	// If the link is never going to be ready, don't wait forever.
	// (The user may not have configured a ctx with a timeout.)

	linkUpTimeout := time.After(c.cfg.LinkUpTimeout)
	if err := c.waitFor(
		iface,
		linkUpTimeout,
		isIPv6LinkReady,
		ErrIPv6LinkTimeout,
	); err != nil {
		return nil, err
	}

	// If user specified a non-multicast address, make sure it's routable before we start.
	if c.cfg.V6ServerAddr != nil {
		if err := c.waitFor(
			iface,
			linkUpTimeout,
			isIPv6RouteReady(c.cfg.V6ServerAddr.IP),
			ErrIPv6RouteTimeout,
		); err != nil {
			return nil, err
		}
	}

	mods := []nclient6.ClientOpt{
		nclient6.WithTimeout(c.cfg.Timeout),
		nclient6.WithRetry(c.cfg.Retries),
		c.getDHCP6Logger(),
	}
	if c.cfg.V6ServerAddr != nil {
		mods = append(mods, nclient6.WithBroadcastAddr(c.cfg.V6ServerAddr))
	}

	conn, err := nclient6.NewIPv6UDPConn(iface.Attrs().Name, clientPort)
	if err != nil {
		return nil, err
	}

	client, err := nclient6.NewWithConn(conn, iface.Attrs().HardwareAddr, mods...)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	// Prepend modifiers with default options, so they can be overriden.
	reqmods := append(
		[]dhcpv6.Modifier{
			dhcpv6.WithNetboot,
		},
		c.cfg.Modifiers6...)

	l.Info().Msg("attempting to get DHCPv6 lease")
	p, err := client.RapidSolicit(c.ctx, reqmods...)
	if err != nil {
		return nil, err
	}

	l.Info().Msgf("DHCPv6 lease acquired: %s", p.Summary())
	return fromNclient6Lease(p, ifname), nil
}
