package network

import (
	"testing"

	"github.com/guregu/null/v6"
	"github.com/stretchr/testify/assert"
)

func TestValidateInvalidIpv4Netmask(t *testing.T) {
	_, err := parseAndValidateStaticIPv4Config(
		&IPv4StaticConfig{
			Address: null.StringFrom("192.168.1.100"),
			Netmask: null.StringFrom("123.45.67.89"),
			Gateway: null.StringFrom("192.168.1.1"),
		},
	)
	assert.Error(t, err, "expected error, got nil")
}

func TestValidateInvalidIpv4Address(t *testing.T) {
	_, err := parseAndValidateStaticIPv4Config(
		&IPv4StaticConfig{
			Address: null.StringFrom("192.168.1.666"),
			Netmask: null.StringFrom("255.255.255.0"),
			Gateway: null.StringFrom("192.168.1.1"),
		},
	)
	assert.Error(t, err, "expected error, got nil")
}

func TestValidateIpv4GatewaySameAsAddress(t *testing.T) {
	_, err := parseAndValidateStaticIPv4Config(
		&IPv4StaticConfig{
			Address: null.StringFrom("192.168.1.1"),
			Netmask: null.StringFrom("255.255.255.0"),
			Gateway: null.StringFrom("192.168.1.1"),
		},
	)
	assert.Error(t, err, "expected error, got nil")
}

func TestValidateIpv4GatewayNotInNetmask(t *testing.T) {
	_, err := parseAndValidateStaticIPv4Config(
		&IPv4StaticConfig{
			Address: null.StringFrom("192.168.1.100"),
			Netmask: null.StringFrom("255.255.255.0"),
			Gateway: null.StringFrom("192.168.2.1"),
		},
	)
	assert.Error(t, err, "expected error, got nil")
}

func TestValidateIpv4GatewayNotGlobalUnicast(t *testing.T) {
	_, err := parseAndValidateStaticIPv4Config(
		&IPv4StaticConfig{
			Address: null.StringFrom("127.0.0.1"),
			Netmask: null.StringFrom("255.255.255.0"),
			Gateway: null.StringFrom("127.0.0.1"),
		},
	)
	assert.Error(t, err, "expected error, got nil")
}

func TestValidateIpv4GatewayPointToPoint(t *testing.T) {
	_, err := parseAndValidateStaticIPv4Config(
		&IPv4StaticConfig{
			Address: null.StringFrom("192.168.1.100"),
			Netmask: null.StringFrom("255.255.255.255"),
			Gateway: null.StringFrom("203.0.113.1"),
		},
	)
	assert.NoError(t, err, "expected no error, got %v", err)
}

func TestValidateIpv4Config(t *testing.T) {
	_, err := parseAndValidateStaticIPv4Config(
		&IPv4StaticConfig{
			Address: null.StringFrom("192.168.1.100"),
			Netmask: null.StringFrom("255.255.255.0"),
			Gateway: null.StringFrom("192.168.1.1"),
		},
	)
	assert.NoError(t, err, "expected no error, got %v", err)
}

func TestValidateIpv6LinkLocalAddress(t *testing.T) {
	_, err := parseAndValidateStaticIPv6Config(
		&IPv6StaticConfig{
			Address:      null.StringFrom("fe80::114"),
			PrefixLength: null.IntFrom(64),
			Gateway:      null.StringFrom("fe80::1"),
		},
	)
	assert.Error(t, err, "expected error, got nil")
}

func TestValidateIpv6GatewaySameAsAddress(t *testing.T) {
	_, err := parseAndValidateStaticIPv6Config(
		&IPv6StaticConfig{
			Address:      null.StringFrom("2001:0db8::2"),
			PrefixLength: null.IntFrom(64),
			Gateway:      null.StringFrom("2001:0db8::2"),
		},
	)
	assert.Error(t, err, "expected error, got nil")
}

func TestValidateIpv6GatewayNotInPrefix(t *testing.T) {
	_, err := parseAndValidateStaticIPv6Config(
		&IPv6StaticConfig{
			Address:      null.StringFrom("2001:0db8::2"),
			PrefixLength: null.IntFrom(64),
			Gateway:      null.StringFrom("2001:0db8:1::1"),
		},
	)
	assert.Error(t, err, "expected error, got nil")
}

func TestValidateIpv6AddressInvalidPrefixLength(t *testing.T) {
	_, err := parseAndValidateStaticIPv6Config(
		&IPv6StaticConfig{
			Address:      null.StringFrom("2001:0db8::2"),
			PrefixLength: null.IntFrom(1919),
			Gateway:      null.StringFrom("2001:0db8::1"),
		},
	)
	assert.Error(t, err, "expected error, got nil")
}

func TestValidateIpv6GatewayLinkLocal(t *testing.T) {
	_, err := parseAndValidateStaticIPv6Config(
		&IPv6StaticConfig{
			Address:      null.StringFrom("2001:0db8::2"),
			PrefixLength: null.IntFrom(64),
			Gateway:      null.StringFrom("fe80::1"),
		},
	)
	assert.NoError(t, err, "expected no error, got %v", err)
}

func TestValidateIpv6Config(t *testing.T) {
	_, err := parseAndValidateStaticIPv6Config(
		&IPv6StaticConfig{
			Address:      null.StringFrom("2001:0db8::2"),
			PrefixLength: null.IntFrom(64),
			Gateway:      null.StringFrom("2001:0db8::1"),
		},
	)
	assert.NoError(t, err, "expected no error, got %v", err)
}
