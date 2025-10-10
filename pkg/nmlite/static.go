package nmlite

import (
	"fmt"
	"net"

	"github.com/jetkvm/kvm/internal/network/types"
	"github.com/jetkvm/kvm/pkg/nmlite/link"
	"github.com/rs/zerolog"
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

// ToIPv4Static applies static IPv4 configuration
func (scm *StaticConfigManager) ToIPv4Static(config *types.IPv4StaticConfig) (*types.ParsedIPConfig, error) {
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

	address := types.IPAddress{
		Family:    link.AfInet,
		Address:   *ipNet,
		Gateway:   gateway,
		Secondary: false,
		Permanent: true,
	}

	return &types.ParsedIPConfig{
		Addresses:   []types.IPAddress{address},
		Nameservers: dns,
		Interface:   scm.ifaceName,
	}, nil
}

// ToIPv6Static applies static IPv6 configuration
func (scm *StaticConfigManager) ToIPv6Static(config *types.IPv6StaticConfig) (*types.ParsedIPConfig, error) {
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

	address := types.IPAddress{
		Family:    link.AfInet6,
		Address:   *ipNet,
		Gateway:   gateway,
		Secondary: false,
		Permanent: true,
	}

	return &types.ParsedIPConfig{
		Addresses:   []types.IPAddress{address},
		Nameservers: dns,
		Interface:   scm.ifaceName,
	}, nil
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

// removeIPv4DefaultRoute removes IPv4 default route
func (scm *StaticConfigManager) removeIPv4DefaultRoute() error {
	netlinkMgr := getNetlinkManager()
	return netlinkMgr.RemoveDefaultRoute(link.AfInet)
}
