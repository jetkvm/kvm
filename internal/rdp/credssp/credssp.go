// Package credssp implements the Credential Security Support Provider (CredSSP)
// protocol as defined in MS-CSSP. This is used for Network Level Authentication (NLA)
// in RDP connections.
//
// This implementation supports both password validation mode (when SetPassword is called)
// and permissive mode (when no password is set). In validation mode, NTLM credentials
// are verified against the configured password with proper NTSTATUS error responses.
package credssp

import (
	"bytes"
	"crypto/rand"
	"encoding/asn1"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

var (
	ErrInvalidTSRequest  = errors.New("credssp: invalid TSRequest")
	ErrInvalidNTLMMsg    = errors.New("credssp: invalid NTLM message")
	ErrAuthFailed        = errors.New("credssp: authentication failed")
	ErrUnexpectedMessage = errors.New("credssp: unexpected message")
)

// NTLM message type constants
const (
	NtlmNegotiate    = 1
	NtlmChallenge    = 2
	NtlmAuthenticate = 3
)

// NTLM signature
var ntlmSignature = []byte("NTLMSSP\x00")

// SPNEGO OIDs
var oidNTLM = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 311, 2, 2, 10}

// TLSConn is the interface required for a TLS connection used by CredSSP.
// This allows CredSSP to work with any TLS implementation (Go's crypto/tls or OpenSSL).
// The server MUST set the server public key via SetServerPublicKey() before calling
// Authenticate(), as CredSSP needs this for pubKeyAuth channel binding.
type TLSConn interface {
	net.Conn
	// Note: SetReadDeadline and SetWriteDeadline are part of net.Conn
}

// Handler manages CredSSP authentication for an RDP connection.
type Handler struct {
	tlsConn          TLSConn
	serverChallenge  []byte
	clientVersion    int    // CredSSP version from client
	clientNonce      []byte // For CredSSP v3+ (CVE-2018-0886 fix)
	clientUsesSPNEGO bool   // True if client wraps NTLM in SPNEGO

	// Authentication
	password        string    // Plaintext password for NTLM validation
	expectedUser    string    // Expected username (empty = accept any)
	expectedDomain  string    // Expected domain (empty = accept any)
	ntlmAuth        *NTLMAuth // NTLM authenticator (created after challenge sent)
	serverPublicKey []byte    // Server's RawSubjectPublicKeyInfo for pubKeyAuth (REQUIRED)
	serverCertDER   []byte    // Full certificate DER (for debugging different formats)

	// For logging
	debugLog func(string, ...interface{})
}

// NewHandler creates a new CredSSP handler.
// The tlsConn must be a TLS connection (any implementation).
// IMPORTANT: Caller MUST call SetServerPublicKey() before Authenticate().
func NewHandler(tlsConn TLSConn) *Handler {
	return &Handler{
		tlsConn:  tlsConn,
		debugLog: func(string, ...interface{}) {}, // No-op by default
	}
}

// SetDebugLog sets a debug logging function.
func (h *Handler) SetDebugLog(fn func(string, ...interface{})) {
	h.debugLog = fn
}

// SetPassword sets the password for NTLM authentication validation.
// If not set, authentication is permissive (accepts any credentials).
func (h *Handler) SetPassword(password string) {
	h.password = password
}

// SetExpectedUsername sets the expected username for authentication validation.
// If not set, any username is accepted (only password is validated).
func (h *Handler) SetExpectedUsername(username string) {
	h.expectedUser = username
}

// SetExpectedDomain sets the expected domain for authentication validation.
// If not set, any domain is accepted.
func (h *Handler) SetExpectedDomain(domain string) {
	h.expectedDomain = domain
}

// SetServerPublicKey sets the server's public key for pubKeyAuth computation.
// This should be the RawSubjectPublicKeyInfo from the TLS certificate.
func (h *Handler) SetServerPublicKey(pubKey []byte) {
	h.serverPublicKey = pubKey
}

// SetServerCertificateDER sets the full certificate DER for debugging different formats.
func (h *Handler) SetServerCertificateDER(certDER []byte) {
	h.serverCertDER = certDER
}

// GetServerCertificateDER returns the full certificate DER.
func (h *Handler) GetServerCertificateDER() []byte {
	return h.serverCertDER
}

// Authenticate performs the CredSSP authentication exchange.
// Returns the username provided by the client (for logging), or error.
func (h *Handler) Authenticate() (username string, err error) {
	// Validate required fields
	if len(h.serverPublicKey) == 0 {
		return "", errors.New("CredSSP: serverPublicKey not set - call SetServerPublicKey() before Authenticate()")
	}

	// Step 1: Receive TSRequest with NTLM NEGOTIATE
	h.debugLog("CredSSP: waiting for NTLM NEGOTIATE")
	tsReq1, err := h.readTSRequest()
	if err != nil {
		return "", fmt.Errorf("read negotiate: %w", err)
	}

	// Log client's TSRequest version and save it
	h.clientVersion = tsReq1.Version
	h.debugLog("CredSSP: client TSRequest version=%d", h.clientVersion)

	// For CredSSP v3+, client may send a nonce
	if len(tsReq1.ClientNonce) > 0 {
		h.clientNonce = tsReq1.ClientNonce
		h.debugLog("CredSSP: client sent nonce (len=%d): % 02X", len(h.clientNonce), h.clientNonce)
	} else {
		h.debugLog("CredSSP: no client nonce in request")
	}

	// Debug: dump raw negoToken before unwrapping
	if len(tsReq1.NegoTokens) > 0 {
		rawToken := tsReq1.NegoTokens[0].Token
		h.debugLog("CredSSP: raw negoToken len=%d, first 64 bytes: % 02X", len(rawToken), rawToken[:min(64, len(rawToken))])
	}

	negoToken := h.extractNegoToken(tsReq1)
	if negoToken == nil || !h.isNTLMNegotiate(negoToken) {
		return "", ErrUnexpectedMessage
	}

	// Parse client's negotiate flags for debugging
	clientFlags := h.parseNTLMNegotiate(negoToken)
	h.debugLog("CredSSP: received NTLM NEGOTIATE, clientFlags=0x%08x, negoLen=%d", clientFlags, len(negoToken))
	// Dump the NTLM NEGOTIATE for debugging
	if len(negoToken) <= 128 {
		h.debugLog("CredSSP: NEGOTIATE hex: % 02X", negoToken)
	}

	// Step 2: Send TSRequest with NTLM CHALLENGE
	h.serverChallenge = make([]byte, 8)
	if _, err := rand.Read(h.serverChallenge); err != nil {
		return "", fmt.Errorf("generate challenge: %w", err)
	}

	challengeMsg := h.buildNTLMChallengeWithFlags(clientFlags)
	h.debugLog("CredSSP: built NTLM CHALLENGE, len=%d", len(challengeMsg))
	// Debug: dump FULL CHALLENGE message
	h.debugLog("CredSSP: CHALLENGE hex FULL: % 02X", challengeMsg)

	// Use client's version (or minimum version 3 for security)
	responseVersion := h.clientVersion
	if responseVersion < 3 {
		responseVersion = 3 // Minimum version 3 for CVE-2018-0886 support
	}
	tsResp := h.buildTSRequest(responseVersion, challengeMsg)
	h.debugLog("CredSSP: built TSRequest with version=%d, len=%d", responseVersion, len(tsResp))
	// Debug: dump first 48 bytes of TSRequest
	tsDebug := 48
	if len(tsResp) < tsDebug {
		tsDebug = len(tsResp)
	}
	h.debugLog("CredSSP: TSRequest hex (first %d bytes): % 02X", tsDebug, tsResp[:tsDebug])

	if err := h.writeTSRequest(tsResp); err != nil {
		return "", fmt.Errorf("write challenge: %w", err)
	}
	h.debugLog("CredSSP: sent NTLM CHALLENGE")

	// Step 3: Receive TSRequest with NTLM AUTHENTICATE
	h.debugLog("CredSSP: waiting for NTLM AUTHENTICATE")
	tsReq2, err := h.readTSRequest()
	if err != nil {
		return "", fmt.Errorf("read authenticate: %w", err)
	}

	// Log what the client sent in step 3
	h.debugLog("CredSSP: AUTHENTICATE TSRequest version=%d, hasNegoTokens=%v, hasPubKeyAuth=%v, hasClientNonce=%v",
		tsReq2.Version, len(tsReq2.NegoTokens) > 0, len(tsReq2.PubKeyAuth) > 0, len(tsReq2.ClientNonce) > 0)
	if len(tsReq2.PubKeyAuth) > 0 {
		h.debugLog("CredSSP: client pubKeyAuth len=%d, first 32 bytes: % 02X", len(tsReq2.PubKeyAuth), tsReq2.PubKeyAuth[:min(32, len(tsReq2.PubKeyAuth))])
	}
	if len(tsReq2.ClientNonce) > 0 {
		h.clientNonce = tsReq2.ClientNonce
		h.debugLog("CredSSP: client nonce in AUTHENTICATE: % 02X", tsReq2.ClientNonce)
	}

	authToken := h.extractNegoToken(tsReq2)
	if authToken == nil {
		return "", ErrUnexpectedMessage
	}

	// Create NTLM authenticator if password validation is enabled
	if h.password != "" {
		h.ntlmAuth = NewNTLMAuth(h.password, h.serverChallenge)
		h.ntlmAuth.SetDebugLog(h.debugLog)
		h.ntlmAuth.SetClientNonce(h.clientNonce)
	}

	var domain string
	if h.ntlmAuth != nil {
		// Parse AUTHENTICATE message and validate credentials
		var parseErr error
		username, domain, parseErr = h.ntlmAuth.ParseAuthenticateMessage(authToken)
		if parseErr != nil {
			return "", fmt.Errorf("parse NTLM AUTHENTICATE: %w", parseErr)
		}
		h.debugLog("CredSSP: received NTLM AUTHENTICATE from user=%s domain=%s", username, domain)

		// Validate NTLMv2 response
		if !h.ntlmAuth.ValidateResponse(username, domain) {
			h.debugLog("CredSSP: NTLM authentication FAILED for user=%s", username)
			h.sendErrorResponse(STATUS_LOGON_FAILURE)
			return username, fmt.Errorf("%w: NTLM password validation failed for user=%s", ErrAuthFailed, username)
		}
		h.debugLog("CredSSP: NTLM authentication SUCCEEDED for user=%s", username)

		// Validate username if expected username is configured
		// Handle UPN format (user@domain) by extracting just the username part
		if h.expectedUser != "" && !usernameMatches(username, h.expectedUser) {
			h.debugLog("CredSSP: username mismatch - expected=%s got=%s", h.expectedUser, username)
			h.sendErrorResponse(STATUS_LOGON_FAILURE)
			return username, fmt.Errorf("%w: username mismatch expected=%s got=%s", ErrAuthFailed, h.expectedUser, username)
		}

		// Validate domain if expected domain is configured
		// Handle UPN format (user@domain) where the NTLM domain field may be empty
		// but the domain is embedded in the username
		if h.expectedDomain != "" && !domainMatches(domain, username, h.expectedDomain) {
			h.debugLog("CredSSP: domain mismatch - expected=%s got=%s (username=%s)", h.expectedDomain, domain, username)
			h.sendErrorResponse(STATUS_LOGON_FAILURE)
			return username, fmt.Errorf("%w: domain mismatch expected=%s got=%s", ErrAuthFailed, h.expectedDomain, domain)
		}

		// Verify client's pubKeyAuth to debug hash mismatches
		if len(tsReq2.PubKeyAuth) > 0 && len(h.serverPublicKey) > 0 {
			h.debugLog("CredSSP: verifying client pubKeyAuth...")
			// Pass certificate DER for additional format testing
			if len(h.serverCertDER) > 0 {
				h.ntlmAuth.SetServerCertificateDER(h.serverCertDER)
			}
			h.ntlmAuth.VerifyClientPubKeyAuth(tsReq2.PubKeyAuth, h.serverPublicKey, h.clientVersion)
		}
	} else {
		// Permissive mode - just extract username
		username = h.extractUsername(authToken)
		h.debugLog("CredSSP: permissive mode - received NTLM AUTHENTICATE from user: %s", username)
	}

	// Step 4: Send final TSRequest with pubKeyAuth
	pubKeyAuth := h.buildPubKeyAuth()
	h.debugLog("CredSSP: building final TSRequest with pubKeyAuth len=%d", len(pubKeyAuth))
	finalResp := h.buildTSRequestFinal(pubKeyAuth)
	h.debugLog("CredSSP: final TSRequest len=%d, first 32 bytes: % 02X", len(finalResp), finalResp[:min(32, len(finalResp))])
	if err := h.writeTSRequest(finalResp); err != nil {
		return "", fmt.Errorf("write final: %w", err)
	}

	// Step 5: Receive final TSRequest with authInfo (encrypted credentials)
	// After the client validates our pubKeyAuth, it sends the final TSRequest
	// containing encrypted credentials in the authInfo field.
	h.debugLog("CredSSP: waiting for client credentials (authInfo)...")
	tsReq3, err := h.readTSRequest()
	if err != nil {
		return "", fmt.Errorf("read credentials: %w", err)
	}

	h.debugLog("CredSSP: received final TSRequest - version=%d, hasNegoTokens=%v, hasAuthInfo=%v, authInfoLen=%d",
		tsReq3.Version, len(tsReq3.NegoTokens) > 0, len(tsReq3.AuthInfo) > 0, len(tsReq3.AuthInfo))

	// The authInfo contains TSCredentials (encrypted with NTLM session key)
	// We don't need to decrypt it for permissive authentication,
	// but we should at least acknowledge we received it.
	if len(tsReq3.AuthInfo) > 0 {
		h.debugLog("CredSSP: received encrypted credentials, authInfo len=%d", len(tsReq3.AuthInfo))
	} else {
		h.debugLog("CredSSP: WARNING - no authInfo in final TSRequest")
	}

	h.debugLog("CredSSP: authentication complete")

	return username, nil
}

// TSRequest represents a CredSSP TSRequest message (simplified).
type tsRequest struct {
	Version     int         `asn1:"explicit,tag:0"`
	NegoTokens  []negoToken `asn1:"optional,explicit,tag:1"`
	AuthInfo    []byte      `asn1:"optional,explicit,tag:2"`
	PubKeyAuth  []byte      `asn1:"optional,explicit,tag:3"`
	ErrorCode   int64       `asn1:"optional,explicit,tag:4"` // NTSTATUS error code (MS-CSSP 2.2.1)
	ClientNonce []byte      `asn1:"optional,explicit,tag:5"` // Added in CredSSP v3 (CVE-2018-0886)
}

// NTSTATUS error codes for CredSSP (MS-ERREF 2.3.1)
// These are unsigned 32-bit values but we use int64 to avoid overflow on 32-bit systems
const (
	STATUS_LOGON_FAILURE int64 = 0xC000006D // Bad username or password
)

type negoToken struct {
	Token []byte `asn1:"explicit,tag:0"`
}

func (h *Handler) readTSRequest() (*tsRequest, error) {
	// Set read deadline
	if err := h.tlsConn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, err
	}

	h.debugLog("CredSSP: waiting to read TSRequest header...")

	// Read length-prefixed data (CredSSP uses raw ASN.1, no framing)
	// First, peek at the ASN.1 header to get length
	header := make([]byte, 6)
	n, err := io.ReadFull(h.tlsConn, header[:2])
	if err != nil {
		h.debugLog("CredSSP: read header failed after %d bytes: %v", n, err)
		return nil, err
	}
	h.debugLog("CredSSP: read header bytes: %02x %02x", header[0], header[1])

	// Parse ASN.1 length
	var length int
	var headerLen int
	if header[1] < 0x80 {
		length = int(header[1])
		headerLen = 2
	} else if header[1] == 0x81 {
		if _, err := io.ReadFull(h.tlsConn, header[2:3]); err != nil {
			return nil, err
		}
		length = int(header[2])
		headerLen = 3
	} else if header[1] == 0x82 {
		if _, err := io.ReadFull(h.tlsConn, header[2:4]); err != nil {
			return nil, err
		}
		length = int(header[2])<<8 | int(header[3])
		headerLen = 4
	} else {
		return nil, ErrInvalidTSRequest
	}

	// Read the full message
	data := make([]byte, headerLen+length)
	copy(data, header[:headerLen])
	if _, err := io.ReadFull(h.tlsConn, data[headerLen:]); err != nil {
		return nil, err
	}

	// Parse TSRequest
	var req tsRequest
	if _, err := asn1.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("unmarshal TSRequest: %w", err)
	}

	return &req, nil
}

func (h *Handler) writeTSRequest(data []byte) error {
	if err := h.tlsConn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	_, err := h.tlsConn.Write(data)
	return err
}

// sendErrorResponse sends a TSRequest with an NTSTATUS error code to the client.
// Per MS-CSSP 3.1.5, when authentication fails the server sends a TSRequest
// containing only the version and errorCode fields before closing the connection.
func (h *Handler) sendErrorResponse(errorCode int64) {
	// Build TSRequest with error code
	// Use client's version or minimum version 3
	version := h.clientVersion
	if version < 3 {
		version = 3
	}

	errReq := tsRequest{
		Version:   version,
		ErrorCode: errorCode,
	}

	data, err := asn1.Marshal(errReq)
	if err != nil {
		h.debugLog("CredSSP: failed to marshal error response: %v", err)
		return
	}

	if err := h.writeTSRequest(data); err != nil {
		h.debugLog("CredSSP: failed to send error response: %v", err)
	} else {
		h.debugLog("CredSSP: sent error response with NTSTATUS 0x%08X", errorCode)
	}
}

func (h *Handler) extractNegoToken(req *tsRequest) []byte {
	if len(req.NegoTokens) == 0 {
		return nil
	}
	token := req.NegoTokens[0].Token

	// Detect if client uses SPNEGO wrapping or sends raw NTLM
	// SPNEGO NegTokenInit starts with 0xA0 (context tag) or 0x60 (APPLICATION)
	// Raw NTLM starts with "NTLMSSP\0" signature
	if len(token) > 0 {
		if bytes.HasPrefix(token, ntlmSignature) {
			// Client sends raw NTLM without SPNEGO
			h.clientUsesSPNEGO = false
			h.debugLog("CredSSP: client sends RAW NTLM (no SPNEGO)")
			return token
		} else if token[0] == 0xA0 || token[0] == 0x60 {
			// Client sends SPNEGO-wrapped NTLM
			h.clientUsesSPNEGO = true
			h.debugLog("CredSSP: client sends SPNEGO-wrapped NTLM")
			return h.unwrapSPNEGO(token)
		}
	}

	// Fallback: try to unwrap SPNEGO
	return h.unwrapSPNEGO(token)
}

func (h *Handler) unwrapSPNEGO(data []byte) []byte {
	// SPNEGO wraps NTLM in a NegTokenInit or NegTokenResp
	// For simplicity, look for NTLMSSP signature
	idx := bytes.Index(data, ntlmSignature)
	if idx >= 0 {
		return data[idx:]
	}
	return data
}

func (h *Handler) isNTLMNegotiate(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	return bytes.HasPrefix(data, ntlmSignature) && data[8] == NtlmNegotiate
}

// parseNTLMNegotiate extracts flags from NTLM NEGOTIATE message
func (h *Handler) parseNTLMNegotiate(data []byte) uint32 {
	if len(data) < 16 {
		return 0
	}
	// Flags are at offset 12-15 (little endian)
	return uint32(data[12]) | uint32(data[13])<<8 | uint32(data[14])<<16 | uint32(data[15])<<24
}

func (h *Handler) extractUsername(authMsg []byte) string {
	// NTLM AUTHENTICATE message structure:
	// 0-7: Signature "NTLMSSP\0"
	// 8-11: MessageType (3)
	// 12-19: LmChallengeResponseFields
	// 20-27: NtChallengeResponseFields
	// 28-35: DomainNameFields
	// 36-43: UserNameFields (Len, MaxLen, Offset)
	// ...
	if len(authMsg) < 44 || !bytes.HasPrefix(authMsg, ntlmSignature) {
		return ""
	}

	// UserNameFields at offset 36
	userLen := int(authMsg[36]) | int(authMsg[37])<<8
	userOffset := int(authMsg[40]) | int(authMsg[41])<<8 | int(authMsg[42])<<16 | int(authMsg[43])<<24

	if userOffset+userLen > len(authMsg) {
		return ""
	}

	// Username is typically UTF-16LE encoded
	usernameBytes := authMsg[userOffset : userOffset+userLen]
	return decodeUTF16LE(usernameBytes)
}

func decodeUTF16LE(data []byte) string {
	if len(data)%2 != 0 {
		return ""
	}
	runes := make([]rune, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		runes[i/2] = rune(data[i]) | rune(data[i+1])<<8
	}
	return string(runes)
}

// equalFoldASCII compares two strings case-insensitively (ASCII only).
// Used for username comparison since NTLM usernames are case-insensitive.
func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// usernameMatches checks if the provided username matches the expected username.
// It handles UPN format (user@domain) by extracting just the username part.
// For example, "admin@jetkvm" matches expected "admin".
func usernameMatches(provided, expected string) bool {
	// Direct match (case-insensitive)
	if equalFoldASCII(provided, expected) {
		return true
	}

	// Check if provided is in UPN format (user@domain)
	// Extract just the user part and compare
	for i := 0; i < len(provided); i++ {
		if provided[i] == '@' {
			userPart := provided[:i]
			if equalFoldASCII(userPart, expected) {
				return true
			}
			break
		}
	}

	// Check if provided is in DOMAIN\user format
	// Extract just the user part and compare
	for i := 0; i < len(provided); i++ {
		if provided[i] == '\\' {
			userPart := provided[i+1:]
			if equalFoldASCII(userPart, expected) {
				return true
			}
			break
		}
	}

	return false
}

// domainMatches checks if the provided domain matches the expected domain.
// It handles UPN format where the NTLM domain field may be empty but the domain
// is embedded in the username (user@domain).
// For example, domain="" with username="admin@jetkvm" matches expected "jetkvm".
func domainMatches(providedDomain, username, expectedDomain string) bool {
	// Direct match (case-insensitive)
	if equalFoldASCII(providedDomain, expectedDomain) {
		return true
	}

	// If provided domain is empty, check if username contains the domain (UPN format)
	if providedDomain == "" {
		// Extract domain from UPN format (user@domain)
		for i := 0; i < len(username); i++ {
			if username[i] == '@' {
				upnDomain := username[i+1:]
				if equalFoldASCII(upnDomain, expectedDomain) {
					return true
				}
				break
			}
		}
	}

	// Check if provided domain is in DOMAIN\user format prefix
	// (Some clients send "DOMAIN" in the domain field for DOMAIN\user logins)
	for i := 0; i < len(username); i++ {
		if username[i] == '\\' {
			domainPart := username[:i]
			if equalFoldASCII(domainPart, expectedDomain) {
				return true
			}
			break
		}
	}

	return false
}

func (h *Handler) buildTSRequest(version int, ntlmToken []byte) []byte {
	var responseToken []byte

	if h.clientUsesSPNEGO {
		// Client uses SPNEGO, wrap our NTLM response in SPNEGO NegTokenResp
		h.debugLog("CredSSP: wrapping NTLM token (len=%d) in SPNEGO", len(ntlmToken))
		responseToken = h.wrapInSPNEGOResp(ntlmToken)
		h.debugLog("CredSSP: SPNEGO token len=%d, first 48 bytes: % 02X", len(responseToken), responseToken[:min(48, len(responseToken))])
	} else {
		// Client sends raw NTLM, respond with raw NTLM (no SPNEGO wrapping)
		h.debugLog("CredSSP: sending RAW NTLM token (len=%d), NO SPNEGO wrapping", len(ntlmToken))
		responseToken = ntlmToken
		h.debugLog("CredSSP: raw NTLM token first 48 bytes: % 02X", ntlmToken[:min(48, len(ntlmToken))])
	}

	// Build TSRequest
	req := tsRequest{
		Version: version,
		NegoTokens: []negoToken{
			{Token: responseToken},
		},
	}

	data, _ := asn1.Marshal(req)
	return data
}

// writeASN1Length writes BER length encoding (handles lengths > 127)
func writeASN1Length(buf *bytes.Buffer, length int) {
	if length < 128 {
		buf.WriteByte(byte(length))
	} else if length < 256 {
		buf.WriteByte(0x81)
		buf.WriteByte(byte(length))
	} else {
		buf.WriteByte(0x82)
		buf.WriteByte(byte(length >> 8))
		buf.WriteByte(byte(length))
	}
}

// negTokenResp represents SPNEGO NegTokenResp for proper ASN.1 marshaling
type negTokenResp struct {
	NegState      asn1.Enumerated       `asn1:"explicit,tag:0,optional"`
	SupportedMech asn1.ObjectIdentifier `asn1:"explicit,tag:1,optional"`
	ResponseToken []byte                `asn1:"explicit,tag:2,optional"`
}

func (h *Handler) wrapInSPNEGOResp(ntlmToken []byte) []byte {
	// Build NegTokenResp structure
	resp := negTokenResp{
		NegState:      1, // accept-incomplete
		SupportedMech: oidNTLM,
		ResponseToken: ntlmToken,
	}

	// Marshal the NegTokenResp SEQUENCE
	seqBytes, err := asn1.Marshal(resp)
	if err != nil {
		h.debugLog("CredSSP: failed to marshal NegTokenResp: %v", err)
		return nil
	}

	// Wrap in NegTokenResp context tag [1] for NegotiationToken CHOICE
	var result bytes.Buffer
	result.WriteByte(0xa1) // [1] context tag
	writeASN1Length(&result, len(seqBytes))
	result.Write(seqBytes)

	return result.Bytes()
}

func (h *Handler) buildPubKeyAuth() []byte {
	// Build pubKeyAuth for the final TSRequest.
	// This binds the authentication to the TLS channel using the server's public key.
	//
	// IMPORTANT: The server public key MUST be set via SetServerPublicKey() before
	// calling Authenticate(). This allows CredSSP to work with any TLS implementation
	// (Go's crypto/tls or OpenSSL) since it doesn't need TLS-specific APIs.

	serverPubKey := h.serverPublicKey

	if len(serverPubKey) == 0 {
		h.debugLog("CredSSP: WARNING - no server public key set, pubKeyAuth will fail")
		h.debugLog("CredSSP: caller must use SetServerPublicKey() before Authenticate()")
	} else {
		h.debugLog("CredSSP: using server public key len=%d", len(serverPubKey))
	}

	// If we have a valid NTLM authenticator with session key, compute proper pubKeyAuth
	if h.ntlmAuth != nil {
		sessionKey := h.ntlmAuth.GetSessionKey()
		if len(sessionKey) > 0 {
			if len(serverPubKey) == 0 {
				h.debugLog("CredSSP: computing pubKeyAuth with empty public key (will likely fail)")
				serverPubKey = []byte{}
			}
			pubKeyAuth := h.ntlmAuth.ComputePubKeyAuth(serverPubKey, h.clientVersion)
			h.debugLog("CredSSP: computed pubKeyAuth len=%d", len(pubKeyAuth))
			return pubKeyAuth
		}
	}

	// Permissive mode fallback - return minimal value
	// Note: This will likely cause authentication to fail with proper clients
	h.debugLog("CredSSP: permissive mode - returning minimal pubKeyAuth")
	return []byte{0x01}
}

func (h *Handler) buildTSRequestFinal(pubKeyAuth []byte) []byte {
	// Use the same version we negotiated with the client
	version := h.clientVersion
	if version < 3 {
		version = 3
	}
	req := tsRequest{
		Version:    version,
		PubKeyAuth: pubKeyAuth,
	}

	data, _ := asn1.Marshal(req)
	return data
}
