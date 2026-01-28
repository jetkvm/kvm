//go:build cgo && linux && arm

// Hardware-accelerated RSA using Rockchip PKA via /dev/rk_pka
//
// This provides RSA signing using the RV1106 PKA (Public Key Accelerator)
// hardware. It requires the rk_pka_uapi kernel module to be loaded.
//
// The module uses the kernel crypto API which automatically selects
// the hardware rsa-rk driver for RSA operations.

package tls

/*
#include <fcntl.h>
#include <unistd.h>
#include <string.h>
#include <errno.h>
#include <sys/ioctl.h>
#include <stdint.h>
#include <stdlib.h>
#include <stdio.h>

// IOCTL definitions matching rk_pka_uapi.h
#define RK_PKA_IOC_MAGIC 'P'

struct rk_pka_key {
    uint32_t key_len;
    uint8_t  key_data[0];
};

struct rk_pka_op {
    uint32_t in_len;
    uint32_t out_len;
    uint64_t in_data;
    uint64_t out_data;
};

#define RK_PKA_IOC_LOAD_KEY   _IOW(RK_PKA_IOC_MAGIC, 1, struct rk_pka_key)
#define RK_PKA_IOC_DECRYPT    _IOWR(RK_PKA_IOC_MAGIC, 2, struct rk_pka_op)
#define RK_PKA_IOC_FREE_KEY   _IO(RK_PKA_IOC_MAGIC, 3)

// Helper to construct ioctl numbers
#define _IOC_NRBITS     8
#define _IOC_TYPEBITS   8
#define _IOC_SIZEBITS   14
#define _IOC_DIRBITS    2

#define _IOC_NRSHIFT    0
#define _IOC_TYPESHIFT  (_IOC_NRSHIFT+_IOC_NRBITS)
#define _IOC_SIZESHIFT  (_IOC_TYPESHIFT+_IOC_TYPEBITS)
#define _IOC_DIRSHIFT   (_IOC_SIZESHIFT+_IOC_SIZEBITS)

#define _IOC_NONE       0U
#define _IOC_WRITE      1U
#define _IOC_READ       2U

#define _IOC(dir,type,nr,size) \
    (((dir)  << _IOC_DIRSHIFT) | \
     ((type) << _IOC_TYPESHIFT) | \
     ((nr)   << _IOC_NRSHIFT) | \
     ((size) << _IOC_SIZESHIFT))

// Actual ioctl numbers for ARM
#define IOCTL_LOAD_KEY  _IOC(_IOC_WRITE, 'P', 1, sizeof(struct rk_pka_key))
#define IOCTL_DECRYPT   _IOC(_IOC_READ|_IOC_WRITE, 'P', 2, sizeof(struct rk_pka_op))
#define IOCTL_FREE_KEY  _IOC(_IOC_NONE, 'P', 3, 0)

static int pka_open(void) {
    return open("/dev/rk_pka", O_RDWR);
}

static int pka_close(int fd) {
    return close(fd);
}

static int pka_load_key(int fd, const uint8_t *key_der, uint32_t key_len, char *err_buf, int err_len) {
    // Allocate buffer for header + key data
    uint32_t total_len = sizeof(struct rk_pka_key) + key_len;
    uint8_t *buf = (uint8_t *)malloc(total_len);
    if (!buf) {
        snprintf(err_buf, err_len, "malloc failed");
        return -1;
    }

    // Fill header
    struct rk_pka_key *hdr = (struct rk_pka_key *)buf;
    hdr->key_len = key_len;
    memcpy(buf + sizeof(struct rk_pka_key), key_der, key_len);

    int ret = ioctl(fd, IOCTL_LOAD_KEY, buf);
    int saved_errno = errno;

    // Clear sensitive data
    memset(buf, 0, total_len);
    free(buf);

    if (ret < 0) {
        snprintf(err_buf, err_len, "ioctl LOAD_KEY failed: %s (errno %d)", strerror(saved_errno), saved_errno);
        return -1;
    }

    return 0;
}

static int pka_decrypt(int fd, const uint8_t *in_data, uint32_t in_len,
                       uint8_t *out_data, uint32_t out_len,
                       char *err_buf, int err_len) {
    struct rk_pka_op op;
    op.in_len = in_len;
    op.out_len = out_len;
    op.in_data = (uint64_t)(uintptr_t)in_data;
    op.out_data = (uint64_t)(uintptr_t)out_data;

    int ret = ioctl(fd, IOCTL_DECRYPT, &op);
    if (ret < 0) {
        snprintf(err_buf, err_len, "ioctl DECRYPT failed: %s (errno %d)", strerror(errno), errno);
        return -1;
    }

    return (int)op.out_len;
}

static int pka_free_key(int fd) {
    return ioctl(fd, IOCTL_FREE_KEY, 0);
}
*/
import "C"

import (
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"io"
	"sync"
	"unsafe"
)

// PKAAvailable checks if the PKA device is available
func PKAAvailable() bool {
	fd := C.pka_open()
	if fd < 0 {
		return false
	}
	C.pka_close(fd)
	return true
}

// PKARSASigner implements crypto.Signer using Rockchip PKA hardware
type PKARSASigner struct {
	fd         C.int
	publicKey  *rsa.PublicKey
	privateKey *rsa.PrivateKey // For fallback and serialization
	keySize    int             // In bytes
	mu         sync.Mutex
}

// NewPKARSASigner creates an RSA signer backed by Rockchip PKA hardware.
// Returns nil and an error if PKA is not available.
func NewPKARSASigner(privateKey *rsa.PrivateKey) (*PKARSASigner, error) {
	if privateKey == nil {
		return nil, fmt.Errorf("nil private key")
	}

	// Open PKA device
	fd := C.pka_open()
	if fd < 0 {
		return nil, fmt.Errorf("PKA device not available")
	}

	// Convert key to DER format
	keyDER := x509.MarshalPKCS1PrivateKey(privateKey)

	// Load key into PKA
	errBuf := make([]byte, 256)
	ret := C.pka_load_key(fd,
		(*C.uint8_t)(unsafe.Pointer(&keyDER[0])),
		C.uint32_t(len(keyDER)),
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.int(len(errBuf)))

	if ret < 0 {
		C.pka_close(fd)
		return nil, fmt.Errorf("failed to load key: %s", C.GoString((*C.char)(unsafe.Pointer(&errBuf[0]))))
	}

	keySize := (privateKey.N.BitLen() + 7) / 8

	signer := &PKARSASigner{
		fd:         fd,
		publicKey:  &privateKey.PublicKey,
		privateKey: privateKey,
		keySize:    keySize,
	}

	return signer, nil
}

// Close releases the PKA resources
func (s *PKARSASigner) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fd >= 0 {
		C.pka_free_key(s.fd)
		C.pka_close(s.fd)
		s.fd = -1
	}
	return nil
}

// RSAPrivateKey returns the underlying RSA private key (for serialization)
func (s *PKARSASigner) RSAPrivateKey() *rsa.PrivateKey {
	return s.privateKey
}

// Public returns the public key
func (s *PKARSASigner) Public() crypto.PublicKey {
	return s.publicKey
}

// Sign signs digest with the private key using PKA hardware.
// For TLS, this performs raw RSA private key operation after the caller
// has applied appropriate padding (PKCS#1 v1.5 or PSS).
func (s *PKARSASigner) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fd < 0 {
		return nil, fmt.Errorf("PKA signer closed")
	}

	// For RSA signing, we need to apply padding first, then do raw RSA
	// The padding creates a value that we then "decrypt" with private key
	hash := opts.HashFunc()
	if hash == 0 {
		return nil, fmt.Errorf("hash function not specified")
	}

	var padded []byte
	var err error

	if pssOpts, ok := opts.(*rsa.PSSOptions); ok {
		// PSS padding - use Go's implementation then raw RSA
		// Unfortunately PSS needs randomness, so we can't easily separate padding from signing
		// Fall back to Go for PSS
		return rsa.SignPSS(rand, s.privateKey, hash, digest, pssOpts)
	} else {
		// PKCS#1 v1.5 padding
		padded, err = pkcs1v15Pad(digest, hash, s.keySize)
		if err != nil {
			return nil, err
		}
	}

	// Perform raw RSA private key operation via PKA
	outBuf := make([]byte, s.keySize)
	errBuf := make([]byte, 256)

	ret := C.pka_decrypt(s.fd,
		(*C.uint8_t)(unsafe.Pointer(&padded[0])),
		C.uint32_t(len(padded)),
		(*C.uint8_t)(unsafe.Pointer(&outBuf[0])),
		C.uint32_t(len(outBuf)),
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.int(len(errBuf)))

	if ret < 0 {
		errStr := C.GoString((*C.char)(unsafe.Pointer(&errBuf[0])))
		return nil, fmt.Errorf("PKA decrypt failed: %s", errStr)
	}

	return outBuf[:ret], nil
}

// pkcs1v15Pad applies PKCS#1 v1.5 signature padding
func pkcs1v15Pad(digest []byte, hash crypto.Hash, keySize int) ([]byte, error) {
	hashInfo, ok := hashPrefixes[hash]
	if !ok {
		return nil, fmt.Errorf("unsupported hash: %v", hash)
	}

	tLen := len(hashInfo) + len(digest)
	if keySize < tLen+11 {
		return nil, fmt.Errorf("key too small for hash")
	}

	// EM = 0x00 || 0x01 || PS || 0x00 || T
	// where PS is padding of 0xff bytes
	em := make([]byte, keySize)
	em[1] = 0x01
	for i := 2; i < keySize-tLen-1; i++ {
		em[i] = 0xff
	}
	em[keySize-tLen-1] = 0x00
	copy(em[keySize-tLen:], hashInfo)
	copy(em[keySize-len(digest):], digest)

	return em, nil
}

// DER prefixes for hash algorithms (PKCS#1 v1.5 DigestInfo)
var hashPrefixes = map[crypto.Hash][]byte{
	crypto.SHA1:   {0x30, 0x21, 0x30, 0x09, 0x06, 0x05, 0x2b, 0x0e, 0x03, 0x02, 0x1a, 0x05, 0x00, 0x04, 0x14},
	crypto.SHA256: {0x30, 0x31, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x01, 0x05, 0x00, 0x04, 0x20},
	crypto.SHA384: {0x30, 0x41, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x02, 0x05, 0x00, 0x04, 0x30},
	crypto.SHA512: {0x30, 0x51, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x03, 0x05, 0x00, 0x04, 0x40},
}
