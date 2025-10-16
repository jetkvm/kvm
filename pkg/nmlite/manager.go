// Package nmlite provides a lightweight network management system.
// It supports multiple network interfaces with static and DHCP configuration,
// IPv4/IPv6 support, and proper separation of concerns.
package nmlite

import (
	"context"
	"fmt"

	"github.com/jetkvm/kvm/internal/sync"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/network/types"
	"github.com/jetkvm/kvm/pkg/nmlite/jetdhcpc"
	"github.com/jetkvm/kvm/pkg/nmlite/link"
	"github.com/rs/zerolog"
)

// NetworkManager manages multiple network interfaces
type NetworkManager struct {
	interfaces map[string]*InterfaceManager
	mu         sync.RWMutex
	logger     *zerolog.Logger
	ctx        context.Context
	cancel     context.CancelFunc

	resolvConf *ResolvConfManager

	// Callback functions for state changes
	onInterfaceStateChange func(iface string, state types.InterfaceState)
	onConfigChange         func(iface string, config *types.NetworkConfig)
	onDHCPLeaseChange      func(iface string, lease *types.DHCPLease)
}

// NewNetworkManager creates a new network manager
func NewNetworkManager(ctx context.Context, logger *zerolog.Logger) *NetworkManager {
	if logger == nil {
		logger = logging.GetSubsystemLogger("nm")
	}

	// Initialize the NetlinkManager singleton
	link.InitializeNetlinkManager(logger)

	ctx, cancel := context.WithCancel(ctx)

	return &NetworkManager{
		interfaces: make(map[string]*InterfaceManager),
		logger:     logger,
		ctx:        ctx,
		cancel:     cancel,
		resolvConf: NewResolvConfManager(logger),
	}
}

// SetHostname sets the hostname and domain for the network manager
func (nm *NetworkManager) SetHostname(hostname string, domain string) error {
	return nm.resolvConf.SetHostname(hostname, domain)
}

// Domain returns the effective domain for the network manager
func (nm *NetworkManager) Domain() string {
	return nm.resolvConf.Domain()
}

// AddInterface adds a new network interface to be managed
func (nm *NetworkManager) AddInterface(iface string, config *types.NetworkConfig) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if _, exists := nm.interfaces[iface]; exists {
		return fmt.Errorf("interface %s already managed", iface)
	}

	im, err := NewInterfaceManager(nm.ctx, iface, config, nm.logger)
	if err != nil {
		return fmt.Errorf("failed to create interface manager for %s: %w", iface, err)
	}

	// Set up callbacks
	im.SetOnStateChange(func(state types.InterfaceState) {
		if nm.onInterfaceStateChange != nil {
			state.Hostname = nm.Hostname()
			nm.onInterfaceStateChange(iface, state)
		}
	})

	im.SetOnConfigChange(func(config *types.NetworkConfig) {
		if nm.onConfigChange != nil {
			nm.onConfigChange(iface, config)
		}
	})

	im.SetOnDHCPLeaseChange(func(lease *types.DHCPLease) {
		if nm.onDHCPLeaseChange != nil {
			nm.onDHCPLeaseChange(iface, lease)
		}
	})

	im.SetOnResolvConfChange(func(family int, resolvConf *types.InterfaceResolvConf) error {
		return nm.resolvConf.SetInterfaceConfig(iface, family, *resolvConf)
	})

	nm.interfaces[iface] = im

	// Start monitoring the interface
	if err := im.Start(); err != nil {
		delete(nm.interfaces, iface)
		return fmt.Errorf("failed to start interface manager for %s: %w", iface, err)
	}

	nm.logger.Info().Str("interface", iface).Msg("added interface to network manager")
	return nil
}

// RemoveInterface removes a network interface from management
func (nm *NetworkManager) RemoveInterface(iface string) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	im, exists := nm.interfaces[iface]
	if !exists {
		return fmt.Errorf("interface %s not managed", iface)
	}

	if err := im.Stop(); err != nil {
		nm.logger.Error().Err(err).Str("interface", iface).Msg("failed to stop interface manager")
	}

	delete(nm.interfaces, iface)
	nm.logger.Info().Str("interface", iface).Msg("removed interface from network manager")
	return nil
}

// GetInterface returns the interface manager for a specific interface
func (nm *NetworkManager) GetInterface(iface string) (*InterfaceManager, error) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	im, exists := nm.interfaces[iface]
	if !exists {
		return nil, fmt.Errorf("interface %s not managed", iface)
	}

	return im, nil
}

// ListInterfaces returns a list of all managed interfaces
func (nm *NetworkManager) ListInterfaces() []string {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	interfaces := make([]string, 0, len(nm.interfaces))
	for iface := range nm.interfaces {
		interfaces = append(interfaces, iface)
	}

	return interfaces
}

// GetInterfaceState returns the current state of a specific interface
func (nm *NetworkManager) GetInterfaceState(iface string) (*types.InterfaceState, error) {
	im, err := nm.GetInterface(iface)
	if err != nil {
		return nil, err
	}

	state := im.GetState()
	state.Hostname = nm.Hostname()

	return state, nil
}

// GetInterfaceConfig returns the current configuration of a specific interface
func (nm *NetworkManager) GetInterfaceConfig(iface string) (*types.NetworkConfig, error) {
	im, err := nm.GetInterface(iface)
	if err != nil {
		return nil, err
	}

	return im.GetConfig(), nil
}

// SetInterfaceConfig updates the configuration of a specific interface
func (nm *NetworkManager) SetInterfaceConfig(iface string, config *types.NetworkConfig) error {
	im, err := nm.GetInterface(iface)
	if err != nil {
		return err
	}

	return im.SetConfig(config)
}

// RenewDHCPLease renews the DHCP lease for a specific interface
func (nm *NetworkManager) RenewDHCPLease(iface string) error {
	im, err := nm.GetInterface(iface)
	if err != nil {
		return err
	}

	return im.RenewDHCPLease()
}

// SetOnInterfaceStateChange sets the callback for interface state changes
func (nm *NetworkManager) SetOnInterfaceStateChange(callback func(iface string, state types.InterfaceState)) {
	nm.onInterfaceStateChange = callback
}

// SetOnConfigChange sets the callback for configuration changes
func (nm *NetworkManager) SetOnConfigChange(callback func(iface string, config *types.NetworkConfig)) {
	nm.onConfigChange = callback
}

// SetOnDHCPLeaseChange sets the callback for DHCP lease changes
func (nm *NetworkManager) SetOnDHCPLeaseChange(callback func(iface string, lease *types.DHCPLease)) {
	nm.onDHCPLeaseChange = callback
}

func (nm *NetworkManager) shouldKillLegacyDHCPClients() bool {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	// TODO: remove it when we need to support multiple interfaces
	for _, im := range nm.interfaces {
		if im.dhcpClient.clientType != "udhcpc" {
			return true
		}

		if im.config.IPv4Mode.String != "dhcp" {
			return true
		}
	}
	return false
}

// CleanUpLegacyDHCPClients cleans up legacy DHCP clients
func (nm *NetworkManager) CleanUpLegacyDHCPClients() error {
	shouldKill := nm.shouldKillLegacyDHCPClients()
	if shouldKill {
		return jetdhcpc.KillUdhcpC(nm.logger)
	}
	return nil
}

// Stop stops the network manager and all managed interfaces
func (nm *NetworkManager) Stop() error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	var lastErr error
	for iface, im := range nm.interfaces {
		if err := im.Stop(); err != nil {
			nm.logger.Error().Err(err).Str("interface", iface).Msg("failed to stop interface manager")
			lastErr = err
		}
	}

	nm.cancel()
	nm.logger.Info().Msg("network manager stopped")
	return lastErr
}
