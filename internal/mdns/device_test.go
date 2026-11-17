package mdns

import (
	"reflect"
	"testing"

	pion_mdns "github.com/pion/mdns/v2"
)

func TestDeviceOptions(t *testing.T) {
	base := DeviceState{
		Hostname: "jetkvm-abc123",
		FQDN:     "jetkvm-abc123.lan",
		Mode:     "auto",
		DeviceID: "abc123",
		Version:  "0.5.10",
	}
	localNames := []string{"jetkvm-abc123.local", "jetkvm-abc123.lan.local"}

	txt := func(setup string) []pion_mdns.TXTEntry {
		return []pion_mdns.TXTEntry{
			{Key: "id", Value: []byte("abc123")},
			{Key: "setup", Value: []byte(setup)},
			{Key: "version", Value: []byte("0.5.10")},
		}
	}
	service := func(port uint16, setup string) *pion_mdns.ServiceInstance {
		return &pion_mdns.ServiceInstance{
			Instance: "jetkvm-abc123",
			Service:  "_jetkvm._tcp",
			Port:     port,
			Text:     txt(setup),
		}
	}

	tests := []struct {
		name    string
		mutate  func(*DeviceState)
		listen  MDNSListenOptions
		service *pion_mdns.ServiceInstance
	}{
		{
			name:    "default",
			listen:  MDNSListenOptions{IPv4: true, IPv6: true},
			service: service(80, "false"),
		},
		{
			name:    "setup done",
			mutate:  func(d *DeviceState) { d.SetupDone = true },
			listen:  MDNSListenOptions{IPv4: true, IPv6: true},
			service: service(80, "true"),
		},
		{
			name:    "TLS on",
			mutate:  func(d *DeviceState) { d.TLSEnabled = true },
			listen:  MDNSListenOptions{IPv4: true, IPv6: true},
			service: service(443, "false"),
		},
		{
			name:    "loopback only",
			mutate:  func(d *DeviceState) { d.LoopbackOnly = true },
			listen:  MDNSListenOptions{IPv4: true, IPv6: true},
			service: nil,
		},
		{
			name: "loopback only with TLS",
			mutate: func(d *DeviceState) {
				d.LoopbackOnly = true
				d.TLSEnabled = true
			},
			listen:  MDNSListenOptions{IPv4: true, IPv6: true},
			service: nil,
		},
		{
			name:    "IPv4 only",
			mutate:  func(d *DeviceState) { d.Mode = "ipv4_only" },
			listen:  MDNSListenOptions{IPv4: true},
			service: service(80, "false"),
		},
		{
			name:    "IPv6 only",
			mutate:  func(d *DeviceState) { d.Mode = "ipv6_only" },
			listen:  MDNSListenOptions{IPv6: true},
			service: service(80, "false"),
		},
		{
			name:    "mDNS disabled",
			mutate:  func(d *DeviceState) { d.Mode = "disabled" },
			listen:  MDNSListenOptions{},
			service: service(80, "false"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := base
			if tt.mutate != nil {
				tt.mutate(&d)
			}
			opts := DeviceOptions(d)

			if *opts.ListenOptions != tt.listen {
				t.Errorf("listen options = %+v, want %+v", *opts.ListenOptions, tt.listen)
			}

			got := buildServiceInstance(opts.Service, localNames)
			if !reflect.DeepEqual(got, tt.service) {
				t.Errorf("service = %+v, want %+v", got, tt.service)
			}
		})
	}
}

func TestBuildServiceInstanceDefaultsInstanceToFirstLocalName(t *testing.T) {
	got := buildServiceInstance(&MDNSService{Type: "_jetkvm._tcp", Port: 80}, []string{"kvm-desk.local", "kvm-desk.lan.local"})
	if got == nil || got.Instance != "kvm-desk" {
		t.Fatalf("instance = %+v, want kvm-desk", got)
	}
}
