package types

import (
	"net/http"
	"net/url"

	"github.com/guregu/null/v6"
)

// IPv4StaticConfig represents static IPv4 configuration
type IPv4StaticConfig struct {
	Address null.String `json:"address,omitempty" validate_type:"ipv4" required:"true"`
	Netmask null.String `json:"netmask,omitempty" validate_type:"ipv4" required:"true"`
	Gateway null.String `json:"gateway,omitempty" validate_type:"ipv4" required:"true"`
	DNS     []string    `json:"dns,omitempty" validate_type:"ipv4" required:"true"`
}

// IPv6StaticConfig represents static IPv6 configuration
type IPv6StaticConfig struct {
	Prefix  null.String `json:"prefix,omitempty" validate_type:"ipv6_prefix" required:"true"`
	Gateway null.String `json:"gateway,omitempty" validate_type:"ipv6" required:"true"`
	DNS     []string    `json:"dns,omitempty" validate_type:"ipv6" required:"true"`
}

// MDNSListenOptions represents MDNS listening options
type MDNSListenOptions struct {
	IPv4 bool
	IPv6 bool
}

// NetworkConfig represents the complete network configuration for an interface
type NetworkConfig struct {
	DHCPClient null.String `json:"dhcp_client,omitempty" one_of:"jetdhcpc,udhcpc" default:"udhcpc"`

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

// GetMDNSMode returns the MDNS mode configuration
func (c *NetworkConfig) GetMDNSMode() *MDNSListenOptions {
	mode := c.MDNSMode.String
	listenOptions := &MDNSListenOptions{
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

// GetTransportProxyFunc returns a function for HTTP proxy configuration
func (c *NetworkConfig) GetTransportProxyFunc() func(*http.Request) (*url.URL, error) {
	return func(*http.Request) (*url.URL, error) {
		if c.HTTPProxy.String == "" {
			return nil, nil
		} else {
			proxyURL, _ := url.Parse(c.HTTPProxy.String)
			return proxyURL, nil
		}
	}
}

// NetworkConfig interface for backward compatibility
type NetworkConfigInterface interface {
	InterfaceName() string
	IPv4Addresses() []IPAddress
	IPv6Addresses() []IPAddress
}
