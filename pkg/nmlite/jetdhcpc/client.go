package jetdhcpc

import (
	"context"
	"errors"
	"net"
	"slices"

	"time"

	"github.com/jetkvm/kvm/internal/sync"
	"github.com/jetkvm/kvm/pkg/nmlite/link"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/jetkvm/kvm/internal/network/types"
	"github.com/rs/zerolog"
)

const (
	VendorIdentifier = "jetkvm"
)

var (
	ErrIPv6LinkTimeout     = errors.New("timeout after waiting for a non-tentative IPv6 address")
	ErrIPv6RouteTimeout    = errors.New("timeout after waiting for an IPv6 route")
	ErrInterfaceUpTimeout  = errors.New("timeout after waiting for an interface to come up")
	ErrInterfaceUpCanceled = errors.New("context canceled while waiting for an interface to come up")
)

type LeaseChangeHandler func(lease *types.DHCPLease)

// Config is a DHCP client configuration.
type Config struct {
	LinkUpTimeout time.Duration

	// Timeout is the timeout for one DHCP request attempt.
	Timeout time.Duration

	// Retries is how many times to retry DHCP attempts.
	Retries int

	// IPv4 is whether to request an IPv4 lease.
	IPv4 bool

	// IPv6 is whether to request an IPv6 lease.
	IPv6 bool

	// Modifiers4 allows modifications to the IPv4 DHCP request.
	Modifiers4 []dhcpv4.Modifier

	// Modifiers6 allows modifications to the IPv6 DHCP request.
	Modifiers6 []dhcpv6.Modifier

	// V6ServerAddr can be a unicast or broadcast destination for DHCPv6
	// messages.
	//
	// If not set, it will default to nclient6's default (all servers &
	// relay agents).
	V6ServerAddr *net.UDPAddr

	// V6ClientPort is the port that is used to send and receive DHCPv6
	// messages.
	//
	// If not set, it will default to dhcpv6's default (546).
	V6ClientPort *int

	// V4ServerAddr can be a unicast or broadcast destination for IPv4 DHCP
	// messages.
	//
	// If not set, it will default to nclient4's default (DHCP broadcast
	// address).
	V4ServerAddr *net.UDPAddr

	// If true, add Client Identifier (61) option to the IPv4 request.
	V4ClientIdentifier bool

	Hostname string

	OnLease4Change LeaseChangeHandler
	OnLease6Change LeaseChangeHandler

	UpdateResolvConf func([]string) error
}

// Client is a DHCP client.
type Client struct {
	types.DHCPClient

	ifaces []string
	cfg    Config
	l      *zerolog.Logger

	ctx context.Context

	// TODO: support multiple interfaces
	currentLease4 *Lease
	currentLease6 *Lease

	mu    sync.Mutex
	cfgMu sync.Mutex

	lease4Mu sync.Mutex
	lease6Mu sync.Mutex

	timer4   *time.Timer
	timer6   *time.Timer
	stateDir string
}

var (
	defaultTimerDuration      = 1 * time.Second
	defaultLinkUpTimeout      = 30 * time.Second
	defaultDHCPTimeout        = 5 * time.Second // DHCP request timeout (not link up timeout)
	maxRenewalAttemptDuration = 2 * time.Hour
)

// NewClient creates a new DHCP client for the given interface.
func NewClient(ctx context.Context, ifaces []string, c *Config, l *zerolog.Logger) (*Client, error) {
	timer4 := time.NewTimer(defaultTimerDuration)
	timer6 := time.NewTimer(defaultTimerDuration)

	cfg := *c
	if cfg.LinkUpTimeout == 0 {
		cfg.LinkUpTimeout = defaultLinkUpTimeout
	}

	if cfg.Timeout == 0 {
		cfg.Timeout = defaultDHCPTimeout
	}

	if cfg.Retries == 0 {
		cfg.Retries = 4
	}

	return &Client{
		ctx:      ctx,
		ifaces:   ifaces,
		cfg:      cfg,
		l:        l,
		stateDir: "/run/jetkvm-dhcp",

		currentLease4: nil,
		currentLease6: nil,

		lease4Mu: sync.Mutex{},
		lease6Mu: sync.Mutex{},

		mu:    sync.Mutex{},
		cfgMu: sync.Mutex{},

		timer4: timer4,
		timer6: timer6,
	}, nil
}

func resetTimer(t *time.Timer, attempt int, l *zerolog.Logger) {
	// Exponential backoff: 1s, 2s, 4s, 8s, max 8s
	backoffAttempt := attempt
	if backoffAttempt > 3 {
		backoffAttempt = 3
	}
	delay := time.Duration(1<<backoffAttempt) * time.Second
	l.Debug().Dur("delay", delay).Int("attempt", attempt).Msg("will retry later")
	t.Reset(delay)
}

func getRenewalTime(lease *Lease) time.Duration {
	if lease.RenewalTime <= 0 || lease.LeaseTime > maxRenewalAttemptDuration/2 {
		return maxRenewalAttemptDuration
	}

	return lease.RenewalTime
}

func (c *Client) requestLoop(t *time.Timer, family int, ifname string) {
	l := c.l.With().Str("interface", ifname).Int("family", family).Logger()
	attempt := 0
	for range t.C {
		l.Info().Int("attempt", attempt).Msg("requesting lease")

		if _, err := c.ensureInterfaceUp(ifname); err != nil {
			l.Error().Err(err).Int("attempt", attempt).Msg("failed to ensure interface up")
			resetTimer(t, attempt, c.l)
			attempt++
			continue
		}

		var (
			lease *Lease
			err   error
		)
		switch family {
		case link.AfInet:
			lease, err = c.requestLease4(ifname)
		case link.AfInet6:
			lease, err = c.requestLease6(ifname)
		}
		if err != nil {
			l.Error().Err(err).Int("attempt", attempt).Msg("failed to request lease")
			resetTimer(t, attempt, c.l)
			attempt++
			continue
		}

		// Successfully obtained lease, reset attempt counter
		attempt = 0
		c.handleLeaseChange(lease)

		nextRenewal := getRenewalTime(lease)

		l.Info().
			Dur("nextRenewal", nextRenewal).
			Dur("leaseTime", lease.LeaseTime).
			Dur("rebindingTime", lease.RebindingTime).
			Msg("sleeping until next renewal")

		t.Reset(nextRenewal)
	}
}

func (c *Client) ensureInterfaceUp(ifname string) (*link.Link, error) {
	nlm := link.GetNetlinkManager()
	iface, err := nlm.GetLinkByName(ifname)
	if err != nil {
		return nil, err
	}
	return nlm.EnsureInterfaceUpWithTimeout(c.ctx, iface, c.cfg.LinkUpTimeout)
}

// Lease4 returns the current IPv4 lease
func (c *Client) Lease4() *types.DHCPLease {
	c.lease4Mu.Lock()
	defer c.lease4Mu.Unlock()

	if c.currentLease4 == nil {
		return nil
	}

	return c.currentLease4.ToDHCPLease()
}

// Lease6 returns the current IPv6 lease
func (c *Client) Lease6() *types.DHCPLease {
	c.lease6Mu.Lock()
	defer c.lease6Mu.Unlock()

	if c.currentLease6 == nil {
		return nil
	}

	return c.currentLease6.ToDHCPLease()
}

// Domain returns the current domain
func (c *Client) Domain() string {
	c.lease4Mu.Lock()
	defer c.lease4Mu.Unlock()

	if c.currentLease4 != nil {
		return c.currentLease4.Domain
	}

	c.lease6Mu.Lock()
	defer c.lease6Mu.Unlock()

	if c.currentLease6 != nil {
		return c.currentLease6.Domain
	}

	return ""
}

// handleLeaseChange handles lease changes
func (c *Client) handleLeaseChange(lease *Lease) {
	// do not use defer here, because we need to unlock the mutex before returning
	ipv4 := lease.p4 != nil

	if ipv4 {
		c.lease4Mu.Lock()
		c.currentLease4 = lease
		c.lease4Mu.Unlock()
	} else {
		c.lease6Mu.Lock()
		c.currentLease6 = lease
		c.lease6Mu.Unlock()
	}

	c.apply()

	// TODO: handle lease expiration
	if c.cfg.OnLease4Change != nil && ipv4 {
		c.cfg.OnLease4Change(lease.ToDHCPLease())
	}

	if c.cfg.OnLease6Change != nil && !ipv4 {
		c.cfg.OnLease6Change(lease.ToDHCPLease())
	}
}

func (c *Client) Renew() error {
	c.timer4.Reset(defaultTimerDuration)
	c.timer6.Reset(defaultTimerDuration)
	return nil
}

func (c *Client) Release() error {
	// TODO: implement
	return nil
}

func (c *Client) SetIPv4(ipv4 bool) {
	c.cfgMu.Lock()
	defer c.cfgMu.Unlock()

	currentIPv4 := c.cfg.IPv4
	c.cfg.IPv4 = ipv4

	if currentIPv4 == ipv4 {
		return
	}

	if !ipv4 {
		c.lease4Mu.Lock()
		c.currentLease4 = nil
		c.lease4Mu.Unlock()

		c.timer4.Stop()
	}

	c.timer4.Reset(defaultTimerDuration)
}

func (c *Client) SetIPv6(ipv6 bool) {
	c.cfgMu.Lock()
	defer c.cfgMu.Unlock()

	currentIPv6 := c.cfg.IPv6
	c.cfg.IPv6 = ipv6

	if currentIPv6 == ipv6 {
		return
	}

	if !ipv6 {
		c.lease6Mu.Lock()
		c.currentLease6 = nil
		c.lease6Mu.Unlock()

		c.timer6.Stop()
	}

	c.timer6.Reset(defaultTimerDuration)
}

func (c *Client) Start() error {
	if err := c.killUdhcpc(); err != nil {
		c.l.Warn().Err(err).Msg("failed to kill udhcpc processes, continuing anyway")
	}

	for _, iface := range c.ifaces {
		if c.cfg.IPv4 {
			go c.requestLoop(c.timer4, link.AfInet, iface)
		}
		if c.cfg.IPv6 {
			go c.requestLoop(c.timer6, link.AfInet6, iface)
		}
	}

	return nil
}

func (c *Client) apply() {
	var (
		iface       string
		nameservers []net.IP
		searchList  []string
		domain      string
	)

	if c.currentLease4 != nil {
		iface = c.currentLease4.InterfaceName
		nameservers = c.currentLease4.DNS
		searchList = c.currentLease4.SearchList
		domain = c.currentLease4.Domain
	}

	if c.currentLease6 != nil {
		iface = c.currentLease6.InterfaceName
		nameservers = append(nameservers, c.currentLease6.DNS...)
		searchList = append(searchList, c.currentLease6.SearchList...)
		domain = c.currentLease6.Domain
	}

	// deduplicate searchList
	searchList = slices.Compact(searchList)

	if c.cfg.UpdateResolvConf == nil {
		c.l.Warn().Msg("no UpdateResolvConf function set, skipping resolv.conf update")
		return
	}

	c.l.Info().
		Str("interface", iface).
		Interface("nameservers", nameservers).
		Interface("searchList", searchList).
		Str("domain", domain).
		Msg("updating resolv.conf")

	// Convert net.IP to string slice
	var nameserverStrings []string
	for _, ns := range nameservers {
		nameserverStrings = append(nameserverStrings, ns.String())
	}

	if err := c.cfg.UpdateResolvConf(nameserverStrings); err != nil {
		c.l.Error().Err(err).Msg("failed to update resolv.conf")
	}
}
