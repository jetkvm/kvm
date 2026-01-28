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

// Sign a pre-computed digest using RSA-PSS.
// The digest has already been hashed by Go - we apply RSA-PSS padding and sign.
// Returns signature length on success, -1 on error.
static int openssl_rsa_sign_pss(
    const unsigned char *key_pem, int key_pem_len,
    const unsigned char *digest, int digest_len,
    int hash_nid,
    unsigned char *sig_out, int sig_out_len,
    char *error_buf, int error_buf_len
) {
    BIO *bio = NULL;
    EVP_PKEY *pkey = NULL;
    EVP_PKEY_CTX *ctx = NULL;
    const EVP_MD *md = NULL;
    size_t sig_len = 0;
    int ret = -1;

    // Clear any stale errors from previous operations
    ERR_clear_error();

    bio = BIO_new_mem_buf(key_pem, key_pem_len);
    if (!bio) {
        snprintf(error_buf, error_buf_len, "BIO_new_mem_buf failed");
        goto cleanup;
    }

    pkey = PEM_read_bio_PrivateKey(bio, NULL, NULL, NULL);
    if (!pkey) {
        unsigned long err = ERR_get_error();
        ERR_error_string_n(err, error_buf, error_buf_len);
        goto cleanup;
    }

    if (EVP_PKEY_base_id(pkey) != EVP_PKEY_RSA) {
        snprintf(error_buf, error_buf_len, "Key is not RSA");
        goto cleanup;
    }

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

    if (EVP_PKEY_CTX_set_rsa_padding(ctx, RSA_PKCS1_PSS_PADDING) <= 0) {
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
    if (EVP_PKEY_CTX_set_rsa_pss_saltlen(ctx, RSA_PSS_SALTLEN_DIGEST) <= 0) {
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
    if (pkey) EVP_PKEY_free(pkey);
    if (bio) BIO_free(bio);
    return ret;
}

// Sign a pre-computed digest using PKCS#1 v1.5.
// Returns signature length on success, -1 on error.
static int openssl_rsa_sign_pkcs1(
    const unsigned char *key_pem, int key_pem_len,
    const unsigned char *digest, int digest_len,
    int hash_nid,
    unsigned char *sig_out, int sig_out_len,
    char *error_buf, int error_buf_len
) {
    BIO *bio = NULL;
    EVP_PKEY *pkey = NULL;
    EVP_PKEY_CTX *ctx = NULL;
    const EVP_MD *md = NULL;
    size_t sig_len = 0;
    int ret = -1;

    ERR_clear_error();

    bio = BIO_new_mem_buf(key_pem, key_pem_len);
    if (!bio) {
        snprintf(error_buf, error_buf_len, "BIO_new_mem_buf failed");
        goto cleanup;
    }

    pkey = PEM_read_bio_PrivateKey(bio, NULL, NULL, NULL);
    if (!pkey) {
        unsigned long err = ERR_get_error();
        ERR_error_string_n(err, error_buf, error_buf_len);
        goto cleanup;
    }

    if (EVP_PKEY_base_id(pkey) != EVP_PKEY_RSA) {
        snprintf(error_buf, error_buf_len, "Key is not RSA");
        goto cleanup;
    }

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

    if (EVP_PKEY_CTX_set_rsa_padding(ctx, RSA_PKCS1_PADDING) <= 0) {
        unsigned long err = ERR_get_error();
        ERR_error_string_n(err, error_buf, error_buf_len);
        goto cleanup;
    }

    if (EVP_PKEY_CTX_set_signature_md(ctx, md) <= 0) {
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
    if (pkey) EVP_PKEY_free(pkey);
    if (bio) BIO_free(bio);
    return ret;
}

// Get RSA key size in bits. Returns -1 on error.
static int openssl_rsa_key_bits(
    const unsigned char *key_pem, int key_pem_len,
    char *error_buf, int error_buf_len
) {
    ERR_clear_error();

    BIO *bio = BIO_new_mem_buf(key_pem, key_pem_len);
    if (!bio) {
        snprintf(error_buf, error_buf_len, "BIO_new_mem_buf failed");
        return -1;
    }

    EVP_PKEY *pkey = PEM_read_bio_PrivateKey(bio, NULL, NULL, NULL);
    BIO_free(bio);
    if (!pkey) {
        unsigned long err = ERR_get_error();
        ERR_error_string_n(err, error_buf, error_buf_len);
        return -1;
    }

    int bits = EVP_PKEY_bits(pkey);
    EVP_PKEY_free(pkey);
    return bits;
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
	"unsafe"
)

// OpenSSL NID values for hash algorithms.
// These are stable across OpenSSL versions.
const (
	nidSHA1   = 64  // NID_sha1
	nidSHA256 = 672 // NID_sha256
	nidSHA384 = 673 // NID_sha384
	nidSHA512 = 674 // NID_sha512
)

// OpenSSLRSASigner implements crypto.Signer using OpenSSL for RSA operations.
// This provides better performance than Go's crypto/rsa on ARM processors.
type OpenSSLRSASigner struct {
	keyPEM     []byte
	publicKey  *rsa.PublicKey
	privateKey *rsa.PrivateKey // Original key for serialization (e.g., RDP needs PKCS#8)
	keyBits    int
}

// NewOpenSSLRSASigner creates an RSA signer backed by OpenSSL.
func NewOpenSSLRSASigner(keyPEM []byte) (*OpenSSLRSASigner, error) {
	if len(keyPEM) == 0 {
		return nil, fmt.Errorf("empty key PEM data")
	}

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

	errBuf := make([]byte, 256)
	keyBits := int(C.openssl_rsa_key_bits(
		(*C.uchar)(unsafe.Pointer(&keyPEM[0])),
		C.int(len(keyPEM)),
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.int(len(errBuf)),
	))
	if keyBits <= 0 {
		errStr := C.GoString((*C.char)(unsafe.Pointer(&errBuf[0])))
		return nil, fmt.Errorf("OpenSSL failed to parse RSA key: %s", errStr)
	}

	return &OpenSSLRSASigner{
		keyPEM:     keyPEM,
		publicKey:  &privateKey.PublicKey,
		privateKey: privateKey,
		keyBits:    keyBits,
	}, nil
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
func (s *OpenSSLRSASigner) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if len(digest) == 0 {
		return nil, fmt.Errorf("empty digest")
	}

	hash := opts.HashFunc()
	if hash == 0 {
		return nil, fmt.Errorf("hash function not specified")
	}

	hashNID, err := hashToNID(hash)
	if err != nil {
		return nil, err
	}

	sigBuf := make([]byte, (s.keyBits+7)/8)
	errBuf := make([]byte, 256)

	var sigLen C.int

	if _, ok := opts.(*rsa.PSSOptions); ok {
		sigLen = C.openssl_rsa_sign_pss(
			(*C.uchar)(unsafe.Pointer(&s.keyPEM[0])),
			C.int(len(s.keyPEM)),
			(*C.uchar)(unsafe.Pointer(&digest[0])),
			C.int(len(digest)),
			C.int(hashNID),
			(*C.uchar)(unsafe.Pointer(&sigBuf[0])),
			C.int(len(sigBuf)),
			(*C.char)(unsafe.Pointer(&errBuf[0])),
			C.int(len(errBuf)),
		)
	} else {
		sigLen = C.openssl_rsa_sign_pkcs1(
			(*C.uchar)(unsafe.Pointer(&s.keyPEM[0])),
			C.int(len(s.keyPEM)),
			(*C.uchar)(unsafe.Pointer(&digest[0])),
			C.int(len(digest)),
			C.int(hashNID),
			(*C.uchar)(unsafe.Pointer(&sigBuf[0])),
			C.int(len(sigBuf)),
			(*C.char)(unsafe.Pointer(&errBuf[0])),
			C.int(len(errBuf)),
		)
	}

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

// WrapRSAKey wraps an RSA private key with an OpenSSL-backed signer.
// Returns the wrapped signer and nil error on success.
// Returns the original key and an error if wrapping fails.
// The caller can use the original key as fallback if desired.
func WrapRSAKey(key crypto.PrivateKey) (crypto.PrivateKey, error) {
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return key, nil // Not RSA, return as-is (not an error)
	}

	keyDER := x509.MarshalPKCS1PrivateKey(rsaKey)
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: keyDER,
	})

	signer, err := NewOpenSSLRSASigner(keyPEM)
	if err != nil {
		return key, fmt.Errorf("OpenSSL RSA signer unavailable: %w", err)
	}

	return signer, nil
}
