package lldp

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

var (
	lldpDstMac    = net.HardwareAddr([]byte{0x01, 0x80, 0xc2, 0x00, 0x00, 0x0e})
	lldpEtherType = layers.EthernetTypeLinkLayerDiscovery
)

func (l *LLDP) toPayloadValues(opts *AdvertiseOptions) []layers.LinkLayerDiscoveryValue {
	// See also: layers.LinkLayerDiscovery.SerializeTo()
	r := []layers.LinkLayerDiscoveryValue{}

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
	if l.Tx.TPacket != nil {
		return nil
	}

	logger := l.l.With().Str("interface", l.interfaceName).Str("direction", "tx").Logger()
	txState := &RunningState{Enabled: true, Logger: &logger}
	txState.Ctx, txState.Cancel = context.WithCancel(context.Background())

	var err error
	txState.TPacket, err = afPacketNewTPacket(l.interfaceName)
	if err != nil {
		return err
	}

	logger.Info().Msg("created TPacket instance for sending LLDP packets")
	l.Tx = txState
	return nil
}

func (l *LLDP) generateTxPackets(interfaceName string, opts *AdvertiseOptions) (gopacket.SerializeBuffer, error) {
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return nil, err
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
		Values: l.toPayloadValues(opts),
	}

	buf := gopacket.NewSerializeBuffer()
	err = gopacket.SerializeLayers(buf, gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}, &ethFrame, &lldpFrame)

	if err != nil {
		return nil, err
	}

	return buf, nil
}

func (l *LLDP) sendTxPackets() error {
	l.mu.RLock()
	interfaceName := l.interfaceName
	opts := l.advertiseOptions
	logger := l.Tx.Logger
	tPacket := l.Tx.TPacket
	l.mu.RUnlock()

	if tPacket == nil {
		logger.Error().Msg("AFPacket not initialized for Tx")
		return fmt.Errorf("AFPacket not initialized for Tx")
	}

	logger.Trace().Msg("generating LLDP Tx packets")
	buf, err := l.generateTxPackets(interfaceName, opts)
	if err != nil {
		logger.Error().Err(err).Msg("unable to serialize packet")
		return err
	}

	logger.Trace().Hex("packet", buf.Bytes()).Msg("sending LLDP packet")
	start := time.Now()

	// send packet
	if err := tPacket.WritePacketData(buf.Bytes()); err != nil {
		logger.Error().Err(err).Msg("unable to send packet")
		return err
	}

	logger.Info().Dur("duration", time.Since(start)).Msg("sent LLDP packet")
	return nil
}

const txInterval = 30 * time.Second // Standard LLDP transmission interval

func (l *LLDP) doSendPeriodically() {
	l.mu.Lock()
	l.Tx.Running = true
	logger := l.Tx.Logger
	txCtx := l.Tx.Ctx
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		l.Tx.Running = false
		l.mu.Unlock()
	}()

	logger.Info().Msg("starting LLDP transmitter")
	ticker := time.NewTicker(txInterval)
	defer ticker.Stop()

	// Send initial packet immediately
	logger.Trace().Msg("sending initial LLDP packet")
	if err := l.sendTxPackets(); err != nil {
		logger.Error().Err(err).Msg("error sending initial LLDP packet")
	}

	for {
		select {
		case <-ticker.C:
			logger.Trace().Msg("time to send periodic LLDP packet")
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
	logger := l.l
	running := l.Tx.Running
	enabled := l.Tx.Enabled
	previousCancel := l.Tx.Cancel
	l.mu.RUnlock()

	if running || !enabled {
		logger.Trace().Bool("running", running).Bool("enabled", enabled).Msg("alrady running Tx or not enabled")
		return nil
	}

	if previousCancel != nil {
		logger.Trace().Msg("stopping previous Tx context before starting new one")
		previousCancel()
	}

	if err := l.setUpTx(); err != nil {
		return fmt.Errorf("failed to set up TX: %w", err)
	}

	go l.doSendPeriodically()

	return nil
}

func (l *LLDP) stopTx() error {
	l.mu.Lock()
	if !l.Tx.Running {
		l.mu.Unlock()
		return nil // Already stopped
	}

	// Cancel context to signal stop
	txCancel := l.Tx.Cancel
	l.Tx.Running = false
	l.mu.Unlock()

	l.Tx.Logger.Info().Msg("stopping LLDP transmitter")

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
