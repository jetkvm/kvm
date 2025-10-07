// Package link provides a wrapper around netlink.Link and provides a singleton netlink manager.
package link

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/network/types"
	"github.com/rs/zerolog"
	"github.com/vishvananda/netlink"
)

const (
	// AfUnspec is the unspecified address family constant
	AfUnspec = 0
	// AfInet is the IPv4 address family constant
	AfInet = 2
	// AfInet6 is the IPv6 address family constant
	AfInet6 = 10

	sysctlBase     = "/proc/sys"
	sysctlFileMode = 0640
)

var (
	ipv4DefaultRoute = net.IPNet{
		IP:   net.IPv4zero,
		Mask: net.CIDRMask(0, 0),
	}

	ipv6DefaultRoute = net.IPNet{
		IP:   net.IPv6zero,
		Mask: net.CIDRMask(0, 0),
	}

	// Singleton instance
	netlinkManagerInstance *NetlinkManager
	netlinkManagerOnce     sync.Once

	// Error definitions
	ErrInterfaceUpTimeout  = errors.New("timeout after waiting for an interface to come up")
	ErrInterfaceUpCanceled = errors.New("context canceled while waiting for an interface to come up")
)

type LinkStateCallbackFunction func(link *Link)
type LinkStateCallback struct {
	Async bool
	Func  LinkStateCallbackFunction
}

// NetlinkManager provides centralized netlink operations
type NetlinkManager struct {
	logger             *zerolog.Logger
	linkStateCh        chan netlink.LinkUpdate
	mu                 sync.RWMutex
	linkStateCallbacks map[string][]LinkStateCallback
}

// Link is a wrapper around netlink.Link
type Link struct {
	netlink.Link
}

// Attrs returns the attributes of the link
func (l *Link) Attrs() *netlink.LinkAttrs {
	return l.Link.Attrs()
}

func (l *Link) AddrList(family int) ([]netlink.Addr, error) {
	return netlink.AddrList(l, family)
}

func (l *Link) IsSame(other *Link) bool {
	if l == nil || other == nil {
		return false
	}

	a := l.Attrs()
	b := other.Attrs()
	if a.OperState != b.OperState {
		return false
	}
	if a.Index != b.Index {
		return false
	}
	if a.MTU != b.MTU {
		return false
	}
	if a.HardwareAddr.String() != b.HardwareAddr.String() {
		return false
	}
	return true
}

func newNetlinkManager(logger *zerolog.Logger) *NetlinkManager {
	if logger == nil {
		logger = &zerolog.Logger{} // Default no-op logger
	}
	n := &NetlinkManager{
		logger:             logger,
		linkStateCallbacks: make(map[string][]LinkStateCallback),
	}
	n.monitorLinkState()
	return n
}

// GetNetlinkManager returns the singleton NetlinkManager instance
func GetNetlinkManager() *NetlinkManager {
	netlinkManagerOnce.Do(func() {
		netlinkManagerInstance = newNetlinkManager(nil)
	})
	return netlinkManagerInstance
}

// InitializeNetlinkManager initializes the singleton NetlinkManager with a logger
func InitializeNetlinkManager(logger *zerolog.Logger) *NetlinkManager {
	netlinkManagerOnce.Do(func() {
		netlinkManagerInstance = newNetlinkManager(logger)
	})
	return netlinkManagerInstance
}

func (nm *NetlinkManager) runCallbacks(update netlink.LinkUpdate) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	ifname := update.Link.Attrs().Name
	callbacks, ok := nm.linkStateCallbacks[ifname]

	l := nm.logger.With().Str("interface", ifname).Logger()
	if !ok {
		l.Trace().Msg("no callbacks for interface")
		return
	}
	for _, callback := range callbacks {
		l.Trace().Interface("callback", callback).Msg("calling callback")

		if callback.Async {
			go callback.Func(&Link{Link: update.Link})
		} else {
			callback.Func(&Link{Link: update.Link})
		}
	}
}

// AddLinkStateCallback adds a callback for link state changes
func (nm *NetlinkManager) AddLinkStateCallback(ifname string, callback LinkStateCallback) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.linkStateCallbacks[ifname] = append(nm.linkStateCallbacks[ifname], callback)
}

// Interface operations
func (nm *NetlinkManager) monitorLinkState() {
	updateCh := make(chan netlink.LinkUpdate)
	// we don't need to stop the subscription, as it will be closed when the program exits
	stopCh := make(chan struct{}) //nolint:unused
	netlink.LinkSubscribe(updateCh, stopCh)

	nm.logger.Info().Msg("link state monitoring started")

	go func() {
		for update := range updateCh {
			nm.runCallbacks(update)
		}
	}()
}

// GetLinkByName gets a network link by name
func (nm *NetlinkManager) GetLinkByName(name string) (*Link, error) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	link, err := netlink.LinkByName(name)
	if err != nil {
		return nil, err
	}
	return &Link{Link: link}, nil
}

// LinkSetUp brings a network interface up
func (nm *NetlinkManager) LinkSetUp(link *Link) error {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return netlink.LinkSetUp(link)
}

// LinkSetDown brings a network interface down
func (nm *NetlinkManager) LinkSetDown(link *Link) error {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return netlink.LinkSetDown(link)
}

// EnsureInterfaceUp ensures the interface is up
func (nm *NetlinkManager) EnsureInterfaceUp(link *Link) error {
	if link.Attrs().OperState == netlink.OperUp {
		return nil
	}
	return nm.LinkSetUp(link)
}

// EnsureInterfaceUpWithTimeout ensures the interface is up with timeout and retry logic
func (nm *NetlinkManager) EnsureInterfaceUpWithTimeout(ctx context.Context, iface *Link, timeout time.Duration) (*Link, error) {
	ifname := iface.Attrs().Name

	l := nm.logger.With().Str("interface", ifname).Logger()

	linkUpTimeout := time.After(timeout)

	attempt := 0
	start := time.Now()

	for {
		link, err := nm.GetLinkByName(ifname)
		if err != nil {
			return nil, err
		}

		state := link.Attrs().OperState
		if state == netlink.OperUp || state == netlink.OperUnknown {
			return link, nil
		}

		l.Info().Str("state", state.String()).Msg("bringing up interface")

		if err = nm.LinkSetUp(link); err != nil {
			l.Error().Err(err).Msg("interface can't make it up")
		}

		l = l.With().Int("attempt", attempt).Dur("duration", time.Since(start)).Logger()

		if attempt > 0 {
			l.Info().Msg("interface up")
		}

		select {
		case <-time.After(500 * time.Millisecond):
			attempt++
			continue
		case <-ctx.Done():
			if err != nil {
				return nil, err
			}
			return nil, ErrInterfaceUpCanceled
		case <-linkUpTimeout:
			attempt++
			l.Error().Msg("interface is still down after timeout")
			if err != nil {
				return nil, err
			}
			return nil, ErrInterfaceUpTimeout
		}
	}
}

// Address operations

// AddrList gets all addresses for a link
func (nm *NetlinkManager) AddrList(link *Link, family int) ([]netlink.Addr, error) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return netlink.AddrList(link, family)
}

// AddrAdd adds an address to a link
func (nm *NetlinkManager) AddrAdd(link *Link, addr *netlink.Addr) error {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return netlink.AddrAdd(link, addr)
}

// AddrDel removes an address from a link
func (nm *NetlinkManager) AddrDel(link *Link, addr *netlink.Addr) error {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return netlink.AddrDel(link, addr)
}

// RemoveAllAddresses removes all addresses of a specific family from a link
func (nm *NetlinkManager) RemoveAllAddresses(link *Link, family int) error {
	addrs, err := nm.AddrList(link, family)
	if err != nil {
		return fmt.Errorf("failed to get addresses: %w", err)
	}

	for _, addr := range addrs {
		if err := nm.AddrDel(link, &addr); err != nil {
			nm.logger.Warn().Err(err).Str("address", addr.IP.String()).Msg("failed to remove address")
		}
	}

	return nil
}

// RemoveNonLinkLocalIPv6Addresses removes all non-link-local IPv6 addresses
func (nm *NetlinkManager) RemoveNonLinkLocalIPv6Addresses(link *Link) error {
	addrs, err := nm.AddrList(link, AfInet6)
	if err != nil {
		return fmt.Errorf("failed to get IPv6 addresses: %w", err)
	}

	for _, addr := range addrs {
		if !addr.IP.IsLinkLocalUnicast() {
			if err := nm.AddrDel(link, &addr); err != nil {
				nm.logger.Warn().Err(err).Str("address", addr.IP.String()).Msg("failed to remove IPv6 address")
			}
		}
	}

	return nil
}

// RouteList gets all routes
func (nm *NetlinkManager) RouteList(link *Link, family int) ([]netlink.Route, error) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return netlink.RouteList(link, family)
}

// RouteAdd adds a route
func (nm *NetlinkManager) RouteAdd(route *netlink.Route) error {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return netlink.RouteAdd(route)
}

// RouteDel removes a route
func (nm *NetlinkManager) RouteDel(route *netlink.Route) error {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return netlink.RouteDel(route)
}

// RouteReplace replaces a route
func (nm *NetlinkManager) RouteReplace(route *netlink.Route) error {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return netlink.RouteReplace(route)
}

// HasDefaultRoute checks if a default route exists for the given family
func (nm *NetlinkManager) HasDefaultRoute(family int) bool {
	routes, err := netlink.RouteList(nil, family)
	if err != nil {
		return false
	}

	for _, route := range routes {
		if route.Dst == nil {
			return true
		}
		if family == AfInet && route.Dst.IP.Equal(net.IPv4zero) && route.Dst.Mask.String() == "0.0.0.0/0" {
			return true
		}
		if family == AfInet6 && route.Dst.IP.Equal(net.IPv6zero) && route.Dst.Mask.String() == "::/0" {
			return true
		}
	}

	return false
}

// AddDefaultRoute adds a default route
func (nm *NetlinkManager) AddDefaultRoute(link *Link, gateway net.IP, family int) error {
	var dst *net.IPNet
	if family == AfInet {
		dst = &ipv4DefaultRoute
	} else if family == AfInet6 {
		dst = &ipv6DefaultRoute
	} else {
		return fmt.Errorf("unsupported address family: %d", family)
	}

	route := &netlink.Route{
		Dst:       dst,
		Gw:        gateway,
		LinkIndex: link.Attrs().Index,
	}

	return nm.RouteReplace(route)
}

// RemoveDefaultRoute removes the default route for the given family
func (nm *NetlinkManager) RemoveDefaultRoute(family int) error {
	routes, err := nm.RouteList(nil, family)
	if err != nil {
		return fmt.Errorf("failed to get routes: %w", err)
	}

	for _, route := range routes {
		if route.Dst != nil {
			if family == AfInet && route.Dst.IP.Equal(net.IPv4zero) && route.Dst.Mask.String() == "0.0.0.0/0" {
				if err := nm.RouteDel(&route); err != nil {
					nm.logger.Warn().Err(err).Msg("failed to remove IPv4 default route")
				}
			}
			if family == AfInet6 && route.Dst.IP.Equal(net.IPv6zero) && route.Dst.Mask.String() == "::/0" {
				if err := nm.RouteDel(&route); err != nil {
					nm.logger.Warn().Err(err).Msg("failed to remove IPv6 default route")
				}
			}
		}
	}

	return nil
}

func (nm *NetlinkManager) ReconcileLinkAddrs(link *Link, expected []*types.IPAddress) error {
	expectedAddrs := make(map[string]bool)
	existingAddrs := make(map[string]bool)

	for _, addr := range expected {
		ipCidr := addr.Address.IP.String() + "/" + addr.Address.Mask.String()
		nm.logger.Trace().Str("address", ipCidr).Msg("adding expected address")
		expectedAddrs[ipCidr] = true
	}

	addrs, err := nm.AddrList(link, AfUnspec)
	if err != nil {
		return fmt.Errorf("failed to get addresses: %w", err)
	}

	for _, addr := range addrs {
		ipCidr := addr.IP.String() + "/" + addr.IPNet.Mask.String()
		existingAddrs[ipCidr] = true
	}

	for _, addr := range expected {
		family := AfUnspec
		if addr.Address.IP.To4() != nil {
			family = AfInet
		} else if addr.Address.IP.To16() != nil {
			family = AfInet6
		}

		ipCidr := addr.Address.IP.String() + "/" + addr.Address.Mask.String()
		if ok := existingAddrs[ipCidr]; !ok {
			ipNet := &net.IPNet{
				IP:   addr.Address.IP,
				Mask: addr.Address.Mask,
			}

			if err := nm.AddrAdd(link, &netlink.Addr{IPNet: ipNet}); err != nil {
				return fmt.Errorf("failed to add address %s: %w", ipCidr, err)
			}

			nm.logger.Info().Str("address", ipCidr).Msg("added address")
		}

		if addr.Gateway != nil {
			nm.logger.Trace().Str("address", ipCidr).Str("gateway", addr.Gateway.String()).Msg("adding default route for address")
			if err := nm.AddDefaultRoute(link, addr.Gateway, family); err != nil {
				return fmt.Errorf("failed to add default route for address %s: %w", ipCidr, err)
			}
		}
	}

	return nil
}

// Sysctl operations

// SetSysctlValues sets sysctl values for the interface
func (nm *NetlinkManager) SetSysctlValues(ifaceName string, values map[string]int) error {
	for name, value := range values {
		name = fmt.Sprintf(name, ifaceName)
		name = strings.ReplaceAll(name, ".", "/")

		if err := os.WriteFile(path.Join(sysctlBase, name), []byte(strconv.Itoa(value)), sysctlFileMode); err != nil {
			return fmt.Errorf("failed to set sysctl %s=%d: %w", name, value, err)
		}
	}
	return nil
}

// EnableIPv6 enables IPv6 on the interface
func (nm *NetlinkManager) EnableIPv6(ifaceName string) error {
	return nm.SetSysctlValues(ifaceName, map[string]int{
		"net.ipv6.conf.%s.disable_ipv6": 0,
		"net.ipv6.conf.%s.accept_ra":    2,
	})
}

// DisableIPv6 disables IPv6 on the interface
func (nm *NetlinkManager) DisableIPv6(ifaceName string) error {
	return nm.SetSysctlValues(ifaceName, map[string]int{
		"net.ipv6.conf.%s.disable_ipv6": 1,
	})
}

// EnableIPv6SLAAC enables IPv6 SLAAC on the interface
func (nm *NetlinkManager) EnableIPv6SLAAC(ifaceName string) error {
	return nm.SetSysctlValues(ifaceName, map[string]int{
		"net.ipv6.conf.%s.disable_ipv6": 0,
		"net.ipv6.conf.%s.accept_ra":    2,
	})
}

// EnableIPv6LinkLocal enables IPv6 link-local only on the interface
func (nm *NetlinkManager) EnableIPv6LinkLocal(ifaceName string) error {
	return nm.SetSysctlValues(ifaceName, map[string]int{
		"net.ipv6.conf.%s.disable_ipv6": 0,
		"net.ipv6.conf.%s.accept_ra":    0,
	})
}

// Utility functions

// ParseIPv4Netmask parses an IPv4 netmask string and returns the IPNet
func (nm *NetlinkManager) ParseIPv4Netmask(address, netmask string) (*net.IPNet, error) {
	if strings.Contains(address, "/") {
		_, ipNet, err := net.ParseCIDR(address)
		if err != nil {
			return nil, fmt.Errorf("invalid IPv4 address: %s", address)
		}
		return ipNet, nil
	}

	ip := net.ParseIP(address)
	if ip == nil {
		return nil, fmt.Errorf("invalid IPv4 address: %s", address)
	}
	if ip.To4() == nil {
		return nil, fmt.Errorf("not an IPv4 address: %s", address)
	}

	mask := net.ParseIP(netmask)
	if mask == nil {
		return nil, fmt.Errorf("invalid IPv4 netmask: %s", netmask)
	}
	if mask.To4() == nil {
		return nil, fmt.Errorf("not an IPv4 netmask: %s", netmask)
	}

	return &net.IPNet{
		IP:   ip,
		Mask: net.IPv4Mask(mask[12], mask[13], mask[14], mask[15]),
	}, nil
}

// ParseIPv6Prefix parses an IPv6 address and prefix length
func (nm *NetlinkManager) ParseIPv6Prefix(address string, prefixLength int) (*net.IPNet, error) {
	if strings.Contains(address, "/") {
		_, ipNet, err := net.ParseCIDR(address)
		if err != nil {
			return nil, fmt.Errorf("invalid IPv6 address: %s", address)
		}
		return ipNet, nil
	}

	ip := net.ParseIP(address)
	if ip == nil {
		return nil, fmt.Errorf("invalid IPv6 address: %s", address)
	}
	if ip.To16() == nil || ip.To4() != nil {
		return nil, fmt.Errorf("not an IPv6 address: %s", address)
	}

	if prefixLength < 0 || prefixLength > 128 {
		return nil, fmt.Errorf("invalid IPv6 prefix length: %d (must be 0-128)", prefixLength)
	}

	return &net.IPNet{
		IP:   ip,
		Mask: net.CIDRMask(prefixLength, 128),
	}, nil
}

// ValidateIPAddress validates an IP address
func (nm *NetlinkManager) ValidateIPAddress(address string, isIPv6 bool) error {
	ip := net.ParseIP(address)
	if ip == nil {
		return fmt.Errorf("invalid IP address: %s", address)
	}

	if isIPv6 {
		if ip.To16() == nil || ip.To4() != nil {
			return fmt.Errorf("not an IPv6 address: %s", address)
		}
	} else {
		if ip.To4() == nil {
			return fmt.Errorf("not an IPv4 address: %s", address)
		}
	}

	return nil
}
