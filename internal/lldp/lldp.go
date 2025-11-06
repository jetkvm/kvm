package lldp

import (
	"context"
	"fmt"
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
	mu sync.RWMutex

	l           *zerolog.Logger
	tPacketRx   *afpacket.TPacket
	tPacketTx   *afpacket.TPacket
	pktSourceRx *gopacket.PacketSource

	enableRx bool
	enableTx bool

	packets          chan gopacket.Packet
	interfaceName    string
	advertiseOptions *AdvertiseOptions
	onChange         func(neighbors []Neighbor)

	neighbors *ttlcache.Cache[string, Neighbor]

	// State tracking
	rxRunning bool
	txRunning bool
	txCtx     context.Context
	txCancel  context.CancelFunc
	rxCtx     context.Context
	rxCancel  context.CancelFunc
}

type AdvertiseOptions struct {
	SysName             string
	SysDescription      string
	PortDescription     string
	SysCapabilities     []string
	EnabledCapabilities []string
}

type Options struct {
	InterfaceName    string
	AdvertiseOptions *AdvertiseOptions
	EnableRx         bool
	EnableTx         bool
	OnChange         func(neighbors []Neighbor)
	Logger           *zerolog.Logger
}

func NewLLDP(opts *Options) *LLDP {
	if opts.Logger == nil {
		opts.Logger = defaultLogger
	}

	if opts.InterfaceName == "" {
		opts.Logger.Fatal().Msg("InterfaceName is required")
	}

	return &LLDP{
		interfaceName:    opts.InterfaceName,
		advertiseOptions: opts.AdvertiseOptions,
		enableRx:         opts.EnableRx,
		enableTx:         opts.EnableTx,
		l:                opts.Logger,
		neighbors:        ttlcache.New(ttlcache.WithTTL[string, Neighbor](1 * time.Hour)),
		onChange:         opts.OnChange,
	}
}

func (l *LLDP) Start() error {
	go l.neighbors.Start()

	if l.enableRx {
		if err := l.startRx(); err != nil {
			return fmt.Errorf("failed to start RX: %w", err)
		}
	}

	// Start TX if enabled
	if l.enableTx {
		if err := l.startTx(); err != nil {
			return fmt.Errorf("failed to start TX: %w", err)
		}
	}

	return nil
}

// StartRx starts the LLDP receiver if not already running
func (l *LLDP) startRx() error {
	l.mu.Lock()
	running := l.rxRunning
	enabled := l.enableRx
	l.mu.Unlock()

	if running || !enabled {
		return nil
	}

	if err := l.setUpCapture(); err != nil {
		return fmt.Errorf("failed to set up capture: %w", err)
	}

	return l.startCapture()
}

// SetAdvertiseOptions updates the advertise options and resends LLDP packets if TX is running
func (l *LLDP) SetAdvertiseOptions(opts *AdvertiseOptions) error {
	l.mu.Lock()
	txRunning := l.txRunning
	l.advertiseOptions = opts
	l.mu.Unlock()

	if txRunning {
		// Immediately resend with new options
		if err := l.sendTxPackets(); err != nil {
			return fmt.Errorf("failed to resend LLDP packet with new options: %w", err)
		}
		l.l.Info().Msg("advertise options changed, resent LLDP packet")
	}

	return nil
}

func (l *LLDP) SetRxAndTx(rx, tx bool) error {
	l.mu.Lock()
	l.enableRx = rx
	l.enableTx = tx
	l.mu.Unlock()

	// if rx is enabled, start the RX
	if rx {
		if err := l.startRx(); err != nil {
			return fmt.Errorf("failed to start RX: %w", err)
		}
	} else {
		if err := l.stopRx(); err != nil {
			return fmt.Errorf("failed to stop RX: %w", err)
		}
	}

	// if tx is enabled, start the TX
	if tx {
		if err := l.startTx(); err != nil {
			return fmt.Errorf("failed to start TX: %w", err)
		}
	} else {
		if err := l.stopTx(); err != nil {
			return fmt.Errorf("failed to stop TX: %w", err)
		}
	}

	return nil
}
