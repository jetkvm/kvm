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

type IPv6Address struct {
	Address           net.IP     `json:"address"`
	Prefix            net.IPNet  `json:"prefix"`
	ValidLifetime     *time.Time `json:"valid_lifetime"`
	PreferredLifetime *time.Time `json:"preferred_lifetime"`
	Scope             int        `json:"scope"`
}

type NetworkInterfaceState struct {
	interfaceName string
	interfaceUp   bool
	ipv4Addr      *net.IP
	ipv4Addresses []string
	ipv6Addr      *net.IP
	ipv6Addresses []IPv6Address
	ipv6LinkLocal *net.IP
	macAddr       *net.HardwareAddr

	l         *zerolog.Logger
	stateLock sync.Mutex

	dhcpClient *udhcpc.DHCPClient

	onStateChange  func(state *NetworkInterfaceState)
	onInitialCheck func(state *NetworkInterfaceState)

	checked bool
}

type NetworkConfig struct {
	Hostname string `json:"hostname,omitempty"`
	Domain   string `json:"domain,omitempty"`

	IPv4Mode   string `json:"ipv4_mode" one_of:"dhcp,static,disabled" default:"dhcp"`
	IPv4Static struct {
		Address string   `json:"address" validate_type:"ipv4"`
		Netmask string   `json:"netmask" validate_type:"ipv4"`
		Gateway string   `json:"gateway" validate_type:"ipv4"`
		DNS     []string `json:"dns" validate_type:"ipv4"`
	} `json:"ipv4_static,omitempty" required_if:"ipv4_mode,static"`

	IPv6Mode   string `json:"ipv6_mode" one_of:"slaac,dhcpv6,slaac_and_dhcpv6,static,link_local,disabled" default:"slaac"`
	IPv6Static struct {
		Address string   `json:"address" validate_type:"ipv6"`
		Netmask string   `json:"netmask" validate_type:"ipv6"`
		Gateway string   `json:"gateway" validate_type:"ipv6"`
		DNS     []string `json:"dns" validate_type:"ipv6"`
	} `json:"ipv6_static,omitempty" required_if:"ipv6_mode,static"`

	LLDPMode     string   `json:"lldp_mode,omitempty" one_of:"disabled,basic,all" default:"basic"`
	LLDPTxTLVs   []string `json:"lldp_tx_tlvs,omitempty" one_of:"chassis,port,system,vlan" default:"chassis,port,system,vlan"`
	MDNSMode     string   `json:"mdns_mode,omitempty" one_of:"disabled,auto,ipv4_only,ipv6_only" default:"auto"`
	TimeSyncMode string   `json:"time_sync_mode,omitempty" one_of:"ntp_only,ntp_and_http,http_only,custom" default:"ntp_and_http"`
}

type RpcIPv6Address struct {
	Address           string     `json:"address"`
	ValidLifetime     *time.Time `json:"valid_lifetime,omitempty"`
	PreferredLifetime *time.Time `json:"preferred_lifetime,omitempty"`
	Scope             int        `json:"scope"`
}

type RpcNetworkState struct {
	InterfaceName string           `json:"interface_name"`
	MacAddress    string           `json:"mac_address"`
	IPv4          string           `json:"ipv4,omitempty"`
	IPv6          string           `json:"ipv6,omitempty"`
	IPv6LinkLocal string           `json:"ipv6_link_local,omitempty"`
	IPv4Addresses []string         `json:"ipv4_addresses,omitempty"`
	IPv6Addresses []RpcIPv6Address `json:"ipv6_addresses,omitempty"`
	DHCPLease     *udhcpc.Lease    `json:"dhcp_lease,omitempty"`
}

type RpcNetworkSettings struct {
	IPv4Mode     string   `json:"ipv4_mode,omitempty"`
	IPv6Mode     string   `json:"ipv6_mode,omitempty"`
	LLDPMode     string   `json:"lldp_mode,omitempty"`
	LLDPTxTLVs   []string `json:"lldp_tx_tlvs,omitempty"`
	MDNSMode     string   `json:"mdns_mode,omitempty"`
	TimeSyncMode string   `json:"time_sync_mode,omitempty"`
}

func lifetimeToTime(lifetime int) *time.Time {
	if lifetime == 0 {
		return nil
	}
	t := time.Now().Add(time.Duration(lifetime) * time.Second)
	return &t
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
				requestDisplayUpdate(true)
			}()
		},
		onInitialCheck: func(state *NetworkInterfaceState) {
			go func() {
				waitCtrlClientConnected()
				requestDisplayUpdate(true)
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
			_, err := s.update()
			if err != nil {
				logger.Error().Err(err).Msg("failed to update network state")
				return
			}

			if currentSession == nil {
				logger.Info().Msg("No active RPC session, skipping network state update")
				return
			}

			writeJSONRPCEvent("networkState", rpcGetNetworkState(), currentSession)
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
		ipv4Addresses       = make([]net.IP, 0)
		ipv4AddressesString = make([]string, 0)
		ipv6Addresses       = make([]IPv6Address, 0)
		ipv6AddressesString = make([]string, 0)
		ipv6LinkLocal       *net.IP
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
			ipv6AddressesString = append(ipv6AddressesString, addr.IPNet.String())
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
	ipv6Addresses := make([]RpcIPv6Address, 0)
	for _, addr := range networkState.ipv6Addresses {
		ipv6Addresses = append(ipv6Addresses, RpcIPv6Address{
			Address:           addr.Prefix.String(),
			ValidLifetime:     addr.ValidLifetime,
			PreferredLifetime: addr.PreferredLifetime,
			Scope:             addr.Scope,
		})
	}
	return RpcNetworkState{
		InterfaceName: networkState.interfaceName,
		MacAddress:    networkState.macAddr.String(),
		IPv4:          networkState.ipv4Addr.String(),
		IPv6:          networkState.ipv6Addr.String(),
		IPv6LinkLocal: networkState.ipv6LinkLocal.String(),
		IPv4Addresses: networkState.ipv4Addresses,
		IPv6Addresses: ipv6Addresses,
		DHCPLease:     networkState.dhcpClient.GetLease(),
	}
}

func rpcGetNetworkSettings() RpcNetworkSettings {
	return RpcNetworkSettings{
		IPv4Mode:     "dhcp",
		IPv6Mode:     "slaac",
		LLDPMode:     "basic",
		LLDPTxTLVs:   []string{"chassis", "port", "system", "vlan"},
		MDNSMode:     "auto",
		TimeSyncMode: "ntp_and_http",
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
