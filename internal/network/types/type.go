package types

import (
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/guregu/null/v6"
)

// IPAddress represents a network interface address
type IPAddress struct {
	Family    int
	Address   net.IPNet
	Gateway   net.IP
	MTU       int
	Secondary bool
	Permanent bool
}

// IPv6Address represents an IPv6 address with lifetime information
type IPv6Address struct {
	Address           net.IP     `json:"address"`
	Prefix            net.IPNet  `json:"prefix"`
	ValidLifetime     *time.Time `json:"valid_lifetime"`
	PreferredLifetime *time.Time `json:"preferred_lifetime"`
	Scope             int        `json:"scope"`
}

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

// NetworkConfig represents the complete network configuration for an interface
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

// MDNSListenOptions represents MDNS listening options
type MDNSListenOptions struct {
	IPv4 bool
	IPv6 bool
}

// GetTransportProxyFunc returns a function for HTTP proxy configuration
func (c *NetworkConfig) GetTransportProxyFunc() func(*http.Request) (*url.URL, error) {
	return func(*http.Request) (*url.URL, error) {
		if c.HTTPProxy.String == "" {
			return nil, nil
		} else {
			proxyUrl, _ := url.Parse(c.HTTPProxy.String)
			return proxyUrl, nil
		}
	}
}

// DHCPLease represents a DHCP lease
type DHCPLease struct {
	InterfaceName string    `json:"interface_name"`
	IPAddress     net.IP    `json:"ip_address"`
	Netmask       net.IP    `json:"netmask"`
	Gateway       net.IP    `json:"gateway"`
	DNS           []net.IP  `json:"dns"`
	SearchList    []string  `json:"search_list"`
	Domain        string    `json:"domain"`
	NTPServers    []net.IP  `json:"ntp_servers"`
	LeaseTime     time.Time `json:"lease_time"`
	RenewalTime   time.Time `json:"renewal_time"`
	RebindingTime time.Time `json:"rebinding_time"`
	ExpiryTime    time.Time `json:"expiry_time"`
}

// InterfaceState represents the current state of a network interface
type InterfaceState struct {
	InterfaceName string        `json:"interface_name"`
	MACAddress    string        `json:"mac_address"`
	Up            bool          `json:"up"`
	Online        bool          `json:"online"`
	IPv4Ready     bool          `json:"ipv4_ready"`
	IPv6Ready     bool          `json:"ipv6_ready"`
	IPv4Address   string        `json:"ipv4_address,omitempty"`
	IPv6Address   string        `json:"ipv6_address,omitempty"`
	IPv6LinkLocal string        `json:"ipv6_link_local,omitempty"`
	IPv4Addresses []string      `json:"ipv4_addresses,omitempty"`
	IPv6Addresses []IPv6Address `json:"ipv6_addresses,omitempty"`
	NTPServers    []net.IP      `json:"ntp_servers,omitempty"`
	DHCPLease4    *DHCPLease    `json:"dhcp_lease,omitempty"`
	DHCPLease6    *DHCPLease    `json:"dhcp_lease6,omitempty"`
	LastUpdated   time.Time     `json:"last_updated"`
}

// NetworkConfig interface for backward compatibility
type NetworkConfigInterface interface {
	InterfaceName() string
	IPv4Addresses() []IPAddress
	IPv6Addresses() []IPAddress
}

func (d *DHCPLease) IsIPv6() bool {
	return d.IPAddress.To4() == nil
}
