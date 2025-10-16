// Package nmlite provides DHCP client functionality for the network manager.
package nmlite

import (
	"context"
	"fmt"

	"github.com/jetkvm/kvm/internal/network/types"
	"github.com/jetkvm/kvm/pkg/nmlite/jetdhcpc"
	"github.com/jetkvm/kvm/pkg/nmlite/udhcpc"
	"github.com/rs/zerolog"
)

// DHCPClient wraps the dhclient package for use in the network manager
type DHCPClient struct {
	ctx        context.Context
	ifaceName  string
	logger     *zerolog.Logger
	client     types.DHCPClient
	clientType string

	// Configuration
	ipv4Enabled bool
	ipv6Enabled bool

	// Callbacks
	onLeaseChange func(lease *types.DHCPLease)
}

// NewDHCPClient creates a new DHCP client
func NewDHCPClient(ctx context.Context, ifaceName string, logger *zerolog.Logger, clientType string) (*DHCPClient, error) {
	if ifaceName == "" {
		return nil, fmt.Errorf("interface name cannot be empty")
	}

	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	return &DHCPClient{
		ctx:        ctx,
		ifaceName:  ifaceName,
		logger:     logger,
		clientType: clientType,
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

func (dc *DHCPClient) initClient() (types.DHCPClient, error) {
	switch dc.clientType {
	case "jetdhcpc":
		return dc.initJetDHCPC()
	case "udhcpc":
		return dc.initUDHCPC()
	default:
		return nil, fmt.Errorf("invalid client type: %s", dc.clientType)
	}
}

func (dc *DHCPClient) initJetDHCPC() (types.DHCPClient, error) {
	return jetdhcpc.NewClient(dc.ctx, []string{dc.ifaceName}, &jetdhcpc.Config{
		IPv4:               dc.ipv4Enabled,
		IPv6:               dc.ipv6Enabled,
		V4ClientIdentifier: true,
		OnLease4Change: func(lease *types.DHCPLease) {
			dc.handleLeaseChange(lease, false)
		},
		OnLease6Change: func(lease *types.DHCPLease) {
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
}

func (dc *DHCPClient) initUDHCPC() (types.DHCPClient, error) {
	c := udhcpc.NewDHCPClient(&udhcpc.DHCPClientOptions{
		InterfaceName: dc.ifaceName,
		PidFile:       "",
		Logger:        dc.logger,
		OnLeaseChange: func(lease *types.DHCPLease) {
			dc.handleLeaseChange(lease, false)
		},
	})
	return c, nil
}

// Start starts the DHCP client
func (dc *DHCPClient) Start() error {
	if dc.client != nil {
		dc.logger.Warn().Msg("DHCP client already started")
		return nil
	}

	dc.logger.Info().Msg("starting DHCP client")

	// Create the underlying DHCP client
	client, err := dc.initClient()

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

func (dc *DHCPClient) Domain() string {
	if dc.client == nil {
		return ""
	}
	return dc.client.Domain()
}

func (dc *DHCPClient) Lease4() *types.DHCPLease {
	if dc.client == nil {
		return nil
	}
	return dc.client.Lease4()
}

func (dc *DHCPClient) Lease6() *types.DHCPLease {
	if dc.client == nil {
		return nil
	}
	return dc.client.Lease6()
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
	if err := dc.client.Renew(); err != nil {
		return fmt.Errorf("failed to renew DHCP lease: %w", err)
	}
	return nil
}

// Release releases the DHCP lease
func (dc *DHCPClient) Release() error {
	if dc.client == nil {
		return fmt.Errorf("DHCP client not started")
	}

	dc.logger.Info().Msg("releasing DHCP lease")
	if err := dc.client.Release(); err != nil {
		return fmt.Errorf("failed to release DHCP lease: %w", err)
	}
	return nil
}

// handleLeaseChange handles lease changes from the underlying DHCP client
func (dc *DHCPClient) handleLeaseChange(lease *types.DHCPLease, isIPv6 bool) {
	if lease == nil {
		return
	}

	dc.logger.Info().
		Bool("ipv6", isIPv6).
		Str("ip", lease.IPAddress.String()).
		Msg("DHCP lease changed")

	// copy the lease to avoid race conditions
	leaseCopy := *lease

	// Notify callback
	if dc.onLeaseChange != nil {
		dc.onLeaseChange(&leaseCopy)
	}
}
