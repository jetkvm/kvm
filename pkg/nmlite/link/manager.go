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
		&netlink.Route{Dst: nil, Table: 254},
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

func (nm *NetlinkManager) reconcileDefaultRoute(link *Link, expected map[string]net.IP, family int) error {
	linkIndex := link.Attrs().Index

	added := 0
	toRemove := make([]*netlink.Route, 0)

	defaultRoutes, err := nm.ListDefaultRoutes(family)
	if err != nil {
		return fmt.Errorf("failed to get default routes: %w", err)
	}

	// check existing default routes
	for _, defaultRoute := range defaultRoutes {
		// only check the default routes for the current link
		// TODO: we should also check others later
		if defaultRoute.LinkIndex != linkIndex {
			continue
		}

		key := defaultRoute.Gw.String()
		if _, ok := expected[key]; !ok {
			toRemove = append(toRemove, &defaultRoute)
			continue
		}

		nm.logger.Warn().Str("gateway", key).Msg("keeping default route")
		delete(expected, key)
	}

	// remove remaining default routes
	for _, defaultRoute := range toRemove {
		nm.logger.Warn().Str("gateway", defaultRoute.Gw.String()).Msg("removing default route")
		if err := nm.RouteDel(defaultRoute); err != nil {
			nm.logger.Warn().Err(err).Msg("failed to remove default route")
		}
	}

	// add remaining expected default routes
	for _, gateway := range expected {
		nm.logger.Warn().Str("gateway", gateway.String()).Msg("adding default route")

		route := &netlink.Route{
			Dst:       &ipv4DefaultRoute,
			Gw:        gateway,
			LinkIndex: linkIndex,
		}
		if family == AfInet6 {
			route.Dst = &ipv6DefaultRoute
		}
		if err := nm.RouteAdd(route); err != nil {
			nm.logger.Warn().Err(err).Interface("route", route).Msg("failed to add default route")
		}
		added++
	}

	nm.logger.Info().
		Int("added", added).
		Int("removed", len(toRemove)).
		Msg("default routes reconciled")

	return nil
}

// ReconcileLink reconciles the addresses and routes of a link
func (nm *NetlinkManager) ReconcileLink(link *Link, expected []types.IPAddress, family int) error {
	toAdd := make([]*types.IPAddress, 0)
	toRemove := make([]*netlink.Addr, 0)
	toUpdate := make([]*types.IPAddress, 0)
	expectedAddrs := make(map[string]*types.IPAddress)

	expectedGateways := make(map[string]net.IP)

	mtu := link.Attrs().MTU
	expectedMTU := mtu
	// add all expected addresses to the map
	for _, addr := range expected {
		expectedAddrs[addr.String()] = &addr
		if addr.Gateway != nil {
			expectedGateways[addr.String()] = addr.Gateway
		}
		if addr.MTU != 0 {
			mtu = addr.MTU
		}
	}
	if expectedMTU != mtu {
		if err := link.SetMTU(expectedMTU); err != nil {
			nm.logger.Warn().Err(err).Int("expected_mtu", expectedMTU).Int("mtu", mtu).Msg("failed to set MTU")
		}
	}

	addrs, err := nm.AddrList(link, family)
	if err != nil {
		return fmt.Errorf("failed to get addresses: %w", err)
	}

	// check existing addresses
	for _, addr := range addrs {
		// skip the link-local address
		if addr.IP.IsLinkLocalUnicast() {
			continue
		}

		expectedAddr, ok := expectedAddrs[addr.IPNet.String()]
		if !ok {
			toRemove = append(toRemove, &addr)
			continue
		}

		// if it's not fully equal, we need to update it
		if !expectedAddr.Compare(addr) {
			toUpdate = append(toUpdate, expectedAddr)
			continue
		}

		// remove it from expected addresses
		delete(expectedAddrs, addr.IPNet.String())
	}

	// add remaining expected addresses
	for _, addr := range expectedAddrs {
		toAdd = append(toAdd, addr)
	}

	for _, addr := range toUpdate {
		netlinkAddr := addr.NetlinkAddr()
		if err := nm.AddrDel(link, &netlinkAddr); err != nil {
			nm.logger.Warn().Err(err).Str("address", addr.Address.String()).Msg("failed to update address")
		}
		// we'll add it again later
		toAdd = append(toAdd, addr)
	}

	for _, addr := range toAdd {
		netlinkAddr := addr.NetlinkAddr()
		if err := nm.AddrAdd(link, &netlinkAddr); err != nil {
			nm.logger.Warn().Err(err).Str("address", addr.Address.String()).Msg("failed to add address")
		}
	}

	for _, netlinkAddr := range toRemove {
		if err := nm.AddrDel(link, netlinkAddr); err != nil {
			nm.logger.Warn().Err(err).Str("address", netlinkAddr.IP.String()).Msg("failed to remove address")
		}
	}

	for _, addr := range toAdd {
		netlinkAddr := addr.NetlinkAddr()
		if err := nm.AddrAdd(link, &netlinkAddr); err != nil {
			nm.logger.Warn().Err(err).Str("address", addr.Address.String()).Msg("failed to add address")
		}
	}

	actualToAdd := len(toAdd) - len(toUpdate)
	if len(toAdd) > 0 || len(toUpdate) > 0 || len(toRemove) > 0 {
		nm.logger.Info().
			Int("added", actualToAdd).
			Int("updated", len(toUpdate)).
			Int("removed", len(toRemove)).
			Msg("addresses reconciled")
	}

	if err := nm.reconcileDefaultRoute(link, expectedGateways, family); err != nil {
		nm.logger.Warn().Err(err).Msg("failed to reconcile default route")
	}

	return nil
}
