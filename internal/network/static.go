package network

import (
	"fmt"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
)

const defaultInterface = "eth0"

const (
	AF_INET  = nl.FAMILY_V4
	AF_INET6 = nl.FAMILY_V6

	sysctlBase     = "/proc/sys"
	sysctlFileMode = 0640
)

type NetworkInterfaceConfig struct {
	config *NetworkConfig
	l      *zerolog.Logger

	ifaceName string
	iface     *netlink.Link

	lock     sync.Mutex
	ipv4Lock sync.Mutex
	ipv6Lock sync.Mutex
}

var (
	ipv4DefaultRoute = net.IPNet{
		IP:   net.IPv4zero,
		Mask: net.CIDRMask(0, 0),
	}

	ipv6DefaultRoute = net.IPNet{
		IP:   net.IPv6zero,
		Mask: net.CIDRMask(0, 0),
	}
)

// NewNetworkInterfaceConfig ...
func NewNetworkInterfaceConfig(ifaceName string, config *NetworkConfig, logger *zerolog.Logger) (*NetworkInterfaceConfig, error) {
	if ifaceName == "" {
		ifaceName = defaultInterface
	}

	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get link by name: %w", err)
	}

	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}

	if logger == nil {
		logger = logging.GetSubsystemLogger("network")
	}

	scopedLogger := logger.With().Str("iface", ifaceName).Logger()

	return &NetworkInterfaceConfig{
		config:    config,
		l:         &scopedLogger,
		ifaceName: ifaceName,
		iface:     &link,
		lock:      sync.Mutex{},
		ipv4Lock:  sync.Mutex{},
		ipv6Lock:  sync.Mutex{},
	}, nil
}

func (c *NetworkInterfaceConfig) Apply(s *NetworkInterfaceState) error {
	if err := c.applyIPv4(s); err != nil {
		return err
	}

	if err := c.applyIPv6(s); err != nil {
		return err
	}

	return nil
}

func (c *NetworkInterfaceConfig) applyIPv4(s *NetworkInterfaceState) error {
	switch c.config.IPv4Mode.String {
	case "static":
		return c.applyIPv4Static(s)
	case "dhcp":
		s.dhcpClient.SetIPv4(true)
		return nil
	case "disabled":
		s.dhcpClient.SetIPv4(false)
		return nil
	default:
		return fmt.Errorf("invalid IPv4 mode: %s", c.config.IPv4Mode.String)
	}
}

func (c *NetworkInterfaceConfig) applyIPv6(s *NetworkInterfaceState) error {
	switch c.config.IPv6Mode.String {
	case "static":
		return c.applyIPv6Static(s)
	case "dhcpv6":
		return fmt.Errorf("not implemented")
	case "slaac":
		return c.applyIPv6Slaac()
	case "slaac_and_dhcpv6":
		return fmt.Errorf("not implemented")
	case "link_local":
		return c.applyIPv6LinkLocalOnly()
	case "disabled":
		return c.disableIPv6()
	default:
		return fmt.Errorf("invalid IPv6 mode: %s", c.config.IPv6Mode.String)
	}
}

func checkIfAddressIsSetOrReturnCurrent(iface *netlink.Link, address net.IP, family int) (bool, []*netlink.Addr) {
	addr, err := netlink.AddrList(*iface, family)
	if err != nil {
		return false, nil
	}

	hit := false
	linkAddrs := make([]*netlink.Addr, 0)

	for _, a := range addr {
		if a.IP.Equal(address) {
			hit = true
			continue
		}

		// we don't want to delete link-local addresses
		if family == AF_INET6 && a.IP.IsLinkLocalUnicast() {
			continue
		}

		linkAddrs = append(linkAddrs, &a)
	}

	return hit, linkAddrs
}

func reconcileLinkAddrs(iface *netlink.Link, family int, expectedAddr *net.IPNet, logger *zerolog.Logger) error {
	// TODO: we need to check if the netmask is the same
	hit, currentLinkAddrs := checkIfAddressIsSetOrReturnCurrent(iface, expectedAddr.IP, family)
	if !hit {
		logger.Info().Interface("ip", expectedAddr).Msg("adding address")
		if err := netlink.AddrAdd(*iface, &netlink.Addr{
			IPNet: expectedAddr,
		}); err != nil {
			logger.Info().Interface("ip", expectedAddr).Msg("failed to set address")
			return fmt.Errorf("failed to add address: %w", err)
		}
	}

	for _, addr := range currentLinkAddrs {
		logger.Info().Interface("ip", addr.IP).Msg("deleting address")
		if err := netlink.AddrDel(*iface, addr); err != nil {
			return fmt.Errorf("failed to delete address: %w", err)
		}
	}

	return nil
}

func checkIfDefaultRouteIsSet(gateway net.IP, family int) bool {
	defaultRoute := ipv4DefaultRoute
	if family == AF_INET6 {
		defaultRoute = ipv6DefaultRoute
	}

	routes, err := netlink.RouteListFiltered(family, &netlink.Route{Dst: &defaultRoute}, netlink.RT_FILTER_DST)
	if err != nil {
		return false
	}

	for _, r := range routes {
		if r.Dst.IP.Equal(defaultRoute.IP) && r.Gw.Equal(gateway) {
			return true
		}
	}
	return false
}

func ensureInterfaceIsUp(iface *netlink.Link) error {
	if (*iface).Attrs().OperState == netlink.OperUp {
		return nil
	}

	if err := netlink.LinkSetUp(*iface); err != nil {
		return fmt.Errorf("failed to set interface up: %w", err)
	}

	return nil
}

func (c *NetworkInterfaceConfig) applyIPv4Static(s *NetworkInterfaceState) error {
	c.ipv4Lock.Lock()
	defer c.ipv4Lock.Unlock()

	config, err := parseAndValidateStaticIPv4Config(c.config.IPv4Static)
	if err != nil {
		return err
	}

	if c.iface == nil {
		return fmt.Errorf("interface handle is nil")
	}

	// disable DHCPv4
	s.dhcpClient.SetIPv4(false)

	if err := ensureInterfaceIsUp(c.iface); err != nil {
		return err
	}

	if err := reconcileLinkAddrs(c.iface, AF_INET, &config.network, c.l); err != nil {
		return err
	}

	if !checkIfDefaultRouteIsSet(config.gateway, AF_INET) {
		c.l.Info().Str("iface", c.ifaceName).Interface("gateway", config.gateway).Msg("adding default route")
		// TODO: support point-to-point
		if err := netlink.RouteReplace(&netlink.Route{
			Dst:       &ipv4DefaultRoute,
			Gw:        config.gateway,
			LinkIndex: (*c.iface).Attrs().Index,
		}); err != nil {
			return fmt.Errorf("failed to add default route: %w", err)
		}
	}

	return ensureInterfaceIsUp(c.iface)
}

func (c *NetworkInterfaceConfig) setSysctlValues(values map[string]int) error {
	for name, value := range values {
		name = fmt.Sprintf(name, c.ifaceName)
		name = strings.ReplaceAll(name, ".", "/")

		if err := os.WriteFile(path.Join(sysctlBase, name), []byte(strconv.Itoa(value)), sysctlFileMode); err != nil {
			return fmt.Errorf("failed to set sysctl %s=%d: %w", name, value, err)
		}
	}
	return nil
}

func (c *NetworkInterfaceConfig) applyIPv4Dhcp() error {
	c.ipv4Lock.Lock()
	defer c.ipv4Lock.Unlock()

	return nil
}

func (c *NetworkInterfaceConfig) applyIPv4Disabled() error {
	c.ipv4Lock.Lock()
	defer c.ipv4Lock.Unlock()

	addr, err := netlink.AddrList(*c.iface, AF_INET)
	if err != nil {
		return fmt.Errorf("failed to get address list: %w", err)
	}

	for _, a := range addr {
		netlink.AddrDel(*c.iface, &a)
	}

	return nil
}

func (c *NetworkInterfaceConfig) applyIPv6Slaac() error {
	c.ipv6Lock.Lock()
	defer c.ipv6Lock.Unlock()

	if err := c.setSysctlValues(map[string]int{
		"net.ipv6.conf.%s.disable_ipv6": 0, // enable IPv6
		"net.ipv6.conf.%s.accept_ra":    2, // accept even if forwarding is disabled
	}); err != nil {
		return err
	}

	return nil
}

func (c *NetworkInterfaceConfig) applyIPv6LinkLocalOnly() error {
	c.ipv6Lock.Lock()
	defer c.ipv6Lock.Unlock()

	if err := c.setSysctlValues(map[string]int{
		"net.ipv6.conf.%s.disable_ipv6": 0, // enable IPv6
		"net.ipv6.conf.%s.accept_ra":    0, // disable RA
	}); err != nil {
		return err
	}

	addr, err := netlink.AddrList(*c.iface, AF_INET6)
	if err != nil {
		return fmt.Errorf("failed to get address list: %w", err)
	}

	for _, a := range addr {
		if !a.IP.IsLinkLocalUnicast() {
			netlink.AddrDel(*c.iface, &a)
		}
	}

	return ensureInterfaceIsUp(c.iface)
}

func (c *NetworkInterfaceConfig) applyIPv6Static(s *NetworkInterfaceState) error {
	c.ipv6Lock.Lock()
	defer c.ipv6Lock.Unlock()

	config, err := parseAndValidateStaticIPv6Config(c.config.IPv6Static)
	if err != nil {
		return err
	}

	if c.iface == nil {
		return fmt.Errorf("interface handle is nil")
	}

	if err := c.setSysctlValues(map[string]int{
		"net.ipv6.conf.%s.disable_ipv6": 0, // enable IPv6
		"net.ipv6.conf.%s.accept_ra":    2, // accept even if forwarding is disabled
	}); err != nil {
		return err
	}

	// disable DHCPv6
	s.dhcpClient.SetIPv6(false)

	if err := reconcileLinkAddrs(c.iface, AF_INET6, &config.prefix, c.l); err != nil {
		return err
	}

	if !checkIfDefaultRouteIsSet(config.gateway, AF_INET6) {
		if err := netlink.RouteReplace(&netlink.Route{
			Dst:       &ipv6DefaultRoute,
			Gw:        config.gateway,
			LinkIndex: (*c.iface).Attrs().Index,
		}); err != nil {
			return fmt.Errorf("failed to add default route: %w", err)
		}
	}

	return ensureInterfaceIsUp(c.iface)
}

func (c *NetworkInterfaceConfig) disableIPv6() error {
	return c.setSysctlValues(map[string]int{
		"net.ipv6.conf.%s.disable_ipv6": 1, // disable IPv6
	})
}
