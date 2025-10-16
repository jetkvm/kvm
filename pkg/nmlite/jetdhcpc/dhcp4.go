package jetdhcpc

import (
	"fmt"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
	"github.com/vishvananda/netlink"
)

func (c *Client) requestLease4(ifname string) (*Lease, error) {
	iface, err := netlink.LinkByName(ifname)
	if err != nil {
		return nil, err
	}

	l := c.l.With().Str("interface", ifname).Logger()

	mods := []nclient4.ClientOpt{
		nclient4.WithTimeout(c.cfg.Timeout),
		nclient4.WithRetry(c.cfg.Retries),
	}
	mods = append(mods, c.getDHCP4Logger(ifname))
	if c.cfg.V4ServerAddr != nil {
		mods = append(mods, nclient4.WithServerAddr(c.cfg.V4ServerAddr))
	}

	client, err := nclient4.New(ifname, mods...)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	// Prepend modifiers with default options, so they can be overridden.
	reqmods := append(
		[]dhcpv4.Modifier{
			dhcpv4.WithOption(dhcpv4.OptClassIdentifier(VendorIdentifier)),
			dhcpv4.WithRequestedOptions(
				dhcpv4.OptionSubnetMask,
				dhcpv4.OptionInterfaceMTU,
				dhcpv4.OptionNTPServers,
				dhcpv4.OptionDomainName,
				dhcpv4.OptionDomainNameServer,
				dhcpv4.OptionDNSDomainSearchList,
			),
		},
		c.cfg.Modifiers4...)

	if c.cfg.V4ClientIdentifier {
		// Client Id is hardware type + mac per RFC 2132 9.14.
		ident := []byte{0x01} // Type ethernet
		ident = append(ident, iface.Attrs().HardwareAddr...)
		reqmods = append(reqmods, dhcpv4.WithOption(dhcpv4.OptClientIdentifier(ident)))
	}

	if c.cfg.Hostname != "" {
		reqmods = append(reqmods, dhcpv4.WithOption(dhcpv4.OptHostName(c.cfg.Hostname)))
	}

	l.Info().Msg("attempting to get DHCPv4 lease")
	var (
		lease  *nclient4.Lease
		reqErr error
	)
	if c.currentLease4 != nil {
		l.Info().Msg("current lease is not nil, renewing")
		lease, reqErr = client.Renew(c.ctx, c.currentLease4.p4, reqmods...)
	} else {
		l.Info().Msg("current lease is nil, requesting new lease")
		lease, reqErr = client.Request(c.ctx, reqmods...)
	}

	if reqErr != nil {
		return nil, reqErr
	}

	if lease == nil || lease.ACK == nil {
		return nil, fmt.Errorf("failed to acquire DHCPv4 lease")
	}

	summaryStructured(lease.ACK, &l).Info().Msgf("DHCPv4 lease acquired: %s", lease.ACK.String())
	l.Trace().Interface("options", lease.ACK.Options.String()).Msg("DHCPv4 lease options")

	return fromNclient4Lease(lease, ifname), nil
}
