package types

import (
	"net"
	"time"

	"github.com/rs/zerolog"
)

// DHCPClient is the interface for a DHCP client.
type DHCPClient interface {
	Domain() string
	Lease4() *DHCPLease
	Lease6() *DHCPLease
	Renew() error
	Release() error
	SetIPv4(enabled bool)
	SetIPv6(enabled bool)
	SetOnLeaseChange(callback func(lease *DHCPLease))
	Start() error
	Stop() error
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
	Routers           IPs           `env:"router" json:"routers,omitempty"`            // A list of routers
	DNS               IPs           `env:"dns" json:"dns_servers,omitempty"`           // A list of DNS servers
	NTPServers        IPs           `env:"ntpsrv" json:"ntp_servers,omitempty"`        // A list of NTP servers
	LPRServers        IPs           `env:"lprsvr" json:"lpr_servers,omitempty"`        // A list of LPR servers
	TimeServers       IPs           `env:"timesvr" json:"_time_servers,omitempty"`     // A list of time servers (obsolete)
	IEN116NameServers IPs           `env:"namesvr" json:"_name_servers,omitempty"`     // A list of IEN 116 name servers (obsolete)
	LogServers        IPs           `env:"logsvr" json:"_log_servers,omitempty"`       // A list of MIT-LCS UDP log servers (obsolete)
	CookieServers     IPs           `env:"cookiesvr" json:"_cookie_servers,omitempty"` // A list of RFC 865 cookie servers (obsolete)
	WINSServers       IPs           `env:"wins" json:"_wins_servers,omitempty"`        // A list of WINS servers
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

func (d DHCPLease) MarshalZerologObject(e *zerolog.Event) {
	e.IPAddr("ip_address", d.IPAddress)
	e.IPAddr("Netmask", d.Netmask)
	e.IPAddr("broadcast", d.Broadcast)
	e.Int("ttl", d.TTL)
	e.Int("mtu", d.MTU)
	e.Str("hostname", d.HostName)
	e.Str("domain", d.Domain)
	e.Strs("search_list", d.SearchList)
	e.IPAddr("bootp_next_server", d.BootPNextServer)
	e.Str("bootp_server_name", d.BootPServerName)
	e.Str("bootp_file", d.BootPFile)
	e.Str("timezone", d.Timezone)
	//TODO IPAddrs
	e.Array("routers", d.Routers)
	e.Array("dns_servers", d.DNS)
	e.Array("ntp_servers", d.NTPServers)
	e.Array("lpr_servers", d.LPRServers)
	e.Array("time_servers", d.TimeServers)
	e.Array("name_servers", d.IEN116NameServers)
	e.Array("log_servers", d.LogServers)
	e.Array("cookie_servers", d.CookieServers)
	e.Array("wins_servers", d.WINSServers)
	e.IPAddr("swap_servers", d.SwapServer)
	e.Int("bootsize", d.BootSize)
	e.Str("root_path", d.RootPath)
	e.Dur("lease", d.LeaseTime)
	e.Dur("renewal", d.RenewalTime)
	e.Dur("rebinding", d.RebindingTime)
	e.Str("dhcp_type", d.DHCPType)
	e.Str("server_id", d.ServerID)
	e.Str("reason", d.Message)
	e.Str("tftp", d.TFTPServerName)
	e.Str("bootfile", d.BootFileName)
	e.Dur("uptime", d.Uptime)
	e.Str("class_identifier", d.ClassIdentifier)
	if d.LeaseExpiry != nil {
		e.Time("lease_expiry", *d.LeaseExpiry)
	}
	e.Str("interface_name", d.InterfaceName)
	e.Str("dhcp_client", d.DHCPClient)
}

// IsIPv6 returns true if the DHCP lease is for an IPv6 address
func (d *DHCPLease) IsIPv6() bool {
	return d.IPAddress.To4() == nil
}

// IPMask returns the IP mask for the DHCP lease
func (d *DHCPLease) IPMask() net.IPMask {
	if d.Netmask == nil {
		return nil
	}

	if d.IsIPv6() {
		return net.IPMask(d.Netmask.To16())
	}

	mask := net.ParseIP(d.Netmask.String())
	return net.IPv4Mask(mask[12], mask[13], mask[14], mask[15])
}

// IPNet returns the IP net for the DHCP lease
func (d *DHCPLease) IPNet() *net.IPNet {
	if d.IPAddress == nil || d.Netmask == nil {
		return nil
	}

	return &net.IPNet{
		IP:   d.IPAddress,
		Mask: d.IPMask(),
	}
}
