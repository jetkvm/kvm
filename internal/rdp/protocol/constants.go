// Package protocol implements the RDP protocol layers.
package protocol

import "time"

// TPKT constants (RFC 1006).
const (
	TPKTVersion      = 3
	TPKTHeaderLength = 4
)

// X.224 TPDU types (ISO 8073 / ITU-T X.224).
const (
	X224ConnectionRequest  = 0xE0
	X224ConnectionConfirm  = 0xD0
	X224DisconnectRequest  = 0x80
	X224Data               = 0xF0
	X224DataEOT            = 0x80 // End of transmission flag
	X224ConnectionReqClass = 0x00 // Class 0
)

// X.224 TPDU header lengths.
const (
	X224ConnectionReqMinLen  = 7 // Minimum CR TPDU length (LI through class)
	X224ConnectionConfirmLen = 7 // CC TPDU length
	X224DataHeaderLen        = 3 // Data TPDU header (LI, code, EOT)
	X224DisconnectReqLen     = 7 // DR TPDU length
)

// RDP negotiation request/response types.
const (
	NegTypeRequest  = 0x01 // TYPE_RDP_NEG_REQ
	NegTypeResponse = 0x02 // TYPE_RDP_NEG_RSP
	NegTypeFailure  = 0x03 // TYPE_RDP_NEG_FAILURE
)

// RDP requested protocols (bitmask).
const (
	ProtocolRDP      = 0x00000000 // Standard RDP Security
	ProtocolTLS      = 0x00000001 // TLS 1.0/1.1/1.2
	ProtocolCredSSP  = 0x00000002 // CredSSP (NLA)
	ProtocolRDSTLS   = 0x00000004 // RDSTLS
	ProtocolHybridEx = 0x00000008 // Hybrid + Early User Auth
)

// RDP negotiation flags.
const (
	NegFlagExtendedClientDataSupported = 0x01
	NegFlagDynvcGfxProtocolSupported   = 0x02
	NegFlagRestrictedAdminModeRequired = 0x08
	NegFlagRedirectedAuthRequired      = 0x10
)

// MCS constants (T.125 / ITU-T X.226).
const (
	MCSConnectInitial  = 0x65 // Application tag for Connect-Initial
	MCSConnectResponse = 0x66 // Application tag for Connect-Response

	MCSBaseChannelID = 1001 // First available dynamic channel ID
	MCSUserIDBase    = 1001 // Base for user channel IDs

	// Domain MCS PDU types (PER encoded)
	MCSErectDomainRequest  = 1
	MCSAttachUserRequest   = 10
	MCSAttachUserConfirm   = 11
	MCSChannelJoinRequest  = 14
	MCSChannelJoinConfirm  = 15
	MCSSendDataRequest     = 25
	MCSSendDataIndication  = 26
	MCSDisconnectUltimatum = 8
)

// MCS result codes.
const (
	MCSResultSuccessful             = 0
	MCSResultDomainMerging          = 1
	MCSResultDomainNotHierarch      = 2
	MCSResultNoSuchChannel          = 3
	MCSResultNoSuchDomain           = 4
	MCSResultNoSuchUser             = 5
	MCSResultNotAdmitted            = 6
	MCSResultOtherUserID            = 7
	MCSResultParametersUnacceptable = 8
	MCSResultTokenNotAvailable      = 9
	MCSResultTokenNotPossessed      = 10
	MCSResultTooManyChannels        = 11
	MCSResultTooManyTokens          = 12
	MCSResultTooManyUsers           = 13
	MCSResultUnspecifiedFailure     = 14
	MCSResultUserRejected           = 15
)

// GCC (T.124) constants.
const (
	// H.221 non-standard key for Microsoft
	GCCConferenceCreateRequest  = 0x00
	GCCConferenceCreateResponse = 0x14

	// Object identifier for T.124
	T124OID = "0.0.20.124.0.1"
)

// Standard RDP channel IDs.
const (
	ChannelMCSGlobalID = 1003 // MCS global channel
	ChannelUserID      = 1007 // User channel (assigned dynamically)
	ChannelIOChannel   = 1003 // I/O channel
)

// RDP channel option flags.
const (
	ChannelOptionInitialized   = 0x80000000
	ChannelOptionEncryptRDP    = 0x40000000
	ChannelOptionEncryptSC     = 0x20000000
	ChannelOptionEncryptCS     = 0x10000000
	ChannelOptionPriorityHigh  = 0x08000000
	ChannelOptionPriorityMed   = 0x04000000
	ChannelOptionPriorityLow   = 0x02000000
	ChannelOptionCompressRDP   = 0x00800000
	ChannelOptionCompress      = 0x00400000
	ChannelOptionShowProtocol  = 0x00200000
	ChannelOptionRemoteControl = 0x00100000
)

// RDP security header flags.
const (
	SecExchangePkt    = 0x0001
	SecTransportReq   = 0x0002
	SecTransportRsp   = 0x0004
	SecEncrypt        = 0x0008
	SecResetSeqno     = 0x0010
	SecIgnoreSeqno    = 0x0020
	SecInfoPkt        = 0x0040
	SecLicensePkt     = 0x0080
	SecLicenseEncrypt = 0x0200
	SecRedirectionPkt = 0x0400
	SecSecureChecksum = 0x0800
	SecAutodetectReq  = 0x1000
	SecAutodetectRsp  = 0x2000
	SecHeartbeat      = 0x4000
	SecFlagsPkt       = 0x8000
)

// RDP PDU types (Share Control Header).
const (
	PDUTypeDemandActive   = 0x01
	PDUTypeConfirmActive  = 0x03
	PDUTypeDeactivateAll  = 0x06
	PDUTypeData           = 0x07
	PDUTypeServerRedirect = 0x0A
)

// RDP Data PDU types (Share Data Header).
const (
	DataPDUTypeUpdate                    = 0x02
	DataPDUTypeControl                   = 0x14
	DataPDUTypePointer                   = 0x1B
	DataPDUTypeInput                     = 0x1C
	DataPDUTypeSynchronize               = 0x1F
	DataPDUTypeRefreshRect               = 0x21
	DataPDUTypeSuppressOutput            = 0x23
	DataPDUTypeShutdownRequest           = 0x24
	DataPDUTypeShutdownDenied            = 0x25
	DataPDUTypeSaveSessionInfo           = 0x26
	DataPDUTypeFontList                  = 0x27
	DataPDUTypeFontMap                   = 0x28
	DataPDUTypeSetKeyboardIndicators     = 0x29
	DataPDUTypeBitmapCachePersistentList = 0x2B
	DataPDUTypeBitmapCacheError          = 0x2C
	DataPDUTypeSetKeyboardImeStatus      = 0x2D
	DataPDUTypeOffscreenCacheError       = 0x2E
	DataPDUTypeSetErrorInfo              = 0x2F
	DataPDUTypeDrawNineGrid              = 0x30
	DataPDUTypeDrawGdiplus               = 0x31
	DataPDUTypeArcStatus                 = 0x32
	DataPDUTypeStatusInfo                = 0x36
	DataPDUTypeMonitorLayout             = 0x37
	DataPDUTypeFrameAck                  = 0x38
)

// Capability set types.
const (
	CapabilityGeneral                = 0x0001
	CapabilityBitmap                 = 0x0002
	CapabilityOrder                  = 0x0003
	CapabilityBitmapCache            = 0x0004
	CapabilityControl                = 0x0005
	CapabilityActivation             = 0x0007
	CapabilityPointer                = 0x0008
	CapabilityShare                  = 0x0009
	CapabilityColorCache             = 0x000A
	CapabilitySound                  = 0x000C
	CapabilityInput                  = 0x000D
	CapabilityFont                   = 0x000E
	CapabilityBrush                  = 0x000F
	CapabilityGlyphCache             = 0x0010
	CapabilityOffscreenCache         = 0x0011
	CapabilityBitmapCacheHostSupport = 0x0012
	CapabilityBitmapCacheV2          = 0x0013
	CapabilityVirtualChannel         = 0x0014
	CapabilityDrawNineGrid           = 0x0015
	CapabilityDrawGdiplus            = 0x0016
	CapabilityRail                   = 0x0017
	CapabilityWindow                 = 0x0018
	CapabilityCompDesk               = 0x0019
	CapabilityMultifragUpdate        = 0x001A
	CapabilityLargePointer           = 0x001B
	CapabilitySurfaceCommands        = 0x001C
	CapabilityBitmapCodecs           = 0x001D
	CapabilityFrameAck               = 0x001E
)

// Timeouts for various protocol phases.
const (
	HandshakeTimeout   = 30 * time.Second
	NegotiationTimeout = 30 * time.Second
	ReadTimeout        = 60 * time.Second
	WriteTimeout       = 5 * time.Second
	KeepAliveInterval  = 30 * time.Second
)

// Maximum sizes.
const (
	MaxChannelNameLen   = 8
	MaxChannels         = 31
	MaxTPKTLength       = 65535
	MaxCredentialLength = 256
	MaxDomainLength     = 52
	MaxUsernameLength   = 512
	MaxPasswordLength   = 512
)

// ChannelName is a fixed-size channel name.
type ChannelName [MaxChannelNameLen]byte

// String returns the channel name as a string.
func (n ChannelName) String() string {
	for i, b := range n {
		if b == 0 {
			return string(n[:i])
		}
	}
	return string(n[:])
}
