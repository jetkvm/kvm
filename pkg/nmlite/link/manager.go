package link

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/jetkvm/kvm/internal/sync"

	"github.com/jetkvm/kvm/internal/network/types"
	"github.com/rs/zerolog"
	"github.com/vishvananda/netlink"
)

// StateChangeHandler is the function type for link state callbacks
type StateChangeHandler func(link *Link)

// StateChangeCallback is the struct for link state callbacks
type StateChangeCallback struct {
	Async bool
	Func  StateChangeHandler
}

// NetlinkManager provides centralized netlink operations
type NetlinkManager struct {
	logger               *zerolog.Logger
	mu                   sync.RWMutex
	stateChangeCallbacks map[string][]StateChangeCallback
}

func newNetlinkManager(logger *zerolog.Logger) *NetlinkManager {
	if logger == nil {
		logger = &zerolog.Logger{} // Default no-op logger
	}
	n := &NetlinkManager{
		logger:               logger,
		stateChangeCallbacks: make(map[string][]StateChangeCallback),
	}
	n.monitorStateChange()
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

// AddStateChangeCallback adds a callback for link state changes
func (nm *NetlinkManager) AddStateChangeCallback(ifname string, callback StateChangeCallback) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if _, ok := nm.stateChangeCallbacks[ifname]; !ok {
		nm.stateChangeCallbacks[ifname] = make([]StateChangeCallback, 0)
	}

	nm.stateChangeCallbacks[ifname] = append(nm.stateChangeCallbacks[ifname], callback)
}

// Interface operations
func (nm *NetlinkManager) monitorStateChange() {
	updateCh := make(chan netlink.LinkUpdate)
	// we don't need to stop the subscription, as it will be closed when the program exits
	stopCh := make(chan struct{}) //nolint:unused
	netlink.LinkSubscribe(updateCh, stopCh)

	nm.logger.Info().Msg("state change monitoring started")

	go func() {
		for update := range updateCh {
			nm.runCallbacks(update)
		}
	}()
}

func (nm *NetlinkManager) runCallbacks(update netlink.LinkUpdate) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	ifname := update.Link.Attrs().Name
	callbacks, ok := nm.stateChangeCallbacks[ifname]

	l := nm.logger.With().Str("interface", ifname).Logger()
	if !ok {
		l.Trace().Msg("no state change callbacks for interface")
		return
	}

	for _, callback := range callbacks {
		l.Trace().
			Interface("callback", callback).
			Bool("async", callback.Async).
			Msg("calling callback")

		if callback.Async {
			go callback.Func(&Link{Link: update.Link})
		} else {
			callback.Func(&Link{Link: update.Link})
		}
	}
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
	switch family {
	case AfInet:
		dst = &ipv4DefaultRoute
	case AfInet6:
		dst = &ipv6DefaultRoute
	default:
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

// ReconcileLink reconciles the addresses and routes of a link
func (nm *NetlinkManager) ReconcileLink(link *Link, expected []*types.IPAddress) error {
	expectedAddrs := make(map[string]bool)
	existingAddrs := make(map[string]bool)

	for _, addr := range expected {
		ipCidr := addr.Address.IP.String() + "/" + addr.Address.Mask.String()
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
