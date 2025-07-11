package confparser

import (
	"net"
	"testing"
	"time"

	"github.com/guregu/null/v6"
)

type testIPv6Address struct { //nolint:unused
	Address           net.IP     `json:"address"`
	Prefix            net.IPNet  `json:"prefix"`
	ValidLifetime     *time.Time `json:"valid_lifetime"`
	PreferredLifetime *time.Time `json:"preferred_lifetime"`
	Scope             int        `json:"scope"`
}

type testIPv4StaticConfig struct {
	Address null.String `json:"address" validate_type:"ipv4" required:"true"`
	Netmask null.String `json:"netmask" validate_type:"ipv4" required:"true"`
	Gateway null.String `json:"gateway" validate_type:"ipv4" required:"true"`
	DNS     []string    `json:"dns" validate_type:"ipv4" required:"true"`
}

type testIPv6StaticConfig struct {
	Address null.String `json:"address" validate_type:"ipv6" required:"true"`
	Prefix  null.String `json:"prefix" validate_type:"ipv6" required:"true"`
	Gateway null.String `json:"gateway" validate_type:"ipv6" required:"true"`
	DNS     []string    `json:"dns" validate_type:"ipv6" required:"true"`
}
type testNetworkConfig struct {
	Hostname null.String `json:"hostname,omitempty"`
	Domain   null.String `json:"domain,omitempty"`

	IPv4Mode   null.String           `json:"ipv4_mode" one_of:"dhcp,static,disabled" default:"dhcp"`
	IPv4Static *testIPv4StaticConfig `json:"ipv4_static,omitempty" required_if:"IPv4Mode=static"`

	IPv6Mode   null.String           `json:"ipv6_mode" one_of:"slaac,dhcpv6,slaac_and_dhcpv6,static,link_local,disabled" default:"slaac"`
	IPv6Static *testIPv6StaticConfig `json:"ipv6_static,omitempty" required_if:"IPv6Mode=static"`

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

func TestValidateConfig(t *testing.T) {
	config := &testNetworkConfig{}

	err := SetDefaultsAndValidate(config)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateIPv4StaticConfigNetmaskRequiredIfStatic(t *testing.T) {
	config := &testNetworkConfig{
		IPv4Static: &testIPv4StaticConfig{
			Address: null.StringFrom("192.168.1.1"),
			Gateway: null.StringFrom("192.168.1.1"),
		},
		IPv4Mode: null.StringFrom("static"),
	}

	err := SetDefaultsAndValidate(config)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestValidateIPv4StaticConfigNetmaskNotRequiredIfStatic(t *testing.T) {
	config := &testNetworkConfig{
		IPv4Static: &testIPv4StaticConfig{
			Address: null.StringFrom("192.168.1.1"),
			Gateway: null.StringFrom("192.168.1.1"),
		},
	}

	err := SetDefaultsAndValidate(config)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateIPv4StaticConfigRequiredIf(t *testing.T) {
	config := &testNetworkConfig{
		IPv4Mode: null.StringFrom("static"),
	}

	err := SetDefaultsAndValidate(config)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestValidateIPv4StaticConfigValidateType(t *testing.T) {
	config := &testNetworkConfig{
		IPv4Static: &testIPv4StaticConfig{
			Address: null.StringFrom("X"),
			Netmask: null.StringFrom("255.255.255.0"),
			Gateway: null.StringFrom("192.168.1.1"),
			DNS:     []string{"8.8.8.8", "8.8.4.4"},
		},
		IPv4Mode: null.StringFrom("static"),
	}

	err := SetDefaultsAndValidate(config)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}
