package lldp

import (
	"encoding/binary"
	"fmt"
	"net"

	"github.com/google/gopacket/layers"
)

var (
	capabilityMap = map[string]uint16{
		"other":        layers.LLDPCapsOther,
		"repeater":     layers.LLDPCapsRepeater,
		"bridge":       layers.LLDPCapsBridge,
		"wlanap":       layers.LLDPCapsWLANAP,
		"router":       layers.LLDPCapsRouter,
		"phone":        layers.LLDPCapsPhone,
		"docsis":       layers.LLDPCapsDocSis,
		"station_only": layers.LLDPCapsStationOnly,
		"cvlan":        layers.LLDPCapsCVLAN,
		"svlan":        layers.LLDPCapsSVLAN,
		"tmpr":         layers.LLDPCapsTmpr,
	}
)

func tlvMgmtAddressToBytes(m *layers.LLDPMgmtAddress) []byte {
	var b []byte
	b = append(b, byte(len(m.Address))+1)   // TLV Length
	b = append(b, byte(m.Subtype))          // Address Subtype
	b = append(b, m.Address...)             // Address
	b = append(b, byte(m.InterfaceSubtype)) // Interface Subtype

	ifIndex := make([]byte, 4) // 4 bytes for the interface number
	binary.BigEndian.PutUint32(ifIndex, m.InterfaceNumber)
	b = append(b, ifIndex...)

	b = append(b, 0) // OID type
	return b
}

func tlvMgmtAddress(m *layers.LLDPMgmtAddress) layers.LinkLayerDiscoveryValue {
	return layers.LinkLayerDiscoveryValue{
		Type:   layers.LLDPTLVMgmtAddress,
		Value:  tlvMgmtAddressToBytes(m),
		Length: uint16(len(tlvMgmtAddressToBytes(m))),
	}
}

// if err := checkLLDPTLVLen(v, 9); err != nil {
// 	return err
// }
// mlen := v.Value[0]
// if err := checkLLDPTLVLen(v, int(mlen+7)); err != nil {
// 	return err
// }
// info.MgmtAddress.Subtype = IANAAddressFamily(v.Value[1])
// info.MgmtAddress.Address = v.Value[2 : mlen+1]
// info.MgmtAddress.InterfaceSubtype = LLDPInterfaceSubtype(v.Value[mlen+1])
// info.MgmtAddress.InterfaceNumber = binary.BigEndian.Uint32(v.Value[mlen+2 : mlen+6])
// olen := v.Value[mlen+6]
// if err := checkLLDPTLVLen(v, int(mlen+7+olen)); err != nil {
// 	return err
// }
// info.MgmtAddress.OID = string(v.Value[mlen+7 : mlen+7+olen])

func checkLLDPTLVLen(v layers.LinkLayerDiscoveryValue, l int) (err error) {
	if len(v.Value) < l {
		err = fmt.Errorf("invalid TLV %v length %d (wanted mimimum %v)", v.Type, len(v.Value), l)
	}
	return
}

// parseTlvMgmtAddress parses the Management Address TLV and returns the Management Address
// structure.
// we don't parse the OID here, as it's not needed for the neighbor cache
func parseTlvMgmtAddress(v layers.LinkLayerDiscoveryValue) *layers.LLDPMgmtAddress {
	if err := checkLLDPTLVLen(v, 9); err != nil {
		return nil
	}

	mlen := v.Value[0]
	if err := checkLLDPTLVLen(v, int(mlen+7)); err != nil {
		return nil
	}

	return &layers.LLDPMgmtAddress{
		Subtype:          layers.IANAAddressFamily(v.Value[1]),
		Address:          v.Value[2 : mlen+1],
		InterfaceSubtype: layers.LLDPInterfaceSubtype(v.Value[mlen+1]),
		InterfaceNumber:  binary.BigEndian.Uint32(v.Value[mlen+2 : mlen+6]),
	}
}

func lldpMgmtAddressToSerializable(m *layers.LLDPMgmtAddress) ManagementAddress {
	var addrString string
	switch m.Subtype {
	case layers.IANAAddressFamilyIPV4:
		addrString = net.IP(m.Address).String()
	case layers.IANAAddressFamilyIPV6:
		addrString = net.IP(m.Address).String()
	default:
		addrString = string(m.Address)
	}

	return ManagementAddress{
		AddressFamily:    m.Subtype.String(),
		Address:          addrString,
		InterfaceSubtype: m.InterfaceSubtype.String(),
		InterfaceNumber:  m.InterfaceNumber,
	}
}

func tlvStringValue(tlvType layers.LLDPTLVType, value string) layers.LinkLayerDiscoveryValue {
	return layers.LinkLayerDiscoveryValue{
		Type:   tlvType,
		Value:  []byte(value),
		Length: uint16(len(value)),
	}
}

func toLLDPCapabilitiesBytes(capabilities []string) uint16 {
	r := uint16(0)
	for _, capability := range capabilities {
		mask, ok := capabilityMap[capability]
		if ok {
			r |= mask
		}
	}
	return r
}
