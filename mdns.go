package kvm

import (
	"github.com/jetkvm/kvm/internal/mdns"
)

var mDNS *mdns.MDNS

func initMdns() error {
	m, err := mdns.NewMDNS(&mdns.MDNSOptions{
		Logger: logger,
		LocalNames: []string{
			networkState.GetHostname(),
			networkState.GetFQDN(),
		},
		ListenOptions: &mdns.MDNSListenOptions{
			IPv4: true,
			IPv6: true,
		},
	})
	if err != nil {
		return err
	}

	err = m.Start()
	if err != nil {
		return err
	}

	mDNS = m

	return nil
}
