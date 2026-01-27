package credssp

// NTLM authentication and session key derivation for CredSSP.
// Implements MS-NLMP and MS-CSSP specifications.

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rc4"
	"crypto/sha256"
	"unicode/utf16"

	"golang.org/x/crypto/md4" //nolint:staticcheck // MD4 required for NTLM authentication compatibility
)

// NTLMAuth handles NTLM authentication validation and session key derivation.
type NTLMAuth struct {
	password        string
	serverChallenge []byte
	clientNonce     []byte // For CredSSP v3+ (CVE-2018-0886)

	// Extracted from AUTHENTICATE message
	ntResponse          []byte
	lmResponse          []byte
	encryptedSessionKey []byte
	keyExchange         bool

	// Derived values
	sessionKey []byte
	debugLog   func(string, ...interface{})

	// For debugging different public key formats
	serverCertDER []byte
}

// NewNTLMAuth creates a new NTLM authenticator.
func NewNTLMAuth(password string, serverChallenge []byte) *NTLMAuth {
	return &NTLMAuth{
		password:        password,
		serverChallenge: serverChallenge,
		debugLog:        func(string, ...interface{}) {},
	}
}

// SetDebugLog sets the debug logging function.
func (a *NTLMAuth) SetDebugLog(fn func(string, ...interface{})) {
	a.debugLog = fn
}

// SetClientNonce sets the client nonce for CredSSP v3+ pubKeyAuth.
func (a *NTLMAuth) SetClientNonce(nonce []byte) {
	a.clientNonce = nonce
}

// SetServerCertificateDER sets the full server certificate DER for format testing.
func (a *NTLMAuth) SetServerCertificateDER(certDER []byte) {
	a.serverCertDER = certDER
}

// computeNTHash computes the NT hash: MD4(password_UTF16LE)
func computeNTHash(password string) []byte {
	// Convert password to UTF-16LE
	utf16Chars := utf16.Encode([]rune(password))
	utf16LE := make([]byte, len(utf16Chars)*2)
	for i, c := range utf16Chars {
		utf16LE[i*2] = byte(c)
		utf16LE[i*2+1] = byte(c >> 8)
	}

	// MD4 hash
	h := md4.New()
	h.Write(utf16LE)
	return h.Sum(nil)
}

// computeNTLMv2Hash computes: HMAC-MD5(NT_hash, uppercase(username) || domain)
func computeNTLMv2Hash(ntHash []byte, username, domain string) []byte {
	// Convert to uppercase UTF-16LE
	usernameUpper := toUpperUTF16LE(username)
	domainUTF16 := toUTF16LE(domain)

	// Concatenate
	data := append(usernameUpper, domainUTF16...)

	// HMAC-MD5
	h := hmac.New(md5.New, ntHash)
	h.Write(data)
	return h.Sum(nil)
}

// toUpperUTF16LE converts string to uppercase UTF-16LE.
func toUpperUTF16LE(s string) []byte {
	// Convert to uppercase runes, then to UTF-16LE
	runes := []rune(s)
	for i, r := range runes {
		if r >= 'a' && r <= 'z' {
			runes[i] = r - 32
		}
	}
	utf16Chars := utf16.Encode(runes)
	result := make([]byte, len(utf16Chars)*2)
	for i, c := range utf16Chars {
		result[i*2] = byte(c)
		result[i*2+1] = byte(c >> 8)
	}
	return result
}

// toUTF16LE converts string to UTF-16LE.
func toUTF16LE(s string) []byte {
	utf16Chars := utf16.Encode([]rune(s))
	result := make([]byte, len(utf16Chars)*2)
	for i, c := range utf16Chars {
		result[i*2] = byte(c)
		result[i*2+1] = byte(c >> 8)
	}
	return result
}

// ParseAuthenticateMessage parses NTLM AUTHENTICATE message and extracts auth data.
func (a *NTLMAuth) ParseAuthenticateMessage(authMsg []byte) (username, domain string, err error) {
	if len(authMsg) < 64 || !bytes.HasPrefix(authMsg, ntlmSignature) || authMsg[8] != NtlmAuthenticate {
		return "", "", ErrInvalidNTLMMsg
	}

	// Parse fields from AUTHENTICATE message:
	// Offset 12-19: LmChallengeResponseFields (Len:2, MaxLen:2, Offset:4)
	// Offset 20-27: NtChallengeResponseFields (Len:2, MaxLen:2, Offset:4)
	// Offset 28-35: DomainNameFields (Len:2, MaxLen:2, Offset:4)
	// Offset 36-43: UserNameFields (Len:2, MaxLen:2, Offset:4)
	// Offset 44-51: WorkstationFields (Len:2, MaxLen:2, Offset:4)
	// Offset 52-59: EncryptedRandomSessionKeyFields (Len:2, MaxLen:2, Offset:4)
	// Offset 60-63: NegotiateFlags

	// LM Response
	lmLen := int(authMsg[12]) | int(authMsg[13])<<8
	lmOff := int(authMsg[16]) | int(authMsg[17])<<8 | int(authMsg[18])<<16 | int(authMsg[19])<<24
	if lmOff+lmLen <= len(authMsg) {
		a.lmResponse = authMsg[lmOff : lmOff+lmLen]
	}

	// NT Response (contains NTProofStr + ClientBlob for NTLMv2)
	ntLen := int(authMsg[20]) | int(authMsg[21])<<8
	ntOff := int(authMsg[24]) | int(authMsg[25])<<8 | int(authMsg[26])<<16 | int(authMsg[27])<<24
	if ntOff+ntLen <= len(authMsg) {
		a.ntResponse = authMsg[ntOff : ntOff+ntLen]
	}

	// Domain
	domainLen := int(authMsg[28]) | int(authMsg[29])<<8
	domainOff := int(authMsg[32]) | int(authMsg[33])<<8 | int(authMsg[34])<<16 | int(authMsg[35])<<24
	if domainOff+domainLen <= len(authMsg) {
		domain = decodeUTF16LE(authMsg[domainOff : domainOff+domainLen])
	}

	// Username
	userLen := int(authMsg[36]) | int(authMsg[37])<<8
	userOff := int(authMsg[40]) | int(authMsg[41])<<8 | int(authMsg[42])<<16 | int(authMsg[43])<<24
	if userOff+userLen <= len(authMsg) {
		username = decodeUTF16LE(authMsg[userOff : userOff+userLen])
	}

	// Encrypted Random Session Key
	encKeyLen := int(authMsg[52]) | int(authMsg[53])<<8
	encKeyOff := int(authMsg[56]) | int(authMsg[57])<<8 | int(authMsg[58])<<16 | int(authMsg[59])<<24
	if encKeyLen > 0 && encKeyOff+encKeyLen <= len(authMsg) {
		a.encryptedSessionKey = authMsg[encKeyOff : encKeyOff+encKeyLen]
		a.keyExchange = true
	}

	// NegotiateFlags
	flags := uint32(authMsg[60]) | uint32(authMsg[61])<<8 | uint32(authMsg[62])<<16 | uint32(authMsg[63])<<24
	a.keyExchange = flags&NTLMSSP_NEGOTIATE_KEY_EXCH != 0

	a.debugLog("NTLM: parsed AUTHENTICATE - user=%s domain=%s lmLen=%d ntLen=%d encKeyLen=%d keyExch=%v flags=0x%08x",
		username, domain, lmLen, ntLen, encKeyLen, a.keyExchange, flags)

	return username, domain, nil
}

// ValidateResponse validates the NTLMv2 response and derives the session key.
// Returns true if authentication succeeds.
func (a *NTLMAuth) ValidateResponse(username, domain string) bool {
	if len(a.ntResponse) < 16 {
		a.debugLog("NTLM: NT response too short: %d bytes", len(a.ntResponse))
		return false
	}

	// NTLMv2 response structure:
	// First 16 bytes: NTProofStr (HMAC-MD5 of serverChallenge + clientBlob)
	// Rest: ClientBlob (timestamp, client nonce, target info, etc.)
	clientNTProofStr := a.ntResponse[:16]
	clientBlob := a.ntResponse[16:]

	a.debugLog("NTLM: validating - ntProofStr=% 02X clientBlobLen=%d", clientNTProofStr, len(clientBlob))

	// Compute expected NTProofStr
	// 1. NT hash = MD4(password)
	ntHash := computeNTHash(a.password)
	a.debugLog("NTLM: NT hash=% 02X", ntHash)

	// 2. NTLMv2 hash = HMAC-MD5(NT hash, uppercase(username) + domain)
	ntlmv2Hash := computeNTLMv2Hash(ntHash, username, domain)
	a.debugLog("NTLM: NTLMv2 hash=% 02X", ntlmv2Hash)

	// 3. Expected NTProofStr = HMAC-MD5(NTLMv2 hash, serverChallenge + clientBlob)
	h := hmac.New(md5.New, ntlmv2Hash)
	h.Write(a.serverChallenge)
	h.Write(clientBlob)
	expectedNTProofStr := h.Sum(nil)

	a.debugLog("NTLM: expected NTProofStr=% 02X", expectedNTProofStr)

	// Compare
	if !hmac.Equal(clientNTProofStr, expectedNTProofStr) {
		a.debugLog("NTLM: NTProofStr mismatch - authentication failed")
		return false
	}

	a.debugLog("NTLM: NTProofStr validated - authentication successful")

	// Derive session key
	// SessionBaseKey = HMAC-MD5(NTLMv2 hash, NTProofStr)
	h = hmac.New(md5.New, ntlmv2Hash)
	h.Write(expectedNTProofStr)
	sessionBaseKey := h.Sum(nil)

	a.debugLog("NTLM: session base key=% 02X keyExchange=%v", sessionBaseKey, a.keyExchange)

	if a.keyExchange && len(a.encryptedSessionKey) == 16 {
		// Decrypt the encrypted random session key using RC4
		// SessionKey = RC4_DECRYPT(SessionBaseKey, EncryptedRandomSessionKey)
		cipher, err := rc4.NewCipher(sessionBaseKey)
		if err != nil {
			a.debugLog("NTLM: RC4 cipher creation failed: %v", err)
			return false
		}
		a.sessionKey = make([]byte, 16)
		cipher.XORKeyStream(a.sessionKey, a.encryptedSessionKey)
		a.debugLog("NTLM: decrypted session key=% 02X", a.sessionKey)
	} else {
		// No key exchange, session key = session base key
		a.sessionKey = sessionBaseKey
		a.debugLog("NTLM: using session base key as session key")
	}

	return true
}

// GetSessionKey returns the derived session key.
func (a *NTLMAuth) GetSessionKey() []byte {
	return a.sessionKey
}

// VerifyClientPubKeyAuth decrypts and verifies the client's pubKeyAuth.
// This helps debug mismatches by showing what hash the client computed.
// Returns the decrypted hash if successful, nil otherwise.
func (a *NTLMAuth) VerifyClientPubKeyAuth(clientPubKeyAuth, serverPublicKey []byte, version int) []byte {
	if len(a.sessionKey) == 0 || len(clientPubKeyAuth) < 48 {
		a.debugLog("VerifyClient: invalid inputs - sessionKeyLen=%d pubKeyAuthLen=%d", len(a.sessionKey), len(clientPubKeyAuth))
		return nil
	}

	// Client uses client-to-server keys
	signKeyInput := append([]byte(nil), a.sessionKey...)
	signKeyInput = append(signKeyInput, []byte("session key to client-to-server signing key magic constant\x00")...)
	signKeyHash := md5.Sum(signKeyInput)
	signKey := signKeyHash[:]

	sealKeyInput := append([]byte(nil), a.sessionKey...)
	sealKeyInput = append(sealKeyInput, []byte("session key to client-to-server sealing key magic constant\x00")...)
	sealKeyHash := md5.Sum(sealKeyInput)
	sealKey := sealKeyHash[:]

	a.debugLog("VerifyClient: client signKey=% 02X sealKey=% 02X", signKey, sealKey)

	// Parse NTLM signature: Version(4) || EncryptedChecksum(8) || SeqNum(4) || EncryptedMessage
	if clientPubKeyAuth[0] != 0x01 || clientPubKeyAuth[1] != 0x00 || clientPubKeyAuth[2] != 0x00 || clientPubKeyAuth[3] != 0x00 {
		a.debugLog("VerifyClient: invalid version in signature")
		return nil
	}

	encryptedChecksum := clientPubKeyAuth[4:12]
	seqNumBytes := clientPubKeyAuth[12:16]
	encryptedMessage := clientPubKeyAuth[16:]

	a.debugLog("VerifyClient: encChecksum=% 02X seqNum=% 02X encMsg=% 02X", encryptedChecksum, seqNumBytes, encryptedMessage)

	// Initialize RC4 with sealing key
	rc4Cipher, err := rc4.NewCipher(sealKey)
	if err != nil {
		return nil
	}

	// Decrypt message first (advances RC4 state)
	decryptedMessage := make([]byte, len(encryptedMessage))
	rc4Cipher.XORKeyStream(decryptedMessage, encryptedMessage)

	// Decrypt checksum (using RC4 state after message decryption)
	decryptedChecksum := make([]byte, 8)
	rc4Cipher.XORKeyStream(decryptedChecksum, encryptedChecksum)

	a.debugLog("VerifyClient: decrypted hash=% 02X", decryptedMessage)
	a.debugLog("VerifyClient: decrypted checksum=% 02X", decryptedChecksum)

	// Verify checksum: HMAC-MD5(SignKey, SeqNum || PlaintextMessage)
	h := hmac.New(md5.New, signKey)
	h.Write(seqNumBytes)
	h.Write(decryptedMessage)
	expectedChecksum := h.Sum(nil)[:8]

	a.debugLog("VerifyClient: expected checksum=% 02X", expectedChecksum)

	if !hmac.Equal(decryptedChecksum, expectedChecksum) {
		a.debugLog("VerifyClient: checksum mismatch!")
	} else {
		a.debugLog("VerifyClient: checksum OK")
	}

	// Now compute what we expect the client to have sent
	// Per MS-CSSP 3.1.5.3: ClientServerHash = SHA256(ClientServerHashMagic || Nonce || SubjectPublicKey)
	// ClientServerHashMagic = "CredSSP Client-To-Server Binding Hash\0" (with null terminator!)
	// SubjectPublicKey = BIT STRING content from SubjectPublicKeyInfo (the RSA public key)
	if version >= 5 && len(a.clientNonce) == 32 {
		// Extract SubjectPublicKey (BIT STRING content from SubjectPublicKeyInfo)
		pubKeyRSA := extractPublicKeyFromSPKI(serverPublicKey)

		// MS-CSSP specification format: SHA256(Magic || Nonce || SubjectPublicKey)
		// Magic = "CredSSP Client-To-Server Binding Hash\0" (38 bytes with null)
		var hashCorrectFormat []byte
		if pubKeyRSA != nil {
			h := sha256.New()
			h.Write([]byte("CredSSP Client-To-Server Binding Hash\x00"))
			h.Write(a.clientNonce)
			h.Write(pubKeyRSA)
			hashCorrectFormat = h.Sum(nil)
			a.debugLog("VerifyClient: CORRECT FORMAT (Magic+Nonce+SubjectPublicKey) len=%d, hash=% 02X", len(pubKeyRSA), hashCorrectFormat)
		}

		// Try with full SPKI in case client uses that
		hFullSPKI := sha256.New()
		hFullSPKI.Write([]byte("CredSSP Client-To-Server Binding Hash\x00"))
		hFullSPKI.Write(a.clientNonce)
		hFullSPKI.Write(serverPublicKey)
		hashWithFullSPKI := hFullSPKI.Sum(nil)
		a.debugLog("VerifyClient: with FULL SPKI (len=%d), hash=% 02X", len(serverPublicKey), hashWithFullSPKI)

		// Also try the old incorrect magic strings in case some clients use them
		var hashOldMagic []byte
		if pubKeyRSA != nil {
			hOld := sha256.New()
			hOld.Write([]byte("CredSSP Client\x00"))
			hOld.Write(a.clientNonce)
			hOld.Write(pubKeyRSA)
			hashOldMagic = hOld.Sum(nil)
			a.debugLog("VerifyClient: OLD magic (CredSSP Client), hash=% 02X", hashOldMagic)
		}

		// Check which one matches
		matched := false
		if pubKeyRSA != nil && bytes.Equal(decryptedMessage, hashCorrectFormat) {
			a.debugLog("VerifyClient: CLIENT HASH MATCHES with CORRECT FORMAT!")
			matched = true
		} else if bytes.Equal(decryptedMessage, hashWithFullSPKI) {
			a.debugLog("VerifyClient: CLIENT HASH MATCHES with FULL SPKI!")
			matched = true
		} else if pubKeyRSA != nil && bytes.Equal(decryptedMessage, hashOldMagic) {
			a.debugLog("VerifyClient: CLIENT HASH MATCHES with OLD magic string!")
			matched = true
		}

		if !matched {
			a.debugLog("VerifyClient: CLIENT HASH MISMATCH!")
			a.debugLog("VerifyClient: client sent=% 02X", decryptedMessage)
			a.debugLog("VerifyClient: expected  =% 02X", hashCorrectFormat)
		}
	}

	return decryptedMessage
}

// ComputePubKeyAuth computes the pubKeyAuth for the final CredSSP TSRequest.
// For CredSSP v5+ (with nonce), uses SHA-256 hash wrapped in NTLM SEAL.
// For CredSSP v3-4, uses RC4 encryption with NTLM signature on incremented key.
// The pubKeyAuth binds authentication to the TLS channel's server public key.
func (a *NTLMAuth) ComputePubKeyAuth(serverPublicKey []byte, version int) []byte {
	if len(a.sessionKey) == 0 {
		a.debugLog("PubKeyAuth: no session key available")
		return nil
	}

	a.debugLog("PubKeyAuth: version=%d nonceLen=%d pubKeyLen=%d", version, len(a.clientNonce), len(serverPublicKey))
	a.debugLog("PubKeyAuth: sessionKey=% 02X", a.sessionKey)
	a.debugLog("PubKeyAuth: clientNonce=% 02X", a.clientNonce)
	if len(serverPublicKey) > 32 {
		a.debugLog("PubKeyAuth: pubKey first 32 bytes=% 02X", serverPublicKey[:32])
	}

	if version >= 5 && len(a.clientNonce) == 32 {
		// CredSSP v5+ with nonce: use plain SHA-256 (NOT HMAC!)
		// Per MS-CSSP 3.1.5.4: ServerClientHash = SHA256(ServerClientHashMagic || Nonce || SubjectPublicKey)
		// The magic string MUST include the null terminator!
		// ServerClientHashMagic = "CredSSP Server-To-Client Binding Hash\0"

		// Extract SubjectPublicKey (BIT STRING content from SubjectPublicKeyInfo)
		pubKeyForHash := extractPublicKeyFromSPKI(serverPublicKey)
		a.debugLog("PubKeyAuth v5+: extractPublicKeyFromSPKI returned len=%d (input SPKI len=%d)",
			len(pubKeyForHash), len(serverPublicKey))
		if pubKeyForHash == nil {
			a.debugLog("PubKeyAuth v5+: RSA key extraction failed, falling back to full SPKI")
			pubKeyForHash = serverPublicKey // Fallback to full SPKI if extraction fails
		}
		a.debugLog("PubKeyAuth v5+: using key for hash, len=%d, first 32 bytes=% 02X", len(pubKeyForHash), pubKeyForHash[:min(32, len(pubKeyForHash))])

		// Plain SHA-256 hash: MagicString || Nonce || SubjectPublicKey
		h := sha256.New()
		h.Write([]byte("CredSSP Server-To-Client Binding Hash\x00")) // Magic with null terminator
		h.Write(a.clientNonce)
		h.Write(pubKeyForHash)
		hashResult := h.Sum(nil)
		a.debugLog("PubKeyAuth: v5+ SHA-256 hash=% 02X", hashResult)

		// Wrap in NTLM SEAL format (encrypt + sign)
		wrapped := a.ntlmSign(hashResult, 0) // SeqNum 0 for server response
		a.debugLog("PubKeyAuth: v5+ NTLM wrapped len=%d", len(wrapped))
		return wrapped
	}

	// CredSSP v3-4: Use NTLM SEAL on the incremented public key
	// For versions < 5, we increment the public key value by 1
	// This is per MS-CSSP 3.1.5: "...the server MUST encrypt the public key + 1"
	//
	// IMPORTANT: Use the extracted RSA public key (same as v5+), not the full SPKI.
	// Some clients (like Remotix) expect only the RSA key portion.
	//
	// NOTE: For CredSSP v3-4, we use NTLM SEAL (not raw RC4) per MS-CSSP 3.1.5.
	// The ntlmSign function handles encryption + signature together.
	pubKeyForIncrement := extractPublicKeyFromSPKI(serverPublicKey)
	if pubKeyForIncrement == nil {
		pubKeyForIncrement = serverPublicKey // Fallback to full SPKI if extraction fails
	}
	a.debugLog("PubKeyAuth v3-4: using RSA key for increment, len=%d", len(pubKeyForIncrement))
	incrementedKey := incrementPublicKey(pubKeyForIncrement)
	a.debugLog("PubKeyAuth v3-4: incremented key first 32 bytes=% 02X", incrementedKey[:min(32, len(incrementedKey))])

	// Wrap in NTLM SEAL format (encrypt + sign) - this is the correct format for v3-4
	wrapped := a.ntlmSign(incrementedKey, 0)
	a.debugLog("PubKeyAuth v3-4: NTLM sealed result len=%d", len(wrapped))
	return wrapped
}

// ntlmSeal wraps a message with NTLM SEAL (encryption + signature).
// Per MS-NLMP 3.4.4.2.1, for Extended Session Security with SEAL:
// 1. Encrypt message with RC4 (advances RC4 state)
// 2. Compute HMAC-MD5 over SeqNum || PLAINTEXT Message (NOT encrypted!)
// 3. Encrypt first 8 bytes of HMAC with RC4 (using state after message encryption)
func (a *NTLMAuth) ntlmSign(message []byte, seqNum uint32) []byte {
	// Derive signing key: MD5(SessionKey || "session key to server-to-client signing key magic constant\0")
	signKeyInput := append([]byte(nil), a.sessionKey...)
	signKeyInput = append(signKeyInput, []byte("session key to server-to-client signing key magic constant\x00")...)
	signKeyHash := md5.Sum(signKeyInput)
	signKey := signKeyHash[:]

	// Derive sealing key: MD5(SessionKey || "session key to server-to-client sealing key magic constant\0")
	sealKeyInput := append([]byte(nil), a.sessionKey...)
	sealKeyInput = append(sealKeyInput, []byte("session key to server-to-client sealing key magic constant\x00")...)
	sealKeyHash := md5.Sum(sealKeyInput)
	sealKey := sealKeyHash[:]

	a.debugLog("PubKeyAuth: signKey=% 02X sealKey=% 02X", signKey, sealKey)

	// SeqNum in little-endian
	seqNumBytes := make([]byte, 4)
	seqNumBytes[0] = byte(seqNum)
	seqNumBytes[1] = byte(seqNum >> 8)
	seqNumBytes[2] = byte(seqNum >> 16)
	seqNumBytes[3] = byte(seqNum >> 24)

	// Step 1: Initialize RC4 with sealing key
	rc4Cipher, err := rc4.NewCipher(sealKey)
	if err != nil {
		a.debugLog("PubKeyAuth: RC4 cipher creation failed: %v", err)
		return message
	}

	// Step 2: Encrypt the message with RC4 (advances RC4 state)
	encryptedMessage := make([]byte, len(message))
	rc4Cipher.XORKeyStream(encryptedMessage, message)
	a.debugLog("PubKeyAuth: encryptedMessage=% 02X", encryptedMessage)

	// Step 3: Compute HMAC-MD5(SignKey, SeqNum || PLAINTEXT Message)
	// Per MS-NLMP 3.4.4.1.2 MAC function: Checksum = HMAC_MD5(SigningKey, SeqNum || Message)
	// The Message here is the PLAINTEXT, not encrypted!
	h := hmac.New(md5.New, signKey)
	h.Write(seqNumBytes)
	h.Write(message) // PLAINTEXT message, not encrypted!
	hmacFull := h.Sum(nil)
	a.debugLog("PubKeyAuth: hmacFull=% 02X", hmacFull)

	// Step 4: Take first 8 bytes of HMAC and encrypt with RC4 (using state after message encryption)
	checksum := hmacFull[:8]
	encryptedChecksum := make([]byte, 8)
	rc4Cipher.XORKeyStream(encryptedChecksum, checksum)

	// Build result: Version (4) || EncryptedChecksum (8) || SeqNum (4) || EncryptedMessage
	result := make([]byte, 16+len(encryptedMessage))
	// Version = 0x00000001 (NTLMSSP_MESSAGE_SIGNATURE)
	result[0] = 0x01
	result[1] = 0x00
	result[2] = 0x00
	result[3] = 0x00
	// Encrypted checksum (8 bytes)
	copy(result[4:12], encryptedChecksum)
	// SeqNum (4 bytes)
	copy(result[12:16], seqNumBytes)
	// Encrypted message
	copy(result[16:], encryptedMessage)

	a.debugLog("PubKeyAuth: NTLM sealed result len=%d sig=% 02X", len(result), result[:16])

	return result
}

// extractPublicKeyFromSPKI extracts the RSA public key from SubjectPublicKeyInfo.
// SubjectPublicKeyInfo structure:
//
//	SEQUENCE {
//	    AlgorithmIdentifier SEQUENCE {
//	        algorithm OID (rsaEncryption: 1.2.840.113549.1.1.1)
//	        parameters ANY (typically NULL)
//	    }
//	    subjectPublicKey BIT STRING {
//	        RSAPublicKey SEQUENCE {
//	            modulus INTEGER
//	            publicExponent INTEGER
//	        }
//	    }
//	}
//
// FreeRDP uses i2d_PublicKey() which extracts just the RSAPublicKey portion,
// not the full SubjectPublicKeyInfo. We need to match that format.
func extractPublicKeyFromSPKI(spki []byte) []byte {
	if len(spki) < 20 {
		return nil
	}

	// Parse outer SEQUENCE
	if spki[0] != 0x30 {
		return nil // Not a SEQUENCE
	}

	offset := 1
	outerLen, lenBytes := parseASN1Length(spki[offset:])
	if outerLen == 0 {
		return nil
	}
	offset += lenBytes

	// Now we're at the contents of the outer SEQUENCE
	// First element: AlgorithmIdentifier SEQUENCE
	if offset >= len(spki) || spki[offset] != 0x30 {
		return nil
	}

	algoIdStart := offset
	offset++ // skip SEQUENCE tag
	algoIdLen, algoIdLenBytes := parseASN1Length(spki[offset:])
	if algoIdLen == 0 {
		return nil
	}
	offset += algoIdLenBytes + algoIdLen // skip past AlgorithmIdentifier entirely
	_ = algoIdStart                      // unused

	// Now we should be at the BIT STRING containing the RSA public key
	if offset >= len(spki) || spki[offset] != 0x03 {
		return nil // Not a BIT STRING
	}
	offset++ // skip BIT STRING tag

	bitStringLen, bitStringLenBytes := parseASN1Length(spki[offset:])
	if bitStringLen == 0 {
		return nil
	}
	offset += bitStringLenBytes

	// First byte of BIT STRING content is the "unused bits" count (typically 0)
	if offset >= len(spki) {
		return nil
	}
	unusedBits := spki[offset]
	offset++

	if unusedBits != 0 {
		// Non-zero unused bits - unusual for RSA public keys
		return nil
	}

	// The rest is the RSA public key (starting with SEQUENCE tag 0x30)
	if offset >= len(spki) || spki[offset] != 0x30 {
		return nil
	}

	// Return from here to the end of the BIT STRING content
	remaining := bitStringLen - 1 // minus the unused bits byte
	if offset+remaining > len(spki) {
		remaining = len(spki) - offset
	}

	return spki[offset : offset+remaining]
}

// extractModulusFromSPKI extracts just the RSA modulus from SubjectPublicKeyInfo.
// This is in case some implementations use only the modulus for the hash.
// Reserved for potential compatibility with non-standard CredSSP implementations.
func extractModulusFromSPKI(spki []byte) []byte { //nolint:unused // Reserved for CredSSP compatibility
	// First extract the RSA public key
	rsaKey := extractPublicKeyFromSPKI(spki)
	if rsaKey == nil {
		return nil
	}

	// RSA public key structure:
	// SEQUENCE {
	//     modulus INTEGER,
	//     publicExponent INTEGER
	// }
	if len(rsaKey) < 10 || rsaKey[0] != 0x30 {
		return nil
	}

	offset := 1
	_, lenBytes := parseASN1Length(rsaKey[offset:])
	if lenBytes == 0 {
		return nil
	}
	offset += lenBytes

	// Now at first INTEGER (modulus)
	if offset >= len(rsaKey) || rsaKey[offset] != 0x02 {
		return nil
	}
	offset++

	modLen, modLenBytes := parseASN1Length(rsaKey[offset:])
	if modLen == 0 {
		return nil
	}
	offset += modLenBytes

	if offset+modLen > len(rsaKey) {
		return nil
	}

	return rsaKey[offset : offset+modLen]
}

// parseASN1Length parses a DER length field and returns (length, bytesConsumed).
// Returns (0, 0) on error.
func parseASN1Length(data []byte) (int, int) {
	if len(data) == 0 {
		return 0, 0
	}

	if data[0] < 0x80 {
		// Short form: single byte length
		return int(data[0]), 1
	}

	if data[0] == 0x80 {
		// Indefinite length - not supported
		return 0, 0
	}

	// Long form: first byte indicates number of length bytes
	numLenBytes := int(data[0] & 0x7f)
	if numLenBytes > 4 || len(data) < 1+numLenBytes {
		return 0, 0
	}

	length := 0
	for i := 0; i < numLenBytes; i++ {
		length = (length << 8) | int(data[1+i])
	}

	return length, 1 + numLenBytes
}

// incrementPublicKey adds 1 to the public key value (big-endian).
// MS-CSSP specifies this for server's pubKeyAuth response.
func incrementPublicKey(pubKey []byte) []byte {
	result := make([]byte, len(pubKey))
	copy(result, pubKey)

	// Add 1 to little-endian value
	carry := byte(1)
	for i := 0; i < len(result) && carry > 0; i++ {
		sum := uint16(result[i]) + uint16(carry)
		result[i] = byte(sum)
		carry = byte(sum >> 8)
	}

	return result
}
