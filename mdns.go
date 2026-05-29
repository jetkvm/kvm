package kvm

import (
	"fmt"

	"github.com/jetkvm/kvm/internal/mdns"
)

// JetkvmServiceType is the DNS-SD service type advertised by the
// device. Bonjour clients on macOS/iOS can discover JetKVM devices
// by browsing for this type, e.g. via
// NWBrowser(for: .bonjour(type: "_jetkvm._tcp", domain: nil)).
const JetkvmServiceType = "_jetkvm._tcp"

var mDNS *mdns.MDNS

func initMdns() error {
	options := getMdnsOptions()
	if options == nil {
		return fmt.Errorf("failed to get mDNS options")
	}

	m, err := mdns.NewMDNS(&mdns.MDNSOptions{
		Logger:        logger,
		LocalNames:    options.LocalNames,
		ListenOptions: options.ListenOptions,
		Service:       options.Service,
	})
	if err != nil {
		return err
	}

	// do not start the server yet, as we need to wait for the network state to be set
	mDNS = m

	return nil
}

// getMdnsServicePort returns the user-facing web server port to
// advertise via Bonjour. When TLS is enabled the HTTPS server on
// port 443 is the primary entry point; otherwise it's plain HTTP on
// port 80.
func getMdnsServicePort() int {
	if config != nil && config.TLSMode != "" {
		return 443
	}
	return 80
}

// buildMdnsService constructs the DNS-SD service registration for
// the JetKVM web server. The TXT records expose firmware version,
// device ID, and the setup state so clients can filter unprovisioned
// devices.
func buildMdnsService() *mdns.MDNSService {
	if networkManager == nil {
		return nil
	}

	instance := networkManager.Hostname()
	if instance == "" {
		instance = GetDefaultHostname()
	}

	setup := "false"
	if config != nil && config.LocalAuthMode != "" {
		setup = "true"
	}

	return &mdns.MDNSService{
		Type:     JetkvmServiceType,
		Instance: instance,
		Port:     getMdnsServicePort(),
		TXT: []mdns.TXTEntry{
			mdns.NewTXTString("version", GetBuiltAppVersion()),
			mdns.NewTXTString("id", GetDeviceID()),
			mdns.NewTXTString("setup", setup),
		},
	}
}
