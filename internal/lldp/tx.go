package lldp

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/rs/zerolog"
)

var (
	lldpDstMac    = net.HardwareAddr([]byte{0x01, 0x80, 0xc2, 0x00, 0x00, 0x0e})
	lldpEtherType = layers.EthernetTypeLinkLayerDiscovery
)

func (l *LLDP) toPayloadValues() []layers.LinkLayerDiscoveryValue {
	// See also: layers.LinkLayerDiscovery.SerializeTo()
	r := []layers.LinkLayerDiscoveryValue{}

	l.mu.RLock()
	opts := l.advertiseOptions
	l.mu.RUnlock()

	if opts == nil {
		return r
	}

	if opts.SysName != "" {
		r = append(r, tlvStringValue(layers.LLDPTLVSysName, opts.SysName))
	}

	if opts.SysDescription != "" {
		r = append(r, tlvStringValue(layers.LLDPTLVSysDescription, opts.SysDescription))
	}

	if opts.IPv4Address != nil {
		r = append(r, tlvMgmtAddress(&layers.LLDPMgmtAddress{
			Subtype:          layers.IANAAddressFamilyIPV4,
			Address:          opts.IPv4Address.To4(),
			InterfaceSubtype: layers.LLDPInterfaceSubtypeifIndex,
			InterfaceNumber:  0,
		}))
	}

	if opts.IPv6Address != nil {
		r = append(r, tlvMgmtAddress(&layers.LLDPMgmtAddress{
			Subtype:          layers.IANAAddressFamilyIPV6,
			Address:          opts.IPv6Address.To16(),
			InterfaceSubtype: layers.LLDPInterfaceSubtypeifIndex,
			InterfaceNumber:  0,
		}))
	}

	if len(opts.SysCapabilities) > 0 {
		value := make([]byte, 4)
		binary.BigEndian.PutUint16(value[0:2], toLLDPCapabilitiesBytes(opts.SysCapabilities))
		binary.BigEndian.PutUint16(value[2:4], toLLDPCapabilitiesBytes(opts.EnabledCapabilities))

		r = append(r, layers.LinkLayerDiscoveryValue{
			Type:   layers.LLDPTLVSysCapabilities,
			Value:  value,
			Length: 4,
		})
	}

	// EndTLV will be added by the serializer, we don't need to add it here
	return r
}

func (l *LLDP) setUpTx() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Check if already set up (double-check pattern to prevent duplicate setup)
	if l.tPacketTx != nil {
		return nil
	}

	logger := l.l.With().Str("interface", l.interfaceName).Logger()
	tPacketTx, err := afPacketNewTPacket(l.interfaceName)
	if err != nil {
		return err
	}
	logger.Info().Msg("created TPacket instance for sending LLDP packets")

	l.tPacketTx = tPacketTx

	return nil
}

func (l *LLDP) sendTxPackets() error {
	l.mu.RLock()
	defer l.mu.RUnlock()

	logger := l.l.With().Str("interface", l.interfaceName).Logger()
	iface, err := net.InterfaceByName(l.interfaceName)
	if err != nil {
		return err
	}

	if l.tPacketTx == nil {
		return fmt.Errorf("AFPacket not initialized")
	}

	// create payload
	ethFrame := layers.Ethernet{
		EthernetType: lldpEtherType,
		SrcMAC:       iface.HardwareAddr,
		DstMAC:       lldpDstMac,
	}

	lldpFrame := layers.LinkLayerDiscovery{
		ChassisID: layers.LLDPChassisID{
			Subtype: layers.LLDPChassisIDSubTypeMACAddr,
			ID:      []byte(iface.HardwareAddr),
		},
		PortID: layers.LLDPPortID{
			Subtype: layers.LLDPPortIDSubtypeIfaceName,
			ID:      []byte(iface.Name),
		},
		TTL:    uint16(3600),
		Values: l.toPayloadValues(),
	}

	buf := gopacket.NewSerializeBuffer()
	if err := gopacket.SerializeLayers(buf, gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}, &ethFrame, &lldpFrame); err != nil {
		l.l.Error().Err(err).Msg("unable to serialize packet")
		return err
	}

	logger.Trace().Hex("packet", buf.Bytes()).Msg("sending LLDP packet")

	// send packet
	if err := l.tPacketTx.WritePacketData(buf.Bytes()); err != nil {
		l.l.Error().Err(err).Msg("unable to send packet")
		return err
	}

	return nil
}

const txInterval = 30 * time.Second // Standard LLDP transmission interval

func (l *LLDP) doSendPeriodically(logger *zerolog.Logger, txCtx context.Context) {
	l.mu.Lock()
	l.txRunning = true
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		l.txRunning = false
		l.mu.Unlock()
	}()

	ticker := time.NewTicker(txInterval)
	defer ticker.Stop()

	// Send initial packet immediately
	if err := l.sendTxPackets(); err != nil {
		logger.Error().Err(err).Msg("error sending initial LLDP packet")
	}

	for {
		select {
		case <-ticker.C:
			if err := l.sendTxPackets(); err != nil {
				logger.Error().Err(err).Msg("error sending LLDP packet")
			}
		case <-txCtx.Done():
			logger.Info().Msg("LLDP transmitter stopped")
			return
		}
	}
}

func (l *LLDP) startTx() error {
	l.mu.RLock()
	running := l.txRunning
	enabled := l.enableTx
	cancel := l.txCancel
	l.mu.RUnlock()

	if running || !enabled {
		return nil
	}

	if cancel != nil {
		cancel()
	}

	l.mu.Lock()
	l.txCtx, l.txCancel = context.WithCancel(context.Background())
	l.mu.Unlock()

	if err := l.setUpTx(); err != nil {
		return fmt.Errorf("failed to set up TX: %w", err)
	}

	logger := l.l.With().Str("interface", l.interfaceName).Logger()
	logger.Info().Msg("starting LLDP transmitter")

	go l.doSendPeriodically(&logger, l.txCtx)

	return nil
}

func (l *LLDP) stopTx() error {
	l.mu.Lock()
	if !l.txRunning {
		l.mu.Unlock()
		return nil // Already stopped
	}

	logger := l.l.With().Str("interface", l.interfaceName).Logger()
	logger.Info().Msg("stopping LLDP transmitter")

	// Cancel context to signal stop
	txCancel := l.txCancel
	l.txRunning = false
	l.mu.Unlock()

	// Cancel context (goroutine will handle cleanup)
	if txCancel != nil {
		txCancel()
	}

	// Wait a bit for goroutine to finish
	// Note: In a production system, you might want to use sync.WaitGroup
	// for proper synchronization, but for now this is acceptable
	time.Sleep(100 * time.Millisecond)

	return nil
}
