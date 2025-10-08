package jetdhcpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"

	"time"

	"github.com/jetkvm/kvm/internal/sync"

	"github.com/go-co-op/gocron/v2"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/jetkvm/kvm/internal/network/types"
	"github.com/jetkvm/kvm/pkg/nmlite/link"
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

	OnLease4Change LeaseChangeHandler
	OnLease6Change LeaseChangeHandler

	UpdateResolvConf func([]string) error
}

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

	scheduler gocron.Scheduler
	stateDir  string
}

// NewClient creates a new DHCP client for the given interface.
func NewClient(ctx context.Context, ifaces []string, c *Config, l *zerolog.Logger) (*Client, error) {
	scheduler, err := gocron.NewScheduler()
	if err != nil {
		return nil, fmt.Errorf("failed to create scheduler: %w", err)
	}

	cfg := *c
	if cfg.LinkUpTimeout == 0 {
		cfg.LinkUpTimeout = 30 * time.Second
	}

	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	if cfg.Retries == 0 {
		cfg.Retries = 3
	}

	return &Client{
		ctx:       ctx,
		ifaces:    ifaces,
		cfg:       cfg,
		l:         l,
		scheduler: scheduler,
		stateDir:  "/run/jetkvm-dhcp",

		currentLease4: nil,
		currentLease6: nil,

		lease4Mu: sync.Mutex{},
		lease6Mu: sync.Mutex{},

		mu:    sync.Mutex{},
		cfgMu: sync.Mutex{},
	}, nil
}

func (c *Client) ensureInterfaceUp(ifname string) (*link.Link, error) {
	nlm := link.GetNetlinkManager()
	iface, err := nlm.GetLinkByName(ifname)
	if err != nil {
		return nil, err
	}
	return nlm.EnsureInterfaceUpWithTimeout(c.ctx, iface, c.cfg.LinkUpTimeout)
}

func (c *Client) sendInitialRequests() chan any {
	return c.sendRequests(c.cfg.IPv4, c.cfg.IPv6)
}

func (c *Client) sendRequestsFamily(
	family int,
	wg *sync.WaitGroup,
	r *chan any,
	l *zerolog.Logger,
	iface *link.Link,
) {
	wg.Add(1)
	go func(iface *link.Link) {
		defer wg.Done()
		var (
			lease *Lease
			err   error
		)
		switch family {
		case link.AfInet:
			lease, err = c.requestLease4(iface)
		case link.AfInet6:
			lease, err = c.requestLease6(iface)
		}
		if err != nil {
			l.Error().Err(err).Msg("Could not get lease")
			return
		}
		(*r) <- lease
	}(iface)
}

func (c *Client) sendRequests(ipv4, ipv6 bool) chan any {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Yeah, this is a hack, until we can cancel all leases in progress.
	r := make(chan any, 3*len(c.ifaces))

	var wg sync.WaitGroup
	for _, iface := range c.ifaces {
		wg.Add(1)
		go func(ifname string) {
			defer wg.Done()

			l := c.l.With().Str("interface", ifname).Logger()

			iface, err := c.ensureInterfaceUp(ifname)
			if err != nil {
				l.Error().Err(err).Msg("Could not bring up interface")
				return
			}

			if ipv4 {
				c.sendRequestsFamily(link.AfInet, &wg, &r, &l, iface)
			}

			if ipv6 {
				c.sendRequestsFamily(link.AfInet6, &wg, &r, &l, iface)
			}
		}(iface)
	}

	go func() {
		wg.Wait()
		close(r)
	}()
	return r
}

func (c *Client) Lease4() *types.DHCPLease {
	c.lease4Mu.Lock()
	defer c.lease4Mu.Unlock()

	if c.currentLease4 == nil {
		return nil
	}

	return c.currentLease4.ToDHCPLease()
}

func (c *Client) Lease6() *types.DHCPLease {
	c.lease6Mu.Lock()
	defer c.lease6Mu.Unlock()

	if c.currentLease6 == nil {
		return nil
	}

	return c.currentLease6.ToDHCPLease()
}

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

func (c *Client) handleLeaseChange(lease *Lease) {
	// do not use defer here, because we need to unlock the mutex before returning

	ipv4 := lease.p4 != nil
	version := "ipv4"

	if ipv4 {
		c.lease4Mu.Lock()
		c.currentLease4 = lease
	} else {
		version = "ipv6"
		c.lease6Mu.Lock()
		c.currentLease6 = lease
	}

	// clear all current jobs with the same tags
	c.scheduler.RemoveByTags(version)

	// add scheduler job to renew the lease
	if lease.RenewalTime > 0 {
		c.scheduler.NewJob(
			gocron.DurationJob(time.Duration(lease.RenewalTime)*time.Second),
			gocron.NewTask(func() {
				c.l.Info().Msg("renewing lease")
				for lease := range c.sendRequests(ipv4, !ipv4) {
					if lease, ok := lease.(*Lease); ok {
						c.handleLeaseChange(lease)
					}
				}
			}),
			gocron.WithName(fmt.Sprintf("renew-%s", version)),
			gocron.WithSingletonMode(gocron.LimitModeWait),
			gocron.WithTags(version),
		)
	}

	c.apply()

	if ipv4 {
		c.lease4Mu.Unlock()
	} else {
		c.lease6Mu.Unlock()
	}

	// TODO: handle lease expiration
	if c.cfg.OnLease4Change != nil && ipv4 {
		c.cfg.OnLease4Change(lease.ToDHCPLease())
	}

	if c.cfg.OnLease6Change != nil && !ipv4 {
		c.cfg.OnLease6Change(lease.ToDHCPLease())
	}
}

func (c *Client) renew() {
	for lease := range c.sendRequests(c.cfg.IPv4, c.cfg.IPv6) {
		if lease, ok := lease.(*Lease); ok {
			c.handleLeaseChange(lease)
		}
	}
}

func (c *Client) Renew() error {
	go c.renew()
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
		c.scheduler.RemoveByTags("ipv4")
	}

	c.sendRequests(ipv4, c.cfg.IPv6)
}

func (c *Client) SetIPv6(ipv6 bool) {
	c.cfg.IPv6 = ipv6
}

func (c *Client) Start() error {
	if err := c.killUdhcpc(); err != nil {
		c.l.Warn().Err(err).Msg("failed to kill udhcpc processes, continuing anyway")
	}

	c.scheduler.Start()

	go func() {
		for lease := range c.sendInitialRequests() {
			if lease, ok := lease.(*Lease); ok {
				c.handleLeaseChange(lease)
			}
		}
	}()

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
