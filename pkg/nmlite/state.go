package nmlite

import "net"

func (nm *NetworkManager) IsOnline() bool {
	for _, iface := range nm.interfaces {
		if iface.IsOnline() {
			return true
		}
	}
	return false
}

func (nm *NetworkManager) IsUp() bool {
	for _, iface := range nm.interfaces {
		if iface.IsUp() {
			return true
		}
	}
	return false
}

func (nm *NetworkManager) Hostname() string {
	return nm.resolvConf.Hostname()
}

func (nm *NetworkManager) FQDN() string {
	return nm.resolvConf.FQDN()
}

func (nm *NetworkManager) NTPServers() []net.IP {
	servers := []net.IP{}
	for _, iface := range nm.interfaces {
		servers = append(servers, iface.NTPServers()...)
	}
	return servers
}

func (nm *NetworkManager) NTPServerStrings() []string {
	servers := []string{}
	for _, server := range nm.NTPServers() {
		servers = append(servers, server.String())
	}
	return servers
}

func (nm *NetworkManager) GetIPv4Addresses() []string {
	for _, iface := range nm.interfaces {
		return iface.GetIPv4Addresses()
	}
	return []string{}
}

func (nm *NetworkManager) GetIPv4Address() string {
	for _, iface := range nm.interfaces {
		return iface.GetIPv4Address()
	}
	return ""
}

func (nm *NetworkManager) GetIPv6Address() string {
	for _, iface := range nm.interfaces {
		return iface.GetIPv6Address()
	}
	return ""
}

func (nm *NetworkManager) GetIPv6Addresses() []string {
	for _, iface := range nm.interfaces {
		return iface.GetIPv6Addresses()
	}
	return []string{}
}

func (nm *NetworkManager) GetMACAddress() string {
	for _, iface := range nm.interfaces {
		return iface.GetMACAddress()
	}
	return ""
}

func (nm *NetworkManager) IPv4Ready() bool {
	for _, iface := range nm.interfaces {
		return iface.IPv4Ready()
	}
	return false
}

func (nm *NetworkManager) IPv6Ready() bool {
	for _, iface := range nm.interfaces {
		return iface.IPv6Ready()
	}
	return false
}

func (nm *NetworkManager) IPv4String() string {
	return nm.GetIPv4Address()
}

func (nm *NetworkManager) IPv6String() string {
	return nm.GetIPv6Address()
}

func (nm *NetworkManager) MACString() string {
	return nm.GetMACAddress()
}
