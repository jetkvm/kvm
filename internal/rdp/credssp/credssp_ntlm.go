package credssp

// NTLM message building for CredSSP authentication.
// This file contains the NTLM CHALLENGE message construction.

import "time"

// AV_PAIR IDs for NTLM TargetInfo
const (
	MsvAvEOL             = 0
	MsvAvNbComputerName  = 1
	MsvAvNbDomainName    = 2
	MsvAvDnsComputerName = 3
	MsvAvDnsDomainName   = 4
	MsvAvTimestamp       = 7
)

// NTLM negotiate flags
const (
	NTLMSSP_NEGOTIATE_UNICODE                   = 0x00000001
	NTLMSSP_NEGOTIATE_OEM                       = 0x00000002
	NTLMSSP_REQUEST_TARGET                      = 0x00000004
	NTLMSSP_NEGOTIATE_SIGN                      = 0x00000010
	NTLMSSP_NEGOTIATE_SEAL                      = 0x00000020
	NTLMSSP_NEGOTIATE_LM_KEY                    = 0x00000080
	NTLMSSP_NEGOTIATE_NTLM                      = 0x00000200
	NTLMSSP_NEGOTIATE_ALWAYS_SIGN               = 0x00008000
	NTLMSSP_TARGET_TYPE_SERVER                  = 0x00020000
	NTLMSSP_NEGOTIATE_EXTENDED_SESSION_SECURITY = 0x00080000
	NTLMSSP_NEGOTIATE_TARGET_INFO               = 0x00800000
	NTLMSSP_NEGOTIATE_VERSION                   = 0x02000000
	NTLMSSP_NEGOTIATE_128                       = 0x20000000
	NTLMSSP_NEGOTIATE_KEY_EXCH                  = 0x40000000
	NTLMSSP_NEGOTIATE_56                        = 0x80000000
)

// buildTargetInfo creates the AV_PAIR list for NTLM CHALLENGE
func (h *Handler) buildTargetInfo() []byte {
	h.debugLog("CredSSP: building TargetInfo AV_PAIRs")

	// Use configured domain or default to "JETKVM"
	nbDomain := h.expectedDomain
	if nbDomain == "" {
		nbDomain = "JETKVM"
	}

	// Build DNS domain name (lowercase with .local suffix if no dots present)
	dnsDomain := nbDomain
	if len(dnsDomain) > 0 {
		// Convert to lowercase for DNS
		dnsDomainLower := make([]byte, len(dnsDomain))
		for i := 0; i < len(dnsDomain); i++ {
			c := dnsDomain[i]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			dnsDomainLower[i] = c
		}
		dnsDomain = string(dnsDomainLower)
		// Add .local suffix if no dots present (not a FQDN)
		hasDot := false
		for i := 0; i < len(dnsDomain); i++ {
			if dnsDomain[i] == '.' {
				hasDot = true
				break
			}
		}
		if !hasDot {
			dnsDomain = dnsDomain + ".local"
		}
	}

	// Build AV_PAIRs for TargetInfo
	domainName := encodeUTF16LE(nbDomain)
	computerName := encodeUTF16LE("JETKVM")
	dnsDomainName := encodeUTF16LE(dnsDomain)
	dnsComputerName := encodeUTF16LE("jetkvm." + dnsDomain)

	// Timestamp in Windows FILETIME format (100-ns intervals since Jan 1, 1601)
	// For simplicity, use current Unix time converted to FILETIME
	// FILETIME = (Unix seconds * 10,000,000) + 116444736000000000
	now := time.Now().Unix()
	fileTime := uint64(now)*10000000 + 116444736000000000
	timestamp := make([]byte, 8)
	timestamp[0] = byte(fileTime)
	timestamp[1] = byte(fileTime >> 8)
	timestamp[2] = byte(fileTime >> 16)
	timestamp[3] = byte(fileTime >> 24)
	timestamp[4] = byte(fileTime >> 32)
	timestamp[5] = byte(fileTime >> 40)
	timestamp[6] = byte(fileTime >> 48)
	timestamp[7] = byte(fileTime >> 56)

	// Calculate total size
	// Each AV_PAIR: 2 bytes ID + 2 bytes length + value
	size := 0
	size += 4 + len(domainName)      // MsvAvNbDomainName
	size += 4 + len(computerName)    // MsvAvNbComputerName
	size += 4 + len(dnsDomainName)   // MsvAvDnsDomainName
	size += 4 + len(dnsComputerName) // MsvAvDnsComputerName
	size += 4 + 8                    // MsvAvTimestamp
	size += 4                        // MsvAvEOL

	info := make([]byte, size)
	offset := 0

	// Helper to add an AV_PAIR
	addPair := func(id uint16, value []byte) {
		info[offset] = byte(id)
		info[offset+1] = byte(id >> 8)
		info[offset+2] = byte(len(value))
		info[offset+3] = byte(len(value) >> 8)
		copy(info[offset+4:], value)
		offset += 4 + len(value)
	}

	// Add AV_PAIRs in order
	addPair(MsvAvNbDomainName, domainName)
	addPair(MsvAvNbComputerName, computerName)
	addPair(MsvAvDnsDomainName, dnsDomainName)
	addPair(MsvAvDnsComputerName, dnsComputerName)
	addPair(MsvAvTimestamp, timestamp)

	// MsvAvEOL - end of list
	info[offset] = MsvAvEOL
	info[offset+1] = 0
	info[offset+2] = 0
	info[offset+3] = 0

	h.debugLog("CredSSP: TargetInfo built, len=%d, hex: % 02X", len(info), info)
	return info
}

func (h *Handler) buildNTLMChallengeWithFlags(clientFlags uint32) []byte {
	// Build NTLM CHALLENGE message with TargetInfo
	// Structure:
	// - 0-7: Signature "NTLMSSP\0"
	// - 8-11: MessageType (2)
	// - 12-19: TargetNameFields (Len:2, MaxLen:2, Offset:4)
	// - 20-23: NegotiateFlags
	// - 24-31: ServerChallenge
	// - 32-39: Reserved
	// - 40-47: TargetInfoFields (Len:2, MaxLen:2, Offset:4)
	// - 48-55: Version
	// - 56+: Payload (TargetName, then TargetInfo)

	// Use configured domain or default to "JETKVM"
	nbDomain := h.expectedDomain
	if nbDomain == "" {
		nbDomain = "JETKVM"
	}
	targetName := encodeUTF16LE(nbDomain)
	targetInfo := h.buildTargetInfo()

	// PayloadOffset = 56 (includes Version)
	payloadOffset := uint32(56)
	targetNameOffset := payloadOffset
	targetInfoOffset := targetNameOffset + uint32(len(targetName))

	// Total message size
	msgSize := int(targetInfoOffset) + len(targetInfo)
	msg := make([]byte, msgSize)

	// Signature
	copy(msg[0:8], ntlmSignature)

	// MessageType = 2 (CHALLENGE)
	msg[8] = NtlmChallenge
	msg[9] = 0
	msg[10] = 0
	msg[11] = 0

	// TargetNameFields (Len, MaxLen, Offset)
	targetNameLen := uint16(len(targetName))
	msg[12] = byte(targetNameLen)
	msg[13] = byte(targetNameLen >> 8)
	msg[14] = byte(targetNameLen)
	msg[15] = byte(targetNameLen >> 8)
	msg[16] = byte(targetNameOffset)
	msg[17] = byte(targetNameOffset >> 8)
	msg[18] = byte(targetNameOffset >> 16)
	msg[19] = byte(targetNameOffset >> 24)

	// NegotiateFlags - be conservative, only set what client requested
	// Plus minimal server-required flags
	flags := clientFlags
	// Only add TARGET_INFO if client didn't explicitly exclude it
	// Modern CredSSP requires TargetInfo for timestamp/MIC computation
	flags |= NTLMSSP_NEGOTIATE_TARGET_INFO
	// Add server type indicator
	flags |= NTLMSSP_TARGET_TYPE_SERVER
	// If we're including version, set the flag
	flags |= NTLMSSP_NEGOTIATE_VERSION

	h.debugLog("CredSSP: NTLM flags client=0x%08x server=0x%08x", clientFlags, flags)

	// Debug: show which flags we're adding
	addedFlags := flags &^ clientFlags
	h.debugLog("CredSSP: flags added by server: 0x%08x", addedFlags)

	msg[20] = byte(flags)
	msg[21] = byte(flags >> 8)
	msg[22] = byte(flags >> 16)
	msg[23] = byte(flags >> 24)

	// ServerChallenge (8 bytes)
	copy(msg[24:32], h.serverChallenge)

	// Reserved (8 bytes) - zeros (already zero)

	// TargetInfoFields (Len, MaxLen, Offset)
	targetInfoLen := uint16(len(targetInfo))
	msg[40] = byte(targetInfoLen)
	msg[41] = byte(targetInfoLen >> 8)
	msg[42] = byte(targetInfoLen)
	msg[43] = byte(targetInfoLen >> 8)
	msg[44] = byte(targetInfoOffset)
	msg[45] = byte(targetInfoOffset >> 8)
	msg[46] = byte(targetInfoOffset >> 16)
	msg[47] = byte(targetInfoOffset >> 24)

	// Version (8 bytes)
	msg[48] = 10 // Major version (Windows 10)
	msg[49] = 0  // Minor version
	msg[50] = 0  // Build number low
	msg[51] = 0  // Build number high
	msg[52] = 0
	msg[53] = 0
	msg[54] = 0
	msg[55] = 15 // NTLM revision current

	// Payload: TargetName
	copy(msg[targetNameOffset:], targetName)

	// Payload: TargetInfo
	copy(msg[targetInfoOffset:], targetInfo)

	return msg
}

func encodeUTF16LE(s string) []byte {
	runes := []rune(s)
	result := make([]byte, len(runes)*2)
	for i, r := range runes {
		result[i*2] = byte(r)
		result[i*2+1] = byte(r >> 8)
	}
	return result
}
