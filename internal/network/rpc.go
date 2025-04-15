package network

import (
	"fmt"
	"time"

	"github.com/jetkvm/kvm/internal/confparser"
	"github.com/jetkvm/kvm/internal/udhcpc"
)

type RpcIPv6Address struct {
	Address           string     `json:"address"`
	ValidLifetime     *time.Time `json:"valid_lifetime,omitempty"`
	PreferredLifetime *time.Time `json:"preferred_lifetime,omitempty"`
	Scope             int        `json:"scope"`
}

type RpcNetworkState struct {
	InterfaceName string           `json:"interface_name"`
	MacAddress    string           `json:"mac_address"`
	IPv4          string           `json:"ipv4,omitempty"`
	IPv6          string           `json:"ipv6,omitempty"`
	IPv6LinkLocal string           `json:"ipv6_link_local,omitempty"`
	IPv4Addresses []string         `json:"ipv4_addresses,omitempty"`
	IPv6Addresses []RpcIPv6Address `json:"ipv6_addresses,omitempty"`
	DHCPLease     *udhcpc.Lease    `json:"dhcp_lease,omitempty"`
}

type RpcNetworkSettings struct {
	NetworkConfig
}

func (s *NetworkInterfaceState) RpcGetNetworkState() RpcNetworkState {
	ipv6Addresses := make([]RpcIPv6Address, 0)

	if s.ipv6Addresses != nil {
		for _, addr := range s.ipv6Addresses {
			ipv6Addresses = append(ipv6Addresses, RpcIPv6Address{
				Address:           addr.Prefix.String(),
				ValidLifetime:     addr.ValidLifetime,
				PreferredLifetime: addr.PreferredLifetime,
				Scope:             addr.Scope,
			})
		}
	}

	return RpcNetworkState{
		InterfaceName: s.interfaceName,
		MacAddress:    s.macAddr.String(),
		IPv4:          s.ipv4Addr.String(),
		IPv6:          s.ipv6Addr.String(),
		IPv6LinkLocal: s.ipv6LinkLocal.String(),
		IPv4Addresses: s.ipv4Addresses,
		IPv6Addresses: ipv6Addresses,
		DHCPLease:     s.dhcpClient.GetLease(),
	}
}

func (s *NetworkInterfaceState) RpcGetNetworkSettings() RpcNetworkSettings {
	if s.config == nil {
		return RpcNetworkSettings{}
	}

	return RpcNetworkSettings{
		NetworkConfig: *s.config,
	}
}

func (s *NetworkInterfaceState) RpcSetNetworkSettings(settings RpcNetworkSettings) error {
	currentSettings := s.config

	err := confparser.SetDefaultsAndValidate(&settings.NetworkConfig)
	if err != nil {
		return err
	}

	if IsSame(currentSettings, settings.NetworkConfig) {
		// no changes, do nothing
		return nil
	}

	s.config = &settings.NetworkConfig
	s.onConfigChange(s.config)

	return nil
}

func (s *NetworkInterfaceState) RpcRenewDHCPLease() error {
	if s.dhcpClient == nil {
		return fmt.Errorf("dhcp client not initialized")
	}

	return s.dhcpClient.Renew()
}
