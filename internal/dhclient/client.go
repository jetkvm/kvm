package dhclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
	"github.com/vishvananda/netlink"
)

const (
	VendorIdentifier = "jetkvm"
)

var (
	logger = logging.GetSubsystemLogger("dhclient")

	ErrIPv6LinkTimeout     = errors.New("timeout after waiting for a non-tentative IPv6 address")
	ErrIPv6RouteTimeout    = errors.New("timeout after waiting for an IPv6 route")
	ErrInterfaceUpTimeout  = errors.New("timeout after waiting for an interface to come up")
	ErrInterfaceUpCanceled = errors.New("context canceled while waiting for an interface to come up")
)

type LeaseChangeHandler func(lease *Lease)

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
}

type Client struct {
	ifaces []netlink.Link
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
}

// NewClient creates a new DHCP client for the given interface.
func NewClient(ctx context.Context, ifaces []netlink.Link, c *Config, l *zerolog.Logger) (*Client, error) {
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

		currentLease4: nil,
		currentLease6: nil,

		lease4Mu: sync.Mutex{},
		lease6Mu: sync.Mutex{},

		mu:    sync.Mutex{},
		cfgMu: sync.Mutex{},
	}, nil
}

func (c *Client) ensureInterfaceUp(iface netlink.Link) (netlink.Link, error) {
	ifname := iface.Attrs().Name

	l := c.l.With().Str("interface", ifname).Logger()

	linkUpTimeout := time.After(c.cfg.LinkUpTimeout)

	for {
		link, err := netlink.LinkByName(ifname)
		if err != nil {
			return nil, err
		}

		state := link.Attrs().OperState
		if state == netlink.OperUp || state == netlink.OperUnknown {
			return link, nil
		}

		l.Info().Interface("state", state).Msg("bringing up interface")

		if err = netlink.LinkSetUp(link); err != nil {
			l.Error().Err(err).Msg("interface can't make it up")
		}

		select {
		case <-time.After(100 * time.Millisecond):
			continue
		case <-c.ctx.Done():
			if err != nil {
				return nil, err
			}
			return nil, ErrInterfaceUpCanceled
		case <-linkUpTimeout:
			l.Error().Msg("interface is still down after timeout")
			if err != nil {
				return nil, err
			}
			return nil, ErrInterfaceUpTimeout
		}
	}
}

func (c *Client) sendInitialRequests() chan interface{} {
	return c.sendRequests(c.cfg.IPv4, c.cfg.IPv6)
}

func (c *Client) sendRequests(ipv4, ipv6 bool) chan interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Yeah, this is a hack, until we can cancel all leases in progress.
	r := make(chan interface{}, 3*len(c.ifaces))

	var wg sync.WaitGroup
	for _, iface := range c.ifaces {
		wg.Add(1)
		go func(iface netlink.Link) {
			defer wg.Done()

			ifname := iface.Attrs().Name
			l := c.l.With().Str("interface", ifname).Logger()

			iface, err := c.ensureInterfaceUp(iface)
			if err != nil {
				l.Error().Err(err).Msg("Could not bring up interface")
				return
			}

			if ipv4 {
				wg.Add(1)
				go func(iface netlink.Link) {
					defer wg.Done()
					lease, err := c.requestLease4(iface)
					if err != nil {
						l.Error().Err(err).Msg("Could not get IPv4 lease")
						return
					}
					r <- lease
				}(iface)
			}

			if ipv6 {
				return // TODO: implement DHCP6
				wg.Add(1)
				go func(iface netlink.Link) {
					defer wg.Done()
					lease, err := c.requestLease6(iface)
					if err != nil {
						l.Error().Err(err).Msg("Could not get IPv6 lease")
						return
					}
					r <- lease
				}(iface)
			}
		}(iface)
	}

	go func() {
		wg.Wait()
		close(r)
	}()
	return r
}

func (c *Client) Lease4() *Lease {
	c.lease4Mu.Lock()
	defer c.lease4Mu.Unlock()

	return c.currentLease4
}

func (c *Client) Lease6() *Lease {
	c.lease6Mu.Lock()
	defer c.lease6Mu.Unlock()

	return c.currentLease6
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
			gocron.DurationJob(lease.RenewalTime),
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
		c.cfg.OnLease4Change(lease)
	}

	if c.cfg.OnLease6Change != nil && !ipv4 {
		c.cfg.OnLease6Change(lease)
	}
}

func (c *Client) renew() {
	for lease := range c.sendRequests(c.cfg.IPv4, c.cfg.IPv6) {
		if lease, ok := lease.(*Lease); ok {
			c.handleLeaseChange(lease)
		}
	}
}

func (c *Client) Renew() {
	go c.renew()
}

func (c *Client) Release() {
	// TODO: implement
}

func (c *Client) SetIPv4(ipv4 bool) {
	c.cfgMu.Lock()
	defer c.cfgMu.Unlock()

	currentIPv4 := c.cfg.IPv4
	c.cfg.IPv4 = ipv4

	if !ipv4 {
		c.lease4Mu.Lock()
		c.currentLease4 = nil
		c.lease4Mu.Unlock()
		c.scheduler.RemoveByTags("ipv4")
	}

	if currentIPv4 || ipv4 {
		// TODO: send initial requests
	}
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

	c.l.Info().
		Str("interface", iface).
		Interface("nameservers", nameservers).
		Interface("searchList", searchList).
		Str("domain", domain).
		Msg("updating resolv.conf")

	if err := updateResolvConf(iface, nameservers, searchList, domain); err != nil {
		c.l.Error().Err(err).Msg("failed to update resolv.conf")
	}
}
