package link

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/jetkvm/kvm/internal/network/types"
	"github.com/jetkvm/kvm/internal/sync"

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
	if err := netlink.LinkSubscribe(updateCh, stopCh); err != nil {
		nm.logger.Error().Err(err).Msg("failed to subscribe to link state changes")
	}

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

	var attempt int
	start := time.Now()

	for {
		link, err := nm.GetLinkByName(ifname)
		if err != nil {
			return nil, err
		}

		state := link.Attrs().OperState

		l = l.With().
			Int("attempt", attempt).
			Dur("duration", time.Since(start)).
			Str("state", state.String()).
			Logger()
		if state == netlink.OperUp || state == netlink.OperUnknown {
			if attempt > 0 {
				l.Info().Int("attempt", attempt-1).Msg("interface is up")
			}
			return link, nil
		}

		l.Info().Msg("bringing up interface")

		// bring up the interface
		if err = nm.LinkSetUp(link); err != nil {
			l.Error().Err(err).Msg("interface can't make it up")
		}

		// refresh the link attributes
		if err = link.Refresh(); err != nil {
			l.Error().Err(err).Msg("failed to refresh link attributes")
		}

		// check the state again
		state = link.Attrs().OperState
		l = l.With().Str("new_state", state.String()).Logger()
		if state == netlink.OperUp {
			l.Info().Msg("interface is up")
			return link, nil
		}
		l.Warn().Msg("interface is still down, retrying")

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
			l.Error().
				Int("attempt", attempt).Msg("interface is still down after timeout")
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

// ListDefaultRoutes lists the default routes for the given family
func (nm *NetlinkManager) ListDefaultRoutes(family int) ([]netlink.Route, error) {
	routes, err := netlink.RouteListFiltered(
		family,
		&netlink.Route{Dst: nil, Table: MainRoutingTable},
		netlink.RT_FILTER_DST|netlink.RT_FILTER_TABLE,
	)
	if err != nil {
		nm.logger.Error().Err(err).Int("family", family).Msg("failed to list default routes")
		return nil, err
	}

	return routes, nil
}

// HasDefaultRoute checks if a default route exists for the given family
func (nm *NetlinkManager) HasDefaultRoute(family int) bool {
	routes, err := nm.ListDefaultRoutes(family)
	if err != nil {
		return false
	}
	return len(routes) > 0
}

// AddDefaultRoute adds a default route
func (nm *NetlinkManager) AddDefaultRoute(link *Link, gateway net.IP, family int, protocol netlink.RouteProtocol) error {
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
		Scope:     netlink.SCOPE_UNIVERSE,
		Table:     MainRoutingTable,
		Protocol:  protocol,
	}

	return nm.RouteReplace(route)
}

// RemoveDefaultRoute removes the default route for the given family
func (nm *NetlinkManager) RemoveDefaultRoute(family int) error {
	l := nm.logger.With().Int("family", family).Logger()
	routes, err := nm.RouteList(nil, family)
	if err != nil {
		l.Error().Err(err).Msg("failed to get route list")
		return fmt.Errorf("failed to get routes: %w", err)
	}
	l.Trace().Int("route_count", len(routes)).Msg("checking routes for default route removal")

	for _, route := range routes {
		if route.Dst != nil {
			if family == AfInet && route.Dst.IP.Equal(net.IPv4zero) && route.Dst.Mask.String() == "0.0.0.0/0" {
				l.Trace().Interface("destination", route.Dst).Msg("removing IPv4 default route")
				if err := nm.RouteDel(&route); err != nil {
					l.Warn().Err(err).Msg("failed to remove IPv4 default route")
				}
			}
			if family == AfInet6 && route.Dst.IP.Equal(net.IPv6zero) && route.Dst.Mask.String() == "::/0" {
				l.Trace().Interface("destination", route.Dst).Msg("removing IPv6 default route")
				if err := nm.RouteDel(&route); err != nil {
					l.Warn().Err(err).Msg("failed to remove IPv6 default route")
				}
			}
		}
	}

	return nil
}

func (nm *NetlinkManager) reconcileDefaultRoutes(link *Link, expected map[string]net.IP, family int, protocol netlink.RouteProtocol) error {
	linkAttrs := link.Attrs()
	l := nm.logger.With().Str("interface", linkAttrs.Name).Int("linkIndex", linkAttrs.Index).Int("family", family).Logger()

	added := 0
	removed := 0
	toRemove := make([]*netlink.Route, 0)

	defaultRoutes, err := nm.ListDefaultRoutes(family)
	if err != nil {
		l.Warn().Err(err).Msg("failed get default routes")
		return fmt.Errorf("failed to get default routes: %w", err)
	}
	l.Debug().Int("defaultRoutes_count", len(defaultRoutes)).Msg("current default routes")

	// check existing default routes
	for _, defaultRoute := range defaultRoutes {
		ll := l.With().Interface("defaultRoute", defaultRoute).Logger()

		// only check the default routes for the current link
		// TODO: we should also check others later
		if defaultRoute.LinkIndex != linkAttrs.Index {
			ll.Trace().Msg("wrong link index, skipping")
			continue
		}

		key := defaultRoute.Gw.String()
		ll.Trace().Str("key", key).Msg("checking default route")

		if _, ok := expected[key]; !ok {
			ll.Debug().Str("key", key).Msg("not in expected routes, marked for removal")
			toRemove = append(toRemove, &defaultRoute)
			continue
		}

		l.Debug().Msg("will keep default route")
		delete(expected, key)
	}

	// remove remaining default routes
	for _, defaultRoute := range toRemove {
		if err := nm.RouteDel(defaultRoute); err != nil {
			l.Warn().Err(err).Msg("failed to remove default route")
			// do not abandon the reconciliation for route removal failure
		}
		l.Debug().Stringer("gateway", defaultRoute.Gw).Msg("removed default route")
		removed++
	}

	// add remaining expected default routes
	for _, gateway := range expected {
		l.Debug().Stringer("gateway", gateway).Msg("adding default route")

		route := &netlink.Route{
			Gw:        gateway,
			LinkIndex: linkAttrs.Index,
			Scope:     netlink.SCOPE_UNIVERSE,
			Table:     MainRoutingTable,
			Protocol:  protocol,
		}

		switch family {
		case AfInet6:
			route.Dst = &ipv6DefaultRoute
		case AfInet:
			route.Dst = &ipv4DefaultRoute
		}

		if err := nm.RouteAdd(route); err != nil {
			l.Warn().Err(err).Interface("route", route).Msg("failed to add default route")
			// do not abandon the reconciliation for route addition failure
			continue
		}
		l.Debug().IPAddr("gateway", gateway).Msg("added default route")
		added++
	}

	nm.logger.Info().
		Int("added", added).
		Int("removed", removed).
		Msg("default routes reconciled")

	return nil
}

// ReconcileLink reconciles the addresses and routes of a link
func (nm *NetlinkManager) ReconcileLink(link *Link, expected []types.IPAddress, family int, protocol netlink.RouteProtocol) error {
	l := nm.logger.With().Interface("link", link.Link).Int("family", family).Logger()

	toAdd := make([]*types.IPAddress, 0)
	toRemove := make([]*netlink.Addr, 0)
	toUpdate := make([]*types.IPAddress, 0)
	expectedAddrs := make(map[string]*types.IPAddress)
	expectedGateways := make(map[string]net.IP)

	mtu := link.Attrs().MTU
	expectedMTU := 0

	// add all expected addresses to the map
	for _, addr := range expected {
		expectedAddrs[addr.String()] = &addr
		if addr.Gateway != nil {
			expectedGateways[addr.Gateway.String()] = addr.Gateway
		}
		if addr.MTU != 0 {
			// we take the smallest MTU among expected addresses
			if expectedMTU == 0 || addr.MTU < expectedMTU {
				expectedMTU = addr.MTU
			}
		}
	}

	l.Trace().Int("expected_mtu", expectedMTU).Int("link_mtu", mtu).Msg("computed MTU")
	if expectedMTU != 0 && expectedMTU != mtu {
		if err := link.SetMTU(expectedMTU); err != nil {
			l.Warn().Err(err).Int("expected_mtu", expectedMTU).Int("current_mtu", mtu).Msg("failed to set MTU")
			// do not abandon the reconciliation for MTU failure
		}
	}

	addrs, err := nm.AddrList(link, family)
	if err != nil {
		l.Error().Err(err).Msg("failed to get addresses")
		return fmt.Errorf("failed to get addresses: %w", err)
	}
	l.Debug().Int("address_count", len(addrs)).Msg("current addresses")

	// check existing addresses
	for _, addr := range addrs {
		// skip the link-local address
		if addr.IP.IsLinkLocalUnicast() {
			l.Trace().Interface("addr", addr).Msg("link lock unicast address, skipping")
			continue
		}

		key := addr.IPNet.String()
		expectedAddr, ok := expectedAddrs[key]
		if !ok {
			l.Trace().Interface("addr", addr).Str("key", key).Msg("not in expected addresses, marked for removal")
			toRemove = append(toRemove, &addr)
			continue
		}

		// found it, so remove it from expected addresses
		delete(expectedAddrs, key)

		// if it's not fully equal, we will need to update it
		if !expectedAddr.Compare(addr) {
			l.Trace().Interface("addr", addr).Interface("expectedAddr", expectedAddr).Msg("addresses are not equal, marked for update")
			toUpdate = append(toUpdate, expectedAddr)
		}
	}

	// add remaining unmatched expected addresses
	for _, addr := range expectedAddrs {
		l.Trace().Interface("addr", addr).Msg("addresses not found, marked for addition")
		toAdd = append(toAdd, addr)
	}

	l.Trace().Int("toAdd_count", len(toAdd)).Int("toRemove_count", len(toRemove)).Int("toUpdate_count", len(toUpdate)).Msg("reconcilliations computed")

	for _, addr := range toUpdate {
		netlinkAddr := addr.NetlinkAddr()
		if err := nm.AddrDel(link, &netlinkAddr); err != nil {
			l.Warn().Err(err).Stringer("address", netlinkAddr).Msg("failed to remove address for update")
		}
		l.Trace().Stringer("address", netlinkAddr).Msg("address removed for update/readdition")
		toAdd = append(toAdd, addr) // add it back after all the other removals
	}

	for _, netlinkAddr := range toRemove {
		if err := nm.AddrDel(link, netlinkAddr); err != nil {
			l.Warn().Err(err).Stringer("address", netlinkAddr).Msg("failed to remove address")
		}
		l.Trace().Stringer("address", netlinkAddr).Msg("removed address")
	}

	for _, addr := range toAdd {
		netlinkAddr := addr.NetlinkAddr()
		if err := nm.AddrAdd(link, &netlinkAddr); err != nil {
			l.Warn().Err(err).Stringer("address", netlinkAddr).Msg("failed to add address")
		}
		l.Trace().Stringer("address", netlinkAddr).Msg("added address")
	}

	actualToAdd := len(toAdd) - len(toUpdate)
	if len(toAdd) > 0 || len(toUpdate) > 0 || len(toRemove) > 0 {
		l.Info().
			Int("added", actualToAdd).
			Int("updated", len(toUpdate)).
			Int("removed", len(toRemove)).
			Msg("addresses reconciled")
	}

	if err := nm.reconcileDefaultRoutes(link, expectedGateways, family, protocol); err != nil {
		l.Warn().Err(err).Msg("failed to reconcile default route")
	}

	return nil
}
