// Package nmlite provides DHCP client functionality for the network manager.
package nmlite

import (
	"context"
	"fmt"

	"github.com/jetkvm/kvm/internal/network/types"
	"github.com/jetkvm/kvm/pkg/nmlite/dhclient"
	"github.com/rs/zerolog"
	"github.com/vishvananda/netlink"
)

// DHCPClient wraps the dhclient package for use in the network manager
type DHCPClient struct {
	ctx       context.Context
	ifaceName string
	logger    *zerolog.Logger
	client    *dhclient.Client
	link      netlink.Link

	// Configuration
	ipv4Enabled bool
	ipv6Enabled bool

	// State management
	// stateManager *DHCPStateManager

	// Callbacks
	onLeaseChange func(lease *types.DHCPLease)
}

// NewDHCPClient creates a new DHCP client
func NewDHCPClient(ctx context.Context, ifaceName string, logger *zerolog.Logger) (*DHCPClient, error) {
	if ifaceName == "" {
		return nil, fmt.Errorf("interface name cannot be empty")
	}

	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	// Create state manager
	// stateManager := NewDHCPStateManager("", logger)

	return &DHCPClient{
		ctx:       ctx,
		ifaceName: ifaceName,
		logger:    logger,
	}, nil
}

// SetIPv4 enables or disables IPv4 DHCP
func (dc *DHCPClient) SetIPv4(enabled bool) {
	dc.ipv4Enabled = enabled
	if dc.client != nil {
		dc.client.SetIPv4(enabled)
	}
}

// SetIPv6 enables or disables IPv6 DHCP
func (dc *DHCPClient) SetIPv6(enabled bool) {
	dc.ipv6Enabled = enabled
	if dc.client != nil {
		dc.client.SetIPv6(enabled)
	}
}

// SetOnLeaseChange sets the callback for lease changes
func (dc *DHCPClient) SetOnLeaseChange(callback func(lease *types.DHCPLease)) {
	dc.onLeaseChange = callback
}

// Start starts the DHCP client
func (dc *DHCPClient) Start() error {
	if dc.client != nil {
		dc.logger.Warn().Msg("DHCP client already started")
		return nil
	}

	dc.logger.Info().Msg("starting DHCP client")

	// Create the underlying DHCP client
	client, err := dhclient.NewClient(dc.ctx, []string{dc.ifaceName}, &dhclient.Config{
		IPv4: dc.ipv4Enabled,
		IPv6: dc.ipv6Enabled,
		OnLease4Change: func(lease *dhclient.Lease) {
			dc.handleLeaseChange(lease, false)
		},
		OnLease6Change: func(lease *dhclient.Lease) {
			dc.handleLeaseChange(lease, true)
		},
		UpdateResolvConf: func(nameservers []string) error {
			// This will be handled by the resolv.conf manager
			dc.logger.Debug().
				Interface("nameservers", nameservers).
				Msg("DHCP client requested resolv.conf update")
			return nil
		},
	}, dc.logger)

	if err != nil {
		return fmt.Errorf("failed to create DHCP client: %w", err)
	}

	dc.client = client

	// Start the client
	if err := dc.client.Start(); err != nil {
		dc.client = nil
		return fmt.Errorf("failed to start DHCP client: %w", err)
	}

	dc.logger.Info().Msg("DHCP client started")
	return nil
}

// Stop stops the DHCP client
func (dc *DHCPClient) Stop() error {
	if dc.client == nil {
		return nil
	}

	dc.logger.Info().Msg("stopping DHCP client")

	dc.client = nil
	dc.logger.Info().Msg("DHCP client stopped")
	return nil
}

// Renew renews the DHCP lease
func (dc *DHCPClient) Renew() error {
	if dc.client == nil {
		return fmt.Errorf("DHCP client not started")
	}

	dc.logger.Info().Msg("renewing DHCP lease")
	dc.client.Renew()
	return nil
}

// Release releases the DHCP lease
func (dc *DHCPClient) Release() error {
	if dc.client == nil {
		return fmt.Errorf("DHCP client not started")
	}

	dc.logger.Info().Msg("releasing DHCP lease")
	dc.client.Release()
	return nil
}

// GetLease4 returns the current IPv4 lease
func (dc *DHCPClient) GetLease4() *types.DHCPLease {
	if dc.client == nil {
		return nil
	}

	lease := dc.client.Lease4()
	if lease == nil {
		return nil
	}

	return dc.convertLease(lease, false)
}

// GetLease6 returns the current IPv6 lease
func (dc *DHCPClient) GetLease6() *types.DHCPLease {
	if dc.client == nil {
		return nil
	}

	lease := dc.client.Lease6()
	if lease == nil {
		return nil
	}

	return dc.convertLease(lease, true)
}

// handleLeaseChange handles lease changes from the underlying DHCP client
func (dc *DHCPClient) handleLeaseChange(lease *dhclient.Lease, isIPv6 bool) {
	if lease == nil {
		return
	}

	convertedLease := dc.convertLease(lease, isIPv6)
	if convertedLease == nil {
		dc.logger.Error().Msg("failed to convert lease")
		return
	}

	dc.logger.Info().
		Bool("ipv6", isIPv6).
		Str("ip", convertedLease.IPAddress.String()).
		Msg("DHCP lease changed")

	// Notify callback
	if dc.onLeaseChange != nil {
		dc.onLeaseChange(convertedLease)
	}
}

// convertLease converts a dhclient.Lease to types.DHCPLease
func (dc *DHCPClient) convertLease(lease *dhclient.Lease, isIPv6 bool) *types.DHCPLease {
	if lease == nil {
		return nil
	}

	return lease.ToDHCPLease()
}
