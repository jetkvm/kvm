package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// GCC implements the T.124 Generic Conference Control protocol.
// This is used for capability exchange during RDP connection setup.

var (
	ErrGCCInvalidData   = errors.New("gcc: invalid data")
	ErrGCCInvalidHeader = errors.New("gcc: invalid conference header")
)

// GCC data block types (MS-RDPBCGR section 2.2.1.3).
const (
	GCCBlockClientCore      = 0xC001
	GCCBlockClientSecurity  = 0xC002
	GCCBlockClientNetwork   = 0xC003
	GCCBlockClientCluster   = 0xC004
	GCCBlockClientMonitor   = 0xC005
	GCCBlockClientMsgChan   = 0xC006
	GCCBlockClientMonitorEx = 0xC008

	GCCBlockServerCore           = 0x0C01
	GCCBlockServerNetwork        = 0x0C03
	GCCBlockServerSecurity       = 0x0C02
	GCCBlockServerMsgChan        = 0x0C04
	GCCBlockServerMultitransport = 0x0C08
)

// ClientCoreData contains core client information.
type ClientCoreData struct {
	Version         uint32
	DesktopWidth    uint16
	DesktopHeight   uint16
	ColorDepth      uint16
	SASSequence     uint16
	KeyboardLayout  uint32
	ClientBuild     uint32
	ClientName      [32]byte // UTF-16LE, null-terminated
	KeyboardType    uint32
	KeyboardSubType uint32
	KeyboardFuncKey uint32
	ImeFileName     [64]byte // UTF-16LE
	// Extended fields (optional)
	PostBeta2ColorDepth   uint16
	ClientProductID       uint16
	SerialNumber          uint32
	HighColorDepth        uint16
	SupportedColorDepths  uint16
	EarlyCapabilityFlags  uint16
	ClientDigProductID    [64]byte // UTF-16LE
	ConnectionType        uint8
	Pad1                  uint8
	ServerSelectedProto   uint32
	DesktopPhysicalWidth  uint32
	DesktopPhysicalHeight uint32
	DesktopOrientation    uint16
	DesktopScaleFactor    uint32
	DeviceScaleFactor     uint32
}

// ClientSecurityData contains client security preferences.
type ClientSecurityData struct {
	EncryptionMethods uint32
	ExtEncryptMethods uint32
}

// ClientNetworkData contains virtual channel definitions.
type ClientNetworkData struct {
	ChannelCount uint32
	Channels     []ChannelDef
}

// ChannelDef defines a virtual channel.
type ChannelDef struct {
	Name    ChannelName
	Options uint32
}

// ConferenceCreateRequest contains parsed client GCC data.
type ConferenceCreateRequest struct {
	CoreData     *ClientCoreData
	SecurityData *ClientSecurityData
	NetworkData  *ClientNetworkData
}

// ParseConferenceCreateRequest parses a GCC Conference Create Request.
func ParseConferenceCreateRequest(data []byte) (*ConferenceCreateRequest, error) {
	if len(data) < 23 {
		return nil, ErrGCCInvalidData
	}

	// Skip the T.124 PER-encoded Conference Create Request header
	// The structure is complex, but we can skip to the user data
	// by finding the H.221 key and user data length

	// Find user data start (after header)
	pos := 0

	// Skip the initial PER header bytes
	// Conference name + locked/listed/conductible flags + terminate flags
	if data[pos] != 0x00 {
		return nil, ErrGCCInvalidHeader
	}
	pos++

	// Skip variable-length conference selector
	if pos >= len(data) {
		return nil, ErrGCCInvalidHeader
	}

	// Look for the H.221 key pattern: 0x44 0x75 0x63 0x61 ("Duca" = McDuca)
	// This marks the start of client data
	for pos < len(data)-4 {
		if data[pos] == 0x44 && data[pos+1] == 0x75 &&
			data[pos+2] == 0x63 && data[pos+3] == 0x61 {
			pos += 4 // Skip "Duca"
			break
		}
		pos++
	}

	if pos >= len(data)-2 {
		return nil, ErrGCCInvalidData
	}

	// Read user data length (2 bytes, little-endian)
	userDataLen := int(binary.LittleEndian.Uint16(data[pos : pos+2]))
	pos += 2

	if pos+userDataLen > len(data) {
		// Adjust if length exceeds buffer
		userDataLen = len(data) - pos
	}

	userData := data[pos : pos+userDataLen]

	return parseClientDataBlocks(userData)
}

// parseClientDataBlocks parses the client data blocks.
func parseClientDataBlocks(data []byte) (*ConferenceCreateRequest, error) {
	ccr := &ConferenceCreateRequest{}
	pos := 0

	for pos+4 <= len(data) {
		blockType := binary.LittleEndian.Uint16(data[pos : pos+2])
		blockLen := int(binary.LittleEndian.Uint16(data[pos+2 : pos+4]))

		if blockLen < 4 || pos+blockLen > len(data) {
			break
		}

		blockData := data[pos+4 : pos+blockLen]

		switch blockType {
		case GCCBlockClientCore:
			ccr.CoreData = parseClientCoreData(blockData)
		case GCCBlockClientSecurity:
			ccr.SecurityData = parseClientSecurityData(blockData)
		case GCCBlockClientNetwork:
			ccr.NetworkData = parseClientNetworkData(blockData)
		}

		pos += blockLen
	}

	return ccr, nil
}

// parseClientCoreData parses the core data block.
func parseClientCoreData(data []byte) *ClientCoreData {
	if len(data) < 128 {
		return nil
	}

	core := &ClientCoreData{
		Version:        binary.LittleEndian.Uint32(data[0:4]),
		DesktopWidth:   binary.LittleEndian.Uint16(data[4:6]),
		DesktopHeight:  binary.LittleEndian.Uint16(data[6:8]),
		ColorDepth:     binary.LittleEndian.Uint16(data[8:10]),
		SASSequence:    binary.LittleEndian.Uint16(data[10:12]),
		KeyboardLayout: binary.LittleEndian.Uint32(data[12:16]),
		ClientBuild:    binary.LittleEndian.Uint32(data[16:20]),
	}

	copy(core.ClientName[:], data[20:52])

	core.KeyboardType = binary.LittleEndian.Uint32(data[52:56])
	core.KeyboardSubType = binary.LittleEndian.Uint32(data[56:60])
	core.KeyboardFuncKey = binary.LittleEndian.Uint32(data[60:64])

	copy(core.ImeFileName[:], data[64:128])

	// Extended fields
	if len(data) >= 134 {
		core.PostBeta2ColorDepth = binary.LittleEndian.Uint16(data[128:130])
		core.ClientProductID = binary.LittleEndian.Uint16(data[130:132])
		core.SerialNumber = binary.LittleEndian.Uint32(data[132:136])
	}

	if len(data) >= 142 {
		core.HighColorDepth = binary.LittleEndian.Uint16(data[136:138])
		core.SupportedColorDepths = binary.LittleEndian.Uint16(data[138:140])
		core.EarlyCapabilityFlags = binary.LittleEndian.Uint16(data[140:142])
	}

	if len(data) >= 206 {
		copy(core.ClientDigProductID[:], data[142:206])
	}

	if len(data) >= 212 {
		core.ConnectionType = data[206]
		core.Pad1 = data[207]
		core.ServerSelectedProto = binary.LittleEndian.Uint32(data[208:212])
	}

	if len(data) >= 224 {
		core.DesktopPhysicalWidth = binary.LittleEndian.Uint32(data[212:216])
		core.DesktopPhysicalHeight = binary.LittleEndian.Uint32(data[216:220])
		core.DesktopOrientation = binary.LittleEndian.Uint16(data[220:222])
		core.DesktopScaleFactor = binary.LittleEndian.Uint32(data[222:226])
		core.DeviceScaleFactor = binary.LittleEndian.Uint32(data[226:230])
	}

	return core
}

// parseClientSecurityData parses the security data block.
func parseClientSecurityData(data []byte) *ClientSecurityData {
	if len(data) < 8 {
		return nil
	}

	return &ClientSecurityData{
		EncryptionMethods: binary.LittleEndian.Uint32(data[0:4]),
		ExtEncryptMethods: binary.LittleEndian.Uint32(data[4:8]),
	}
}

// parseClientNetworkData parses the network data block.
func parseClientNetworkData(data []byte) *ClientNetworkData {
	if len(data) < 4 {
		return nil
	}

	channelCount := binary.LittleEndian.Uint32(data[0:4])

	if channelCount > MaxChannels {
		channelCount = MaxChannels
	}

	network := &ClientNetworkData{
		ChannelCount: channelCount,
		Channels:     make([]ChannelDef, channelCount),
	}

	pos := 4
	for i := uint32(0); i < channelCount; i++ {
		if pos+12 > len(data) {
			break
		}

		copy(network.Channels[i].Name[:], data[pos:pos+8])
		network.Channels[i].Options = binary.LittleEndian.Uint32(data[pos+8 : pos+12])
		pos += 12
	}

	return network
}

// ServerCoreData contains core server information.
type ServerCoreData struct {
	Version              uint32
	ClientRequestedProto uint32
	EarlyCapFlags        uint32
}

// ServerNetworkData contains server channel assignments.
type ServerNetworkData struct {
	MCSChannelID uint16
	ChannelCount uint16
	ChannelIDs   []uint16
}

// ServerSecurityData contains server security settings.
type ServerSecurityData struct {
	EncryptionMethod  uint32
	EncryptionLevel   uint32
	ServerRandomLen   uint32
	ServerCertLen     uint32
	ServerRandom      []byte
	ServerCertificate []byte
}

// BuildConferenceCreateResponse builds a GCC Conference Create Response.
// This follows MS-RDPBCGR section 2.2.1.4 - Server MCS Connect Response PDU.
// The encoding matches xrdp's implementation exactly.
func BuildConferenceCreateResponse(coreData *ServerCoreData, networkData *ServerNetworkData, securityData *ServerSecurityData) []byte {
	// Build user data blocks first (SC_CORE, SC_SECURITY, SC_NET - order matters!)
	userData := buildServerDataBlocks(coreData, networkData, securityData)

	// GCC Conference Create Response structure (matching xrdp):
	// 1. T.124 OID (7 bytes): 00 05 00 14 7c 00 01
	// 2. Fixed header: 2a 14 76 0a 01 01 00 01 c0 00
	// 3. H.221 key "McDn": 4d 63 44 6e
	// 4. User data length (PER encoded)
	// 5. User data blocks

	result := make([]byte, 0, len(userData)+64)

	// 1. T.124 Object Identifier: {itu-t(0) recommendation(0) t(20) t124(124) version(0) 1}
	result = append(result, 0x00, 0x05, 0x00, 0x14, 0x7C, 0x00, 0x01)

	// 2. PER-encoded connect-response (matching xrdp exactly)
	// The bytes 0x2A 0x14 are FIXED values in the T.124 PER encoding for ConnectGCCPDU
	// 0x2A = part of connect-response encoding
	// 0x14 = choice indicator for conference-create-response
	result = append(result, 0x2A, 0x14)

	// ConferenceCreateResponse fixed bytes (from xrdp)
	result = append(result,
		0x76,       // ConferenceCreateResponse start
		0x0A,       // tag value
		0x01,       // additional encoding
		0x01,       // result present
		0x00,       // result = success
		0x01,       // userData present
		0xC0, 0x00, // nodeID encoded
	)

	// 3. H.221 NonStandardIdentifier key "McDn"
	result = append(result, 0x4D, 0x63, 0x44, 0x6E)

	// 4. User data length (PER encoded)
	if len(userData) < 0x80 {
		result = append(result, byte(len(userData)))
	} else {
		result = append(result, byte(0x80|(len(userData)>>8)), byte(len(userData)))
	}

	// 5. User data blocks (SC_CORE, SC_SECURITY, SC_NET)
	result = append(result, userData...)

	return result
}

// buildServerDataBlocks builds the server data blocks.
// Order MUST be: SC_CORE, SC_SECURITY, SC_NET per MS-RDPBCGR section 2.2.1.4
func buildServerDataBlocks(coreData *ServerCoreData, networkData *ServerNetworkData, securityData *ServerSecurityData) []byte {
	result := make([]byte, 0, 256)
	fmt.Println("DEBUG buildServerDataBlocks: starting")

	// 1. Server Core Data (required, must be first)
	if coreData != nil {
		core := make([]byte, 16)
		binary.LittleEndian.PutUint16(core[0:2], GCCBlockServerCore)
		binary.LittleEndian.PutUint16(core[2:4], 16) // Block length
		binary.LittleEndian.PutUint32(core[4:8], coreData.Version)
		binary.LittleEndian.PutUint32(core[8:12], coreData.ClientRequestedProto)
		binary.LittleEndian.PutUint32(core[12:16], coreData.EarlyCapFlags)
		result = append(result, core...)
	}

	// 2. Server Security Data (required, must come before SC_NET)
	if securityData != nil {
		blockLen := 12 + len(securityData.ServerRandom) + len(securityData.ServerCertificate)
		security := make([]byte, blockLen)
		binary.LittleEndian.PutUint16(security[0:2], GCCBlockServerSecurity)
		binary.LittleEndian.PutUint16(security[2:4], uint16(blockLen))
		binary.LittleEndian.PutUint32(security[4:8], securityData.EncryptionMethod)
		binary.LittleEndian.PutUint32(security[8:12], securityData.EncryptionLevel)

		if len(securityData.ServerRandom) > 0 || len(securityData.ServerCertificate) > 0 {
			binary.LittleEndian.PutUint32(security[12:16], securityData.ServerRandomLen)
			binary.LittleEndian.PutUint32(security[16:20], securityData.ServerCertLen)
			pos := 20
			copy(security[pos:], securityData.ServerRandom)
			pos += len(securityData.ServerRandom)
			copy(security[pos:], securityData.ServerCertificate)
		}

		result = append(result, security...)
	}

	// 3. Server Network Data (optional, must come after SC_SECURITY)
	if networkData != nil {
		blockLen := 8 + len(networkData.ChannelIDs)*2
		// Pad to 4-byte boundary
		if blockLen%4 != 0 {
			blockLen += 4 - (blockLen % 4)
		}

		fmt.Printf("DEBUG SC_NET: MCSChannelID=%d, ChannelCount=%d, blockLen=%d, channelIDs=%v\n",
			networkData.MCSChannelID, networkData.ChannelCount, blockLen, networkData.ChannelIDs)

		network := make([]byte, blockLen)
		binary.LittleEndian.PutUint16(network[0:2], GCCBlockServerNetwork)
		binary.LittleEndian.PutUint16(network[2:4], uint16(blockLen))
		binary.LittleEndian.PutUint16(network[4:6], networkData.MCSChannelID)
		binary.LittleEndian.PutUint16(network[6:8], networkData.ChannelCount)

		pos := 8
		for _, chID := range networkData.ChannelIDs {
			binary.LittleEndian.PutUint16(network[pos:pos+2], chID)
			pos += 2
		}

		fmt.Printf("DEBUG SC_NET hex: % 02X\n", network)
		result = append(result, network...)
	}

	fmt.Printf("DEBUG Server data blocks total: %d bytes, hex: % 02X\n", len(result), result)
	return result
}

// GetClientName returns the client name as a string.
func (c *ClientCoreData) GetClientName() string {
	// ClientName is UTF-16LE encoded
	name := make([]byte, 0, 16)
	for i := 0; i < len(c.ClientName)-1; i += 2 {
		if c.ClientName[i] == 0 && c.ClientName[i+1] == 0 {
			break
		}
		// Simple ASCII extraction (ignoring high byte)
		if c.ClientName[i+1] == 0 {
			name = append(name, c.ClientName[i])
		}
	}
	return string(name)
}

// RDPVersion returns the RDP version from the client core data.
func (c *ClientCoreData) RDPVersion() string {
	switch c.Version {
	case 0x00080001:
		return "4.0"
	case 0x00080004:
		return "5.0"
	case 0x00080005:
		return "5.1"
	case 0x00080006:
		return "5.2"
	case 0x00080007:
		return "6.0"
	case 0x00080008:
		return "6.1"
	case 0x00080009:
		return "7.0"
	case 0x0008000A:
		return "7.1"
	case 0x0008000B:
		return "8.0"
	case 0x0008000C:
		return "8.1"
	case 0x0008000D:
		return "10.0"
	case 0x0008000E:
		return "10.1"
	case 0x0008000F:
		return "10.2"
	case 0x00080010:
		return "10.3"
	case 0x00080011:
		return "10.4"
	case 0x00080012:
		return "10.5"
	case 0x00080013:
		return "10.6"
	case 0x00080014:
		return "10.7"
	default:
		return fmt.Sprintf("Unknown(0x%08X)", c.Version)
	}
}
