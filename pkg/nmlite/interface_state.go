package nmlite

import (
	"fmt"
	"time"

	"github.com/jetkvm/kvm/internal/network/types"
	"github.com/jetkvm/kvm/pkg/nmlite/link"
	"github.com/vishvananda/netlink"
)

// updateInterfaceState updates the current interface state
func (im *InterfaceManager) updateInterfaceState() error {
	nl, err := im.link()
	if err != nil {
		return fmt.Errorf("failed to get interface: %w", err)
	}

	var stateChanged bool

	attrs := nl.Attrs()

	// We should release the lock before calling the callbacks
	// to avoid deadlocks
	im.stateMu.Lock()

	// Check if the interface is up
	isUp := attrs.OperState == netlink.OperUp
	if im.state.Up != isUp {
		im.state.Up = isUp
		stateChanged = true
	}

	// Check if the interface is online
	isOnline := isUp && nl.HasGlobalUnicastAddress()
	if im.state.Online != isOnline {
		im.state.Online = isOnline
		stateChanged = true
	}

	// Check if the MAC address has changed
	if im.state.MACAddress != attrs.HardwareAddr.String() {
		im.state.MACAddress = attrs.HardwareAddr.String()
		stateChanged = true
	}

	// Update IP addresses
	if ipChanged, err := im.updateInterfaceStateAddresses(nl); err != nil {
		im.logger.Error().Err(err).Msg("failed to update IP addresses")
	} else if ipChanged {
		stateChanged = true
	}

	im.state.LastUpdated = time.Now()
	im.stateMu.Unlock()

	// Notify callback if state changed
	if stateChanged && im.onStateChange != nil {
		im.logger.Debug().Interface("state", im.state).Msg("notifying state change")
		im.onStateChange(*im.state)
	}

	return nil
}

// updateIPAddresses updates the IP addresses in the state
func (im *InterfaceManager) updateInterfaceStateAddresses(nl *link.Link) (bool, error) {
	mgr := getNetlinkManager()

	addrs, err := nl.AddrList(link.AfUnspec)
	if err != nil {
		return false, fmt.Errorf("failed to get addresses: %w", err)
	}

	var (
		ipv4Addresses        []string
		ipv6Addresses        []types.IPv6Address
		ipv4Addr, ipv6Addr   string
		ipv6LinkLocal        string
		ipv6Gateway          string
		ipv4Ready, ipv6Ready = false, false
		stateChanged         = false
	)

	routes, _ := mgr.ListDefaultRoutes(link.AfInet6)
	if len(routes) > 0 {
		ipv6Gateway = routes[0].Gw.String()
	}

	for _, addr := range addrs {
		if addr.IP.To4() != nil {
			// IPv4 address
			ipv4Addresses = append(ipv4Addresses, addr.IPNet.String())
			if ipv4Addr == "" {
				ipv4Addr = addr.IP.String()
				ipv4Ready = true
			}
			continue
		}

		// IPv6 address (if it's not an IPv4 address, it must be an IPv6 address)
		if addr.IP.IsLinkLocalUnicast() {
			ipv6LinkLocal = addr.IP.String()
			continue
		} else if !addr.IP.IsGlobalUnicast() {
			continue
		}

		ipv6Addresses = append(ipv6Addresses, types.IPv6Address{
			Address:           addr.IP,
			Prefix:            *addr.IPNet,
			Scope:             addr.Scope,
			Flags:             addr.Flags,
			ValidLifetime:     lifetimeToTime(addr.ValidLft),
			PreferredLifetime: lifetimeToTime(addr.PreferedLft),
		})
		if ipv6Addr == "" {
			ipv6Addr = addr.IP.String()
			ipv6Ready = true
		}
	}

	if !sortAndCompareStringSlices(im.state.IPv4Addresses, ipv4Addresses) {
		im.state.IPv4Addresses = ipv4Addresses
		stateChanged = true
	}

	if !sortAndCompareIPv6AddressSlices(im.state.IPv6Addresses, ipv6Addresses) {
		im.state.IPv6Addresses = ipv6Addresses
		stateChanged = true
	}

	if im.state.IPv4Address != ipv4Addr {
		im.state.IPv4Address = ipv4Addr
		stateChanged = true
	}

	if im.state.IPv6Address != ipv6Addr {
		im.state.IPv6Address = ipv6Addr
		stateChanged = true
	}
	if im.state.IPv6LinkLocal != ipv6LinkLocal {
		im.state.IPv6LinkLocal = ipv6LinkLocal
		stateChanged = true
	}

	if im.state.IPv6Gateway != ipv6Gateway {
		im.state.IPv6Gateway = ipv6Gateway
		stateChanged = true
	}

	if im.state.IPv4Ready != ipv4Ready {
		im.state.IPv4Ready = ipv4Ready
		stateChanged = true
	}

	if im.state.IPv6Ready != ipv6Ready {
		im.state.IPv6Ready = ipv6Ready
		stateChanged = true
	}

	return stateChanged, nil
}
