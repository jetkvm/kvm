package types

import (
	"net"
	"slices"
	"time"

	"github.com/vishvananda/netlink"
)

// IPAddress represents a network interface address
type IPAddress struct {
	Family    int
	Address   net.IPNet
	Gateway   net.IP
	MTU       int
	Secondary bool
	Permanent bool
}

func (a *IPAddress) String() string {
	return a.Address.String()
}

func (a *IPAddress) Compare(n netlink.Addr) bool {
	if !a.Address.IP.Equal(n.IP) {
		return false
	}
	if slices.Compare(a.Address.Mask, n.Mask) != 0 {
		return false
	}
	return true
}

func (a *IPAddress) NetlinkAddr() netlink.Addr {
	return netlink.Addr{
		IPNet: &a.Address,
	}
}

func (a *IPAddress) DefaultRoute(linkIndex int) netlink.Route {
	return netlink.Route{
		Dst:       nil,
		Gw:        a.Gateway,
		LinkIndex: linkIndex,
	}
}

// ParsedIPConfig represents the parsed IP configuration
type ParsedIPConfig struct {
	Addresses   []IPAddress
	Nameservers []net.IP
	SearchList  []string
	Domain      string
	MTU         int
	Interface   string
}

// IPv6Address represents an IPv6 address with lifetime information
type IPv6Address struct {
	Address           net.IP     `json:"address"`
	Prefix            net.IPNet  `json:"prefix"`
	ValidLifetime     *time.Time `json:"valid_lifetime"`
	PreferredLifetime *time.Time `json:"preferred_lifetime"`
	Flags             int        `json:"flags"`
	Scope             int        `json:"scope"`
}

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
