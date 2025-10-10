package types

import (
	"time"

	"golang.org/x/sys/unix"
)

// RpcIPv6Address is the RPC representation of an IPv6 address
type RpcIPv6Address struct {
	Address           string     `json:"address"`
	Prefix            string     `json:"prefix"`
	ValidLifetime     *time.Time `json:"valid_lifetime"`
	PreferredLifetime *time.Time `json:"preferred_lifetime"`
	Scope             int        `json:"scope"`
	Flags             int        `json:"flags"`
	FlagSecondary     bool       `json:"flag_secondary"`
	FlagPermanent     bool       `json:"flag_permanent"`
	FlagTemporary     bool       `json:"flag_temporary"`
	FlagStablePrivacy bool       `json:"flag_stable_privacy"`
	FlagDeprecated    bool       `json:"flag_deprecated"`
	FlagOptimistic    bool       `json:"flag_optimistic"`
	FlagDADFailed     bool       `json:"flag_dad_failed"`
	FlagTentative     bool       `json:"flag_tentative"`
}

// RpcInterfaceState is the RPC representation of an interface state
type RpcInterfaceState struct {
	InterfaceState
	IPv6Addresses []RpcIPv6Address `json:"ipv6_addresses"`
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
