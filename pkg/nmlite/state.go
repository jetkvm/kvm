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
	return nm.IsOnline()
}

func (nm *NetworkManager) GetHostname() string {
	return "jetkvm"
}

func (nm *NetworkManager) GetFQDN() string {
	return "jetkvm.local"
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

func (nm *NetworkManager) IPv4String() string {
	l := nm.GetIPv4Addresses()
	if len(l) == 0 {
		return ""
	}
	return l[0]
}

func (nm *NetworkManager) IPv6String() string {
	l := nm.GetIPv6Addresses()
	if len(l) == 0 {
		return ""
	}
	return l[0]
}

func (nm *NetworkManager) MACString() string {
	return nm.GetMACAddress()
}
