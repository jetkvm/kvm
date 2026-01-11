// Package crypto provides hardware-accelerated cryptographic operations
// for JetKVM, including DTLS cipher suites that use RV1106 hardware crypto.
package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"sync/atomic"

	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/clientcertificate"
	"github.com/pion/dtls/v3/pkg/crypto/prf"
	"github.com/pion/dtls/v3/pkg/protocol"
	"github.com/pion/dtls/v3/pkg/protocol/recordlayer"
)

const (
	gcmTagLength   = 16
	gcmNonceLength = 12
)

var errCipherSuiteNotInit = errors.New("cipher suite not initialized")

// HardwareGCM provides AES-GCM using hardware acceleration.
type HardwareGCM struct {
	localAEAD, remoteAEAD       AEAD
	localWriteIV, remoteWriteIV []byte
}

// minIVLength is the minimum IV length required for GCM nonce construction.
// The first 4 bytes of the IV are used as the implicit nonce; the remaining
// 8 bytes come from the DTLS record explicit nonce.
const minIVLength = 4

// NewHardwareGCM creates a DTLS GCM cipher using hardware crypto.
// Returns error if IV lengths are less than minIVLength (4 bytes).
func NewHardwareGCM(localKey, localWriteIV, remoteKey, remoteWriteIV []byte) (*HardwareGCM, error) {
	// Validate IV lengths before creating expensive AEAD sessions
	if len(localWriteIV) < minIVLength {
		return nil, fmt.Errorf("localWriteIV too short: got %d bytes, need at least %d", len(localWriteIV), minIVLength)
	}
	if len(remoteWriteIV) < minIVLength {
		return nil, fmt.Errorf("remoteWriteIV too short: got %d bytes, need at least %d", len(remoteWriteIV), minIVLength)
	}

	localAEAD, err := NewAESGCM(localKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create local AEAD: %w", err)
	}

	remoteAEAD, err := NewAESGCM(remoteKey)
	if err != nil {
		localAEAD.Close()
		return nil, fmt.Errorf("failed to create remote AEAD: %w", err)
	}

	return &HardwareGCM{
		localAEAD:     localAEAD,
		localWriteIV:  localWriteIV,
		remoteAEAD:    remoteAEAD,
		remoteWriteIV: remoteWriteIV,
	}, nil
}

// Close releases hardware resources.
func (g *HardwareGCM) Close() error {
	var errs []error
	if g.localAEAD != nil {
		if err := g.localAEAD.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if g.remoteAEAD != nil {
		if err := g.remoteAEAD.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Encrypt encrypts a DTLS RecordLayer message.
func (g *HardwareGCM) Encrypt(pkt *recordlayer.RecordLayer, raw []byte) ([]byte, error) {
	payload := raw[pkt.Header.Size():]
	raw = raw[:pkt.Header.Size()]

	nonce := make([]byte, gcmNonceLength)
	copy(nonce, g.localWriteIV[:4])
	if _, err := rand.Read(nonce[4:]); err != nil {
		return nil, err
	}

	var additionalData []byte
	if pkt.Header.ContentType == protocol.ContentTypeConnectionID {
		additionalData = generateAEADAdditionalDataCID(&pkt.Header, len(payload))
	} else {
		additionalData = generateAEADAdditionalData(&pkt.Header, len(payload))
	}
	encryptedPayload := g.localAEAD.Seal(nil, nonce, payload, additionalData)
	r := make([]byte, len(raw)+len(nonce[4:])+len(encryptedPayload))
	copy(r, raw)
	copy(r[len(raw):], nonce[4:])
	copy(r[len(raw)+len(nonce[4:]):], encryptedPayload)

	// Update recordLayer size to include explicit nonce
	binary.BigEndian.PutUint16(r[pkt.Header.Size()-2:], uint16(len(r)-pkt.Header.Size()))

	return r, nil
}

// Decrypt decrypts a DTLS RecordLayer message.
func (g *HardwareGCM) Decrypt(header recordlayer.Header, in []byte) ([]byte, error) {
	err := header.Unmarshal(in)
	switch {
	case err != nil:
		return nil, err
	case header.ContentType == protocol.ContentTypeChangeCipherSpec:
		// Nothing to encrypt with ChangeCipherSpec
		return in, nil
	case len(in) <= (8 + header.Size()):
		return nil, errors.New("not enough room for nonce")
	}

	nonce := make([]byte, 0, gcmNonceLength)
	nonce = append(append(nonce, g.remoteWriteIV[:4]...), in[header.Size():header.Size()+8]...)
	out := in[header.Size()+8:]

	var additionalData []byte
	if header.ContentType == protocol.ContentTypeConnectionID {
		additionalData = generateAEADAdditionalDataCID(&header, len(out)-gcmTagLength)
	} else {
		additionalData = generateAEADAdditionalData(&header, len(out)-gcmTagLength)
	}
	out, err = g.remoteAEAD.Open(out[:0], nonce, out, additionalData)
	if err != nil {
		return nil, fmt.Errorf("decrypt packet: %w", err)
	}

	return append(in[:header.Size()], out...), nil
}

func generateAEADAdditionalData(h *recordlayer.Header, payloadLen int) []byte {
	var additionalData [13]byte

	// SequenceNumber MUST be set first
	// we only want uint48, clobbering an extra 2 (using uint64, Golang doesn't have uint48)
	binary.BigEndian.PutUint64(additionalData[:], h.SequenceNumber)
	binary.BigEndian.PutUint16(additionalData[:], h.Epoch)
	additionalData[8] = byte(h.ContentType)
	additionalData[9] = h.Version.Major
	additionalData[10] = h.Version.Minor
	binary.BigEndian.PutUint16(additionalData[11:], uint16(payloadLen))
	return additionalData[:]
}

func generateAEADAdditionalDataCID(h *recordlayer.Header, payloadLen int) []byte {
	// For Connection ID, the additional data format is different
	// See RFC 9146
	additionalData := make([]byte, 13+len(h.ConnectionID))

	// SequenceNumber MUST be set first
	binary.BigEndian.PutUint64(additionalData[:], h.SequenceNumber)
	binary.BigEndian.PutUint16(additionalData[:], h.Epoch)
	additionalData[8] = byte(h.ContentType)
	additionalData[9] = h.Version.Major
	additionalData[10] = h.Version.Minor
	binary.BigEndian.PutUint16(additionalData[11:], uint16(payloadLen))
	copy(additionalData[13:], h.ConnectionID)
	return additionalData
}

// TLSEcdheEcdsaWithAes128GcmSha256Hardware is a hardware-accelerated cipher suite.
type TLSEcdheEcdsaWithAes128GcmSha256Hardware struct {
	gcm atomic.Value // *HardwareGCM
}

// Ensure we implement the CipherSuite interface
var _ dtls.CipherSuite = (*TLSEcdheEcdsaWithAes128GcmSha256Hardware)(nil)

// CertificateType returns what type of certificate this CipherSuite exchanges.
func (c *TLSEcdheEcdsaWithAes128GcmSha256Hardware) CertificateType() clientcertificate.Type {
	return clientcertificate.ECDSASign
}

// KeyExchangeAlgorithm controls what key exchange algorithm is using during the handshake.
func (c *TLSEcdheEcdsaWithAes128GcmSha256Hardware) KeyExchangeAlgorithm() dtls.CipherSuiteKeyExchangeAlgorithm {
	return dtls.CipherSuiteKeyExchangeAlgorithmEcdhe
}

// ECC uses Elliptic Curve Cryptography.
func (c *TLSEcdheEcdsaWithAes128GcmSha256Hardware) ECC() bool {
	return true
}

// ID returns the ID of the CipherSuite.
func (c *TLSEcdheEcdsaWithAes128GcmSha256Hardware) ID() dtls.CipherSuiteID {
	return dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256
}

func (c *TLSEcdheEcdsaWithAes128GcmSha256Hardware) String() string {
	return "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256_HARDWARE"
}

// HashFunc returns the hashing func for this CipherSuite.
func (c *TLSEcdheEcdsaWithAes128GcmSha256Hardware) HashFunc() func() hash.Hash {
	return sha256.New
}

// AuthenticationType controls what authentication method is using during the handshake.
func (c *TLSEcdheEcdsaWithAes128GcmSha256Hardware) AuthenticationType() dtls.CipherSuiteAuthenticationType {
	return dtls.CipherSuiteAuthenticationTypeCertificate
}

// IsInitialized returns if the CipherSuite has keying material and can
// encrypt/decrypt packets.
func (c *TLSEcdheEcdsaWithAes128GcmSha256Hardware) IsInitialized() bool {
	return c.gcm.Load() != nil
}

// Deinit releases hardware crypto resources. Must be called when the DTLS
// connection is closed to prevent leaking /dev/crypto sessions.
func (c *TLSEcdheEcdsaWithAes128GcmSha256Hardware) Deinit() error {
	gcm, ok := c.gcm.Swap(nil).(*HardwareGCM)
	if !ok || gcm == nil {
		return nil
	}
	return gcm.Close()
}

// Init initializes the internal Cipher with keying material.
func (c *TLSEcdheEcdsaWithAes128GcmSha256Hardware) Init(masterSecret, clientRandom, serverRandom []byte, isClient bool) error {
	if c.IsInitialized() {
		return errors.New("cipher suite already initialized")
	}

	const (
		prfMacLen = 0
		prfKeyLen = 16
		prfIvLen  = 4
	)

	keys, err := prf.GenerateEncryptionKeys(
		masterSecret, clientRandom, serverRandom, prfMacLen, prfKeyLen, prfIvLen, c.HashFunc(),
	)
	if err != nil {
		return err
	}

	var gcm *HardwareGCM
	if isClient {
		gcm, err = NewHardwareGCM(keys.ClientWriteKey, keys.ClientWriteIV, keys.ServerWriteKey, keys.ServerWriteIV)
	} else {
		gcm, err = NewHardwareGCM(keys.ServerWriteKey, keys.ServerWriteIV, keys.ClientWriteKey, keys.ClientWriteIV)
	}
	if err != nil {
		return err
	}
	c.gcm.Store(gcm)

	return nil
}

// Encrypt encrypts a single TLS RecordLayer.
func (c *TLSEcdheEcdsaWithAes128GcmSha256Hardware) Encrypt(pkt *recordlayer.RecordLayer, raw []byte) ([]byte, error) {
	cipherSuite, ok := c.gcm.Load().(*HardwareGCM)
	if !ok {
		return nil, fmt.Errorf("%w, unable to encrypt", errCipherSuiteNotInit)
	}

	return cipherSuite.Encrypt(pkt, raw)
}

// Decrypt decrypts a single TLS RecordLayer.
func (c *TLSEcdheEcdsaWithAes128GcmSha256Hardware) Decrypt(h recordlayer.Header, raw []byte) ([]byte, error) {
	cipherSuite, ok := c.gcm.Load().(*HardwareGCM)
	if !ok {
		return nil, fmt.Errorf("%w, unable to decrypt", errCipherSuiteNotInit)
	}

	return cipherSuite.Decrypt(h, raw)
}

// HardwareCipherSuites returns cipher suites that use hardware crypto.
// Use this with dtls.Config.CustomCipherSuites.
func HardwareCipherSuites() []dtls.CipherSuite {
	return []dtls.CipherSuite{
		&TLSEcdheEcdsaWithAes128GcmSha256Hardware{},
	}
}
