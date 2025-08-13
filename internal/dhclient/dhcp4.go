package dhclient

import (
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
	"github.com/vishvananda/netlink"
)

func (c *Client) requestLease4(iface netlink.Link) (*Lease, error) {
	ifname := iface.Attrs().Name
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

	// Prepend modifiers with default options, so they can be overriden.
	reqmods := append(
		[]dhcpv4.Modifier{
			dhcpv4.WithOption(dhcpv4.OptClassIdentifier(VendorIdentifier)),
			dhcpv4.WithRequestedOptions(dhcpv4.OptionSubnetMask),
		},
		c.cfg.Modifiers4...)

	if c.cfg.V4ClientIdentifier {
		// Client Id is hardware type + mac per RFC 2132 9.14.
		ident := []byte{0x01} // Type ethernet
		ident = append(ident, iface.Attrs().HardwareAddr...)
		reqmods = append(reqmods, dhcpv4.WithOption(dhcpv4.OptClientIdentifier(ident)))
	}

	l.Info().Msg("attempting to get DHCPv4 lease")
	lease, err := client.Request(c.ctx, reqmods...)
	if err != nil {
		return nil, err
	}

	l.Info().Msgf("DHCPv4 lease acquired: %s", lease.ACK.Summary())

	return fromNclient4Lease(lease, ifname), nil
}
