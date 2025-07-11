package network

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/guregu/null/v6"
	"github.com/jetkvm/kvm/internal/mdns"
	"golang.org/x/net/idna"
)

type IPv6Address struct {
	Address           net.IP     `json:"address"`
	Prefix            net.IPNet  `json:"prefix"`
	ValidLifetime     *time.Time `json:"valid_lifetime"`
	PreferredLifetime *time.Time `json:"preferred_lifetime"`
	Scope             int        `json:"scope"`
}

type IPv4StaticConfig struct {
	Address null.String `json:"address,omitempty" validate_type:"ipv4" required:"true"`
	Netmask null.String `json:"netmask,omitempty" validate_type:"ipv4" required:"true"`
	Gateway null.String `json:"gateway,omitempty" validate_type:"ipv4" required:"true"`
	DNS     []string    `json:"dns,omitempty" validate_type:"ipv4" required:"true"`
}

type IPv6StaticConfig struct {
	Address null.String `json:"address,omitempty" validate_type:"ipv6" required:"true"`
	Prefix  null.String `json:"prefix,omitempty" validate_type:"ipv6" required:"true"`
	Gateway null.String `json:"gateway,omitempty" validate_type:"ipv6" required:"true"`
	DNS     []string    `json:"dns,omitempty" validate_type:"ipv6" required:"true"`
}
type NetworkConfig struct {
	Hostname  null.String `json:"hostname,omitempty" validate_type:"hostname"`
	HTTPProxy null.String `json:"http_proxy,omitempty" validate_type:"proxy"`
	Domain    null.String `json:"domain,omitempty" validate_type:"hostname"`

	IPv4Mode   null.String       `json:"ipv4_mode,omitempty" one_of:"dhcp,static,disabled" default:"dhcp"`
	IPv4Static *IPv4StaticConfig `json:"ipv4_static,omitempty" required_if:"IPv4Mode=static"`

	IPv6Mode   null.String       `json:"ipv6_mode,omitempty" one_of:"slaac,dhcpv6,slaac_and_dhcpv6,static,link_local,disabled" default:"slaac"`
	IPv6Static *IPv6StaticConfig `json:"ipv6_static,omitempty" required_if:"IPv6Mode=static"`

	LLDPMode                null.String `json:"lldp_mode,omitempty" one_of:"disabled,basic,all" default:"basic"`
	LLDPTxTLVs              []string    `json:"lldp_tx_tlvs,omitempty" one_of:"chassis,port,system,vlan" default:"chassis,port,system,vlan"`
	MDNSMode                null.String `json:"mdns_mode,omitempty" one_of:"disabled,auto,ipv4_only,ipv6_only" default:"auto"`
	TimeSyncMode            null.String `json:"time_sync_mode,omitempty" one_of:"ntp_only,ntp_and_http,http_only,custom" default:"ntp_and_http"`
	TimeSyncOrdering        []string    `json:"time_sync_ordering,omitempty" one_of:"http,ntp,ntp_dhcp,ntp_user_provided,http_user_provided" default:"ntp,http"`
	TimeSyncDisableFallback null.Bool   `json:"time_sync_disable_fallback,omitempty" default:"false"`
	TimeSyncParallel        null.Int    `json:"time_sync_parallel,omitempty" default:"4"`
	TimeSyncNTPServers      []string    `json:"time_sync_ntp_servers,omitempty" validate_type:"ipv4_or_ipv6" required_if:"TimeSyncOrdering=ntp_user_provided"`
	TimeSyncHTTPUrls        []string    `json:"time_sync_http_urls,omitempty" validate_type:"url" required_if:"TimeSyncOrdering=http_user_provided"`
}

func (c *NetworkConfig) GetMDNSMode() *mdns.MDNSListenOptions {
	mode := c.MDNSMode.String
	listenOptions := &mdns.MDNSListenOptions{
		IPv4: true,
		IPv6: true,
	}

	switch mode {
	case "ipv4_only":
		listenOptions.IPv6 = false
	case "ipv6_only":
		listenOptions.IPv4 = false
	case "disabled":
		listenOptions.IPv4 = false
		listenOptions.IPv6 = false
	}

	return listenOptions
}

func (s *NetworkConfig) GetTransportProxyFunc() func(*http.Request) (*url.URL, error) {
	return func(*http.Request) (*url.URL, error) {
		if s.HTTPProxy.String == "" {
			return nil, nil
		} else {
			proxyUrl, _ := url.Parse(s.HTTPProxy.String)
			return proxyUrl, nil
		}
	}
}

func (s *NetworkInterfaceState) GetHostname() string {
	hostname := ToValidHostname(s.config.Hostname.String)

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
	domain := ToValidDomain(s.config.Domain.String)

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
