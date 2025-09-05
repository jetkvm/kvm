package udhcpc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type Lease struct {
	// from https://udhcp.busybox.net/README.udhcpc
	IPAddress         net.IP        `env:"ip" json:"ip"`                               // The obtained IP
	Netmask           net.IP        `env:"subnet" json:"netmask"`                      // The assigned subnet mask
	Broadcast         net.IP        `env:"broadcast" json:"broadcast"`                 // The broadcast address for this network
	TTL               int           `env:"ipttl" json:"ttl,omitempty"`                 // The TTL to use for this network
	MTU               int           `env:"mtu" json:"mtu,omitempty"`                   // The MTU to use for this network
	HostName          string        `env:"hostname" json:"hostname,omitempty"`         // The assigned hostname
	Domain            string        `env:"domain" json:"domain,omitempty"`             // The domain name of the network
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
	DHCPType          string        `env:"dhcptype" json:"dhcp_type,omitempty"`        // DHCP message type (safely ignored)
	ServerID          string        `env:"serverid" json:"server_id,omitempty"`        // The IP of the server
	Message           string        `env:"message" json:"reason,omitempty"`            // Reason for a DHCPNAK
	TFTPServerName    string        `env:"tftp" json:"tftp,omitempty"`                 // The TFTP server name
	BootFileName      string        `env:"bootfile" json:"bootfile,omitempty"`         // The boot file name
	Uptime            time.Duration `env:"uptime" json:"uptime,omitempty"`             // The uptime of the device when the lease was obtained, in seconds
	LeaseExpiry       *time.Time    `json:"lease_expiry,omitempty"`                    // The expiry time of the lease
	isEmpty           map[string]bool
}

func (l *Lease) setIsEmpty(m map[string]bool) {
	l.isEmpty = m
}

func (l *Lease) IsEmpty(key string) bool {
	return l.isEmpty[key]
}

func (l *Lease) ToJSON() string {
	json, err := json.Marshal(l)
	if err != nil {
		return ""
	}
	return string(json)
}

func (l *Lease) SetLeaseExpiry() (time.Time, error) {
	if l.Uptime == 0 || l.LeaseTime == 0 {
		return time.Time{}, fmt.Errorf("uptime or lease time isn't set")
	}

	// get the uptime of the device

	file, err := os.Open("/proc/uptime")
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to open uptime file: %w", err)
	}
	defer file.Close()

	var uptime time.Duration

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		text := scanner.Text()
		parts := strings.Split(text, " ")
		uptime, err = time.ParseDuration(parts[0] + "s")

		if err != nil {
			return time.Time{}, fmt.Errorf("failed to parse uptime: %w", err)
		}
	}

	relativeLeaseRemaining := (l.Uptime + l.LeaseTime) - uptime
	leaseExpiry := time.Now().Add(relativeLeaseRemaining)

	l.LeaseExpiry = &leaseExpiry

	return leaseExpiry, nil
}

func UnmarshalDHCPCLease(lease *Lease, str string) error {
	// parse the lease file as a map
	data := make(map[string]string)
	for line := range strings.SplitSeq(str, "\n") {
		line = strings.TrimSpace(line)
		// skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		data[key] = value
	}

	// now iterate over the lease struct and set the values
	leaseType := reflect.TypeOf(lease).Elem()
	leaseValue := reflect.ValueOf(lease).Elem()

	valuesParsed := make(map[string]bool)

	for i := 0; i < leaseType.NumField(); i++ {
		field := leaseValue.Field(i)

		// get the env tag
		key := leaseType.Field(i).Tag.Get("env")
		if key == "" {
			continue
		}

		valuesParsed[key] = false

		// get the value from the data map
		value, ok := data[key]
		if !ok || value == "" {
			continue
		}

		switch field.Interface().(type) {
		case string:
			field.SetString(value)
		case int:
			val, err := strconv.Atoi(value)
			if err != nil {
				continue
			}
			field.SetInt(int64(val))
		case time.Duration:
			val, err := time.ParseDuration(value + "s")
			if err != nil {
				continue
			}
			field.Set(reflect.ValueOf(val))
		case net.IP:
			ip := net.ParseIP(value)
			if ip == nil {
				continue
			}
			field.Set(reflect.ValueOf(ip))
		case []net.IP:
			val := make([]net.IP, 0)
			for ipStr := range strings.FieldsSeq(value) {
				ip := net.ParseIP(ipStr)
				if ip == nil {
					continue
				}
				val = append(val, ip)
			}
			field.Set(reflect.ValueOf(val))
		default:
			return fmt.Errorf("unsupported field `%s` type: %s", key, field.Type().String())
		}

		valuesParsed[key] = true
	}

	lease.setIsEmpty(valuesParsed)

	return nil
}
