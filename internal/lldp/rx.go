package lldp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/afpacket"
	"github.com/google/gopacket/layers"
	"github.com/rs/zerolog"
)

const IFNAMSIZ = 16

var (
	lldpDefaultTTL = 120 * time.Second
	cdpDefaultTTL  = 180 * time.Second
)

var multicastAddrs = []string{
	// LLDP
	"01:80:C2:00:00:00",
	"01:80:C2:00:00:03",
	"01:80:C2:00:00:0E",
	// CDP
	"01:00:0C:CC:CC:CC",
}

func (l *LLDP) setUpPacketSourceUnderLock(tPacket *afpacket.TPacket, logger *zerolog.Logger) (*gopacket.PacketSource, error) {
	// set up multicast addresses
	// otherwise the kernel might discard the packets
	// another workaround would be to enable promiscuous mode but that's too tricky
	for _, mac := range multicastAddrs {
		hwAddr, err := net.ParseMAC(mac)
		if err != nil {
			logger.Error().
				Str("mac", mac).
				MACAddr("hwaddr", hwAddr).
				Err(err).
				Msg("unable to parse MAC address")
			continue
		}

		if err := addMulticastAddr(l.interfaceName, hwAddr); err != nil {
			logger.Error().
				MACAddr("hwaddr", hwAddr).
				Err(err).
				Msg("unable to add multicast address")
			continue
		}

		logger.Info().
			MACAddr("hwaddr", hwAddr).
			Msg("added multicast address")
	}

	if err := tPacket.SetBPF(bpfFilter); err != nil {
		logger.Error().
			Err(err).
			Msg("unable to set BPF filter")
		return nil, err
	}

	logger.Info().Msg("BPF filter set")
	return gopacket.NewPacketSource(tPacket, layers.LayerTypeEthernet), nil
}

func (l *LLDP) doCapture() {
	l.mu.Lock()
	l.Rx.Running = true
	ctx := l.Rx.Ctx
	logger := l.Rx.Logger
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		l.Rx.Running = false
		l.mu.Unlock()
	}()

	if l.pktSourceRx == nil || ctx == nil {
		logger.Error().Msg("packet source or RX context not initialized")
		return
	}

	l.rxWaitGroup.Add(1)
	defer l.rxWaitGroup.Done()

	// TODO: use a channel to handle the packets
	// PacketSource.Packets() is not reliable and can cause panics and the upstream hasn't fixed it yet
	for {
		// check if the context is done before blocking call
		select {
		case <-ctx.Done():
			logger.Info().Msg("RX context cancelled")
			return
		default:
		}

		logger.Trace().Msg("waiting for next packet")
		packet, err := l.pktSourceRx.NextPacket()

		if err != nil {
			logger.Error().
				Err(err).
				Msg("error getting next packet")

			// Immediately break for known unrecoverable errors
			if errors.Is(err, io.EOF) ||
				errors.Is(err, io.ErrUnexpectedEOF) ||
				errors.Is(err, io.ErrNoProgress) ||
				errors.Is(err, io.ErrClosedPipe) ||
				errors.Is(err, io.ErrShortBuffer) ||
				errors.Is(err, syscall.EBADF) ||
				strings.Contains(err.Error(), "use of closed file") {
				logger.Error().Msg("unrecoverable error, stopping capture")
				return
			}

			continue
		}

		logger.Trace().Interface("packet", packet).Msg("got next packet")

		if err := l.handlePacket(packet, logger); err != nil {
			logger.Error().
				Err(err).
				Msg("error handling packet")
			continue
		}
	}
}

func (l *LLDP) prepareCapture() (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.Rx.Running {
		l.l.Debug().Msg("LLDP receiver already running")
		return false, nil // Already running
	}

	logger := l.l.With().Str("interface", l.interfaceName).Str("direction", "rx").Logger()
	logger.Info().Msg("starting capture LLDP ethernet frames")

	tPacket, err := afPacketNewTPacket(l.interfaceName)
	if err != nil {
		logger.Error().Err(err).Msg("could not create TPacket instance for receiving LLDP packets")
		return false, err
	}

	l.pktSourceRx, err = l.setUpPacketSourceUnderLock(tPacket, &logger)
	if err != nil {
		logger.Error().Err(err).Msg("could not create packet source for receiving LLDP packets")
		tPacket.Close()
		return false, err
	}

	runningState := &RunningState{Enabled: true, Logger: &logger}
	runningState.Ctx, runningState.Cancel = context.WithCancel(context.Background())
	runningState.TPacket = tPacket
	l.Rx = runningState

	logger.Debug().Msg("created packet source for receiving LLDP packets")
	return true, nil
}

func (l *LLDP) startCapture() error {
	// set up the logger and context (inside a lock)
	startRunning, err := l.prepareCapture()

	if err != nil {
		return err
	}

	if startRunning {
		go l.doCapture()
	}

	return nil
}

func (l *LLDP) handlePacket(packet gopacket.Packet, logger *zerolog.Logger) error {
	linkLayer := packet.LinkLayer()
	if linkLayer == nil {
		return fmt.Errorf("no link layer")
	}

	srcMac := linkLayer.LinkFlow().Src().String()
	dstMac := linkLayer.LinkFlow().Dst().String()

	logger.Trace().
		Str("src_mac", srcMac).
		Str("dst_mac", dstMac).
		Int("length", len(packet.Data())).
		Hex("data", packet.Data()).
		Msg("received packet")

	lldpRaw := packet.Layer(layers.LayerTypeLinkLayerDiscovery)
	if lldpRaw != nil {
		logger.Trace().Hex("packet", packet.Data()).Msg("received LLDP frame")

		lldpInfo := packet.Layer(layers.LayerTypeLinkLayerDiscoveryInfo)
		if lldpInfo == nil {
			return fmt.Errorf("no LLDP info layer")
		}

		return l.handlePacketLLDP(
			srcMac,
			lldpRaw.(*layers.LinkLayerDiscovery),
			lldpInfo.(*layers.LinkLayerDiscoveryInfo),
		)
	}

	cdpRaw := packet.Layer(layers.LayerTypeCiscoDiscovery)
	if cdpRaw != nil {
		logger.Trace().Hex("packet", packet.Data()).Msg("received CDP frame")

		cdpInfo := packet.Layer(layers.LayerTypeCiscoDiscoveryInfo)
		if cdpInfo == nil {
			return fmt.Errorf("no CDP info layer")
		}

		return l.handlePacketCDP(
			srcMac,
			cdpRaw.(*layers.CiscoDiscovery),
			cdpInfo.(*layers.CiscoDiscoveryInfo),
		)
	}

	return nil
}

func capabilitiesToString(capabilities layers.LLDPCapabilities) []string {
	capStr := []string{}
	if capabilities.Other {
		capStr = append(capStr, "other")
	}
	if capabilities.Repeater {
		capStr = append(capStr, "repeater")
	}
	if capabilities.Bridge {
		capStr = append(capStr, "bridge")
	}
	if capabilities.WLANAP {
		capStr = append(capStr, "wlanap")
	}
	if capabilities.Router {
		capStr = append(capStr, "router")
	}
	if capabilities.Phone {
		capStr = append(capStr, "phone")
	}
	if capabilities.DocSis {
		capStr = append(capStr, "docsis")
	}
	return capStr
}

func (l *LLDP) handlePacketLLDP(mac string, raw *layers.LinkLayerDiscovery, info *layers.LinkLayerDiscoveryInfo) error {
	n := newNeighbor(mac, NeighborSourceLLDP)

	ttl := lldpDefaultTTL

	for _, v := range raw.Values {
		switch v.Type {
		case layers.LLDPTLVChassisID:
			n.ChassisID = string(raw.ChassisID.ID)
			n.Values["chassis_id"] = n.ChassisID
		case layers.LLDPTLVPortID:
			n.PortID = string(raw.PortID.ID)
			n.Values["port_id"] = n.PortID
		case layers.LLDPTLVPortDescription:
			n.PortDescription = info.PortDescription
			n.Values["port_description"] = n.PortDescription
		case layers.LLDPTLVSysName:
			n.SystemName = info.SysName
			n.Values["system_name"] = n.SystemName
		case layers.LLDPTLVSysDescription:
			n.SystemDescription = info.SysDescription
			n.Values["system_description"] = n.SystemDescription
		case layers.LLDPTLVMgmtAddress:
			mgmtAddress := parseTlvMgmtAddress(v)
			if mgmtAddress != nil {
				n.ManagementAddresses = append(
					n.ManagementAddresses,
					lldpMgmtAddressToSerializable(mgmtAddress),
				)
			}
		case layers.LLDPTLVSysCapabilities:
			n.Capabilities = capabilitiesToString(info.SysCapabilities.EnabledCap)
		case layers.LLDPTLVTTL:
			n.TTL = uint16(raw.TTL)
			ttl = time.Duration(n.TTL) * time.Second
			n.Values["ttl"] = fmt.Sprintf("%d", n.TTL)
		case layers.LLDPTLVOrgSpecific:
			for _, org := range info.OrgTLVs {
				n.Values[fmt.Sprintf("org_specific_%d", org.OUI)] = string(org.Info)
			}
		}
	}

	// delete the neighbor if the TTL is less than 1 second
	// LLDP shutdown frame should have a TTL of 0 and contains mandatory TLVs only
	// but we will simply ignore the TLVs check for now
	if ttl < 1*time.Second {
		l.deleteNeighbor(n)
	} else {
		l.addNeighbor(n, ttl)
	}

	return nil
}

func (l *LLDP) handlePacketCDP(mac string, raw *layers.CiscoDiscovery, info *layers.CiscoDiscoveryInfo) error {
	// TODO: implement full CDP parsing
	n := newNeighbor(mac, NeighborSourceCDP)

	ttl := cdpDefaultTTL

	n.ChassisID = info.DeviceID
	n.PortID = info.PortID
	n.SystemName = info.SysName
	n.SystemDescription = info.Platform
	n.TTL = uint16(raw.TTL)

	if n.TTL > 1 {
		ttl = time.Duration(n.TTL) * time.Second
	}

	for _, addr := range info.MgmtAddresses {
		addrFamily := "ipv4"
		if addr.To4() == nil {
			addrFamily = "ipv6"
		}
		n.ManagementAddresses = append(n.ManagementAddresses, ManagementAddress{
			AddressFamily:    addrFamily,
			Address:          addr.String(),
			InterfaceSubtype: "if_name",
			InterfaceNumber:  0,
			OID:              "",
		})
	}

	l.addNeighbor(n, ttl)

	return nil
}

func (l *LLDP) stopCapture() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.Rx.Running {
		return nil // Already stopped
	}

	logger := l.Rx.Logger
	rxCancel := l.Rx.Cancel
	tPacket := l.Rx.TPacket

	logger.Info().Msg("stopping LLDP receiver")

	// Cancel context to signal stop
	if rxCancel != nil {
		l.Rx.Cancel = nil // so we don't cancel again
		rxCancel()
		logger.Info().Msg("cancelled RX context, waiting for goroutine to finish")
	}

	// stop the TPacketRx
	go func() {
		if tPacket == nil {
			return
		}

		// write an empty packet to the TPacketRx to interrupt the blocking read
		// it's a shitty workaround until https://github.com/google/gopacket/pull/777 is merged,
		// or we have a better solution, see https://github.com/google/gopacket/issues/1064
		_ = tPacket.WritePacketData([]byte{})
	}()

	// wait for the goroutine to finish
	start := time.Now()
	l.rxWaitGroup.Wait()
	logger.Info().Dur("duration", time.Since(start)).Msg("RX goroutine finished")

	l.Rx.Running = false

	// close the packet source first because it's constructed with the Rx.TPacket
	if l.pktSourceRx != nil {
		logger.Info().Msg("closing packet source")
		l.pktSourceRx = nil
	}

	if tPacket != nil {
		logger.Info().Msg("closing RX TPacket")
		tPacket.Close()
		l.Rx.TPacket = nil
	}

	return nil
}

func (l *LLDP) stopRx() error {
	if err := l.stopCapture(); err != nil {
		return err
	}

	// clean up the neighbors table
	l.flushNeighbors()
	l.onChange([]Neighbor{})

	return nil
}
