package jetdhcpc

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/jetkvm/kvm/internal/network/types"
)

var (
	defaultLeaseTime   = time.Duration(30 * time.Minute)
	defaultRenewalTime = time.Duration(15 * time.Minute)
)

// Lease is a network configuration obtained by DHCP.
type Lease struct {
	types.DHCPLease

	p4 *nclient4.Lease
	p6 *dhcpv6.Message

	isEmpty map[string]bool
}

// ToDHCPLease converts a lease to a DHCP lease.
func (l *Lease) ToDHCPLease() *types.DHCPLease {
	lease := &l.DHCPLease
	lease.DHCPClient = "jetdhcpc"
	return lease
}

// fromNclient4Lease creates a lease from a nclient4.Lease.
func fromNclient4Lease(l *nclient4.Lease, iface string) *Lease {
	lease := &Lease{}

	lease.p4 = l

	// only the fields that we need are set
	lease.Routers = l.ACK.Router()
	lease.IPAddress = l.ACK.YourIPAddr

	lease.Netmask = net.IP(l.ACK.SubnetMask())
	lease.Broadcast = l.ACK.BroadcastAddress()

	lease.NTPServers = l.ACK.NTPServers()

	lease.HostName = l.ACK.HostName()
	lease.Domain = l.ACK.DomainName()

	searchList := l.ACK.DomainSearch()
	if searchList != nil {
		lease.SearchList = searchList.Labels
	}

	lease.DNS = l.ACK.DNS()

	lease.ClassIdentifier = l.ACK.ClassIdentifier()
	lease.ServerID = l.ACK.ServerIdentifier().String()

	mtu := l.ACK.Options.Get(dhcpv4.OptionInterfaceMTU)
	if mtu != nil {
		lease.MTU = int(binary.BigEndian.Uint16(mtu))
	}

	lease.Message = l.ACK.Message()
	lease.LeaseTime = l.ACK.IPAddressLeaseTime(defaultLeaseTime)
	lease.RenewalTime = l.ACK.IPAddressRenewalTime(defaultRenewalTime)

	lease.InterfaceName = iface

	return lease
}

// fromNclient6Lease creates a lease from a nclient6.Message.
func fromNclient6Lease(l *dhcpv6.Message, iface string) *Lease {
	lease := &Lease{}

	lease.p6 = l

	iana := l.Options.OneIANA()
	if iana == nil {
		return nil
	}

	address := iana.Options.OneAddress()
	if address == nil {
		return nil
	}

	lease.IPAddress = address.IPv6Addr
	lease.Netmask = net.IP(net.CIDRMask(128, 128))
	lease.DNS = l.Options.DNS()
	// lease.LeaseTime = iana.Options.OnePreferredLifetime()
	// lease.RenewalTime = iana.Options.OneValidLifetime()
	// lease.RebindingTime = iana.Options.OneRebindingTime()

	lease.InterfaceName = iface

	return lease
}

func (l *Lease) setIsEmpty(m map[string]bool) {
	l.isEmpty = m
}

// IsEmpty returns true if the lease is empty for the given key.
func (l *Lease) IsEmpty(key string) bool {
	return l.isEmpty[key]
}

// ToJSON returns the lease as a JSON string.
func (l *Lease) ToJSON() string {
	json, err := json.Marshal(l)
	if err != nil {
		return ""
	}
	return string(json)
}

// SetLeaseExpiry sets the lease expiry time.
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

func (l *Lease) Apply() error {
	if l.p4 != nil {
		return l.applyIPv4()
	}

	if l.p6 != nil {
		return l.applyIPv6()
	}

	return nil
}

func (l *Lease) applyIPv4() error {
	return nil
}

func (l *Lease) applyIPv6() error {
	return nil
}

// UnmarshalDHCPCLease unmarshals a lease from a string.
func UnmarshalDHCPCLease(lease *Lease, str string) error {
	// parse the lease file as a map
	data := make(map[string]string)
	for _, line := range strings.Split(str, "\n") {
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
			for _, ipStr := range strings.Fields(value) {
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

// MarshalDHCPCLease marshals a lease to a string.
func MarshalDHCPCLease(lease *Lease) (string, error) {
	leaseType := reflect.TypeOf(lease).Elem()
	leaseValue := reflect.ValueOf(lease).Elem()

	leaseFile := ""

	for i := 0; i < leaseType.NumField(); i++ {
		field := leaseValue.Field(i)
		key := leaseType.Field(i).Tag.Get("env")
		if key == "" {
			continue
		}

		outValue := ""

		switch field.Interface().(type) {
		case string:
			outValue = field.String()
		case int:
			outValue = strconv.Itoa(int(field.Int()))
		case time.Duration:
			outValue = strconv.Itoa(int(field.Int()))
		case net.IP:
			outValue = field.String()
		case []net.IP:
			ips := field.Interface().([]net.IP)
			ipStrings := make([]string, len(ips))
			for i, ip := range ips {
				ipStrings[i] = ip.String()
			}
			outValue = strings.Join(ipStrings, " ")
		default:
			return "", fmt.Errorf("unsupported field `%s` type: %s", key, field.Type().String())
		}

		leaseFile += fmt.Sprintf("%s=%s\n", key, outValue)
	}

	return leaseFile, nil
}
