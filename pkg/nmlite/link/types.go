package link

import (
	"net"
)

// IPv4Address represents an IPv4 address and its gateway
type IPv4Address struct {
	Address   net.IPNet
	Gateway   net.IP
	Secondary bool
	Permanent bool
}

// IPv4Config represents the configuration for an IPv4 interface
type IPv4Config struct {
	Addresses   []IPv4Address
	Nameservers []net.IP
	SearchList  []string
	Domain      string
	MTU         int
	Interface   string
}
