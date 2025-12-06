package types

import (
	"net"
	"slices"
	"time"

	"github.com/rs/zerolog"
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

func (ip IPAddress) MarshalZerologObject(e *zerolog.Event) {
	e.Int("family", ip.Family)
	e.IPPrefix("address", ip.Address)
	e.IPAddr("gateway", ip.Gateway)
	e.Int("mtu", int(ip.MTU))
	e.Bool("secondary", ip.Secondary)
	e.Bool("permanent", ip.Permanent)
}

type IPAddresses []IPAddress

func (addrs IPAddresses) MarshalZerologArray(e *zerolog.Array) {
	for _, addr := range addrs {
		e.Object(&addr)
	}
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

type IPs []net.IP

func (addrs IPs) MarshalZerologArray(e *zerolog.Array) {
	for _, addr := range addrs {
		e.IPAddr(addr)
	}
}

// ParsedIPConfig represents the parsed IP configuration
type ParsedIPConfig struct {
	Addresses   IPAddresses
	Nameservers IPs
	SearchList  []string
	Domain      string
	MTU         int
	Interface   string
}

func (a ParsedIPConfig) MarshalZerologObject(e *zerolog.Event) {
	e.Array("addresses", a.Addresses)
	e.Array("nameservers", a.Nameservers)
	e.Strs("search_list", a.SearchList)
	e.Str("domain", a.Domain)
	e.Int("mtu", a.MTU)
	e.Str("interface", a.Interface)
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

func (a IPv6Address) MarshalZerologObject(e *zerolog.Event) {
	e.IPAddr("address", a.Address)
	e.IPPrefix("prefix", a.Prefix)
	if a.ValidLifetime != nil {
		e.Time("valid_lifetime", *a.ValidLifetime)
	}
	if a.PreferredLifetime != nil {
		e.Time("preferred_lifetime", *a.PreferredLifetime)
	}
	e.Int("flags", a.Flags)
	e.Int("scope", a.Scope)
}

type IPv6Addresses []IPv6Address

func (addrs IPv6Addresses) MarshalZerologArray(e *zerolog.Array) {
	for _, addr := range addrs {
		e.Object(addr)
	}
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

func (a RpcIPv6Address) MarshalZerologObject(e *zerolog.Event) {
	e.Str("address", a.Address)
	e.Str("prefix", a.Prefix)
	if a.ValidLifetime != nil {
		e.Time("valid_lifetime", *a.ValidLifetime)
	}
	if a.PreferredLifetime != nil {
		e.Time("preferred_lifetime", *a.PreferredLifetime)
	}
	e.Int("scope", a.Scope)
	e.Int("flags", a.Flags)
	e.Bool("flag_secondary", a.FlagSecondary)
	e.Bool("flag_permanent", a.FlagPermanent)
	e.Bool("flag_temporary", a.FlagTemporary)
	e.Bool("flag_stable_privacy", a.FlagStablePrivacy)
	e.Bool("flag_deprecated", a.FlagDeprecated)
	e.Bool("flag_optimistic", a.FlagOptimistic)
	e.Bool("flag_dad_failed", a.FlagDADFailed)
	e.Bool("flag_tentative", a.FlagTentative)
}

type RpcIPv6Addresses []RpcIPv6Address

func (addrs RpcIPv6Addresses) MarshalZerologArray(e *zerolog.Array) {
	for _, addr := range addrs {
		e.Object(addr)
	}
}
