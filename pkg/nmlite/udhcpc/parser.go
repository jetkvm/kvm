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

	"github.com/jetkvm/kvm/internal/network/types"
)

type Lease struct {
	types.DHCPLease
	// from https://udhcp.busybox.net/README.udhcpc
	isEmpty map[string]bool
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

// ToDHCPLease converts a lease to a DHCP lease.
func (l *Lease) ToDHCPLease() *types.DHCPLease {
	lease := &l.DHCPLease
	lease.DHCPClient = "udhcpc"
	return lease
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

// UnmarshalDHCPCLease unmarshals a lease from a string.
func UnmarshalDHCPCLease(obj *Lease, str string) error {
	lease := &obj.DHCPLease

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

	obj.setIsEmpty(valuesParsed)

	return nil
}
