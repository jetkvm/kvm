// Package credssp implements the Credential Security Support Provider (CredSSP)
// protocol as defined in MS-CSSP. This is used for Network Level Authentication (NLA)
// in RDP connections.
//
// This implementation is "permissive" - it accepts any credentials without validation.
// This allows RDP connections to proceed through NLA negotiation.
package credssp

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/asn1"
	"errors"
	"fmt"
	"io"
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

// Handler manages CredSSP authentication for an RDP connection.
type Handler struct {
	tlsConn          *tls.Conn
	serverChallenge  []byte
	clientVersion    int    // CredSSP version from client
	clientNonce      []byte // For CredSSP v3+ (CVE-2018-0886 fix)
	clientUsesSPNEGO bool   // True if client wraps NTLM in SPNEGO

	// For logging
	debugLog func(string, ...interface{})
}

// NewHandler creates a new CredSSP handler.
func NewHandler(tlsConn *tls.Conn) *Handler {
	return &Handler{
		tlsConn:  tlsConn,
		debugLog: func(string, ...interface{}) {}, // No-op by default
	}
}

// SetDebugLog sets a debug logging function.
func (h *Handler) SetDebugLog(fn func(string, ...interface{})) {
	h.debugLog = fn
}

// Authenticate performs the CredSSP authentication exchange.
// Returns the username provided by the client (for logging), or error.
func (h *Handler) Authenticate() (username string, err error) {
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

	username = h.extractUsername(authToken)
	h.debugLog("CredSSP: received NTLM AUTHENTICATE from user: %s", username)

	// Step 4: Send final TSRequest (accept authentication)
	// We're permissive - accept any credentials
	// For pubKeyAuth, we need to send something the client will accept
	pubKeyAuth := h.buildPubKeyAuth()
	h.debugLog("CredSSP: building final TSRequest with pubKeyAuth len=%d", len(pubKeyAuth))
	finalResp := h.buildTSRequestFinal(pubKeyAuth)
	h.debugLog("CredSSP: final TSRequest len=%d, first 32 bytes: % 02X", len(finalResp), finalResp[:min(32, len(finalResp))])
	if err := h.writeTSRequest(finalResp); err != nil {
		return "", fmt.Errorf("write final: %w", err)
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
	ClientNonce []byte      `asn1:"optional,explicit,tag:5"` // Added in CredSSP v3 (CVE-2018-0886)
}

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
	// Build pubKeyAuth for the final TSRequest
	// This binds the authentication to the TLS channel
	// For simplicity, we use a minimal value that clients accept

	// Get server's public key from TLS connection
	state := h.tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		// Use our own cert's public key
		certs := state.PeerCertificates
		if len(certs) == 0 {
			// Fallback: return empty (some clients accept this)
			return nil
		}
	}

	// For permissive mode, return minimal pubKeyAuth
	// Real implementation would compute proper channel binding
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
