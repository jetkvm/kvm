package network

import (
	"fmt"
	"net"
	"time"

	"golang.org/x/net/idna"
)

type IPv6Address struct {
	Address           net.IP     `json:"address"`
	Prefix            net.IPNet  `json:"prefix"`
	ValidLifetime     *time.Time `json:"valid_lifetime"`
	PreferredLifetime *time.Time `json:"preferred_lifetime"`
	Scope             int        `json:"scope"`
}

type NetworkConfig struct {
	Hostname string `json:"hostname,omitempty"`
	Domain   string `json:"domain,omitempty"`

	IPv4Mode   string `json:"ipv4_mode" one_of:"dhcp,static,disabled" default:"dhcp"`
	IPv4Static struct {
		Address string   `json:"address" validate_type:"ipv4"`
		Netmask string   `json:"netmask" validate_type:"ipv4"`
		Gateway string   `json:"gateway" validate_type:"ipv4"`
		DNS     []string `json:"dns" validate_type:"ipv4"`
	} `json:"ipv4_static,omitempty" required_if:"ipv4_mode,static"`

	IPv6Mode   string `json:"ipv6_mode" one_of:"slaac,dhcpv6,slaac_and_dhcpv6,static,link_local,disabled" default:"slaac"`
	IPv6Static struct {
		Address string   `json:"address" validate_type:"ipv6"`
		Netmask string   `json:"netmask" validate_type:"ipv6"`
		Gateway string   `json:"gateway" validate_type:"ipv6"`
		DNS     []string `json:"dns" validate_type:"ipv6"`
	} `json:"ipv6_static,omitempty" required_if:"ipv6_mode,static"`

	LLDPMode                string   `json:"lldp_mode,omitempty" one_of:"disabled,basic,all" default:"basic"`
	LLDPTxTLVs              []string `json:"lldp_tx_tlvs,omitempty" one_of:"chassis,port,system,vlan" default:"chassis,port,system,vlan"`
	MDNSMode                string   `json:"mdns_mode,omitempty" one_of:"disabled,auto,ipv4_only,ipv6_only" default:"auto"`
	TimeSyncMode            string   `json:"time_sync_mode,omitempty" one_of:"ntp_only,ntp_and_http,http_only,custom" default:"ntp_and_http"`
	TimeSyncOrdering        []string `json:"time_sync_ordering,omitempty" one_of:"http,ntp,ntp_dhcp,ntp_user_provided,ntp_fallback" default:"ntp,http"`
	TimeSyncDisableFallback bool     `json:"time_sync_disable_fallback,omitempty" default:"false"`
	TimeSyncParallel        int      `json:"time_sync_parallel,omitempty" default:"4"`
}

func (s *NetworkInterfaceState) GetHostname() string {
	hostname := ToValidHostname(s.config.Hostname)

	if hostname == "" {
		return s.defaultHostname
	}

	return hostname
}

func ToValidDomain(domain string) string {
	ascii, err := idna.Lookup.ToASCII(domain)
	if err != nil {
		return ""
	}

	return ascii
}

func (s *NetworkInterfaceState) GetDomain() string {
	domain := ToValidDomain(s.config.Domain)

	if domain == "" {
		lease := s.dhcpClient.GetLease()
		if lease != nil && lease.Domain != "" {
			domain = ToValidDomain(lease.Domain)
		}
	}

	if domain == "" {
		return "local"
	}

	return domain
}

func (s *NetworkInterfaceState) GetFQDN() string {
	return fmt.Sprintf("%s.%s", s.GetHostname(), s.GetDomain())
}
