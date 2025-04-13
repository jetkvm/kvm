package kvm

import (
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/jetkvm/kvm/internal/udhcpc"
	"github.com/rs/zerolog"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
)

var (
	networkState *NetworkInterfaceState
)

type DhcpTargetState int

const (
	DhcpTargetStateDoNothing DhcpTargetState = iota
	DhcpTargetStateStart
	DhcpTargetStateStop
	DhcpTargetStateRenew
	DhcpTargetStateRelease
)

type NetworkInterfaceState struct {
	interfaceName string
	interfaceUp   bool
	ipv4Addr      *net.IP
	ipv6Addr      *net.IP
	macAddr       *net.HardwareAddr

	l         *zerolog.Logger
	stateLock sync.Mutex

	dhcpClient *udhcpc.DHCPClient

	onStateChange  func(state *NetworkInterfaceState)
	onInitialCheck func(state *NetworkInterfaceState)

	checked bool
}

type RpcNetworkState struct {
	InterfaceName string        `json:"interface_name"`
	MacAddress    string        `json:"mac_address"`
	IPv4          string        `json:"ipv4,omitempty"`
	IPv6          string        `json:"ipv6,omitempty"`
	DHCPLease     *udhcpc.Lease `json:"dhcp_lease,omitempty"`
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

func (s *NetworkInterfaceState) MAC() *net.HardwareAddr {
	return s.macAddr
}

func (s *NetworkInterfaceState) MACString() string {
	if s.macAddr == nil {
		return ""
	}
	return s.macAddr.String()
}

const (
	// TODO: add support for multiple interfaces
	NetIfName = "eth0"
)

func NewNetworkInterfaceState(ifname string) *NetworkInterfaceState {
	logger := networkLogger.With().Str("interface", ifname).Logger()

	s := &NetworkInterfaceState{
		interfaceName: ifname,
		stateLock:     sync.Mutex{},
		l:             &logger,
		onStateChange: func(state *NetworkInterfaceState) {
			go func() {
				waitCtrlClientConnected()
				requestDisplayUpdate()
			}()
		},
		onInitialCheck: func(state *NetworkInterfaceState) {
			go func() {
				waitCtrlClientConnected()
				requestDisplayUpdate()
			}()
		},
	}

	// use a pid file for udhcpc if the system version is 0.2.4 or higher
	dhcpPidFile := ""
	systemVersionLocal, _, _ := GetLocalVersion()
	if systemVersionLocal != nil &&
		systemVersionLocal.Compare(semver.MustParse("0.2.4")) >= 0 {
		dhcpPidFile = fmt.Sprintf("/run/udhcpc.%s.pid", ifname)
	}

	// create the dhcp client
	dhcpClient := udhcpc.NewDHCPClient(&udhcpc.DHCPClientOptions{
		InterfaceName: ifname,
		PidFile:       dhcpPidFile,
		Logger:        &logger,
		OnLeaseChange: func(lease *udhcpc.Lease) {
			_, _ = s.update()
		},
	})

	s.dhcpClient = dhcpClient

	return s
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
	addrs, err := netlink.AddrList(iface, nl.FAMILY_ALL)
	if err != nil {
		s.l.Error().Err(err).Msg("failed to get ip addresses")
		return dhcpTargetState, err
	}

	var (
		ipv4Addresses = make([]net.IP, 0)
		ipv6Addresses = make([]net.IP, 0)
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
		} else if addr.IP.To16() != nil {
			scopedLogger := s.l.With().Str("ipv6", addr.IP.String()).Logger()
			// check if it's a link local address
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
			ipv6Addresses = append(ipv6Addresses, addr.IP)
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

	if len(ipv6Addresses) > 0 {
		// compare the addresses to see if there's a change
		if s.ipv6Addr == nil || s.ipv6Addr.String() != ipv6Addresses[0].String() {
			scopedLogger := s.l.With().Str("ipv6", ipv6Addresses[0].String()).Logger()
			if s.ipv6Addr != nil {
				scopedLogger.Info().
					Str("old_ipv6", s.ipv6Addr.String()).
					Msg("IPv6 address changed")
			} else {
				scopedLogger.Info().Msg("IPv6 address found")
			}
			s.ipv6Addr = &ipv6Addresses[0]
			changed = true
		}
	}

	// if it's the initial check, we'll set changed to false
	initialCheck := !s.checked
	if initialCheck {
		s.checked = true
		changed = false
	}

	if initialCheck {
		s.onInitialCheck(s)
	} else if changed {
		s.onStateChange(s)
	}

	return dhcpTargetState, nil
}

func (s *NetworkInterfaceState) CheckAndUpdateDhcp() error {
	dhcpTargetState, err := s.update()
	if err != nil {
		return ErrorfL(s.l, "failed to update network state", err)
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

func (s *NetworkInterfaceState) HandleLinkUpdate(update netlink.LinkUpdate) {
	if update.Link.Attrs().Name == s.interfaceName {
		s.l.Info().Interface("update", update).Msg("interface link update received")
		_ = s.CheckAndUpdateDhcp()
	}
}

func rpcGetNetworkState() RpcNetworkState {
	return RpcNetworkState{
		InterfaceName: networkState.interfaceName,
		MacAddress:    networkState.macAddr.String(),
		IPv4:          networkState.ipv4Addr.String(),
		IPv6:          networkState.ipv6Addr.String(),
		DHCPLease:     networkState.dhcpClient.GetLease(),
	}
}

func rpcRenewDHCPLease() error {
	if networkState == nil {
		return fmt.Errorf("network state not initialized")
	}
	if networkState.dhcpClient == nil {
		return fmt.Errorf("dhcp client not initialized")
	}

	return networkState.dhcpClient.Renew()
}

func initNetwork() {
	ensureConfigLoaded()

	updates := make(chan netlink.LinkUpdate)
	done := make(chan struct{})

	if err := netlink.LinkSubscribe(updates, done); err != nil {
		networkLogger.Warn().Err(err).Msg("failed to subscribe to link updates")
		return
	}

	// TODO: support multiple interfaces
	networkState = NewNetworkInterfaceState(NetIfName)
	go networkState.dhcpClient.Run() // nolint:errcheck

	if err := networkState.CheckAndUpdateDhcp(); err != nil {
		os.Exit(1)
	}

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case update := <-updates:
				networkState.HandleLinkUpdate(update)
			case <-ticker.C:
				_ = networkState.CheckAndUpdateDhcp()
			case <-done:
				return
			}
		}
	}()
}
