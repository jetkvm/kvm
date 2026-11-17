package kvm

import (
	"fmt"

	"github.com/jetkvm/kvm/internal/mdns"
)

var (
	mDNS                  *mdns.MDNS
	webServerLoopbackOnly bool
)

func initMdns() error {
	webServerLoopbackOnly = config.LocalLoopbackOnly

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

func getMdnsOptions() *mdns.MDNSOptions {
	if networkManager == nil {
		return nil
	}

	hostname := networkManager.Hostname()
	if hostname == "" {
		hostname = GetDefaultHostname()
	}

	return mdns.DeviceOptions(mdns.DeviceState{
		Hostname:     hostname,
		FQDN:         networkManager.FQDN(),
		Mode:         config.NetworkConfig.MDNSMode.String,
		TLSEnabled:   config.TLSMode != "",
		LoopbackOnly: webServerLoopbackOnly,
		SetupDone:    config.LocalAuthMode != "",
		DeviceID:     GetDeviceID(),
		Version:      GetBuiltAppVersion(),
	})
}
