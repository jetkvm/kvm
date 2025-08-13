package network

import (
	"bytes"
	"fmt"
	"net"
)

var (
	netMask32 = net.IPv4Mask(255, 255, 255, 255)
)

func parseIP(ip string, family int) (net.IP, error) {
	addr := net.ParseIP(ip)
	if addr == nil {
		return nil, fmt.Errorf("failed to parse IP: %s", ip)
	}

	switch family {
	case AF_INET:
		ia := addr.To4()
		if ia == nil {
			return nil, fmt.Errorf("address is not a valid IPv4 address")
		}
		return ia, nil
	case AF_INET6:
		ia := addr.To16()
		if ia == nil {
			return nil, fmt.Errorf("address is not a valid IPv6 address")
		}
		return ia, nil
	default:
		return nil, fmt.Errorf("invalid family: %d", family)
	}
}

func parseAndValidateUnicastIP(ip string, family int) (net.IP, error) {
	addr, err := parseIP(ip, family)
	if err != nil {
		return nil, err
	}

	if !addr.IsGlobalUnicast() {
		return nil, fmt.Errorf("address is not a global unicast address")
	}

	return addr, nil
}

func isValidNetMask(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return false
	}
	var m net.IPMask
	if v4 := ip.To4(); v4 != nil {
		m = net.IPMask(v4) // 4 bytes
	} else {
		v6 := ip.To16()
		if v6 == nil {
			return false
		}
		m = net.IPMask(v6) // 16 bytes
	}
	ones, bits := m.Size()
	// Non-contiguous masks return (0,0). /0 is valid and returns (0, 32|128).
	return !(ones == 0 && bits == 0)
}

type parsedIPv4Config struct {
	address net.IP
	netmask net.IPMask
	network net.IPNet
	gateway net.IP
}

type parsedIPv6Config struct {
	address net.IP
	prefix  net.IPNet
	gateway net.IP
}

func parseAndValidateStaticIPv4Config(config *IPv4StaticConfig) (*parsedIPv4Config, error) {
	addr, err := parseAndValidateUnicastIP(config.Address.String, AF_INET)
	if err != nil {
		return nil, fmt.Errorf("failed to parse address: %w", err)
	}

	netmask, err := parseIP(config.Netmask.String, AF_INET)
	if err != nil {
		return nil, fmt.Errorf("failed to parse netmask: %w", err)
	}
	if !isValidNetMask(config.Netmask.String) {
		return nil, fmt.Errorf("netmask is not a valid netmask")
	}

	gateway, err := parseAndValidateUnicastIP(config.Gateway.String, AF_INET)
	if err != nil {
		return nil, fmt.Errorf("failed to parse gateway: %w", err)
	}

	if addr.Equal(gateway) {
		return nil, fmt.Errorf("address and gateway cannot be the same")
	}

	netMask := net.IPMask(netmask)

	ipNet := net.IPNet{
		IP:   addr,
		Mask: netMask,
	}

	if !ipNet.Contains(gateway) && !bytes.Equal(ipNet.Mask, netMask32) {
		return nil, fmt.Errorf("address is not in the same subnet as the gateway")
	}

	return &parsedIPv4Config{
		address: addr,
		netmask: netMask,
		network: ipNet,
		gateway: gateway,
	}, nil
}

func parseAndValidateStaticIPv6Config(config *IPv6StaticConfig) (*parsedIPv6Config, error) {
	addr, prefix, err := net.ParseCIDR(fmt.Sprintf("%s/%d", config.Address.String, config.PrefixLength.Int64))
	if err != nil {
		return nil, fmt.Errorf("failed to parse prefix: %w", err)
	}

	if !addr.IsGlobalUnicast() {
		return nil, fmt.Errorf("address is not a global unicast address")
	}

	gateway, err := parseIP(config.Gateway.String, AF_INET6)
	if err != nil {
		return nil, fmt.Errorf("failed to parse gateway: %w", err)
	}

	if addr.Equal(gateway) {
		return nil, fmt.Errorf("address and gateway cannot be the same")
	}

	if !prefix.Contains(gateway) && !gateway.IsLinkLocalUnicast() {
		return nil, fmt.Errorf("gateway is not in the same subnet as the address or is a link-local address")
	}

	return &parsedIPv6Config{
		address: addr,
		prefix:  *prefix,
		gateway: gateway,
	}, nil
}

func validateIPv4Config(config *NetworkConfig) error {
	switch config.IPv4Mode.String {
	case "static":
		_, err := parseAndValidateStaticIPv4Config(config.IPv4Static)
		return err
	case "dhcp":
		return nil
	case "disabled":
		return nil
	default:
		return fmt.Errorf("invalid IPv4 mode: %s", config.IPv4Mode.String)
	}
}

func validateIPv6Config(config *NetworkConfig) error {
	switch config.IPv6Mode.String {
	case "static":
		_, err := parseAndValidateStaticIPv6Config(config.IPv6Static)
		return err
	case "slaac":
		return nil
	case "dhcpv6":
		return fmt.Errorf("not implemented")
	case "slaac_and_dhcpv6":
		return fmt.Errorf("not implemented")
	case "link_local":
		return nil
	case "disabled":
		return nil
	default:
		return fmt.Errorf("invalid IPv6 mode: %s", config.IPv6Mode.String)
	}
}
