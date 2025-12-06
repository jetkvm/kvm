package types

import (
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sys/unix"
)

// InterfaceState represents the current state of a network interface
type InterfaceState struct {
	InterfaceName string        `json:"interface_name"`
	Hostname      string        `json:"hostname"`
	MACAddress    string        `json:"mac_address"`
	Up            bool          `json:"up"`
	Online        bool          `json:"online"`
	IPv4Ready     bool          `json:"ipv4_ready"`
	IPv6Ready     bool          `json:"ipv6_ready"`
	IPv4Address   string        `json:"ipv4_address,omitempty"`
	IPv6Address   string        `json:"ipv6_address,omitempty"`
	IPv6LinkLocal string        `json:"ipv6_link_local,omitempty"`
	IPv6Gateway   string        `json:"ipv6_gateway,omitempty"`
	IPv4Addresses []string      `json:"ipv4_addresses,omitempty"`
	IPv6Addresses IPv6Addresses `json:"ipv6_addresses,omitempty"`
	NTPServers    IPs           `json:"ntp_servers,omitempty"`
	DHCPLease4    *DHCPLease    `json:"dhcp_lease,omitempty"`
	DHCPLease6    *DHCPLease    `json:"dhcp_lease6,omitempty"`
	LastUpdated   time.Time     `json:"last_updated"`
}

func (a InterfaceState) MarshalZerologObject(e *zerolog.Event) {
	e.Str("interface_name", a.InterfaceName)
	e.Str("hostname", a.Hostname)
	e.Str("mac", a.MACAddress)
	e.Bool("up", a.Up)
	e.Bool("online", a.Online)
	e.Bool("ipv4_ready", a.IPv4Ready)
	e.Bool("ipv6_ready", a.IPv6Ready)
	e.Str("ipv4_address", a.IPv4Address)
	e.Str("ipv6_address", a.IPv6Address)
	e.Str("ipv6_link_local", a.IPv6LinkLocal)
	e.Str("ipv6_gateway", a.IPv6Gateway)
	e.Strs("ipv4_addresses", a.IPv4Addresses)
	e.Array("ipv6_addresses", a.IPv6Addresses)
	e.Array("ntp_servers", a.NTPServers)
	// DHCP leases can be nil
	if a.DHCPLease4 != nil {
		e.Object("dhcp_lease", a.DHCPLease4)
	}
	if a.DHCPLease6 != nil {
		e.Object("dhcp_lease6", a.DHCPLease6)
	}
	e.Time("last_updated", a.LastUpdated)
}

// RpcInterfaceState is the RPC representation of an interface state
type RpcInterfaceState struct {
	InterfaceState
	IPv6Addresses RpcIPv6Addresses `json:"ipv6_addresses"`
}

func (s RpcInterfaceState) MarshalZerologObject(e *zerolog.Event) {
	e.Object("interface_state", s.InterfaceState)
	e.Array("ipv6_addresses", s.IPv6Addresses)
}

// ToRpcInterfaceState converts an InterfaceState to a RpcInterfaceState
func (s *InterfaceState) ToRpcInterfaceState() *RpcInterfaceState {
	addrs := make([]RpcIPv6Address, len(s.IPv6Addresses))
	for i, addr := range s.IPv6Addresses {
		addrs[i] = RpcIPv6Address{
			Address:           addr.Address.String(),
			Prefix:            addr.Prefix.String(),
			ValidLifetime:     addr.ValidLifetime,
			PreferredLifetime: addr.PreferredLifetime,
			Scope:             addr.Scope,
			Flags:             addr.Flags,
			FlagSecondary:     addr.Flags&unix.IFA_F_SECONDARY != 0,
			FlagPermanent:     addr.Flags&unix.IFA_F_PERMANENT != 0,
			FlagTemporary:     addr.Flags&unix.IFA_F_TEMPORARY != 0,
			FlagStablePrivacy: addr.Flags&unix.IFA_F_STABLE_PRIVACY != 0,
			FlagDeprecated:    addr.Flags&unix.IFA_F_DEPRECATED != 0,
			FlagOptimistic:    addr.Flags&unix.IFA_F_OPTIMISTIC != 0,
			FlagDADFailed:     addr.Flags&unix.IFA_F_DADFAILED != 0,
			FlagTentative:     addr.Flags&unix.IFA_F_TENTATIVE != 0,
		}
	}
	return &RpcInterfaceState{
		InterfaceState: *s,
		IPv6Addresses:  addrs,
	}
}
