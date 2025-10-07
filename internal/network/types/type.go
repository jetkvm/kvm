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
	DHCPClient null.String `json:"dhcp_client,omitempty" one_of:"jetdhcpc,udhcpc" default:"jetdhcpc"`

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

// DHCPLease is a network configuration obtained by DHCP.
type DHCPLease struct {
	// from https://udhcp.busybox.net/README.udhcpc
	IPAddress         net.IP        `env:"ip" json:"ip"`                               // The obtained IP
	Netmask           net.IP        `env:"subnet" json:"netmask"`                      // The assigned subnet mask
	Broadcast         net.IP        `env:"broadcast" json:"broadcast"`                 // The broadcast address for this network
	TTL               int           `env:"ipttl" json:"ttl,omitempty"`                 // The TTL to use for this network
	MTU               int           `env:"mtu" json:"mtu,omitempty"`                   // The MTU to use for this network
	HostName          string        `env:"hostname" json:"hostname,omitempty"`         // The assigned hostname
	Domain            string        `env:"domain" json:"domain,omitempty"`             // The domain name of the network
	SearchList        []string      `env:"search" json:"search_list,omitempty"`        // The search list for the network
	BootPNextServer   net.IP        `env:"siaddr" json:"bootp_next_server,omitempty"`  // The bootp next server option
	BootPServerName   string        `env:"sname" json:"bootp_server_name,omitempty"`   // The bootp server name option
	BootPFile         string        `env:"boot_file" json:"bootp_file,omitempty"`      // The bootp boot file option
	Timezone          string        `env:"timezone" json:"timezone,omitempty"`         // Offset in seconds from UTC
	Routers           []net.IP      `env:"router" json:"routers,omitempty"`            // A list of routers
	DNS               []net.IP      `env:"dns" json:"dns_servers,omitempty"`           // A list of DNS servers
	NTPServers        []net.IP      `env:"ntpsrv" json:"ntp_servers,omitempty"`        // A list of NTP servers
	LPRServers        []net.IP      `env:"lprsvr" json:"lpr_servers,omitempty"`        // A list of LPR servers
	TimeServers       []net.IP      `env:"timesvr" json:"_time_servers,omitempty"`     // A list of time servers (obsolete)
	IEN116NameServers []net.IP      `env:"namesvr" json:"_name_servers,omitempty"`     // A list of IEN 116 name servers (obsolete)
	LogServers        []net.IP      `env:"logsvr" json:"_log_servers,omitempty"`       // A list of MIT-LCS UDP log servers (obsolete)
	CookieServers     []net.IP      `env:"cookiesvr" json:"_cookie_servers,omitempty"` // A list of RFC 865 cookie servers (obsolete)
	WINSServers       []net.IP      `env:"wins" json:"_wins_servers,omitempty"`        // A list of WINS servers
	SwapServer        net.IP        `env:"swapsvr" json:"_swap_server,omitempty"`      // The IP address of the client's swap server
	BootSize          int           `env:"bootsize" json:"bootsize,omitempty"`         // The length in 512 octect blocks of the bootfile
	RootPath          string        `env:"rootpath" json:"root_path,omitempty"`        // The path name of the client's root disk
	LeaseTime         time.Duration `env:"lease" json:"lease,omitempty"`               // The lease time, in seconds
	RenewalTime       time.Duration `env:"renewal" json:"renewal,omitempty"`           // The renewal time, in seconds
	RebindingTime     time.Duration `env:"rebinding" json:"rebinding,omitempty"`       // The rebinding time, in seconds
	DHCPType          string        `env:"dhcptype" json:"dhcp_type,omitempty"`        // DHCP message type (safely ignored)
	ServerID          string        `env:"serverid" json:"server_id,omitempty"`        // The IP of the server
	Message           string        `env:"message" json:"reason,omitempty"`            // Reason for a DHCPNAK
	TFTPServerName    string        `env:"tftp" json:"tftp,omitempty"`                 // The TFTP server name
	BootFileName      string        `env:"bootfile" json:"bootfile,omitempty"`         // The boot file name
	Uptime            time.Duration `env:"uptime" json:"uptime,omitempty"`             // The uptime of the device when the lease was obtained, in seconds
	ClassIdentifier   string        `env:"classid" json:"class_identifier,omitempty"`  // The class identifier
	LeaseExpiry       *time.Time    `json:"lease_expiry,omitempty"`                    // The expiry time of the lease

	InterfaceName string `json:"interface_name,omitempty"` // The name of the interface
	DHCPClient    string `json:"dhcp_client,omitempty"`    // The DHCP client that obtained the lease
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

// IsIPv6 returns true if the DHCP lease is for an IPv6 address
func (d *DHCPLease) IsIPv6() bool {
	return d.IPAddress.To4() == nil
}

// IPMask returns the IP mask for the DHCP lease
func (d *DHCPLease) IPMask() net.IPMask {
	if d.IsIPv6() {
		// TODO: not implemented
		return nil
	}

	mask := net.ParseIP(d.Netmask.String())
	return net.IPv4Mask(mask[12], mask[13], mask[14], mask[15])
}

// IPNet returns the IP net for the DHCP lease
func (d *DHCPLease) IPNet() *net.IPNet {
	if d.IsIPv6() {
		// TODO: not implemented
		return nil
	}

	return &net.IPNet{
		IP:   d.IPAddress,
		Mask: d.IPMask(),
	}
}
