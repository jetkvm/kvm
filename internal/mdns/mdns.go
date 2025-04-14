package mdns

import (
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync"

	"github.com/jetkvm/kvm/internal/logging"
	pion_mdns "github.com/pion/mdns/v2"
	"github.com/rs/zerolog"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

type MDNS struct {
	conn *pion_mdns.Conn
	lock sync.Mutex
	l    *zerolog.Logger

	localNames    []string
	listenOptions *MDNSListenOptions
}

type MDNSListenOptions struct {
	IPv4 bool
	IPv6 bool
}

type MDNSOptions struct {
	Logger        *zerolog.Logger
	LocalNames    []string
	ListenOptions *MDNSListenOptions
}

const (
	DefaultAddressIPv4 = pion_mdns.DefaultAddressIPv4
	DefaultAddressIPv6 = pion_mdns.DefaultAddressIPv6
)

func NewMDNS(opts *MDNSOptions) (*MDNS, error) {
	if opts.Logger == nil {
		opts.Logger = logging.GetDefaultLogger()
	}

	return &MDNS{
		l:             opts.Logger,
		lock:          sync.Mutex{},
		localNames:    opts.LocalNames,
		listenOptions: opts.ListenOptions,
	}, nil
}

func (m *MDNS) start(allowRestart bool) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.conn != nil {
		if !allowRestart {
			return fmt.Errorf("mDNS server already running")
		}

		m.conn.Close()
	}

	addr4, err := net.ResolveUDPAddr("udp4", DefaultAddressIPv4)
	if err != nil {
		return err
	}

	addr6, err := net.ResolveUDPAddr("udp6", DefaultAddressIPv6)
	if err != nil {
		return err
	}

	l4, err := net.ListenUDP("udp4", addr4)
	if err != nil {
		return err
	}

	l6, err := net.ListenUDP("udp6", addr6)
	if err != nil {
		return err
	}

	scopeLogger := m.l.With().Interface("local_names", m.localNames).Logger()

	newLocalNames := make([]string, len(m.localNames))
	for i, name := range m.localNames {
		newLocalNames[i] = strings.TrimRight(strings.ToLower(name), ".")
		if !strings.HasSuffix(newLocalNames[i], ".local") {
			newLocalNames[i] = newLocalNames[i] + ".local"
		}
	}

	mDNSConn, err := pion_mdns.Server(ipv4.NewPacketConn(l4), ipv6.NewPacketConn(l6), &pion_mdns.Config{
		LocalNames:    newLocalNames,
		LoggerFactory: logging.GetPionDefaultLoggerFactory(),
	})

	if err != nil {
		scopeLogger.Warn().Err(err).Msg("failed to start mDNS server")
		return err
	}

	m.conn = mDNSConn
	scopeLogger.Info().Msg("mDNS server started")

	return nil
}

func (m *MDNS) Start() error {
	return m.start(false)
}

func (m *MDNS) Restart() error {
	return m.start(true)
}

func (m *MDNS) Stop() error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.conn == nil {
		return nil
	}

	return m.conn.Close()
}

func (m *MDNS) SetLocalNames(localNames []string, always bool) error {
	if reflect.DeepEqual(m.localNames, localNames) && !always {
		return nil
	}

	m.localNames = localNames
	m.Restart()

	return nil
}

func (m *MDNS) SetListenOptions(listenOptions *MDNSListenOptions) error {
	if m.listenOptions != nil &&
		m.listenOptions.IPv4 == listenOptions.IPv4 &&
		m.listenOptions.IPv6 == listenOptions.IPv6 {
		return nil
	}

	m.listenOptions = listenOptions
	m.Restart()

	return nil
}
