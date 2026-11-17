//go:build linux

package wireguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jetkvm/kvm/internal/sync"
	"github.com/rs/zerolog"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

const (
	// InterfaceName is the WireGuard interface the device creates.
	InterfaceName = "wg0"
	// RouteTable holds the routes to the peers' allowed IPs. It is only
	// consulted for traffic sent from the tunnel addresses (see RulePriority).
	RouteTable = 51820
	// RulePriority is the priority of the "from <tunnel address> lookup
	// RouteTable" rules; it comes before the main table (32766).
	RulePriority = 31000

	moduleName = "wireguard"

	// endpoints given as host names are resolved again while a peer with a
	// keepalive has not completed a handshake for longer than staleHandshake
	resolveInterval = 30 * time.Second
	staleHandshake  = 3 * time.Minute
	resolveTimeout  = 5 * time.Second
)

// Supported reports whether the kernel has WireGuard, either loaded or as a
// module that can be loaded.
func Supported() bool {
	if _, err := os.Stat("/sys/module/" + moduleName); err == nil {
		return true
	}
	var uts unix.Utsname
	if err := unix.Uname(&uts); err != nil {
		return false
	}
	release := unix.ByteSliceToString(uts.Release[:])
	dep, err := os.ReadFile("/lib/modules/" + release + "/modules.dep")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(dep), "\n") {
		name, _, _ := strings.Cut(line, ":")
		name = name[strings.LastIndexByte(name, '/')+1:]
		if name == moduleName+".ko" || strings.HasPrefix(name, moduleName+".ko.") {
			return true
		}
	}
	return false
}

// Manager keeps the WireGuard interface in line with a Config.
type Manager struct {
	logger *zerolog.Logger
	// applyMu serializes Apply, which can wait on DNS; mu guards the fields
	// below so Status does not wait for it.
	applyMu sync.Mutex
	mu      sync.Mutex
	cfg     Config
	lastErr string
	// stopResolver stops the endpoint resolver of the applied config and
	// waits for it to exit, so it cannot write an endpoint after Apply.
	stopResolver func()
}

// NewManager returns a Manager. Nothing changes on the device until Apply.
func NewManager(logger *zerolog.Logger) *Manager {
	return &Manager{logger: logger}
}

// Apply brings the interface in line with cfg, which must have been
// validated: it is set up when cfg is enabled and removed otherwise. The
// error is also reported in Status until the next Apply.
func (m *Manager) Apply(cfg Config) error {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()

	m.mu.Lock()
	stop := m.stopResolver
	m.stopResolver = nil
	m.cfg = cfg
	m.lastErr = ""
	m.mu.Unlock()
	if stop != nil {
		stop()
	}

	var err error
	if cfg.Enabled {
		err = m.setUp(cfg)
	} else {
		err = tearDown()
	}
	if err != nil {
		m.mu.Lock()
		m.lastErr = err.Error()
		m.mu.Unlock()
		m.logger.Error().Err(err).Bool("enabled", cfg.Enabled).Msg("failed to apply WireGuard config")
		return err
	}

	if cfg.Enabled {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		m.mu.Lock()
		m.stopResolver = func() {
			cancel()
			<-done
		}
		m.mu.Unlock()
		go func() {
			defer close(done)
			m.resolveEndpoints(ctx, cfg)
		}()
		m.logger.Info().Strs("addresses", cfg.Addresses).Int("peers", len(cfg.Peers)).Msg("WireGuard interface up")
	} else {
		m.logger.Info().Msg("WireGuard interface removed")
	}
	return nil
}

// SetError records a config that could not be applied, such as an invalid
// stored config at boot, so Status reports it.
func (m *Manager) SetError(cfg Config, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
	m.lastErr = err.Error()
}

func (m *Manager) setUp(cfg Config) error {
	if err := loadModule(); err != nil {
		return err
	}

	link, err := ensureLink()
	if err != nil {
		return err
	}

	devCfg, err := m.deviceConfig(cfg)
	if err != nil {
		return err
	}
	client, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("failed to open WireGuard control: %w", err)
	}
	defer client.Close()
	if err := client.ConfigureDevice(InterfaceName, *devCfg); err != nil {
		return fmt.Errorf("failed to configure %s: %w", InterfaceName, err)
	}

	addrs := make([]netip.Prefix, 0, len(cfg.Addresses))
	for _, a := range cfg.Addresses {
		p, err := parseAddress(a)
		if err != nil {
			return err
		}
		addrs = append(addrs, p)
	}
	if err := reconcileAddresses(link, addrs); err != nil {
		return err
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to bring %s up: %w", InterfaceName, err)
	}

	var allowed []netip.Prefix
	for _, p := range cfg.Peers {
		for _, a := range p.AllowedIPs {
			prefix, err := parseAllowedIP(a)
			if err != nil {
				return err
			}
			allowed = append(allowed, prefix)
		}
	}
	if err := reconcileRoutes(link, allowed); err != nil {
		return err
	}
	return reconcileRules(addrs)
}

// deviceConfig builds the wgctrl config. An endpoint that cannot be resolved
// yet, for example while DNS is not up at boot, is left out and filled in by
// the resolver. Endpoints are resolved in parallel so a save waits for at
// most one resolveTimeout.
func (m *Manager) deviceConfig(cfg Config) (*wgtypes.Config, error) {
	endpoints := make([]*net.UDPAddr, len(cfg.Peers))
	var wg sync.WaitGroup
	for i, p := range cfg.Peers {
		if p.Endpoint == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ep, err := resolveEndpoint(context.Background(), p.Endpoint)
			if err != nil {
				m.logger.Warn().Err(err).Str("endpoint", p.Endpoint).Msg("failed to resolve WireGuard endpoint, will retry")
				return
			}
			endpoints[i] = ep
		}()
	}
	wg.Wait()

	priv, err := wgtypes.ParseKey(cfg.PrivateKey)
	if err != nil {
		return nil, errors.New("private key is not a valid WireGuard key")
	}
	port := cfg.ListenPort
	devCfg := &wgtypes.Config{
		PrivateKey:   &priv,
		ListenPort:   &port,
		ReplacePeers: true,
		Peers:        make([]wgtypes.PeerConfig, 0, len(cfg.Peers)),
	}

	for i, p := range cfg.Peers {
		pub, err := wgtypes.ParseKey(p.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("peer %s: public key is not a valid WireGuard key", p.PublicKey)
		}
		keepalive := time.Duration(p.PersistentKeepalive) * time.Second
		pc := wgtypes.PeerConfig{
			PublicKey:                   pub,
			PersistentKeepaliveInterval: &keepalive,
			ReplaceAllowedIPs:           true,
		}
		if p.PresharedKey != "" {
			psk, err := wgtypes.ParseKey(p.PresharedKey)
			if err != nil {
				return nil, fmt.Errorf("peer %s: preshared key is not a valid WireGuard key", p.PublicKey)
			}
			pc.PresharedKey = &psk
		}
		pc.Endpoint = endpoints[i]
		for _, a := range p.AllowedIPs {
			prefix, err := parseAllowedIP(a)
			if err != nil {
				return nil, err
			}
			pc.AllowedIPs = append(pc.AllowedIPs, *prefixToIPNet(prefix))
		}
		devCfg.Peers = append(devCfg.Peers, pc)
	}
	return devCfg, nil
}

func loadModule() error {
	if _, err := os.Stat("/sys/module/" + moduleName); err == nil {
		return nil
	}
	out, err := exec.Command("modprobe", moduleName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to load the WireGuard kernel module: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ensureLink() (netlink.Link, error) {
	link, err := netlink.LinkByName(InterfaceName)
	if err == nil {
		if link.Type() != "wireguard" {
			return nil, fmt.Errorf("interface %s exists and is not a WireGuard interface", InterfaceName)
		}
		return link, nil
	}
	var notFound netlink.LinkNotFoundError
	if !errors.As(err, &notFound) {
		return nil, fmt.Errorf("failed to look up %s: %w", InterfaceName, err)
	}

	attrs := netlink.NewLinkAttrs()
	attrs.Name = InterfaceName
	if err := netlink.LinkAdd(&netlink.Wireguard{LinkAttrs: attrs}); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", InterfaceName, err)
	}
	return netlink.LinkByName(InterfaceName)
}

// reconcileAddresses gives the link exactly the given addresses, each as a
// single host (/32 or /128). A shorter prefix would put a route to the whole
// VPN range in the main table, sending replies to VPN clients that reached
// the device over the LAN into the tunnel.
func reconcileAddresses(link netlink.Link, addrs []netip.Prefix) error {
	want := make(map[netip.Addr]bool, len(addrs))
	for _, a := range addrs {
		want[a.Addr()] = true
	}

	current, err := netlink.AddrList(link, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to list addresses of %s: %w", InterfaceName, err)
	}
	for _, a := range current {
		ip, ok := netip.AddrFromSlice(a.IP)
		ones, bits := a.Mask.Size()
		if ok && want[ip.Unmap()] && ones == bits {
			delete(want, ip.Unmap())
			continue
		}
		if err := netlink.AddrDel(link, &a); err != nil {
			return fmt.Errorf("failed to remove address %s from %s: %w", a.IPNet, InterfaceName, err)
		}
	}

	for ip := range want {
		addr := &netlink.Addr{IPNet: prefixToIPNet(netip.PrefixFrom(ip, ip.BitLen()))}
		if ip.Is6() {
			// no duplicate address detection: the tunnel has no other hosts
			addr.Flags = unix.IFA_F_NODAD
		}
		if err := netlink.AddrAdd(link, addr); err != nil {
			return fmt.Errorf("failed to add address %s to %s: %w", ip, InterfaceName, err)
		}
	}
	return nil
}

// reconcileRoutes makes RouteTable route exactly the allowed IPs to the link.
func reconcileRoutes(link netlink.Link, allowed []netip.Prefix) error {
	filter := &netlink.Route{Table: RouteTable}
	current, err := netlink.RouteListFiltered(netlink.FAMILY_ALL, filter, netlink.RT_FILTER_TABLE)
	if err != nil {
		return fmt.Errorf("failed to list routes in table %d: %w", RouteTable, err)
	}
	for _, r := range current {
		if err := netlink.RouteDel(&r); err != nil {
			return fmt.Errorf("failed to remove route %s from table %d: %w", r.Dst, RouteTable, err)
		}
	}

	for _, prefix := range allowed {
		route := &netlink.Route{
			LinkIndex: link.Attrs().Index,
			Dst:       prefixToIPNet(prefix),
			Table:     RouteTable,
			Scope:     netlink.SCOPE_LINK,
		}
		if err := netlink.RouteReplace(route); err != nil {
			return fmt.Errorf("failed to add route %s to table %d: %w", prefix, RouteTable, err)
		}
	}
	return nil
}

// reconcileRules makes traffic from each tunnel address, and only that, use
// RouteTable.
func reconcileRules(addrs []netip.Prefix) error {
	if err := deleteRules(); err != nil {
		return err
	}
	for _, a := range addrs {
		rule := netlink.NewRule()
		rule.Priority = RulePriority
		rule.Table = RouteTable
		rule.Src = prefixToIPNet(netip.PrefixFrom(a.Addr(), a.Addr().BitLen()))
		rule.Family = netlink.FAMILY_V4
		if a.Addr().Is6() {
			rule.Family = netlink.FAMILY_V6
		}
		if err := netlink.RuleAdd(rule); err != nil {
			return fmt.Errorf("failed to add routing rule for %s: %w", a.Addr(), err)
		}
	}
	return nil
}

func deleteRules() error {
	for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		filter := netlink.NewRule()
		filter.Table = RouteTable
		rules, err := netlink.RuleListFiltered(family, filter, netlink.RT_FILTER_TABLE)
		if err != nil {
			return fmt.Errorf("failed to list routing rules: %w", err)
		}
		for _, r := range rules {
			if err := netlink.RuleDel(&r); err != nil {
				return fmt.Errorf("failed to remove routing rule %s: %w", r, err)
			}
		}
	}
	return nil
}

// tearDown removes the rules and the interface; the kernel drops the routes
// in RouteTable along with the interface.
func tearDown() error {
	if err := deleteRules(); err != nil {
		return err
	}
	link, err := netlink.LinkByName(InterfaceName)
	if err != nil {
		var notFound netlink.LinkNotFoundError
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("failed to look up %s: %w", InterfaceName, err)
	}
	if link.Type() != "wireguard" {
		return nil
	}
	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("failed to remove %s: %w", InterfaceName, err)
	}
	return nil
}

// resolveEndpoints resolves host name endpoints again while their peer has
// no recent handshake, so the tunnel recovers when DNS was not ready at boot
// or the server's address changed.
func (m *Manager) resolveEndpoints(ctx context.Context, cfg Config) {
	ticker := time.NewTicker(resolveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		client, err := wgctrl.New()
		if err != nil {
			continue
		}
		dev, err := client.Device(InterfaceName)
		if err != nil {
			client.Close()
			continue
		}
		current := make(map[wgtypes.Key]wgtypes.Peer, len(dev.Peers))
		for _, p := range dev.Peers {
			current[p.PublicKey] = p
		}

		var updates []wgtypes.PeerConfig
		for _, p := range cfg.Peers {
			pub, err := wgtypes.ParseKey(p.PublicKey)
			if err != nil || p.Endpoint == "" {
				continue
			}
			host, _, _ := splitEndpoint(p.Endpoint)
			peer, ok := current[pub]
			if !ok {
				continue
			}
			// A literal IP never changes. A resolved name is kept while it
			// works; without a keepalive an idle tunnel has no handshakes, so
			// a stale handshake says nothing and the name is kept as well.
			if peer.Endpoint != nil {
				if net.ParseIP(host) != nil || p.PersistentKeepalive == 0 {
					continue
				}
				if time.Since(peer.LastHandshakeTime) < staleHandshake {
					continue
				}
			}
			ep, err := resolveEndpoint(ctx, p.Endpoint)
			if err != nil {
				m.logger.Debug().Err(err).Str("endpoint", p.Endpoint).Msg("failed to resolve WireGuard endpoint")
				continue
			}
			if peer.Endpoint != nil && peer.Endpoint.IP.Equal(ep.IP) && peer.Endpoint.Port == ep.Port {
				continue
			}
			updates = append(updates, wgtypes.PeerConfig{PublicKey: pub, UpdateOnly: true, Endpoint: ep})
		}

		// the config may have been replaced while resolving
		if len(updates) > 0 && ctx.Err() == nil {
			if err := client.ConfigureDevice(InterfaceName, wgtypes.Config{Peers: updates}); err != nil {
				m.logger.Warn().Err(err).Msg("failed to update WireGuard endpoints")
			} else {
				for _, u := range updates {
					m.logger.Info().Stringer("endpoint", u.Endpoint).Msg("updated WireGuard endpoint")
				}
			}
		}
		client.Close()
	}
}

func resolveEndpoint(ctx context.Context, endpoint string) (*net.UDPAddr, error) {
	host, port, err := splitEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		return &net.UDPAddr{IP: ip, Port: port}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	// prefer IPv4: it works on more networks
	for _, ip := range ips {
		if ip.To4() != nil {
			return &net.UDPAddr{IP: ip, Port: port}, nil
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses found for %s", host)
	}
	return &net.UDPAddr{IP: ips[0], Port: port}, nil
}

// Status reports the applied config and the live state of the interface.
func (m *Manager) Status() Status {
	m.mu.Lock()
	cfg := m.cfg
	lastErr := m.lastErr
	m.mu.Unlock()

	st := Status{Enabled: cfg.Enabled, Error: lastErr, Peers: []PeerStatus{}}
	names := make(map[string]string, len(cfg.Peers))
	for _, p := range cfg.Peers {
		names[p.PublicKey] = p.Name
	}

	link, err := netlink.LinkByName(InterfaceName)
	if err != nil || link.Type() != "wireguard" {
		return st
	}
	st.Up = link.Attrs().Flags&net.FlagUp != 0

	client, err := wgctrl.New()
	if err != nil {
		return st
	}
	defer client.Close()
	dev, err := client.Device(InterfaceName)
	if err != nil {
		return st
	}
	st.ListenPort = dev.ListenPort
	for _, p := range dev.Peers {
		ps := PeerStatus{
			Name:      names[p.PublicKey.String()],
			PublicKey: p.PublicKey.String(),
			RxBytes:   p.ReceiveBytes,
			TxBytes:   p.TransmitBytes,
		}
		if p.Endpoint != nil {
			ps.Endpoint = p.Endpoint.String()
		}
		if !p.LastHandshakeTime.IsZero() {
			secs := int64(time.Since(p.LastHandshakeTime).Seconds())
			ps.SecondsSinceHandshake = &secs
		}
		st.Peers = append(st.Peers, ps)
	}
	return st
}

func prefixToIPNet(p netip.Prefix) *net.IPNet {
	return &net.IPNet{
		IP:   net.IP(p.Addr().AsSlice()),
		Mask: net.CIDRMask(p.Bits(), p.Addr().BitLen()),
	}
}
