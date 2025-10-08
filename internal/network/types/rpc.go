package types

import "time"

type RpcIPv6Address struct {
	Address           string     `json:"address"`
	Prefix            string     `json:"prefix"`
	ValidLifetime     *time.Time `json:"valid_lifetime"`
	PreferredLifetime *time.Time `json:"preferred_lifetime"`
	Scope             int        `json:"scope"`
}

type RpcInterfaceState struct {
	InterfaceState
	IPv6Addresses []RpcIPv6Address `json:"ipv6_addresses"`
}

func (s *InterfaceState) ToRpcInterfaceState() *RpcInterfaceState {
	addrs := make([]RpcIPv6Address, len(s.IPv6Addresses))
	for i, addr := range s.IPv6Addresses {
		addrs[i] = RpcIPv6Address{
			Address:           addr.Address.String(),
			Prefix:            addr.Prefix.String(),
			ValidLifetime:     addr.ValidLifetime,
			PreferredLifetime: addr.PreferredLifetime,
			Scope:             addr.Scope,
		}
	}
	return &RpcInterfaceState{
		InterfaceState: *s,
		IPv6Addresses:  addrs,
	}
}
