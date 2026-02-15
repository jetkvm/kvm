package kvm

import (
	"fmt"

	"github.com/jetkvm/kvm/internal/mdns"
)

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
	})
	if err != nil {
		return err
	}

	// do not start the server yet, as we need to wait for the network state to be set
	mDNS = m

	return nil
}
