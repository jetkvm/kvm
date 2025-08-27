package network

import (
	"fmt"
	"net"
	"sync"

	"github.com/jetkvm/kvm/internal/confparser"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/udhcpc"
	"github.com/rs/zerolog"

	"github.com/vishvananda/netlink"
)

type NetworkInterfaceState struct {
	interfaceName string
	interfaceUp   bool
	ipv4Addr      *net.IP
	ipv4Addresses []string
	ipv6Addr      *net.IP
	ipv6Addresses []IPv6Address
	ipv6LinkLocal *net.IP
	ntpAddresses  []*net.IP
	macAddr       *net.HardwareAddr

	l         *zerolog.Logger
	stateLock sync.Mutex

	config     *NetworkConfig
	dhcpClient *udhcpc.DHCPClient

	defaultHostname string
	currentHostname string
	currentFqdn     string

	onStateChange  func(state *NetworkInterfaceState)
	onInitialCheck func(state *NetworkInterfaceState)
	cbConfigChange func(config *NetworkConfig)

	checked bool
}

type NetworkInterfaceOptions struct {
	InterfaceName     string
	DhcpPidFile       string
	Logger            *zerolog.Logger
	DefaultHostname   string
	OnStateChange     func(state *NetworkInterfaceState)
	OnInitialCheck    func(state *NetworkInterfaceState)
	OnDhcpLeaseChange func(lease *udhcpc.Lease)
	OnConfigChange    func(config *NetworkConfig)
	NetworkConfig     *NetworkConfig
}

func NewNetworkInterfaceState(opts *NetworkInterfaceOptions) (*NetworkInterfaceState, error) {
	if opts.NetworkConfig == nil {
		return nil, fmt.Errorf("NetworkConfig can not be nil")
	}

	if opts.DefaultHostname == "" {
		opts.DefaultHostname = "jetkvm"
	}

	err := confparser.SetDefaultsAndValidate(opts.NetworkConfig)
	if err != nil {
		return nil, err
	}

	l := opts.Logger
	s := &NetworkInterfaceState{
		interfaceName:   opts.InterfaceName,
		defaultHostname: opts.DefaultHostname,
		stateLock:       sync.Mutex{},
		l:               l,
		onStateChange:   opts.OnStateChange,
		onInitialCheck:  opts.OnInitialCheck,
		cbConfigChange:  opts.OnConfigChange,
		config:          opts.NetworkConfig,
		ntpAddresses:    make([]*net.IP, 0),
	}

	// create the dhcp client
	dhcpClient := udhcpc.NewDHCPClient(&udhcpc.DHCPClientOptions{
		InterfaceName: opts.InterfaceName,
		PidFile:       opts.DhcpPidFile,
		Logger:        l,
		OnLeaseChange: func(lease *udhcpc.Lease) {
			_, err := s.update()
			if err != nil {
				opts.Logger.Error().Err(err).Msg("failed to update network state")
				return
			}
			_ = s.updateNtpServersFromLease(lease)
			_ = s.setHostnameIfNotSame()

			opts.OnDhcpLeaseChange(lease)
		},
	})

	s.dhcpClient = dhcpClient

	return s, nil
}

func (s *NetworkInterfaceState) IsUp() bool {
	return s.interfaceUp
}

func (s *NetworkInterfaceState) HasIPAssigned() bool {
	return s.ipv4Addr != nil || s.ipv6Addr != nil
}

func (s *NetworkInterfaceState) IsOnline() bool {
	return s.IsUp() && s.HasIPAssigned()
}

func (s *NetworkInterfaceState) IPv4() *net.IP {
	return s.ipv4Addr
}

func (s *NetworkInterfaceState) IPv4String() string {
	if s.ipv4Addr == nil {
		return "..."
	}
	return s.ipv4Addr.String()
}

func (s *NetworkInterfaceState) IPv6() *net.IP {
	return s.ipv6Addr
}

func (s *NetworkInterfaceState) IPv6String() string {
	if s.ipv6Addr == nil {
		return "..."
	}
	return s.ipv6Addr.String()
}

func (s *NetworkInterfaceState) NtpAddresses() []*net.IP {
	return s.ntpAddresses
}

func (s *NetworkInterfaceState) NtpAddressesString() []string {
	ntpServers := []string{}

	if s != nil {
		s.l.Debug().Any("s", s).Msg("getting NTP address strings")

		if len(s.ntpAddresses) > 0 {
			for _, server := range s.ntpAddresses {
				s.l.Debug().IPAddr("server", *server).Msg("converting NTP address")
				ntpServers = append(ntpServers, server.String())
			}
		}
	}

	return ntpServers
}

func (s *NetworkInterfaceState) MAC() *net.HardwareAddr {
	return s.macAddr
}

func (s *NetworkInterfaceState) MACString() string {
	if s.macAddr == nil {
		return ""
	}
	return s.macAddr.String()
}

func (s *NetworkInterfaceState) update() (DhcpTargetState, error) {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	dhcpTargetState := DhcpTargetStateDoNothing

	iface, err := netlink.LinkByName(s.interfaceName)
	if err != nil {
		s.l.Error().Err(err).Msg("failed to get interface")
		return dhcpTargetState, err
	}

	// detect if the interface status changed
	var changed bool
	attrs := iface.Attrs()
	state := attrs.OperState
	newInterfaceUp := state == netlink.OperUp

	// check if the interface is coming up
	interfaceGoingUp := !s.interfaceUp && newInterfaceUp
	interfaceGoingDown := s.interfaceUp && !newInterfaceUp

	if s.interfaceUp != newInterfaceUp {
		s.interfaceUp = newInterfaceUp
		changed = true
	}

	if changed {
		if interfaceGoingUp {
			s.l.Info().Msg("interface state transitioned to up")
			dhcpTargetState = DhcpTargetStateRenew
		} else if interfaceGoingDown {
			s.l.Info().Msg("interface state transitioned to down")
		}
	}

	// set the mac address
	s.macAddr = &attrs.HardwareAddr

	// get the ip addresses
	addrs, err := netlinkAddrs(iface)
	if err != nil {
		return dhcpTargetState, logging.ErrorfL(s.l, "failed to get ip addresses", err)
	}

	var (
		ipv4Addresses       = make([]net.IP, 0)
		ipv4AddressesString = make([]string, 0)
		ipv6Addresses       = make([]IPv6Address, 0)
		// ipv6AddressesString = make([]string, 0)
		ipv6LinkLocal *net.IP
	)

	for _, addr := range addrs {
		if addr.IP.To4() != nil {
			scopedLogger := s.l.With().Str("ipv4", addr.IP.String()).Logger()
			if interfaceGoingDown {
				// remove all IPv4 addresses from the interface.
				scopedLogger.Info().Msg("state transitioned to down, removing IPv4 address")
				err := netlink.AddrDel(iface, &addr)
				if err != nil {
					scopedLogger.Warn().Err(err).Msg("failed to delete address")
				}
				// notify the DHCP client to release the lease
				dhcpTargetState = DhcpTargetStateRelease
				continue
			}
			ipv4Addresses = append(ipv4Addresses, addr.IP)
			ipv4AddressesString = append(ipv4AddressesString, addr.IPNet.String())
		} else if addr.IP.To16() != nil {
			if s.config.IPv6Mode.String == "disabled" {
				continue
			}

			scopedLogger := s.l.With().Str("ipv6", addr.IP.String()).Logger()
			// check if it's a link local address
			if addr.IP.IsLinkLocalUnicast() {
				ipv6LinkLocal = &addr.IP
				continue
			}

			if !addr.IP.IsGlobalUnicast() {
				scopedLogger.Trace().Msg("not a global unicast address, skipping")
				continue
			}

			if interfaceGoingDown {
				scopedLogger.Info().Msg("state transitioned to down, removing IPv6 address")
				err := netlink.AddrDel(iface, &addr)
				if err != nil {
					scopedLogger.Warn().Err(err).Msg("failed to delete address")
				}
				continue
			}
			ipv6Addresses = append(ipv6Addresses, IPv6Address{
				Address:           addr.IP,
				Prefix:            *addr.IPNet,
				ValidLifetime:     lifetimeToTime(addr.ValidLft),
				PreferredLifetime: lifetimeToTime(addr.PreferedLft),
				Scope:             addr.Scope,
			})
			// ipv6AddressesString = append(ipv6AddressesString, addr.IPNet.String())
		}
	}

	if len(ipv4Addresses) > 0 {
		// compare the addresses to see if there's a change
		if s.ipv4Addr == nil || s.ipv4Addr.String() != ipv4Addresses[0].String() {
			scopedLogger := s.l.With().Str("ipv4", ipv4Addresses[0].String()).Logger()
			if s.ipv4Addr != nil {
				scopedLogger.Info().
					Str("old_ipv4", s.ipv4Addr.String()).
					Msg("IPv4 address changed")
			} else {
				scopedLogger.Info().Msg("IPv4 address found")
			}
			s.ipv4Addr = &ipv4Addresses[0]
			changed = true
		}
	}
	s.ipv4Addresses = ipv4AddressesString

	if s.config.IPv6Mode.String != "disabled" {
		if ipv6LinkLocal != nil {
			if s.ipv6LinkLocal == nil || s.ipv6LinkLocal.String() != ipv6LinkLocal.String() {
				scopedLogger := s.l.With().Str("ipv6", ipv6LinkLocal.String()).Logger()
				if s.ipv6LinkLocal != nil {
					scopedLogger.Info().
						Str("old_ipv6", s.ipv6LinkLocal.String()).
						Msg("IPv6 link local address changed")
				} else {
					scopedLogger.Info().Msg("IPv6 link local address found")
				}
				s.ipv6LinkLocal = ipv6LinkLocal
				changed = true
			}
		}
		s.ipv6Addresses = ipv6Addresses

		if len(ipv6Addresses) > 0 {
			// compare the addresses to see if there's a change
			if s.ipv6Addr == nil || s.ipv6Addr.String() != ipv6Addresses[0].Address.String() {
				scopedLogger := s.l.With().Str("ipv6", ipv6Addresses[0].Address.String()).Logger()
				if s.ipv6Addr != nil {
					scopedLogger.Info().
						Str("old_ipv6", s.ipv6Addr.String()).
						Msg("IPv6 address changed")
				} else {
					scopedLogger.Info().Msg("IPv6 address found")
				}
				s.ipv6Addr = &ipv6Addresses[0].Address
				changed = true
			}
		}
	}

	// if it's the initial check, we'll set changed to false
	initialCheck := !s.checked
	if initialCheck {
		s.checked = true
		changed = false
		if dhcpTargetState == DhcpTargetStateRenew {
			// it's the initial check, we'll start the DHCP client
			// dhcpTargetState = DhcpTargetStateStart
			// TODO: manage DHCP client start/stop
			dhcpTargetState = DhcpTargetStateDoNothing
		}
	}

	if initialCheck {
		s.onInitialCheck(s)
	} else if changed {
		s.onStateChange(s)
	}

	return dhcpTargetState, nil
}

func (s *NetworkInterfaceState) updateNtpServersFromLease(lease *udhcpc.Lease) error {
	if lease != nil && len(lease.NTPServers) > 0 {
		s.l.Info().Msg("lease found, updating DHCP NTP addresses")
		s.ntpAddresses = make([]*net.IP, 0, len(lease.NTPServers))

		for _, ntpServer := range lease.NTPServers {
			if ntpServer != nil {
				s.l.Info().IPAddr("ntp_server", ntpServer).Msg("NTP server found in lease")
				s.ntpAddresses = append(s.ntpAddresses, &ntpServer)
			}
		}
	} else {
		s.l.Info().Msg("no NTP servers found in lease")
		s.ntpAddresses = make([]*net.IP, 0, len(s.config.TimeSyncNTPServers))
	}

	return nil
}

func (s *NetworkInterfaceState) CheckAndUpdateDhcp() error {
	dhcpTargetState, err := s.update()
	if err != nil {
		return logging.ErrorfL(s.l, "failed to update network state", err)
	}

	switch dhcpTargetState {
	case DhcpTargetStateRenew:
		s.l.Info().Msg("renewing DHCP lease")
		_ = s.dhcpClient.Renew()
	case DhcpTargetStateRelease:
		s.l.Info().Msg("releasing DHCP lease")
		_ = s.dhcpClient.Release()
	case DhcpTargetStateStart:
		s.l.Warn().Msg("dhcpTargetStateStart not implemented")
	case DhcpTargetStateStop:
		s.l.Warn().Msg("dhcpTargetStateStop not implemented")
	}

	return nil
}

func (s *NetworkInterfaceState) onConfigChange(config *NetworkConfig) {
	_ = s.setHostnameIfNotSame()
	s.cbConfigChange(config)
}
