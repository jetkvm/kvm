package rdpgw

import (
	"encoding/hex"
	"fmt"
	"net"
	"sync/atomic"

	"github.com/rs/zerolog"
)

// UDPListener captures RDP ShortPath / URCP packets on the advertised UDP port.
// Phase 1: logs all incoming packets for reverse-engineering.
// Future: implement actual URCP transport for Windows App ShortPath.
type UDPListener struct {
	conn    *net.UDPConn
	logger  *zerolog.Logger
	running atomic.Bool
	done    chan struct{}
}

// NewUDPListener creates a new UDP listener for ShortPath discovery.
func NewUDPListener(logger *zerolog.Logger) *UDPListener {
	return &UDPListener{logger: logger}
}

// Start begins listening on the given port for UDP packets.
func (l *UDPListener) Start(bindAddr string, port int) error {
	if l.running.Load() {
		return nil
	}

	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", bindAddr, port))
	if err != nil {
		return fmt.Errorf("resolve UDP address: %w", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("listen UDP: %w", err)
	}

	l.conn = conn
	l.done = make(chan struct{})
	l.running.Store(true)

	l.logger.Info().Str("addr", addr.String()).Msg("ShortPath UDP listener started")

	go l.readLoop()
	return nil
}

// Stop closes the UDP listener.
func (l *UDPListener) Stop() {
	if !l.running.CompareAndSwap(true, false) {
		return
	}

	l.conn.Close()
	<-l.done
	l.logger.Info().Msg("ShortPath UDP listener stopped")
}

func (l *UDPListener) readLoop() {
	defer close(l.done)

	buf := make([]byte, 65536)
	var pktCount uint64

	for l.running.Load() {
		n, remoteAddr, err := l.conn.ReadFromUDP(buf)
		if err != nil {
			if l.running.Load() {
				l.logger.Warn().Err(err).Msg("UDP read error")
			}
			return
		}

		pktCount++

		// Log every packet with hex dump for reverse-engineering
		// In production this would be rate-limited, but for discovery phase we want everything
		preview := buf[:n]
		if n > 64 {
			preview = buf[:64]
		}

		l.logger.Info().
			Str("from", remoteAddr.String()).
			Int("size", n).
			Uint64("pkt", pktCount).
			Str("hex", hex.EncodeToString(preview)).
			Msg("ShortPath UDP packet received")
	}
}
