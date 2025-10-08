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
