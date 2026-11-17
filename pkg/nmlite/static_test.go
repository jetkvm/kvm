package nmlite

import (
	"io"
	"testing"

	"github.com/guregu/null/v6"
	"github.com/jetkvm/kvm/internal/confparser"
	"github.com/jetkvm/kvm/internal/network/types"
	"github.com/rs/zerolog"
)

func TestIPv4StaticOptionalGatewayAndDNS(t *testing.T) {
	logger := zerolog.New(io.Discard)
	manager, err := NewStaticConfigManager("eth0", &logger)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		gateway string
		dns     []string
		invalid bool
	}{
		{name: "local subnet"},
		{name: "gateway without DNS", gateway: "192.0.2.1"},
		{name: "DNS without gateway", dns: []string{"192.0.2.53"}},
		{name: "gateway and DNS", gateway: "192.0.2.1", dns: []string{"192.0.2.53"}},
		{name: "invalid gateway", gateway: "invalid", invalid: true},
		{name: "IPv6 gateway", gateway: "2001:db8::1", invalid: true},
		{name: "invalid DNS", dns: []string{"invalid"}, invalid: true},
		{name: "IPv6 DNS", dns: []string{"2001:db8::53"}, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := &types.IPv4StaticConfig{
				Address: null.StringFrom("192.0.2.2"),
				Netmask: null.StringFrom("255.255.255.0"),
				Gateway: null.StringFrom(test.gateway),
				DNS:     test.dns,
			}
			validation := confparser.SetDefaultsAndValidate(config)
			parsed, err := manager.ToIPv4Static(config)
			if test.invalid {
				if validation == nil || err == nil {
					t.Fatalf("invalid input accepted: validation=%v, parse=%v", validation, err)
				}
				return
			}
			if validation != nil || err != nil {
				t.Fatalf("valid input rejected: validation=%v, parse=%v", validation, err)
			}
			if len(parsed.Addresses) != 1 || parsed.Addresses[0].Address.String() != "192.0.2.2/24" {
				t.Fatalf("unexpected address: %+v", parsed.Addresses)
			}
			gateway := parsed.Addresses[0].Gateway
			if test.gateway == "" && gateway != nil {
				t.Fatalf("unexpected default gateway: %v", gateway)
			}
			if test.gateway != "" && gateway.String() != test.gateway {
				t.Fatalf("gateway = %v, want %s", gateway, test.gateway)
			}
			if len(parsed.Nameservers) != len(test.dns) {
				t.Fatalf("unexpected DNS servers: %v", parsed.Nameservers)
			}
		})
	}
}
