package lldp

import (
	"fmt"
	"net"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/rs/zerolog"
	"golang.org/x/net/bpf"
)

const IFNAMSIZ = 16

var (
	lldpDefaultTTL = 120 * time.Second
	cdpDefaultTTL  = 180 * time.Second
)

// from lldpd
// https://github.com/lldpd/lldpd/blob/9034c9332cca0c8b1a20e1287f0e5fed81f7eb2a/src/daemon/lldpd.h#L246
var bpfFilter = []bpf.RawInstruction{
	{0x30, 0, 0, 0x00000000}, {0x54, 0, 0, 0x00000001}, {0x15, 0, 16, 0x00000001},
	{0x28, 0, 0, 0x0000000c}, {0x15, 0, 6, 0x000088cc},
	{0x20, 0, 0, 0x00000002}, {0x15, 2, 0, 0xc200000e},
	{0x15, 1, 0, 0xc2000003}, {0x15, 0, 2, 0xc2000000},
	{0x28, 0, 0, 0x00000000}, {0x15, 12, 13, 0x00000180},
	{0x20, 0, 0, 0x00000002}, {0x15, 0, 2, 0x52cccccc},
	{0x28, 0, 0, 0x00000000}, {0x15, 8, 9, 0x000001e0},
	{0x15, 1, 0, 0x0ccccccc}, {0x15, 0, 2, 0x81000100},
	{0x28, 0, 0, 0x00000000}, {0x15, 4, 5, 0x00000100},
	{0x20, 0, 0, 0x00000002}, {0x15, 0, 3, 0x2b000000},
	{0x28, 0, 0, 0x00000000}, {0x15, 0, 1, 0x000000e0},
	{0x6, 0, 0, 0x00040000},
	{0x6, 0, 0, 0x00000000},
}

var multicastAddrs = []string{
	// LLDP
	"01:80:C2:00:00:00",
	"01:80:C2:00:00:03",
	"01:80:C2:00:00:0E",
	// CDP
	"01:00:0C:CC:CC:CC",
}

func (l *LLDP) setUpCapture() error {
	logger := l.l.With().Str("interface", l.interfaceName).Logger()
	tPacket, err := afPacketNewTPacket(l.interfaceName)
	if err != nil {
		return err
	}
	logger.Info().Msg("created TPacket")

	// set up multicast addresses
	// otherwise the kernel might discard the packets
	// another workaround would be to enable promiscuous mode but that's too tricky
	for _, mac := range multicastAddrs {
		hwAddr, err := net.ParseMAC(mac)
		if err != nil {
			logger.Error().Msgf("unable to parse MAC address %s: %s", mac, err)
			continue
		}

		if err := addMulticastAddr(l.interfaceName, hwAddr); err != nil {
			logger.Error().Msgf("unable to add multicast address %s: %s", mac, err)
			continue
		}

		logger.Info().
			Interface("hwaddr", hwAddr).
			Msgf("added multicast address")
	}

	if err = tPacket.SetBPF(bpfFilter); err != nil {
		logger.Error().Msgf("unable to set BPF filter: %s", err)
		tPacket.Close()
		return err
	}
	logger.Info().Msg("BPF filter set")

	l.pktSource = gopacket.NewPacketSource(tPacket, layers.LayerTypeEthernet)
	l.tPacket = tPacket

	return nil
}

func (l *LLDP) startCapture() error {
	logger := l.l.With().Str("interface", l.interfaceName).Logger()
	if l.tPacket == nil {
		return fmt.Errorf("AFPacket not initialized")
	}

	if l.pktSource == nil {
		return fmt.Errorf("packet source not initialized")
	}

	go func() {
		logger.Info().Msg("starting capture LLDP ethernet frames")

		for packet := range l.pktSource.Packets() {
			if err := l.handlePacket(packet, &logger); err != nil {
				logger.Error().Msgf("error handling packet: %s", err)
			}
		}
	}()

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
		logger.Trace().Msgf("Found LLDP Frame")

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
		logger.Trace().Msgf("Found CDP Frame")

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

func (l *LLDP) handlePacketLLDP(mac string, raw *layers.LinkLayerDiscovery, info *layers.LinkLayerDiscoveryInfo) error {
	n := &Neighbor{
		Values: make(map[string]string),
		Source: "lldp",
		Mac:    mac,
	}
	gotEnd := false

	ttl := lldpDefaultTTL

	for _, v := range raw.Values {
		switch v.Type {
		case layers.LLDPTLVEnd:
			gotEnd = true
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
			// n.ManagementAddress = info.MgmtAddress.Address
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

	if gotEnd || ttl < 1*time.Second {
		l.deleteNeighbor(mac)
	} else {
		l.addNeighbor(mac, *n, ttl)
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
		n.ManagementAddress = fmt.Sprintf("%s", info.MgmtAddresses[0])
	}

	l.addNeighbor(mac, *n, ttl)

	return nil
}

func (l *LLDP) shutdownCapture() error {
	if l.tPacket != nil {
		l.tPacket.Close()
		l.tPacket = nil
	}

	if l.pktSource != nil {
		l.pktSource = nil
	}

	return nil
}
