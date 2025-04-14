package network

import (
	"fmt"
	"time"

	"github.com/guregu/null/v6"
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
	Hostname     null.String `json:"hostname,omitempty"`
	Domain       null.String `json:"domain,omitempty"`
	IPv4Mode     null.String `json:"ipv4_mode,omitempty"`
	IPv6Mode     null.String `json:"ipv6_mode,omitempty"`
	LLDPMode     null.String `json:"lldp_mode,omitempty"`
	LLDPTxTLVs   []string    `json:"lldp_tx_tlvs,omitempty"`
	MDNSMode     null.String `json:"mdns_mode,omitempty"`
	TimeSyncMode null.String `json:"time_sync_mode,omitempty"`
}

func (s *NetworkInterfaceState) RpcGetNetworkState() RpcNetworkState {
	ipv6Addresses := make([]RpcIPv6Address, 0)
	for _, addr := range s.ipv6Addresses {
		ipv6Addresses = append(ipv6Addresses, RpcIPv6Address{
			Address:           addr.Prefix.String(),
			ValidLifetime:     addr.ValidLifetime,
			PreferredLifetime: addr.PreferredLifetime,
			Scope:             addr.Scope,
		})
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
	return RpcNetworkSettings{
		Hostname:     null.StringFrom(s.config.Hostname),
		Domain:       null.StringFrom(s.config.Domain),
		IPv4Mode:     null.StringFrom(s.config.IPv4Mode),
		IPv6Mode:     null.StringFrom(s.config.IPv6Mode),
		LLDPMode:     null.StringFrom(s.config.LLDPMode),
		LLDPTxTLVs:   s.config.LLDPTxTLVs,
		MDNSMode:     null.StringFrom(s.config.MDNSMode),
		TimeSyncMode: null.StringFrom(s.config.TimeSyncMode),
	}
}

func (s *NetworkInterfaceState) RpcSetNetworkSettings(settings RpcNetworkSettings) error {
	changeset := make(map[string]string)
	currentSettings := s.config

	if !settings.Hostname.IsZero() {
		changeset["hostname"] = settings.Hostname.String
		currentSettings.Hostname = settings.Hostname.String
	}

	if !settings.Domain.IsZero() {
		changeset["domain"] = settings.Domain.String
		currentSettings.Domain = settings.Domain.String
	}

	if !settings.IPv4Mode.IsZero() {
		changeset["ipv4_mode"] = settings.IPv4Mode.String
		currentSettings.IPv4Mode = settings.IPv4Mode.String
	}

	if !settings.IPv6Mode.IsZero() {
		changeset["ipv6_mode"] = settings.IPv6Mode.String
		currentSettings.IPv6Mode = settings.IPv6Mode.String
	}

	if !settings.LLDPMode.IsZero() {
		changeset["lldp_mode"] = settings.LLDPMode.String
		currentSettings.LLDPMode = settings.LLDPMode.String
	}

	if !settings.MDNSMode.IsZero() {
		changeset["mdns_mode"] = settings.MDNSMode.String
		currentSettings.MDNSMode = settings.MDNSMode.String
	}

	if !settings.TimeSyncMode.IsZero() {
		changeset["time_sync_mode"] = settings.TimeSyncMode.String
		currentSettings.TimeSyncMode = settings.TimeSyncMode.String
	}

	if len(changeset) > 0 {
		s.config = currentSettings
	}

	return nil
}

func (s *NetworkInterfaceState) RpcRenewDHCPLease() error {
	if s.dhcpClient == nil {
		return fmt.Errorf("dhcp client not initialized")
	}

	return s.dhcpClient.Renew()
}
