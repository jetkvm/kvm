package myip

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

type PublicIP struct {
	IPAddress   net.IP    `json:"ip"`
	LastUpdated time.Time `json:"last_updated"`
}

func (pi PublicIP) MarshalZerologObject(e *zerolog.Event) {
	e.IPAddr("address", pi.IPAddress)
	e.Time("updated", pi.LastUpdated)
}

type PublicIPs []PublicIP

func (ips PublicIPs) MarshalZerologArray(a *zerolog.Array) {
	for _, ip := range ips {
		a.Object(ip)
	}
}

type HttpClientGetter func(family int) *http.Client

type PublicIPState struct {
	addresses   PublicIPs
	lastUpdated time.Time

	cloudflareEndpoint string // cdn-cgi/trace domain
	apiEndpoint        string // api endpoint
	ipv4               bool
	ipv6               bool
	httpClient         HttpClientGetter

	timer  *time.Timer
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
}

func (ps *PublicIPState) MarshalZerologObject(e *zerolog.Event) {
	e.Array("addresses", ps.addresses)
	e.Time("lastUpdated", ps.lastUpdated)
	e.Bool("ipv4", ps.ipv4)
	e.Bool("ipv6", ps.ipv6)
}

type PublicIPStateConfig struct {
	CloudflareEndpoint string
	APIEndpoint        string
	IPv4               bool
	IPv6               bool
	HttpClientGetter   HttpClientGetter
}

func stripURLPath(s string) string {
	parsed, err := url.Parse(s)
	if err != nil {
		return ""
	}
	scheme := parsed.Scheme
	if scheme != "http" && scheme != "https" {
		scheme = "https"
	}

	return fmt.Sprintf("%s://%s", scheme, parsed.Host)
}

// NewPublicIPState creates a new PublicIPState
func NewPublicIPState(config *PublicIPStateConfig) *PublicIPState {
	ctx, cancel := context.WithCancel(context.Background())
	ps := &PublicIPState{
		addresses:          make([]PublicIP, 0),
		lastUpdated:        time.Now(),
		cloudflareEndpoint: stripURLPath(config.CloudflareEndpoint),
		apiEndpoint:        config.APIEndpoint,
		ipv4:               config.IPv4,
		ipv6:               config.IPv6,
		httpClient:         config.HttpClientGetter,
		ctx:                ctx,
		cancel:             cancel,
	}
	// Start the timer automatically
	ps.Start()
	return ps
}

// SetFamily sets if we need to track IPv4 and IPv6 public IP addresses
func (ps *PublicIPState) SetIPv4AndIPv6(ipv4, ipv6 bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.ipv4 = ipv4
	ps.ipv6 = ipv6
}

// SetCloudflareEndpoint sets the Cloudflare endpoint
func (ps *PublicIPState) SetCloudflareEndpoint(endpoint string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.cloudflareEndpoint = stripURLPath(endpoint)
}

// SetAPIEndpoint sets the API endpoint
func (ps *PublicIPState) SetAPIEndpoint(endpoint string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.apiEndpoint = endpoint
}

// GetAddresses returns the public IP addresses
func (ps *PublicIPState) GetAddresses() []PublicIP {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	return ps.addresses
}

// Start starts the timer loop to check public IP addresses periodically
func (ps *PublicIPState) Start() {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Stop any existing timer
	if ps.timer != nil {
		ps.timer.Stop()
	}

	if ps.cancel != nil {
		ps.cancel()
	}

	// Create new context and cancel function
	ps.ctx, ps.cancel = context.WithCancel(context.Background())

	// Start the timer loop in a goroutine
	go ps.timerLoop(ps.ctx)
}

// Stop stops the timer loop
func (ps *PublicIPState) Stop() {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.cancel != nil {
		ps.cancel()
		ps.cancel = nil
	}

	if ps.timer != nil {
		ps.timer.Stop()
		ps.timer = nil
	}
}

// ForceUpdate forces an update of the public IP addresses
func (ps *PublicIPState) ForceUpdate() error {
	return ps.checkIPs(context.Background())
}

func (ps *PublicIPState) getLogger() *zerolog.Logger {
	logger := logging.GetSubsystemLogger("myip").
		With().
		Object("ps", ps).
		Logger()
	return &logger
}

// timerLoop runs the periodic IP check loop
func (ps *PublicIPState) timerLoop(ctx context.Context) {
	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()

	// Store timer reference for Stop() to access
	ps.mu.Lock()
	ps.timer = timer
	ps.mu.Unlock()

	// Perform initial check immediately
	checkIPs := func() {
		if err := ps.checkIPs(ctx); err != nil {
			ps.getLogger().Error().Err(err).Msg("failed to check public IP addresses")
		}
	}

	checkIPs()

	for {
		select {
		case <-timer.C:
			// Perform the check
			checkIPs()

			// Reset the timer for the next check
			timer.Reset(5 * time.Minute)

		case <-ctx.Done():
			// Timer was stopped
			return
		}
	}
}
