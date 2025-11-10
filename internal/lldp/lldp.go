package lldp

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/google/gopacket"
	"github.com/google/gopacket/afpacket"
	"github.com/jetkvm/kvm/internal/logging"
	"github.com/rs/zerolog"
)

var defaultLogger = logging.GetSubsystemLogger("lldp")

type RunningState struct {
	Enabled bool
	Running bool
	Logger  *zerolog.Logger
	TPacket *afpacket.TPacket
	Ctx     context.Context
	Cancel  context.CancelFunc
}

type LLDP struct {
	mu sync.RWMutex
	l  *zerolog.Logger

	interfaceName    string
	advertiseOptions *AdvertiseOptions
	onChange         func(neighbors []Neighbor)

	neighbors   map[neighborCacheKey]Neighbor
	neighborsMu sync.RWMutex

	Tx *RunningState
	Rx *RunningState

	pktSourceRx *gopacket.PacketSource
	rxWaitGroup *sync.WaitGroup
}

type AdvertiseOptions struct {
	SysName             string
	SysDescription      string
	PortDescription     string
	IPv4Address         *net.IP
	IPv6Address         *net.IP
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
		Rx: &RunningState{
			Enabled: opts.EnableRx,
		},
		Tx: &RunningState{
			Enabled: opts.EnableTx,
		},
		rxWaitGroup: &sync.WaitGroup{},
		l:           opts.Logger,
		neighbors:   make(map[neighborCacheKey]Neighbor),
		onChange:    opts.OnChange,
	}
}

func (l *LLDP) Start() error {
	l.mu.RLock()
	rxEnabled, txEnabled := l.Rx.Enabled, l.Tx.Enabled
	l.mu.RUnlock()

	if rxEnabled {
		if err := l.startRx(); err != nil {
			return fmt.Errorf("failed to start RX: %w", err)
		}
	}

	// Start TX if enabled
	if txEnabled {
		if err := l.startTx(); err != nil {
			return fmt.Errorf("failed to start TX: %w", err)
		}
	}

	return nil
}

// StartRx starts the LLDP receiver if not already running
func (l *LLDP) startRx() error {
	l.mu.RLock()
	running := l.Rx.Running
	enabled := l.Rx.Enabled
	l.mu.RUnlock()

	if running || !enabled {
		return nil
	}

	return l.startCapture()
}

// SetAdvertiseOptions updates the advertise options and resends LLDP packets if TX is running
func (l *LLDP) SetAdvertiseOptions(opts *AdvertiseOptions) error {
	l.mu.Lock()
	logger := l.l
	txRunning := l.Tx.Running
	l.advertiseOptions = opts
	l.mu.Unlock()

	if txRunning {
		// Immediately resend with new options
		if err := l.sendTxPackets(); err != nil {
			return fmt.Errorf("failed to resend LLDP packet with new options: %w", err)
		}
		logger.Info().Msg("advertise options changed, resent LLDP packet")
	}

	return nil
}

func (l *LLDP) SetRxAndTx(rx, tx bool) error {
	l.mu.Lock()
	l.Rx.Enabled = rx
	l.Tx.Enabled = tx
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
