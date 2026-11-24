package kvm

import (
	"net/netip"
	"testing"
)

func TestExternalIPFromHost(t *testing.T) {
	ownAddrs := []netip.Addr{
		netip.MustParseAddr("127.0.0.1"),
		netip.MustParseAddr("10.3.5.31"),
		netip.MustParseAddr("100.100.100.2"),
		netip.MustParseAddr("fe80::1"),
	}

	tests := []struct {
		name     string
		host     string
		ownAddrs []netip.Addr
		want     string
	}{
		{name: "foreign address", host: "10.233.0.2", ownAddrs: ownAddrs, want: "10.233.0.2"},
		{name: "foreign address with port", host: "10.233.0.2:8080", ownAddrs: ownAddrs, want: "10.233.0.2"},
		{name: "own address", host: "10.3.5.31", ownAddrs: ownAddrs, want: ""},
		{name: "own address with port", host: "10.3.5.31:443", ownAddrs: ownAddrs, want: ""},
		{name: "second own address", host: "100.100.100.2", ownAddrs: ownAddrs, want: ""},
		{name: "loopback", host: "127.0.0.1:8080", ownAddrs: ownAddrs, want: ""},
		{name: "hostname", host: "jetkvm.local", ownAddrs: ownAddrs, want: ""},
		{name: "hostname with port", host: "kvm.example.com:8443", ownAddrs: ownAddrs, want: ""},
		{name: "foreign IPv6 literal with brackets", host: "[2001:db8::2]", ownAddrs: ownAddrs, want: ""},
		{name: "foreign IPv6 literal with brackets and port", host: "[2001:db8::2]:8080", ownAddrs: ownAddrs, want: ""},
		{name: "own IPv6 literal with brackets", host: "[fe80::1]:80", ownAddrs: ownAddrs, want: ""},
		{name: "IPv4-mapped IPv6 literal", host: "[::ffff:10.233.0.2]", ownAddrs: ownAddrs, want: ""},
		{name: "empty", host: "", ownAddrs: ownAddrs, want: ""},
		{name: "port only", host: ":80", ownAddrs: ownAddrs, want: ""},
		{name: "not an address", host: "10.233.0", ownAddrs: ownAddrs, want: ""},
		{name: "own addresses unknown", host: "10.233.0.2", ownAddrs: nil, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := externalIPFromHost(tt.host, tt.ownAddrs); got != tt.want {
				t.Fatalf("externalIPFromHost(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}
