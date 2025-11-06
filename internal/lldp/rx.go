package lldp

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"time"

	"github.com/google/gopacket"
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

func (l *LLDP) setUpCapture() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.tPacketRx != nil {
		return nil
	}

	logger := l.l.With().Str("interface", l.interfaceName).Logger()
	tPacketRx, err := afPacketNewTPacket(l.interfaceName)
	if err != nil {
		return err
	}
	logger.Info().Msg("created TPacketRx")

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

	if err = tPacketRx.SetBPF(bpfFilter); err != nil {
		logger.Error().
			Err(err).
			Msg("unable to set BPF filter")
		tPacketRx.Close()
		return err
	}
	logger.Info().Msg("BPF filter set")

	l.pktSourceRx = gopacket.NewPacketSource(tPacketRx, layers.LayerTypeEthernet)
	l.tPacketRx = tPacketRx

	return nil
}

func (l *LLDP) doCapture(logger *zerolog.Logger) {
	l.rxWaitGroup.Add(1)
	defer l.rxWaitGroup.Done()

	// TODO: use a channel to handle the packets
	// PacketSource.Packets() is not reliable and can cause panics and the upstream hasn't fixed it yet
	for {
		// check if the context is done before blocking call
		select {
		case <-l.rxCtx.Done():
			logger.Info().Msg("RX context cancelled")
			return
		default:
		}

		logger.Trace().Msg("waiting for next packet")
		packet, err := l.pktSourceRx.NextPacket()
		logger.Trace().Interface("packet", packet).Err(err).Msg("got next packet")

		if err != nil {
			logger.Error().
				Err(err).
				Msg("error getting next packet")

			// Immediately break for known unrecoverable errors
			if err == io.EOF || err == io.ErrUnexpectedEOF ||
				err == io.ErrNoProgress || err == io.ErrClosedPipe || err == io.ErrShortBuffer ||
				err == syscall.EBADF ||
				strings.Contains(err.Error(), "use of closed file") {
				return
			}

			continue
		}

		if err := l.handlePacket(packet, logger); err != nil {
			logger.Error().
				Err(err).
				Msg("error handling packet")
			continue
		}
	}
}

func (l *LLDP) startCapture() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.rxRunning {
		return nil // Already running
	}

	if l.tPacketRx == nil {
		return fmt.Errorf("AFPacket not initialized")
	}

	if l.pktSourceRx == nil {
		return fmt.Errorf("packet source not initialized")
	}

	logger := l.l.With().Str("interface", l.interfaceName).Logger()
	logger.Info().Msg("starting capture LLDP ethernet frames")

	// Create a new context for this instance
	l.rxCtx, l.rxCancel = context.WithCancel(context.Background())
	l.rxRunning = true

	go l.doCapture(&logger)

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
		l.l.Trace().Hex("packet", packet.Data()).Msg("received LLDP frame")

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
		l.l.Trace().Hex("packet", packet.Data()).Msg("received CDP frame")

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
	n := &Neighbor{
		Values: make(map[string]string),
		Source: "lldp",
		Mac:    mac,
	}

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
			n.ManagementAddress = &ManagementAddress{
				AddressFamily:    info.MgmtAddress.Subtype.String(),
				Address:          net.IP(info.MgmtAddress.Address).String(),
				InterfaceSubtype: info.MgmtAddress.InterfaceSubtype.String(),
				InterfaceNumber:  info.MgmtAddress.InterfaceNumber,
				OID:              info.MgmtAddress.OID,
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
	n := &Neighbor{
		Values: make(map[string]string),
		Source: "cdp",
		Mac:    mac,
	}

	ttl := cdpDefaultTTL

	n.ChassisID = info.DeviceID
	n.PortID = info.PortID
	n.SystemName = info.SysName
	n.SystemDescription = info.Platform
	n.TTL = uint16(raw.TTL)

	if n.TTL > 1 {
		ttl = time.Duration(n.TTL) * time.Second
	}

	if len(info.MgmtAddresses) > 0 {
		ip := info.MgmtAddresses[0]
		ipFamily := "ipv4"
		if ip.To4() == nil {
			ipFamily = "ipv6"
		}

		l.l.Info().
			Str("ip", ip.String()).
			Str("ip_family", ipFamily).
			Interface("ip", ip).
			Interface("info", info).
			Msg("parsed IP address")

		n.ManagementAddress = &ManagementAddress{
			AddressFamily:    ipFamily,
			Address:          ip.String(),
			InterfaceSubtype: "if_name",
			InterfaceNumber:  0,
			OID:              "",
		}
	}

	l.addNeighbor(n, ttl)

	return nil
}

func (l *LLDP) stopCapture() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.rxRunning {
		return nil // Already stopped
	}

	logger := l.l.With().Str("interface", l.interfaceName).Logger()
	logger.Info().Msg("stopping LLDP receiver")

	// Cancel context to signal stop
	rxCancel := l.rxCancel
	if rxCancel != nil {
		rxCancel()
		l.rxCancel = nil

		logger.Info().Msg("cancelled RX context, waiting for goroutine to finish")
	}

	// stop the TPacketRx
	go func() {
		if l.tPacketRx == nil {
			return
		}

		// write an empty packet to the TPacketRx to interrupt the blocking read
		// it's a shitty workaround until https://github.com/google/gopacket/pull/777 is merged,
		// or we have a better solution, see https://github.com/google/gopacket/issues/1064
		l.tPacketRx.WritePacketData([]byte{})
	}()

	// wait for the goroutine to finish
	start := time.Now()
	l.rxWaitGroup.Wait()
	logger.Info().Dur("duration", time.Since(start)).Msg("RX goroutine finished")

	l.rxRunning = false

	if l.tPacketRx != nil {
		logger.Info().Msg("closing TPacketRx")
		l.tPacketRx.Close()
		l.tPacketRx = nil
	}

	if l.pktSourceRx != nil {
		logger.Info().Msg("closing packet source")
		l.pktSourceRx = nil
	}

	return nil
}

func (l *LLDP) stopRx() error {
	if err := l.stopCapture(); err != nil {
		return err
	}

	// clean up the neighbors table
	l.neighbors.DeleteAll()
	l.onChange([]Neighbor{})

	return nil
}
