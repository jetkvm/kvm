package nmlite

import (
	"fmt"
	"net"

	"github.com/jetkvm/kvm/internal/network/types"
	"github.com/jetkvm/kvm/pkg/nmlite/link"
	"github.com/rs/zerolog"
	"github.com/vishvananda/netlink"
)

// StaticConfigManager manages static network configuration
type StaticConfigManager struct {
	ifaceName string
	logger    *zerolog.Logger
}

// NewStaticConfigManager creates a new static configuration manager
func NewStaticConfigManager(ifaceName string, logger *zerolog.Logger) (*StaticConfigManager, error) {
	if ifaceName == "" {
		return nil, fmt.Errorf("interface name cannot be empty")
	}

	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	return &StaticConfigManager{
		ifaceName: ifaceName,
		logger:    logger,
	}, nil
}

// ApplyIPv4Static applies static IPv4 configuration
func (scm *StaticConfigManager) ApplyIPv4Static(config *types.IPv4StaticConfig) error {
	scm.logger.Info().Msg("applying static IPv4 configuration")

	// Parse and validate configuration
	ipv4Config, err := scm.parseIPv4Config(config)
	if err != nil {
		return fmt.Errorf("failed to parse IPv4 config: %w", err)
	}

	// Get interface
	netlinkMgr := getNetlinkManager()
	link, err := netlinkMgr.GetLinkByName(scm.ifaceName)
	if err != nil {
		return fmt.Errorf("failed to get interface: %w", err)
	}

	// Ensure interface is up
	if err := netlinkMgr.EnsureInterfaceUp(link); err != nil {
		return fmt.Errorf("failed to bring interface up: %w", err)
	}

	// Apply IP address
	if err := scm.applyIPv4Address(link, ipv4Config); err != nil {
		return fmt.Errorf("failed to apply IPv4 address: %w", err)
	}

	// Apply default route
	if err := scm.applyIPv4Route(link, ipv4Config); err != nil {
		return fmt.Errorf("failed to apply IPv4 route: %w", err)
	}

	scm.logger.Info().Msg("static IPv4 configuration applied successfully")
	return nil
}

// ApplyIPv6Static applies static IPv6 configuration
func (scm *StaticConfigManager) ApplyIPv6Static(config *types.IPv6StaticConfig) error {
	scm.logger.Info().Msg("applying static IPv6 configuration")

	// Parse and validate configuration
	ipv6Config, err := scm.parseIPv6Config(config)
	if err != nil {
		return fmt.Errorf("failed to parse IPv6 config: %w", err)
	}

	// Get interface
	netlinkMgr := getNetlinkManager()
	link, err := netlinkMgr.GetLinkByName(scm.ifaceName)
	if err != nil {
		return fmt.Errorf("failed to get interface: %w", err)
	}

	// Enable IPv6
	if err := scm.enableIPv6(); err != nil {
		return fmt.Errorf("failed to enable IPv6: %w", err)
	}

	// Ensure interface is up
	if err := netlinkMgr.EnsureInterfaceUp(link); err != nil {
		return fmt.Errorf("failed to bring interface up: %w", err)
	}

	// Apply IP address
	if err := scm.applyIPv6Address(link, ipv6Config); err != nil {
		return fmt.Errorf("failed to apply IPv6 address: %w", err)
	}

	// Apply default route
	if err := scm.applyIPv6Route(link, ipv6Config); err != nil {
		return fmt.Errorf("failed to apply IPv6 route: %w", err)
	}

	scm.logger.Info().Msg("static IPv6 configuration applied successfully")
	return nil
}

// DisableIPv4 disables IPv4 on the interface
func (scm *StaticConfigManager) DisableIPv4() error {
	scm.logger.Info().Msg("disabling IPv4")

	netlinkMgr := getNetlinkManager()
	iface, err := netlinkMgr.GetLinkByName(scm.ifaceName)
	if err != nil {
		return fmt.Errorf("failed to get interface: %w", err)
	}

	// Remove all IPv4 addresses
	if err := netlinkMgr.RemoveAllAddresses(iface, link.AfInet); err != nil {
		return fmt.Errorf("failed to remove IPv4 addresses: %w", err)
	}

	// Remove default route
	if err := scm.removeIPv4DefaultRoute(); err != nil {
		scm.logger.Warn().Err(err).Msg("failed to remove IPv4 default route")
	}

	scm.logger.Info().Msg("IPv4 disabled")
	return nil
}

// DisableIPv6 disables IPv6 on the interface
func (scm *StaticConfigManager) DisableIPv6() error {
	scm.logger.Info().Msg("disabling IPv6")
	netlinkMgr := getNetlinkManager()
	return netlinkMgr.DisableIPv6(scm.ifaceName)
}

// EnableIPv6SLAAC enables IPv6 SLAAC
func (scm *StaticConfigManager) EnableIPv6SLAAC() error {
	scm.logger.Info().Msg("enabling IPv6 SLAAC")
	netlinkMgr := getNetlinkManager()
	return netlinkMgr.EnableIPv6SLAAC(scm.ifaceName)
}

// EnableIPv6LinkLocal enables IPv6 link-local only
func (scm *StaticConfigManager) EnableIPv6LinkLocal() error {
	scm.logger.Info().Msg("enabling IPv6 link-local only")

	netlinkMgr := getNetlinkManager()
	if err := netlinkMgr.EnableIPv6LinkLocal(scm.ifaceName); err != nil {
		return err
	}

	// Remove all non-link-local IPv6 addresses
	link, err := netlinkMgr.GetLinkByName(scm.ifaceName)
	if err != nil {
		return fmt.Errorf("failed to get interface: %w", err)
	}

	if err := netlinkMgr.RemoveNonLinkLocalIPv6Addresses(link); err != nil {
		return fmt.Errorf("failed to remove non-link-local IPv6 addresses: %w", err)
	}

	return netlinkMgr.EnsureInterfaceUp(link)
}

// parseIPv4Config parses and validates IPv4 static configuration
func (scm *StaticConfigManager) parseIPv4Config(config *types.IPv4StaticConfig) (*parsedIPv4Config, error) {
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}

	// Parse IP address and netmask
	ipNet, err := link.ParseIPv4Netmask(config.Address.String, config.Netmask.String)
	if err != nil {
		return nil, err
	}
	scm.logger.Info().Str("ipNet", ipNet.String()).Interface("ipc", config).Msg("parsed IPv4 address and netmask")

	// Parse gateway
	gateway := net.ParseIP(config.Gateway.String)
	if gateway == nil {
		return nil, fmt.Errorf("invalid gateway: %s", config.Gateway.String)
	}

	// Parse DNS servers
	var dns []net.IP
	for _, dnsStr := range config.DNS {
		if err := link.ValidateIPAddress(dnsStr, false); err != nil {
			return nil, fmt.Errorf("invalid DNS server: %w", err)
		}
		dns = append(dns, net.ParseIP(dnsStr))
	}

	return &parsedIPv4Config{
		network: *ipNet,
		gateway: gateway,
		dns:     dns,
	}, nil
}

// parseIPv6Config parses and validates IPv6 static configuration
func (scm *StaticConfigManager) parseIPv6Config(config *types.IPv6StaticConfig) (*parsedIPv6Config, error) {
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}

	// Parse IP address and prefix
	ipNet, err := link.ParseIPv6Prefix(config.Prefix.String, 64) // Default to /64 if not specified
	if err != nil {
		return nil, err
	}

	// Parse gateway
	gateway := net.ParseIP(config.Gateway.String)
	if gateway == nil {
		return nil, fmt.Errorf("invalid gateway: %s", config.Gateway.String)
	}

	// Parse DNS servers
	var dns []net.IP
	for _, dnsStr := range config.DNS {
		dnsIP := net.ParseIP(dnsStr)
		if dnsIP == nil {
			return nil, fmt.Errorf("invalid DNS server: %s", dnsStr)
		}
		dns = append(dns, dnsIP)
	}

	return &parsedIPv6Config{
		prefix:  *ipNet,
		gateway: gateway,
		dns:     dns,
	}, nil
}

// applyIPv4Address applies IPv4 address to interface
func (scm *StaticConfigManager) applyIPv4Address(iface *link.Link, config *parsedIPv4Config) error {
	netlinkMgr := getNetlinkManager()

	// Remove existing IPv4 addresses
	if err := netlinkMgr.RemoveAllAddresses(iface, link.AfInet); err != nil {
		return fmt.Errorf("failed to remove existing IPv4 addresses: %w", err)
	}

	// Add new address
	addr := &netlink.Addr{
		IPNet: &config.network,
	}
	if err := netlinkMgr.AddrAdd(iface, addr); err != nil {
		return fmt.Errorf("failed to add IPv4 address: %w", err)
	}

	scm.logger.Info().Str("address", config.network.String()).Msg("IPv4 address applied")
	return nil
}

// applyIPv6Address applies IPv6 address to interface
func (scm *StaticConfigManager) applyIPv6Address(iface *link.Link, config *parsedIPv6Config) error {
	netlinkMgr := getNetlinkManager()

	// Remove existing global IPv6 addresses
	if err := netlinkMgr.RemoveNonLinkLocalIPv6Addresses(iface); err != nil {
		return fmt.Errorf("failed to remove existing IPv6 addresses: %w", err)
	}

	// Add new address
	addr := &netlink.Addr{
		IPNet: &config.prefix,
	}
	if err := netlinkMgr.AddrAdd(iface, addr); err != nil {
		return fmt.Errorf("failed to add IPv6 address: %w", err)
	}

	scm.logger.Info().Str("address", config.prefix.String()).Msg("IPv6 address applied")
	return nil
}

// applyIPv4Route applies IPv4 default route
func (scm *StaticConfigManager) applyIPv4Route(iface *link.Link, config *parsedIPv4Config) error {
	netlinkMgr := getNetlinkManager()

	// Check if default route already exists
	if netlinkMgr.HasDefaultRoute(link.AfInet) {
		scm.logger.Info().Msg("IPv4 default route already exists")
		return nil
	}

	// Add default route
	if err := netlinkMgr.AddDefaultRoute(iface, config.gateway, link.AfInet); err != nil {
		return fmt.Errorf("failed to add IPv4 default route: %w", err)
	}

	scm.logger.Info().Str("gateway", config.gateway.String()).Msg("IPv4 default route applied")
	return nil
}

// applyIPv6Route applies IPv6 default route
func (scm *StaticConfigManager) applyIPv6Route(iface *link.Link, config *parsedIPv6Config) error {
	netlinkMgr := getNetlinkManager()

	// Check if default route already exists
	if netlinkMgr.HasDefaultRoute(link.AfInet6) {
		scm.logger.Info().Msg("IPv6 default route already exists")
		return nil
	}

	// Add default route
	if err := netlinkMgr.AddDefaultRoute(iface, config.gateway, link.AfInet6); err != nil {
		return fmt.Errorf("failed to add IPv6 default route: %w", err)
	}

	scm.logger.Info().Str("gateway", config.gateway.String()).Msg("IPv6 default route applied")
	return nil
}

// removeIPv4DefaultRoute removes IPv4 default route
func (scm *StaticConfigManager) removeIPv4DefaultRoute() error {
	netlinkMgr := getNetlinkManager()
	return netlinkMgr.RemoveDefaultRoute(link.AfInet)
}

// enableIPv6 enables IPv6 on the interface
func (scm *StaticConfigManager) enableIPv6() error {
	netlinkMgr := getNetlinkManager()
	return netlinkMgr.EnableIPv6(scm.ifaceName)
}

// parsedIPv4Config represents parsed IPv4 configuration
type parsedIPv4Config struct {
	network net.IPNet
	gateway net.IP
	dns     []net.IP
}

// parsedIPv6Config represents parsed IPv6 configuration
type parsedIPv6Config struct {
	prefix  net.IPNet
	gateway net.IP
	dns     []net.IP
}
