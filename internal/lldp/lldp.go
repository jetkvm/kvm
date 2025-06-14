package lldp

import (
	"context"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/afpacket"
	"github.com/jellydator/ttlcache/v3"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

var defaultLogger = logging.GetSubsystemLogger("lldp")

type LLDP struct {
	l         *zerolog.Logger
	tPacket   *afpacket.TPacket
	pktSource *gopacket.PacketSource
	rxCtx     context.Context
	rxCancel  context.CancelFunc
	rxLock    sync.Mutex

	enableRx bool
	enableTx bool

	packets       chan gopacket.Packet
	interfaceName string
	stop          chan struct{}

	neighbors *ttlcache.Cache[string, Neighbor]
}

type LLDPOptions struct {
	InterfaceName string
	EnableRx      bool
	EnableTx      bool
	Logger        *zerolog.Logger
}

func NewLLDP(opts *LLDPOptions) *LLDP {
	if opts.Logger == nil {
		opts.Logger = defaultLogger
	}

	if opts.InterfaceName == "" {
		opts.Logger.Fatal().Msg("InterfaceName is required")
	}

	return &LLDP{
		interfaceName: opts.InterfaceName,
		enableRx:      opts.EnableRx,
		enableTx:      opts.EnableTx,
		l:             opts.Logger,
		neighbors:     ttlcache.New(ttlcache.WithTTL[string, Neighbor](1 * time.Hour)),
	}
}

func (l *LLDP) Start() error {
	l.rxLock.Lock()
	defer l.rxLock.Unlock()

	if l.rxCtx != nil {
		l.l.Info().Msg("LLDP already started")
		return nil
	}

	l.rxCtx, l.rxCancel = context.WithCancel(context.Background())

	if l.enableRx {
		l.l.Info().Msg("setting up AF_PACKET")
		if err := l.setUpCapture(); err != nil {
			l.l.Error().Err(err).Msg("unable to set up AF_PACKET")
			return err
		}
		if err := l.startCapture(); err != nil {
			l.l.Error().Err(err).Msg("unable to start capture")
			return err
		}
	}

	go l.neighbors.Start()

	return nil
}

func (l *LLDP) Stop() error {
	l.rxLock.Lock()
	defer l.rxLock.Unlock()

	if l.rxCancel != nil {
		l.rxCancel()
		l.rxCancel = nil
		l.rxCtx = nil
	}

	if l.enableRx {
		_ = l.shutdownCapture()
	}

	l.neighbors.Stop()
	l.neighbors.DeleteAll()

	return nil
}
