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

// Update PDU types (used in SlowPath Graphics Update PDUs).
const (
	UpdateTypeOrders  = 0x0000 // Orders Update
	UpdateTypeBitmap  = 0x0001 // Bitmap Update
	UpdateTypePalette = 0x0002 // Palette Update
	UpdateTypeSynchr  = 0x0003 // Synchronize Update
)

// Bitmap data flags (TS_BITMAP_DATA).
const (
	BitmapCompressionNone = 0x0000 // No compression
	BitmapCompression     = 0x0001 // RLE compression used
	BitmapNoComprHdr      = 0x0400 // No compression header
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

// General Capability extraFlags (MS-RDPBCGR 2.2.7.1.1).
const (
	ExtraFlagsFastPathOutputSupported = 0x0001 // FASTPATH_OUTPUT_SUPPORTED
	ExtraFlagsNoBitmapCompressionHdr  = 0x0400 // NO_BITMAP_COMPRESSION_HDR
	ExtraFlagsLongCredentialsSupported = 0x0004 // LONG_CREDENTIALS_SUPPORTED
	ExtraFlagsAutoReconnectSupported  = 0x0008 // AUTORECONNECT_SUPPORTED
	ExtraFlagsEncSaltedChecksum       = 0x0002 // ENC_SALTED_CHECKSUM
)

// Fast-Path Output PDU constants (MS-RDPBCGR 2.2.9.1.2).
const (
	// Fast-Path update codes (bits 0-3 of updateHeader)
	FastPathUpdateOrders       = 0x00 // FASTPATH_UPDATETYPE_ORDERS
	FastPathUpdateBitmap       = 0x01 // FASTPATH_UPDATETYPE_BITMAP
	FastPathUpdatePalette      = 0x02 // FASTPATH_UPDATETYPE_PALETTE
	FastPathUpdateSynchronize  = 0x03 // FASTPATH_UPDATETYPE_SYNCHRONIZE
	FastPathUpdateSurfCmds     = 0x04 // FASTPATH_UPDATETYPE_SURFCMDS
	FastPathUpdatePtrNull      = 0x05 // FASTPATH_UPDATETYPE_PTR_NULL
	FastPathUpdatePtrDefault   = 0x06 // FASTPATH_UPDATETYPE_PTR_DEFAULT
	FastPathUpdatePtrPosition  = 0x08 // FASTPATH_UPDATETYPE_PTR_POSITION
	FastPathUpdateColor        = 0x09 // FASTPATH_UPDATETYPE_COLOR
	FastPathUpdateCachedPointer = 0x0A // FASTPATH_UPDATETYPE_CACHED
	FastPathUpdatePointer      = 0x0B // FASTPATH_UPDATETYPE_POINTER
	FastPathUpdateLargePointer = 0x0C // FASTPATH_UPDATETYPE_LARGE_POINTER

	// Fast-Path fragmentation flags (bits 4-5 of updateHeader)
	// Per MS-RDPBCGR 2.2.9.1.2.1: bits 4-5 encode: 0=single, 1=last, 2=first, 3=next
	FastPathFragSingle = 0x00 // FASTPATH_FRAGMENT_SINGLE (0 << 4)
	FastPathFragLast   = 0x10 // FASTPATH_FRAGMENT_LAST (1 << 4)
	FastPathFragFirst  = 0x20 // FASTPATH_FRAGMENT_FIRST (2 << 4)
	FastPathFragNext   = 0x30 // FASTPATH_FRAGMENT_NEXT (3 << 4)

	// Fast-Path compression flag (bit 6 of updateHeader)
	FastPathOutputCompressed = 0x40 // FASTPATH_OUTPUT_COMPRESSION_USED

	// Fast-Path action codes (bits 0-1 of fpOutputHeader)
	FastPathActionFastPath = 0x00 // FASTPATH_OUTPUT_ACTION_FASTPATH
	FastPathActionX224     = 0x03 // FASTPATH_OUTPUT_ACTION_X224

	// Fast-Path encryption flags (bits 6-7 of fpOutputHeader)
	FastPathOutputSecureChecksum = 0x40 // FASTPATH_OUTPUT_SECURE_CHECKSUM
	FastPathOutputEncrypted      = 0x80 // FASTPATH_OUTPUT_ENCRYPTED
)

// Timeouts for various protocol phases.
const (
	HandshakeTimeout   = 30 * time.Second
	NegotiationTimeout = 30 * time.Second
	ReadTimeout        = 0 // No timeout - blocking read for maximum responsiveness
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
