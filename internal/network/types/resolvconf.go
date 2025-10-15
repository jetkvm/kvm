package types

import "net"

// InterfaceResolvConf represents the DNS configuration for a network interface
type InterfaceResolvConf struct {
	NameServers []net.IP `json:"nameservers"`
	SearchList  []string `json:"search_list"`
	Domain      string   `json:"domain,omitempty"` // TODO: remove this once we have a better way to handle the domain
	Source      string   `json:"source,omitempty"`
}

// InterfaceResolvConfMap ..
type InterfaceResolvConfMap map[string]InterfaceResolvConf

// ResolvConf represents the DNS configuration for the system
type ResolvConf struct {
	ConfigIPv4 InterfaceResolvConfMap `json:"config_ipv4"`
	ConfigIPv6 InterfaceResolvConfMap `json:"config_ipv6"`
	Domain     string                 `json:"domain"`
	HostName   string                 `json:"host_name"`
}
