package tailscale

import (
	"errors"
	"fmt"
	"net"
	"net/netip"

	"github.com/vishvananda/netlink"
	"tailscale.com/net/tsaddr"
	"tailscale.com/net/tstun"
)

const tunName = "tailscale0"

func OpenTUN(logf func(format string, args ...any)) (*Link, error) {
	dev, name, err := tstun.New(logf, tunName)
	if err != nil {
		return nil, err
	}

	nl, err := netlink.LinkByName(name)
	if err == nil {
		err = netlink.LinkSetUp(nl)
	}
	if err != nil {
		_ = dev.Close()
		return nil, fmt.Errorf("failed to bring up %s: %w", name, err)
	}

	return &Link{
		Device: dev,
		SetAddrs: func(addrs []netip.Addr) error {
			return syncLink(nl, addrs)
		},
	}, nil
}

func syncLink(nl netlink.Link, addrs []netip.Addr) error {
	current, err := netlink.AddrList(nl, netlink.FAMILY_ALL)
	if err != nil {
		return err
	}

	var errs []error
	for _, addr := range current {
		ip, ok := netip.AddrFromSlice(addr.IP)
		if !ok || ip.IsLinkLocalUnicast() || containsAddr(addrs, ip.Unmap()) {
			continue
		}
		if err := netlink.AddrDel(nl, &addr); err != nil {
			errs = append(errs, fmt.Errorf("remove %s: %w", ip, err))
		}
	}

	for _, ip := range addrs {
		if err := netlink.AddrReplace(nl, &netlink.Addr{IPNet: ipNet(netip.PrefixFrom(ip, ip.BitLen()))}); err != nil {
			errs = append(errs, fmt.Errorf("add %s: %w", ip, err))
		}
	}

	for _, prefix := range TailnetRoutes(addrs) {
		route := &netlink.Route{LinkIndex: nl.Attrs().Index, Dst: ipNet(prefix), Scope: netlink.SCOPE_LINK}
		if err := netlink.RouteReplace(route); err != nil {
			errs = append(errs, fmt.Errorf("route %s: %w", prefix, err))
		}
	}

	return errors.Join(errs...)
}

func TailnetRoutes(addrs []netip.Addr) []netip.Prefix {
	var routes []netip.Prefix
	for _, prefix := range []netip.Prefix{tsaddr.CGNATRange(), tsaddr.TailscaleULARange()} {
		for _, ip := range addrs {
			if ip.Is4() == prefix.Addr().Is4() {
				routes = append(routes, prefix)
				break
			}
		}
	}
	return routes
}

func containsAddr(addrs []netip.Addr, ip netip.Addr) bool {
	for _, addr := range addrs {
		if addr == ip {
			return true
		}
	}
	return false
}

func ipNet(prefix netip.Prefix) *net.IPNet {
	return &net.IPNet{
		IP:   prefix.Addr().AsSlice(),
		Mask: net.CIDRMask(prefix.Bits(), prefix.Addr().BitLen()),
	}
}
