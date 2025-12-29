package link

import (
	"net"

	"github.com/vishvananda/netlink"
)

// IPv4Address represents an IPv4 address and its gateway
type IPv4Address struct {
	Address   net.IPNet
	Gateway   net.IP
	Secondary bool
	Permanent bool
}

const (
	MainRoutingTable int                   = 254
	DhcpProtocol     netlink.RouteProtocol = 3
	StaticProtocol   netlink.RouteProtocol = 4
)
