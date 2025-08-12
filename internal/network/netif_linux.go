//go:build linux

package network

import (
	"time"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
)

func (s *NetworkInterfaceState) HandleLinkUpdate(update netlink.LinkUpdate) {
	if update.Link.Attrs().Name == s.interfaceName {
		s.l.Info().Interface("update", update).Msg("interface link update received")
		_ = s.CheckAndUpdateDhcp()
	}
}

func (s *NetworkInterfaceState) Run() error {
	updates := make(chan netlink.LinkUpdate)
	done := make(chan struct{})

	if err := netlink.LinkSubscribe(updates, done); err != nil {
		s.l.Warn().Err(err).Msg("failed to subscribe to link updates")
		return err
	}

	_ = s.setHostnameIfNotSame()

	// run the dhcp client
	go s.dhcpClient.Run() // nolint:errcheck

	if err := s.CheckAndUpdateDhcp(); err != nil {
		return err
	}

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case update := <-updates:
				s.HandleLinkUpdate(update)
			case <-ticker.C:
				_ = s.CheckAndUpdateDhcp()
			case <-done:
				return
			}
		}
	}()

	return nil
}

func netlinkAddrs(iface netlink.Link) ([]netlink.Addr, error) {
	return netlink.AddrList(iface, nl.FAMILY_ALL)
}
