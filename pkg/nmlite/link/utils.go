package link

import (
	"fmt"
	"net"
	"strings"
)

// ParseIPv4Netmask parses an IPv4 netmask string and returns the IPNet
func ParseIPv4Netmask(address, netmask string) (*net.IPNet, error) {
	if strings.Contains(address, "/") {
		_, ipNet, err := net.ParseCIDR(address)
		if err != nil {
			return nil, fmt.Errorf("invalid IPv4 address: %s", address)
		}
		return ipNet, nil
	}

	ip := net.ParseIP(address)
	if ip == nil {
		return nil, fmt.Errorf("invalid IPv4 address: %s", address)
	}
	if ip.To4() == nil {
		return nil, fmt.Errorf("not an IPv4 address: %s", address)
	}

	mask := net.ParseIP(netmask)
	if mask == nil {
		return nil, fmt.Errorf("invalid IPv4 netmask: %s", netmask)
	}
	if mask.To4() == nil {
		return nil, fmt.Errorf("not an IPv4 netmask: %s", netmask)
	}

	return &net.IPNet{
		IP:   ip,
		Mask: net.IPv4Mask(mask[12], mask[13], mask[14], mask[15]),
	}, nil
}

// ParseIPv6Prefix parses an IPv6 address and prefix length
func ParseIPv6Prefix(address string, prefixLength int) (*net.IPNet, error) {
	var ipNet net.IPMask = nil
	if strings.Contains(address, "/") {
		_, ipCidr, err := net.ParseCIDR(address)
		if err != nil {
			return nil, fmt.Errorf("invalid IPv6 address: %s", address)
		}
		address = address[:strings.Index(address, "/")]
		ipNet = ipCidr.Mask
	}

	ip := net.ParseIP(address)
	if ip == nil {
		return nil, fmt.Errorf("invalid IPv6 address: %s", address)
	}
	if ip.To16() == nil || ip.To4() != nil {
		return nil, fmt.Errorf("not an IPv6 address: %s", address)
	}

	if ipNet == nil && (prefixLength < 0 || prefixLength > 128) {
		return nil, fmt.Errorf("invalid IPv6 prefix length: %d (must be 0-128)", prefixLength)
	}

	if ipNet == nil {
		ipNet = net.CIDRMask(prefixLength, 128)
	}

	return &net.IPNet{
		IP:   ip,
		Mask: ipNet,
	}, nil
}

// ValidateIPAddress validates an IP address
func ValidateIPAddress(address string, isIPv6 bool) error {
	ip := net.ParseIP(address)
	if ip == nil {
		return fmt.Errorf("invalid IP address: %s", address)
	}

	if isIPv6 {
		if ip.To16() == nil || ip.To4() != nil {
			return fmt.Errorf("not an IPv6 address: %s", address)
		}
	} else {
		if ip.To4() == nil {
			return fmt.Errorf("not an IPv4 address: %s", address)
		}
	}

	return nil
}
