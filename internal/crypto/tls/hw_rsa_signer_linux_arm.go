//go:build cgo && linux && arm

// OpenSSL-backed RSA signer for improved TLS performance on ARM.
// This delegates RSA signing operations to OpenSSL, which has optimized
// assembly implementations that significantly outperform Go's pure-Go
// crypto/rsa on ARM processors.
//
// Note: This uses OpenSSL's software RSA implementation. The devcrypto
// engine only accelerates symmetric operations (AES), not asymmetric (RSA).

package tls

/*
// Note: LDFLAGS for OpenSSL are provided by the build system (Makefile)
// which handles static linking. Do not add #cgo LDFLAGS here.
#define OPENSSL_SUPPRESS_DEPRECATED

#include <openssl/ssl.h>
#include <openssl/err.h>
#include <openssl/pem.h>
#include <openssl/evp.h>
#include <openssl/rsa.h>
#include <stdlib.h>
#include <string.h>

// Parse PEM key and return EVP_PKEY pointer. Caller must free with EVP_PKEY_free.
static EVP_PKEY* openssl_parse_rsa_key(
    const unsigned char *key_pem, int key_pem_len,
    char *error_buf, int error_buf_len
) {
    ERR_clear_error();

    BIO *bio = BIO_new_mem_buf(key_pem, key_pem_len);
    if (!bio) {
        snprintf(error_buf, error_buf_len, "BIO_new_mem_buf failed");
        return NULL;
    }

    EVP_PKEY *pkey = PEM_read_bio_PrivateKey(bio, NULL, NULL, NULL);
    BIO_free(bio);

    if (!pkey) {
        unsigned long err = ERR_get_error();
        ERR_error_string_n(err, error_buf, error_buf_len);
        return NULL;
    }

    if (EVP_PKEY_base_id(pkey) != EVP_PKEY_RSA) {
        snprintf(error_buf, error_buf_len, "Key is not RSA");
        EVP_PKEY_free(pkey);
        return NULL;
    }

    return pkey;
}

// Free EVP_PKEY (called from Go finalizer).
static void openssl_free_pkey(EVP_PKEY *pkey) {
    if (pkey) {
        EVP_PKEY_free(pkey);
    }
}

// Get RSA key size in bits from cached EVP_PKEY.
static int openssl_pkey_bits(EVP_PKEY *pkey) {
    return EVP_PKEY_bits(pkey);
}

// Sign a pre-computed digest using cached EVP_PKEY.
// Set use_pss=1 for RSA-PSS (TLS 1.3), use_pss=0 for PKCS#1 v1.5 (TLS 1.2).
// Returns signature length on success, -1 on error.
static int openssl_rsa_sign_cached(
    EVP_PKEY *pkey,
    const unsigned char *digest, int digest_len,
    int hash_nid, int use_pss,
    unsigned char *sig_out, int sig_out_len,
    char *error_buf, int error_buf_len
) {
    EVP_PKEY_CTX *ctx = NULL;
    const EVP_MD *md = NULL;
    size_t sig_len = 0;
    int ret = -1;

    ERR_clear_error();

    md = EVP_get_digestbynid(hash_nid);
    if (!md) {
        snprintf(error_buf, error_buf_len, "Unknown hash NID: %d", hash_nid);
        goto cleanup;
    }

    ctx = EVP_PKEY_CTX_new(pkey, NULL);
    if (!ctx) {
        snprintf(error_buf, error_buf_len, "EVP_PKEY_CTX_new failed");
        goto cleanup;
    }

    if (EVP_PKEY_sign_init(ctx) <= 0) {
        unsigned long err = ERR_get_error();
        ERR_error_string_n(err, error_buf, error_buf_len);
        goto cleanup;
    }

    int padding = use_pss ? RSA_PKCS1_PSS_PADDING : RSA_PKCS1_PADDING;
    if (EVP_PKEY_CTX_set_rsa_padding(ctx, padding) <= 0) {
        unsigned long err = ERR_get_error();
        ERR_error_string_n(err, error_buf, error_buf_len);
        goto cleanup;
    }

    if (EVP_PKEY_CTX_set_signature_md(ctx, md) <= 0) {
        unsigned long err = ERR_get_error();
        ERR_error_string_n(err, error_buf, error_buf_len);
        goto cleanup;
    }

    // Salt length = digest length per RFC 8446 Section 4.2.3 (TLS 1.3)
    if (use_pss && EVP_PKEY_CTX_set_rsa_pss_saltlen(ctx, RSA_PSS_SALTLEN_DIGEST) <= 0) {
        unsigned long err = ERR_get_error();
        ERR_error_string_n(err, error_buf, error_buf_len);
        goto cleanup;
    }

    sig_len = sig_out_len;
    if (EVP_PKEY_sign(ctx, sig_out, &sig_len, digest, digest_len) <= 0) {
        unsigned long err = ERR_get_error();
        ERR_error_string_n(err, error_buf, error_buf_len);
        goto cleanup;
    }

    ret = (int)sig_len;

cleanup:
    if (ctx) EVP_PKEY_CTX_free(ctx);
    return ret;
}
*/
import "C"

import (
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"runtime"
	"sync"
	"unsafe"

	"github.com/jetkvm/kvm/internal/logging"
)

var rsaSignerLogger = logging.GetSubsystemLogger("crypto.tls")

// OpenSSL NID values for hash algorithms.
// These are stable across OpenSSL versions.
const (
	nidSHA1   = 64  // NID_sha1
	nidSHA256 = 672 // NID_sha256
	nidSHA384 = 673 // NID_sha384
	nidSHA512 = 674 // NID_sha512
)

// Expected digest sizes for validation.
var hashSizes = map[crypto.Hash]int{
	crypto.SHA1:   20,
	crypto.SHA256: 32,
	crypto.SHA384: 48,
	crypto.SHA512: 64,
}

// OpenSSLRSASigner implements crypto.Signer using OpenSSL for RSA operations.
// This provides better performance than Go's crypto/rsa on ARM processors.
// Thread-safe: uses mutex to protect OpenSSL operations.
type OpenSSLRSASigner struct {
	pkey       *C.EVP_PKEY     // Cached OpenSSL key (parsed once at creation)
	publicKey  *rsa.PublicKey  // Go public key for Public() method
	privateKey *rsa.PrivateKey // Original key for serialization (e.g., RDP needs PKCS#8)
	keyBits    int
	mu         sync.Mutex // Protects OpenSSL operations
}

// NewOpenSSLRSASigner creates an RSA signer backed by OpenSSL.
// The key is parsed once and cached for the lifetime of the signer.
func NewOpenSSLRSASigner(keyPEM []byte) (*OpenSSLRSASigner, error) {
	if len(keyPEM) == 0 {
		return nil, fmt.Errorf("empty key PEM data")
	}

	// Parse with Go to extract public key and validate
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	var privateKey *rsa.PrivateKey
	var err error

	switch block.Type {
	case "RSA PRIVATE KEY":
		privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return nil, fmt.Errorf("failed to parse PKCS8 key: %w", err2)
		}
		var ok bool
		privateKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("key is not RSA")
		}
	default:
		return nil, fmt.Errorf("unsupported key type: %s", block.Type)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to parse RSA key: %w", err)
	}

	// Parse with OpenSSL and cache the EVP_PKEY
	errBuf := make([]byte, 256)
	var pinner runtime.Pinner
	pinner.Pin(&keyPEM[0])
	pinner.Pin(&errBuf[0])

	pkey := C.openssl_parse_rsa_key(
		(*C.uchar)(unsafe.Pointer(&keyPEM[0])),
		C.int(len(keyPEM)),
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.int(len(errBuf)),
	)

	pinner.Unpin()

	if pkey == nil {
		errStr := C.GoString((*C.char)(unsafe.Pointer(&errBuf[0])))
		return nil, fmt.Errorf("OpenSSL failed to parse RSA key: %s", errStr)
	}

	keyBits := int(C.openssl_pkey_bits(pkey))

	signer := &OpenSSLRSASigner{
		pkey:       pkey,
		publicKey:  &privateKey.PublicKey,
		privateKey: privateKey,
		keyBits:    keyBits,
	}

	// Set finalizer to free OpenSSL resources when signer is garbage collected
	runtime.SetFinalizer(signer, func(s *OpenSSLRSASigner) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.pkey != nil {
			C.openssl_free_pkey(s.pkey)
			s.pkey = nil
		}
	})

	return signer, nil
}

// RSAPrivateKey returns the underlying RSA private key.
// This is needed for serialization (e.g., RDP needs to encode keys to PKCS#8).
func (s *OpenSSLRSASigner) RSAPrivateKey() *rsa.PrivateKey {
	return s.privateKey
}

// Public returns the public key.
func (s *OpenSSLRSASigner) Public() crypto.PublicKey {
	return s.publicKey
}

// Sign signs digest with the private key using OpenSSL.
// Thread-safe: uses mutex to protect OpenSSL operations.
func (s *OpenSSLRSASigner) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if len(digest) == 0 {
		return nil, fmt.Errorf("empty digest")
	}

	hash := opts.HashFunc()
	if hash == 0 {
		return nil, fmt.Errorf("hash function not specified")
	}

	// Validate digest length
	expectedLen, ok := hashSizes[hash]
	if ok && len(digest) != expectedLen {
		return nil, fmt.Errorf("invalid digest length: expected %d, got %d", expectedLen, len(digest))
	}

	hashNID, err := hashToNID(hash)
	if err != nil {
		return nil, err
	}

	sigBuf := make([]byte, (s.keyBits+7)/8)
	errBuf := make([]byte, 256)

	_, usePSS := opts.(*rsa.PSSOptions)

	// Pin Go memory before passing to C
	var pinner runtime.Pinner
	pinner.Pin(&digest[0])
	pinner.Pin(&sigBuf[0])
	pinner.Pin(&errBuf[0])

	s.mu.Lock()
	sigLen := C.openssl_rsa_sign_cached(
		s.pkey,
		(*C.uchar)(unsafe.Pointer(&digest[0])),
		C.int(len(digest)),
		C.int(hashNID),
		C.int(boolToInt(usePSS)),
		(*C.uchar)(unsafe.Pointer(&sigBuf[0])),
		C.int(len(sigBuf)),
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.int(len(errBuf)),
	)
	s.mu.Unlock()

	pinner.Unpin()

	if sigLen < 0 {
		errStr := C.GoString((*C.char)(unsafe.Pointer(&errBuf[0])))
		return nil, fmt.Errorf("OpenSSL RSA sign failed: %s", errStr)
	}

	return sigBuf[:sigLen], nil
}

func hashToNID(h crypto.Hash) (int, error) {
	switch h {
	case crypto.SHA256:
		return nidSHA256, nil
	case crypto.SHA384:
		return nidSHA384, nil
	case crypto.SHA512:
		return nidSHA512, nil
	case crypto.SHA1:
		return nidSHA1, nil
	default:
		return 0, fmt.Errorf("unsupported hash function: %v", h)
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// hardwareRSAMode controls which RSA signing backend to use.
// Options: "openssl" (OpenSSL optimized assembly), "disabled" (Go crypto)
var hardwareRSAMode = "openssl"

// SetHardwareRSAMode sets the RSA acceleration mode.
func SetHardwareRSAMode(mode string) {
	hardwareRSAMode = mode
	rsaSignerLogger.Debug().Str("mode", mode).Msg("hardware RSA mode configured")
}

// GetHardwareRSAMode returns the current RSA acceleration mode.
func GetHardwareRSAMode() string {
	return hardwareRSAMode
}

// GetSignerName returns a human-readable name for the signer backend.
func GetSignerName(key any) string {
	if _, ok := key.(*OpenSSLRSASigner); ok {
		return "OpenSSL"
	}
	return "Go crypto"
}

// WrapRSAKey wraps an RSA private key with an OpenSSL-accelerated signer.
// When mode is "disabled", returns the original key unchanged.
func WrapRSAKey(key crypto.PrivateKey) (crypto.PrivateKey, error) {
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return key, nil
	}

	if hardwareRSAMode == "disabled" {
		return key, nil
	}

	keyDER := x509.MarshalPKCS1PrivateKey(rsaKey)
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: keyDER,
	})

	signer, err := NewOpenSSLRSASigner(keyPEM)
	if err != nil {
		return key, fmt.Errorf("OpenSSL RSA acceleration unavailable: %w", err)
	}

	rsaSignerLogger.Debug().
		Int("keyBits", rsaKey.N.BitLen()).
		Msg("using OpenSSL RSA acceleration")
	return signer, nil
}
