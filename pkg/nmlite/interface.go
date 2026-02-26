package nmlite

import (
	"context"
	"fmt"
	"net"
	"net/netip"

	"time"

	"github.com/jetkvm/kvm/internal/confparser"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/network/types"
	"github.com/jetkvm/kvm/internal/sync"

	"github.com/jetkvm/kvm/pkg/nmlite/link"

	"github.com/mdlayher/ndp"
	"github.com/rs/zerolog"
	"github.com/vishvananda/netlink"
)

type ResolvConfChangeCallback func(family int, resolvConf *types.InterfaceResolvConf) error

// InterfaceManager manages a single network interface
type InterfaceManager struct {
	ctx       context.Context
	ifaceName string
	config    *types.NetworkConfig
	logger    *zerolog.Logger
	state     *types.InterfaceState
	linkState *link.Link
	stateMu   sync.RWMutex

	// Network components
	staticConfig *StaticConfigManager
	dhcpClient   *DHCPClient

	// Callbacks
	onStateChange      func(state types.InterfaceState)
	onConfigChange     func(config *types.NetworkConfig)
	onDHCPLeaseChange  func(lease *types.DHCPLease)
	onResolvConfChange ResolvConfChangeCallback

	// Control
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewInterfaceManager creates a new interface manager
func NewInterfaceManager(ctx context.Context, ifaceName string, config *types.NetworkConfig, logger *zerolog.Logger) (*InterfaceManager, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if logger == nil {
		logger = logging.GetSubsystemLogger("interface")
	}

	scopedLogger := logger.With().Str("interface", ifaceName).Logger()

	// Validate and set defaults
	if err := confparser.SetDefaultsAndValidate(config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	im := &InterfaceManager{
		ctx:       ctx,
		ifaceName: ifaceName,
		config:    config,
		logger:    &scopedLogger,
		state: &types.InterfaceState{
			InterfaceName: ifaceName,
			// LastUpdated:   time.Now(),
		},
		stopCh: make(chan struct{}),
	}

	// Initialize components
	var err error
	im.staticConfig, err = NewStaticConfigManager(ifaceName, &scopedLogger)
	if err != nil {
		return nil, fmt.Errorf("failed to create static config manager: %w", err)
	}

	// create the dhcp client
	im.dhcpClient, err = NewDHCPClient(ctx, ifaceName, &scopedLogger, config.DHCPClient.String)
	if err != nil {
		return nil, fmt.Errorf("failed to create DHCP client: %w", err)
	}

	// Set up DHCP client callbacks
	im.dhcpClient.SetOnLeaseChange(func(lease *types.DHCPLease) {
		if im.config.IPv4Mode.String != "dhcp" {
			im.logger.Warn().Str("mode", im.config.IPv4Mode.String).Msg("ignoring DHCP lease, current mode is not DHCP")
			return
		}

		if err := im.applyDHCPLease(lease); err != nil {
			im.logger.Error().Err(err).Msg("failed to apply DHCP lease")
		}
		im.updateStateFromDHCPLease(lease)
		if im.onDHCPLeaseChange != nil {
			im.onDHCPLeaseChange(lease)
		}
	})

	return im, nil
}

// Start starts managing the interface
func (im *InterfaceManager) Start() error {
	im.stateMu.Lock()
	defer im.stateMu.Unlock()

	im.logger.Info().Msg("starting interface manager")

	// Start monitoring interface state
	im.wg.Add(1)
	go im.monitorInterfaceState()

	nl := getNetlinkManager()

	// Set the link state
	linkState, err := nl.GetLinkByName(im.ifaceName)
	if err != nil {
		return fmt.Errorf("failed to get interface: %w", err)
	}
	im.linkState = linkState

	// Bring the interface up
	_, linkUpErr := nl.EnsureInterfaceUpWithTimeout(
		im.ctx,
		im.linkState,
		30*time.Second,
	)

	// Set callback after the interface is up
	nl.AddStateChangeCallback(im.ifaceName, link.StateChangeCallback{
		Async: true,
		Func: func(link *link.Link) {
			im.handleLinkStateChange(link)
		},
	})

	if linkUpErr != nil {
		im.logger.Error().Err(linkUpErr).Msg("failed to bring interface up, continuing anyway")
	} else {
		// Apply initial configuration
		if err := im.applyConfiguration(); err != nil {
			im.logger.Error().Err(err).Msg("failed to apply initial configuration")
			return err
		}
	}

	im.logger.Info().Msg("interface manager started")
	return nil
}

// Stop stops managing the interface
func (im *InterfaceManager) Stop() error {
	im.logger.Info().Msg("stopping interface manager")

	close(im.stopCh)
	im.wg.Wait()

	// Stop DHCP client
	if im.dhcpClient != nil {
		if err := im.dhcpClient.Stop(); err != nil {
			return fmt.Errorf("failed to stop DHCP client: %w", err)
		}
	}

	im.logger.Info().Msg("interface manager stopped")
	return nil
}

func (im *InterfaceManager) link() (*link.Link, error) {
	nl := getNetlinkManager()
	if nl == nil {
		return nil, fmt.Errorf("netlink manager not initialized")
	}
	return nl.GetLinkByName(im.ifaceName)
}

// IsUp returns true if the interface is up
func (im *InterfaceManager) IsUp() bool {
	im.stateMu.RLock()
	defer im.stateMu.RUnlock()

	if im.state == nil {
		return false
	}

	return im.state.Up
}

// IsOnline returns true if the interface is online
func (im *InterfaceManager) IsOnline() bool {
	im.stateMu.RLock()
	defer im.stateMu.RUnlock()

	if im.state == nil {
		return false
	}

	return im.state.Online
}

// IPv4Ready returns true if the interface has an IPv4 address
func (im *InterfaceManager) IPv4Ready() bool {
	im.stateMu.RLock()
	defer im.stateMu.RUnlock()

	if im.state == nil {
		return false
	}

	return im.state.IPv4Ready
}

// IPv6Ready returns true if the interface has an IPv6 address
func (im *InterfaceManager) IPv6Ready() bool {
	im.stateMu.RLock()
	defer im.stateMu.RUnlock()

	if im.state == nil {
		return false
	}

	return im.state.IPv6Ready
}

// GetIPv4Addresses returns the IPv4 addresses of the interface
func (im *InterfaceManager) GetIPv4Addresses() []string {
	im.stateMu.RLock()
	defer im.stateMu.RUnlock()

	if im.state == nil {
		return []string{}
	}

	return im.state.IPv4Addresses
}

// GetIPv4Address returns the IPv4 address of the interface
func (im *InterfaceManager) GetIPv4Address() string {
	im.stateMu.RLock()
	defer im.stateMu.RUnlock()

	if im.state == nil {
		return ""
	}

	return im.state.IPv4Address
}

// GetIPv6Address returns the IPv6 address of the interface
func (im *InterfaceManager) GetIPv6Address() string {
	im.stateMu.RLock()
	defer im.stateMu.RUnlock()

	if im.state == nil {
		return ""
	}

	return im.state.IPv6Address
}

// GetIPv6Addresses returns the IPv6 addresses of the interface
func (im *InterfaceManager) GetIPv6Addresses() []string {
	im.stateMu.RLock()
	defer im.stateMu.RUnlock()

	addresses := []string{}

	if im.state == nil {
		return addresses
	}

	for _, addr := range im.state.IPv6Addresses {
		addresses = append(addresses, addr.Address.String())
	}

	return addresses
}

// GetMACAddress returns the MAC address of the interface
func (im *InterfaceManager) GetMACAddress() string {
	im.stateMu.RLock()
	defer im.stateMu.RUnlock()

	if im.state == nil {
		return ""
	}

	return im.state.MACAddress
}

// GetState returns the current interface state
func (im *InterfaceManager) GetState() *types.InterfaceState {
	im.stateMu.RLock()
	defer im.stateMu.RUnlock()

	// Return a copy to avoid race conditions
	im.logger.Debug().Interface("state", im.state).Msg("getting interface state")

	state := *im.state
	return &state
}

// NTPServers returns the NTP servers of the interface
func (im *InterfaceManager) NTPServers() []net.IP {
	im.stateMu.RLock()
	defer im.stateMu.RUnlock()

	if im.state == nil {
		return []net.IP{}
	}

	return im.state.NTPServers
}

func (im *InterfaceManager) Domain() string {
	im.stateMu.RLock()
	defer im.stateMu.RUnlock()

	if im.state == nil {
		return ""
	}

	if im.state.DHCPLease4 != nil {
		return im.state.DHCPLease4.Domain
	}

	if im.state.DHCPLease6 != nil {
		return im.state.DHCPLease6.Domain
	}

	return ""
}

// GetConfig returns the current interface configuration
func (im *InterfaceManager) GetConfig() *types.NetworkConfig {
	// Return a copy to avoid race conditions
	config := *im.config
	return &config
}

// SetConfig updates the interface configuration
func (im *InterfaceManager) SetConfig(config *types.NetworkConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	// Validate and set defaults
	if err := confparser.SetDefaultsAndValidate(config); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	im.config = config

	// Apply the new configuration
	if err := im.applyConfiguration(); err != nil {
		im.logger.Error().Err(err).Msg("failed to apply new configuration")
		return err
	}

	// Notify callback
	if im.onConfigChange != nil {
		im.onConfigChange(config)
	}

	im.logger.Info().Msg("configuration updated")
	return nil
}

// RenewDHCPLease renews the DHCP lease
func (im *InterfaceManager) RenewDHCPLease() error {
	if im.dhcpClient == nil {
		return fmt.Errorf("DHCP client not available")
	}

	return im.dhcpClient.Renew()
}

// SetOnStateChange sets the callback for state changes
func (im *InterfaceManager) SetOnStateChange(callback func(state types.InterfaceState)) {
	im.onStateChange = callback
}

// SetOnConfigChange sets the callback for configuration changes
func (im *InterfaceManager) SetOnConfigChange(callback func(config *types.NetworkConfig)) {
	im.onConfigChange = callback
}

// SetOnDHCPLeaseChange sets the callback for DHCP lease changes
func (im *InterfaceManager) SetOnDHCPLeaseChange(callback func(lease *types.DHCPLease)) {
	im.onDHCPLeaseChange = callback
}

// SetOnResolvConfChange sets the callback for resolv.conf changes
func (im *InterfaceManager) SetOnResolvConfChange(callback ResolvConfChangeCallback) {
	im.onResolvConfChange = callback
}

// applyConfiguration applies the current configuration to the interface
func (im *InterfaceManager) applyConfiguration() error {
	im.logger.Info().Msg("applying configuration")

	// Apply IPv4 configuration
	if err := im.applyIPv4Config(); err != nil {
		return fmt.Errorf("failed to apply IPv4 config: %w", err)
	}

	// Apply IPv6 configuration
	if err := im.applyIPv6Config(); err != nil {
		return fmt.Errorf("failed to apply IPv6 config: %w", err)
	}

	return nil
}

// applyIPv4Config applies IPv4 configuration
func (im *InterfaceManager) applyIPv4Config() error {
	mode := im.config.IPv4Mode.String
	im.logger.Info().Str("mode", mode).Msg("applying IPv4 configuration")

	switch mode {
	case "static":
		return im.applyIPv4Static()
	case "dhcp":
		return im.applyIPv4DHCP()
	case "disabled":
		return im.disableIPv4()
	default:
		return fmt.Errorf("invalid IPv4 mode: %s", mode)
	}
}

// applyIPv6Config applies IPv6 configuration
func (im *InterfaceManager) applyIPv6Config() error {
	mode := im.config.IPv6Mode.String
	im.logger.Info().Str("mode", mode).Msg("applying IPv6 configuration")

	switch mode {
	case "static":
		return im.applyIPv6Static()
	case "dhcpv6":
		return im.applyIPv6DHCP()
	case "slaac":
		return im.applyIPv6SLAAC()
	case "slaac_and_dhcpv6":
		return im.applyIPv6SLAACAndDHCP()
	case "link_local":
		return im.applyIPv6LinkLocal()
	case "disabled":
		return im.disableIPv6()
	default:
		return fmt.Errorf("invalid IPv6 mode: %s", mode)
	}
}

// applyIPv4Static applies static IPv4 configuration
func (im *InterfaceManager) applyIPv4Static() error {
	if im.config.IPv4Static == nil {
		return fmt.Errorf("IPv4 static configuration is nil")
	}

	im.logger.Info().Msg("stopping DHCP for IPv4")

	// Disable DHCP
	if im.dhcpClient != nil {
		im.dhcpClient.SetIPv4(false)
	}

	im.logger.Info().Interface("config", im.config.IPv4Static).Msg("applying IPv4 static configuration")

	config, err := im.staticConfig.ToIPv4Static(im.config.IPv4Static)
	if err != nil {
		return fmt.Errorf("failed to convert IPv4 static configuration: %w", err)
	}

	im.logger.Info().Interface("config", config).Msg("converted IPv4 static configuration")

	if err := im.onResolvConfChange(link.AfInet, &types.InterfaceResolvConf{
		NameServers: config.Nameservers,
		Source:      "static",
	}); err != nil {
		im.logger.Warn().Err(err).Msg("failed to update resolv.conf")
	}

	return im.ReconcileLinkAddrs(config.Addresses, link.AfInet, link.StaticProtocol)
}

// applyIPv4DHCP applies DHCP IPv4 configuration
func (im *InterfaceManager) applyIPv4DHCP() error {
	if im.dhcpClient == nil {
		return fmt.Errorf("DHCP client not available")
	}

	// Enable DHCP
	im.dhcpClient.SetIPv4(true)
	return im.dhcpClient.Start()
}

// disableIPv4 disables IPv4
func (im *InterfaceManager) disableIPv4() error {
	// Disable DHCP
	if im.dhcpClient != nil {
		im.dhcpClient.SetIPv4(false)
	}

	// Remove all IPv4 addresses
	return im.staticConfig.DisableIPv4()
}

// applyIPv6Static applies static IPv6 configuration
func (im *InterfaceManager) applyIPv6Static() error {
	if im.config.IPv6Static == nil {
		return fmt.Errorf("IPv6 static configuration is nil")
	}

	im.logger.Info().Msg("stopping DHCPv6")
	// Disable DHCPv6
	if im.dhcpClient != nil {
		im.dhcpClient.SetIPv6(false)
	}

	// Apply static configuration
	config, err := im.staticConfig.ToIPv6Static(im.config.IPv6Static)
	if err != nil {
		return fmt.Errorf("failed to convert IPv6 static configuration: %w", err)
	}
	im.logger.Info().Interface("config", config).Msg("converted IPv6 static configuration")

	if err := im.onResolvConfChange(link.AfInet6, &types.InterfaceResolvConf{
		NameServers: config.Nameservers,
		Source:      "static",
	}); err != nil {
		im.logger.Warn().Err(err).Msg("failed to update resolv.conf")
	}

	return im.ReconcileLinkAddrs(config.Addresses, link.AfInet6, link.StaticProtocol)
}

// applyIPv6DHCP applies DHCPv6 configuration
func (im *InterfaceManager) applyIPv6DHCP() error {
	if im.dhcpClient == nil {
		return fmt.Errorf("DHCP client not available")
	}

	// Enable DHCPv6
	im.dhcpClient.SetIPv6(true)
	return im.dhcpClient.Start()
}

// applyIPv6SLAAC applies SLAAC configuration
func (im *InterfaceManager) applyIPv6SLAAC() error {
	// Disable DHCPv6
	if im.dhcpClient != nil {
		im.dhcpClient.SetIPv6(false)
	}

	// Remove static IPv6 configuration
	l, err := im.link()
	if err != nil {
		return fmt.Errorf("failed to get interface: %w", err)
	}

	netlinkMgr := getNetlinkManager()

	// Ensure interface is up
	if err := netlinkMgr.EnsureInterfaceUp(l); err != nil {
		return fmt.Errorf("failed to bring interface up: %w", err)
	}

	if err := netlinkMgr.RemoveNonLinkLocalIPv6Addresses(l); err != nil {
		return fmt.Errorf("failed to remove non-link-local IPv6 addresses: %w", err)
	}

	if err := im.SendRouterSolicitation(); err != nil {
		im.logger.Error().Err(err).Msg("failed to send router solicitation, continuing anyway")
	}

	// Enable SLAAC
	return im.staticConfig.EnableIPv6SLAAC()
}

// applyIPv6SLAACAndDHCP applies SLAAC + DHCPv6 configuration
func (im *InterfaceManager) applyIPv6SLAACAndDHCP() error {
	// Enable both SLAAC and DHCPv6
	if im.dhcpClient != nil {
		im.dhcpClient.SetIPv6(true)
		if err := im.dhcpClient.Start(); err != nil {
			return fmt.Errorf("failed to start DHCP client: %w", err)
		}
	}

	return im.staticConfig.EnableIPv6SLAAC()
}

// applyIPv6LinkLocal applies link-local only IPv6 configuration
func (im *InterfaceManager) applyIPv6LinkLocal() error {
	// Disable DHCPv6
	if im.dhcpClient != nil {
		im.dhcpClient.SetIPv6(false)
	}

	// Enable link-local only
	return im.staticConfig.EnableIPv6LinkLocal()
}

// disableIPv6 disables IPv6
func (im *InterfaceManager) disableIPv6() error {
	// Disable DHCPv6
	if im.dhcpClient != nil {
		im.dhcpClient.SetIPv6(false)
	}

	// Disable IPv6
	return im.staticConfig.DisableIPv6()
}

func (im *InterfaceManager) handleLinkStateChange(link *link.Link) {
	{
		im.stateMu.Lock()
		defer im.stateMu.Unlock()

		if link.IsSame(im.linkState) {
			return
		}

		im.linkState = link
	}

	im.logger.Info().Interface("link", link).Msg("link state changed")

	operState := link.Attrs().OperState
	if operState == netlink.OperUp {
		im.handleLinkUp()
	} else {
		im.handleLinkDown()
	}
}

// SendRouterSolicitation sends a router solicitation
func (im *InterfaceManager) SendRouterSolicitation() error {
	im.logger.Info().Msg("sending router solicitation")
	m := &ndp.RouterSolicitation{}

	l, err := im.link()
	if err != nil {
		return fmt.Errorf("failed to get interface: %w", err)
	}

	if l.Attrs().OperState != netlink.OperUp {
		return fmt.Errorf("interface %s is not up", im.ifaceName)
	}

	iface := l.Interface()
	if iface == nil {
		return fmt.Errorf("failed to get net.Interface for %s", im.ifaceName)
	}

	hwAddr := l.HardwareAddr()
	if hwAddr == nil {
		return fmt.Errorf("failed to get hardware address for %s", im.ifaceName)
	}

	c, _, err := ndp.Listen(iface, ndp.LinkLocal)
	if err != nil {
		return fmt.Errorf("failed to create NDP listener on %s: %w", im.ifaceName, err)
	}

	m.Options = append(m.Options, &ndp.LinkLayerAddress{
		Addr:      hwAddr,
		Direction: ndp.Source,
	})

	targetAddr := netip.MustParseAddr("ff02::2")

	if err := c.WriteTo(m, nil, targetAddr); err != nil {
		c.Close()
		return fmt.Errorf("failed to write to %s: %w", targetAddr.String(), err)
	}

	im.logger.Info().Msg("router solicitation sent")
	c.Close()

	return nil
}

func (im *InterfaceManager) handleLinkUp() {
	im.logger.Info().Msg("link up")

	if err := im.applyConfiguration(); err != nil {
		im.logger.Error().Err(err).Msg("failed to apply configuration")
	}

	if im.config.IPv4Mode.String == "dhcp" {
		if err := im.dhcpClient.Renew(); err != nil {
			im.logger.Error().Err(err).Msg("failed to renew DHCP lease")
		}
	}

	if im.config.IPv6Mode.String == "slaac" {
		if err := im.staticConfig.EnableIPv6SLAAC(); err != nil {
			im.logger.Error().Err(err).Msg("failed to enable IPv6 SLAAC")
		}
		if err := im.SendRouterSolicitation(); err != nil {
			im.logger.Error().Err(err).Msg("failed to send router solicitation")
		}
	}
}

func (im *InterfaceManager) handleLinkDown() {
	im.logger.Info().Msg("link down")

	if im.config.IPv4Mode.String == "dhcp" {
		if err := im.dhcpClient.Stop(); err != nil {
			im.logger.Error().Err(err).Msg("failed to stop DHCP client")
		}
	}

	netlinkMgr := getNetlinkManager()
	if err := netlinkMgr.RemoveAllAddresses(im.linkState, link.AfInet); err != nil {
		im.logger.Error().Err(err).Msg("failed to remove all IPv4 addresses")
	}

	if err := netlinkMgr.RemoveNonLinkLocalIPv6Addresses(im.linkState); err != nil {
		im.logger.Error().Err(err).Msg("failed to remove non-link-local IPv6 addresses")
	}
}

// monitorInterfaceState monitors the interface state and updates accordingly
func (im *InterfaceManager) monitorInterfaceState() {
	defer im.wg.Done()

	im.logger.Debug().Msg("monitoring interface state")
	// TODO: use netlink subscription instead of polling
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-im.ctx.Done():
			return
		case <-im.stopCh:
			return
		case <-ticker.C:
			if err := im.updateInterfaceState(); err != nil {
				im.logger.Error().Err(err).Msg("failed to update interface state")
			}
		}
	}
}

// updateStateFromDHCPLease updates the state from a DHCP lease
func (im *InterfaceManager) updateStateFromDHCPLease(lease *types.DHCPLease) {
	family := link.AfInet

	im.stateMu.Lock()
	if lease.IsIPv6() {
		im.state.DHCPLease6 = lease
		family = link.AfInet6
	} else {
		im.state.DHCPLease4 = lease
	}
	im.stateMu.Unlock()

	// Update resolv.conf with DNS information
	if im.onResolvConfChange == nil {
		return
	}

	if im.ifaceName == "" {
		im.logger.Warn().Msg("interface name is empty, skipping resolv.conf update")
		return
	}

	if err := im.onResolvConfChange(family, &types.InterfaceResolvConf{
		NameServers: lease.DNS,
		SearchList:  lease.SearchList,
		Source:      "dhcp",
		Domain:      lease.Domain,
	}); err != nil {
		im.logger.Warn().Err(err).Msg("failed to update resolv.conf")
	}
}

// ReconcileLinkAddrs reconciles the link addresses
func (im *InterfaceManager) ReconcileLinkAddrs(addrs []types.IPAddress, family int, protocol netlink.RouteProtocol) error {
	nl := getNetlinkManager()
	link, err := im.link()
	if err != nil {
		return fmt.Errorf("failed to get interface: %w", err)
	}
	if link == nil {
		return fmt.Errorf("failed to get interface: %w", err)
	}

	return nl.ReconcileLink(link, addrs, family, protocol)
}

// applyDHCPLease applies DHCP lease configuration using ReconcileLinkAddrs
func (im *InterfaceManager) applyDHCPLease(lease *types.DHCPLease) error {
	if lease == nil {
		return fmt.Errorf("DHCP lease is nil")
	}

	if lease.DHCPClient != "jetdhcpc" {
		im.logger.Warn().Str("dhcp_client", lease.DHCPClient).Msg("ignoring DHCP lease, not implemented yet")
		return nil
	}

	if lease.IsIPv6() {
		im.logger.Warn().Msg("ignoring IPv6 DHCP lease, not implemented yet")
		return nil
	}

	// Convert DHCP lease to IPv4Config
	ipv4Config := im.convertDHCPLeaseToIPv4Config(lease)

	// Apply the configuration using ReconcileLinkAddrs
	return im.ReconcileLinkAddrs([]types.IPAddress{*ipv4Config}, link.AfInet, link.DhcpProtocol)
}

// convertDHCPLeaseToIPv4Config converts a DHCP lease to IPv4Config
func (im *InterfaceManager) convertDHCPLeaseToIPv4Config(lease *types.DHCPLease) *types.IPAddress {
	ipNet := lease.IPNet()
	if ipNet == nil {
		return nil
	}

	// Create IPv4Address
	ipv4Addr := types.IPAddress{
		Address:   *ipNet,
		Gateway:   lease.Routers[0],
		Secondary: false,
		Permanent: false,
	}

	im.logger.Trace().
		Interface("ipv4Addr", ipv4Addr).
		Interface("lease", lease).
		Msg("converted DHCP lease to IPv4Config")

	// Create IPv4Config
	return &ipv4Addr
}
