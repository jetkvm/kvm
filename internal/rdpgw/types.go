package rdpgw

// MS-TSGU (RD Gateway Transport) protocol constants.
// Reference: [MS-TSGU] Terminal Services Gateway Server Protocol

// Packet types (MS-TSGU 2.2.9.1 HTTP_PACKET_HEADER)
const (
	pktTypeHandshakeRequest     uint16 = 0x01
	pktTypeHandshakeResponse    uint16 = 0x02
	pktTypeTunnelCreate         uint16 = 0x04
	pktTypeTunnelResponse       uint16 = 0x05
	pktTypeTunnelAuth           uint16 = 0x06
	pktTypeTunnelAuthResponse   uint16 = 0x07
	pktTypeChannelCreate        uint16 = 0x08
	pktTypeChannelResponse      uint16 = 0x09
	pktTypeData                 uint16 = 0x0A
	pktTypeServiceMessage       uint16 = 0x0C
	pktTypeKeepAlive            uint16 = 0x0D
	pktTypeCloseChannel         uint16 = 0x10
	pktTypeCloseChannelResponse uint16 = 0x11
)

// HTTP_EXTENDED_AUTH flags (MS-TSGU 2.2.9.2.1.1)
const (
	httpExtendedAuthPAA       uint16 = 0x02 // Pluggable authentication via PAA cookie
	httpExtendedAuthSCRedCard uint16 = 0x04 // Smart card authentication
)

// HTTP_CAPABILITY flags (MS-TSGU 2.2.9.2.1.2 / 2.2.9.2.1.3)
const (
	httpCapabilityTypeQUIC        uint32 = 0x01
	httpCapabilityUDPTransport    uint32 = 0x20
	httpCapabilityTypeServiceMsg  uint32 = 0x04
	httpCapabilityTypeExtAuth     uint32 = 0x02
	httpCapabilityTypeIdleTimeout uint32 = 0x10
	httpCapabilityTypeReauth      uint32 = 0x40
	httpCapabilityTypeMsgRequest  uint32 = 0x100
	httpCapabilityTypeMsgResponse uint32 = 0x200
)

// HTTP_TUNNEL_RESPONSE field IDs (MS-TSGU 2.2.9.2.1.4)
const (
	httpTunnelResponseFieldTunnelID uint16 = 0x01
	httpTunnelResponseFieldCaps     uint16 = 0x02
	httpTunnelResponseFieldSOHResp  uint16 = 0x04
	httpTunnelResponseFieldConsent  uint16 = 0x10
)

// HTTP_CHANNEL_RESPONSE field IDs (MS-TSGU 2.2.9.2.1.6)
const (
	httpChannelResponseFieldChannelID uint16 = 0x01
	httpChannelResponseFieldUDPPort   uint16 = 0x04
)

// Tunnel auth redirect flags (MS-TSGU 2.2.9.2.1.5)
const (
	httpTunnelAuthRedirectFlagsEnable  uint32 = 0x01
	httpTunnelAuthRedirectFlagsDisable uint32 = 0x00
)

// Error codes (MS-TSGU 2.2.9.2.1.7 — subset we use)
const (
	errorCodeSuccess    uint32 = 0x00000000
	errorCodeDenied     uint32 = 0x00000001
	errorCodeQuarantine uint32 = 0x00000003
)

// Gateway connection state machine
type connState int

const (
	stateInitialized connState = iota
	stateHandshake
	stateTunnel
	stateAuthorized
	stateOpened
	stateClosed
)
