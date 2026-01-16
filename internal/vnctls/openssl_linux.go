//go:build linux && arm

package vnctls

/*
#include <openssl/ssl.h>
#include <openssl/err.h>
#include <openssl/dh.h>
#include <openssl/bn.h>
#include <openssl/engine.h>
#include <openssl/bio.h>
#include <openssl/pem.h>
#include <openssl/x509.h>
#include <openssl/evp.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>
#include <fcntl.h>
#include <unistd.h>

static int set_blocking(int fd) {
    int flags = fcntl(fd, F_GETFL, 0);
    if (flags == -1) return -1;
    flags &= ~O_NONBLOCK;
    return fcntl(fd, F_SETFL, flags);
}

static int hw_crypto_initialized = 0;
static ENGINE *devcrypto_engine = NULL;

// List available engines for debugging
static void list_engines() {
    ENGINE *e;
    fprintf(stderr, "INFO: OpenSSL VNC TLS: Available engines:\n");
    for (e = ENGINE_get_first(); e != NULL; e = ENGINE_get_next(e)) {
        fprintf(stderr, "INFO:   - %s (%s)\n", ENGINE_get_id(e), ENGINE_get_name(e));
    }
}

// Try to load engine by ID with detailed error reporting
static ENGINE* try_load_engine(const char* engine_id) {
    ENGINE *e = ENGINE_by_id(engine_id);
    if (e == NULL) {
        fprintf(stderr, "INFO: OpenSSL VNC TLS: Engine '%s' not found\n", engine_id);
        return NULL;
    }

    fprintf(stderr, "INFO: OpenSSL VNC TLS: Found engine '%s' (%s)\n",
            ENGINE_get_id(e), ENGINE_get_name(e));

    if (!ENGINE_init(e)) {
        unsigned long err = ERR_get_error();
        char err_buf[256];
        ERR_error_string_n(err, err_buf, sizeof(err_buf));
        fprintf(stderr, "INFO: OpenSSL VNC TLS: Engine '%s' init FAILED: %s\n", engine_id, err_buf);
        ENGINE_free(e);
        return NULL;
    }

    // Set as default for ciphers and digests
    if (!ENGINE_set_default_ciphers(e)) {
        fprintf(stderr, "INFO: OpenSSL VNC TLS: Engine '%s' set_default_ciphers failed\n", engine_id);
    }
    if (!ENGINE_set_default_digests(e)) {
        fprintf(stderr, "INFO: OpenSSL VNC TLS: Engine '%s' set_default_digests failed\n", engine_id);
    }

    fprintf(stderr, "INFO: OpenSSL VNC TLS: Engine '%s' ENABLED for hardware crypto\n", engine_id);
    return e;
}

static void openssl_init() {
    if (hw_crypto_initialized) return;
    hw_crypto_initialized = 1;

    // Initialize OpenSSL with all built-in engines and providers
    OPENSSL_init_ssl(OPENSSL_INIT_LOAD_SSL_STRINGS | OPENSSL_INIT_LOAD_CRYPTO_STRINGS, NULL);

    // Load all built-in engines and config - essential for static builds
    OPENSSL_init_crypto(
        OPENSSL_INIT_ENGINE_ALL_BUILTIN |
        OPENSSL_INIT_LOAD_CONFIG |
        OPENSSL_INIT_ADD_ALL_CIPHERS |
        OPENSSL_INIT_ADD_ALL_DIGESTS,
        NULL
    );

    // For static builds, explicitly load built-in engines
    ENGINE_load_builtin_engines();

    fprintf(stderr, "INFO: OpenSSL VNC TLS: OpenSSL %s initialized\n", OPENSSL_VERSION_TEXT);
    fprintf(stderr, "INFO: OpenSSL VNC TLS: Attempting to load hardware crypto engine...\n");

    // List what's available
    list_engines();

    // Try devcrypto first (uses /dev/crypto kernel interface - best for Rockchip)
    devcrypto_engine = try_load_engine("devcrypto");

    // Fall back to afalg (uses AF_ALG socket interface)
    if (devcrypto_engine == NULL) {
        devcrypto_engine = try_load_engine("afalg");
    }

    // Fall back to dynamic loading if built-in didn't work
    if (devcrypto_engine == NULL) {
        fprintf(stderr, "INFO: OpenSSL VNC TLS: Trying dynamic engine load...\n");
        ENGINE *dyn = ENGINE_by_id("dynamic");
        if (dyn != NULL) {
            if (ENGINE_ctrl_cmd_string(dyn, "SO_PATH", "devcrypto", 0) &&
                ENGINE_ctrl_cmd_string(dyn, "LOAD", NULL, 0)) {
                devcrypto_engine = dyn;
                if (ENGINE_init(devcrypto_engine)) {
                    ENGINE_set_default_ciphers(devcrypto_engine);
                    fprintf(stderr, "INFO: OpenSSL VNC TLS: Dynamic devcrypto engine loaded\n");
                } else {
                    ENGINE_free(devcrypto_engine);
                    devcrypto_engine = NULL;
                }
            } else {
                ENGINE_free(dyn);
            }
        }
    }

    if (devcrypto_engine == NULL) {
        fprintf(stderr, "INFO: OpenSSL VNC TLS: NO hardware crypto engine available, using software AES\n");
        fprintf(stderr, "INFO: OpenSSL VNC TLS: For better performance, ensure /dev/crypto is available\n");
    }
}

static DH* create_dh_params() {
    DH *dh = DH_new();
    if (dh == NULL) return NULL;

    // RFC 3526 MODP Group 14 (2048-bit) - much stronger than 1024-bit
    static const unsigned char dh2048_p[] = {
        0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
        0xC9, 0x0F, 0xDA, 0xA2, 0x21, 0x68, 0xC2, 0x34,
        0xC4, 0xC6, 0x62, 0x8B, 0x80, 0xDC, 0x1C, 0xD1,
        0x29, 0x02, 0x4E, 0x08, 0x8A, 0x67, 0xCC, 0x74,
        0x02, 0x0B, 0xBE, 0xA6, 0x3B, 0x13, 0x9B, 0x22,
        0x51, 0x4A, 0x08, 0x79, 0x8E, 0x34, 0x04, 0xDD,
        0xEF, 0x95, 0x19, 0xB3, 0xCD, 0x3A, 0x43, 0x1B,
        0x30, 0x2B, 0x0A, 0x6D, 0xF2, 0x5F, 0x14, 0x37,
        0x4F, 0xE1, 0x35, 0x6D, 0x6D, 0x51, 0xC2, 0x45,
        0xE4, 0x85, 0xB5, 0x76, 0x62, 0x5E, 0x7E, 0xC6,
        0xF4, 0x4C, 0x42, 0xE9, 0xA6, 0x37, 0xED, 0x6B,
        0x0B, 0xFF, 0x5C, 0xB6, 0xF4, 0x06, 0xB7, 0xED,
        0xEE, 0x38, 0x6B, 0xFB, 0x5A, 0x89, 0x9F, 0xA5,
        0xAE, 0x9F, 0x24, 0x11, 0x7C, 0x4B, 0x1F, 0xE6,
        0x49, 0x28, 0x66, 0x51, 0xEC, 0xE4, 0x5B, 0x3D,
        0xC2, 0x00, 0x7C, 0xB8, 0xA1, 0x63, 0xBF, 0x05,
        0x98, 0xDA, 0x48, 0x36, 0x1C, 0x55, 0xD3, 0x9A,
        0x69, 0x16, 0x3F, 0xA8, 0xFD, 0x24, 0xCF, 0x5F,
        0x83, 0x65, 0x5D, 0x23, 0xDC, 0xA3, 0xAD, 0x96,
        0x1C, 0x62, 0xF3, 0x56, 0x20, 0x85, 0x52, 0xBB,
        0x9E, 0xD5, 0x29, 0x07, 0x70, 0x96, 0x96, 0x6D,
        0x67, 0x0C, 0x35, 0x4E, 0x4A, 0xBC, 0x98, 0x04,
        0xF1, 0x74, 0x6C, 0x08, 0xCA, 0x18, 0x21, 0x7C,
        0x32, 0x90, 0x5E, 0x46, 0x2E, 0x36, 0xCE, 0x3B,
        0xE3, 0x9E, 0x77, 0x2C, 0x18, 0x0E, 0x86, 0x03,
        0x9B, 0x27, 0x83, 0xA2, 0xEC, 0x07, 0xA2, 0x8F,
        0xB5, 0xC5, 0x5D, 0xF0, 0x6F, 0x4C, 0x52, 0xC9,
        0xDE, 0x2B, 0xCB, 0xF6, 0x95, 0x58, 0x17, 0x18,
        0x39, 0x95, 0x49, 0x7C, 0xEA, 0x95, 0x6A, 0xE5,
        0x15, 0xD2, 0x26, 0x18, 0x98, 0xFA, 0x05, 0x10,
        0x15, 0x72, 0x8E, 0x5A, 0x8A, 0xAC, 0xAA, 0x68,
        0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF
    };
    static const unsigned char dh2048_g[] = { 0x02 };

    BIGNUM *p = BN_bin2bn(dh2048_p, sizeof(dh2048_p), NULL);
    BIGNUM *g = BN_bin2bn(dh2048_g, sizeof(dh2048_g), NULL);

    if (p == NULL || g == NULL || !DH_set0_pqg(dh, p, NULL, g)) {
        BN_free(p);
        BN_free(g);
        DH_free(dh);
        return NULL;
    }

    return dh;
}

static SSL_CTX* create_vnc_ssl_ctx(int use_cert, const char* cert_pem, const char* key_pem) {
    SSL_CTX *ctx = SSL_CTX_new(TLS_server_method());
    if (ctx == NULL) return NULL;

    // Security level 1 disables < 80-bit security (RC4, DES, export ciphers, etc.)
    // Note: We use level 1 instead of 2 because anonymous DH (ADH) ciphers are required
    // for VeNCrypt TLSVncAuth mode which doesn't use certificates
    SSL_CTX_set_security_level(ctx, 1);
    // TLS 1.2 minimum - TLS 1.0 and 1.1 are deprecated and have known vulnerabilities
    SSL_CTX_set_min_proto_version(ctx, TLS1_2_VERSION);
    SSL_CTX_set_max_proto_version(ctx, TLS1_3_VERSION);

    if (use_cert) {
        // When using certificates, prefer AEAD ciphers with forward secrecy
        // Prioritize GCM modes, then SHA384/SHA256 variants
        // Also support ECDSA certificates (common with Let's Encrypt)
        SSL_CTX_set_cipher_list(ctx,
            "ECDHE-ECDSA-AES256-GCM-SHA384:"
            "ECDHE-ECDSA-AES128-GCM-SHA256:"
            "ECDHE-ECDSA-CHACHA20-POLY1305:"
            "ECDHE-RSA-AES256-GCM-SHA384:"
            "ECDHE-RSA-AES128-GCM-SHA256:"
            "ECDHE-RSA-CHACHA20-POLY1305:"
            "DHE-RSA-AES256-GCM-SHA384:"
            "DHE-RSA-AES128-GCM-SHA256:"
            "DHE-RSA-CHACHA20-POLY1305");

        // Load certificate from PEM string in memory
        BIO *cert_bio = BIO_new_mem_buf(cert_pem, -1);
        if (cert_bio == NULL) {
            SSL_CTX_free(ctx);
            return NULL;
        }

        // Read certificate chain from BIO
        X509 *cert = PEM_read_bio_X509(cert_bio, NULL, NULL, NULL);
        if (cert == NULL) {
            BIO_free(cert_bio);
            SSL_CTX_free(ctx);
            return NULL;
        }

        if (SSL_CTX_use_certificate(ctx, cert) != 1) {
            X509_free(cert);
            BIO_free(cert_bio);
            SSL_CTX_free(ctx);
            return NULL;
        }
        X509_free(cert);

        // Read any additional chain certificates
        X509 *ca_cert;
        while ((ca_cert = PEM_read_bio_X509(cert_bio, NULL, NULL, NULL)) != NULL) {
            if (SSL_CTX_add_extra_chain_cert(ctx, ca_cert) != 1) {
                X509_free(ca_cert);
                // Continue - not fatal if chain cert fails
            }
            // Note: SSL_CTX_add_extra_chain_cert takes ownership, don't free
        }
        // Clear the error from the last failed read (expected at end of chain)
        ERR_clear_error();
        BIO_free(cert_bio);

        // Load private key from PEM string in memory
        BIO *key_bio = BIO_new_mem_buf(key_pem, -1);
        if (key_bio == NULL) {
            SSL_CTX_free(ctx);
            return NULL;
        }

        EVP_PKEY *pkey = PEM_read_bio_PrivateKey(key_bio, NULL, NULL, NULL);
        BIO_free(key_bio);

        if (pkey == NULL) {
            SSL_CTX_free(ctx);
            return NULL;
        }

        if (SSL_CTX_use_PrivateKey(ctx, pkey) != 1) {
            EVP_PKEY_free(pkey);
            SSL_CTX_free(ctx);
            return NULL;
        }
        EVP_PKEY_free(pkey);
    } else {
        // Anonymous DH mode for VeNCrypt TLSVncAuth (no certificates)
        // These ciphers provide encryption without server authentication
        // Note: This is intentional for VNC protocol compatibility
        // GCM (AEAD) preferred, CBC fallback for compatibility with clients like Jump Desktop
        // Excludes SHA-1 based ciphers for better security
        SSL_CTX_set_cipher_list(ctx,
            "ADH-AES256-GCM-SHA384:"
            "ADH-AES128-GCM-SHA256:"
            "ADH-AES256-SHA256:"
            "ADH-AES128-SHA256");
    }

    DH *dh = create_dh_params();
    if (dh == NULL) {
        SSL_CTX_free(ctx);
        return NULL;
    }
    SSL_CTX_set_tmp_dh(ctx, dh);
    DH_free(dh);

    SSL_CTX_set_ecdh_auto(ctx, 1);
    SSL_CTX_set_verify(ctx, SSL_VERIFY_NONE, NULL);
    SSL_CTX_set_session_cache_mode(ctx, SSL_SESS_CACHE_SERVER);

    return ctx;
}

static char* get_ssl_error_string() {
    char *buf = (char*)malloc(512);
    buf[0] = '\0';
    int pos = 0;
    unsigned long err;
    while ((err = ERR_get_error()) != 0 && pos < 400) {
        if (pos > 0) pos += snprintf(buf + pos, 512 - pos, "; ");
        char tmp[128];
        ERR_error_string_n(err, tmp, sizeof(tmp));
        pos += snprintf(buf + pos, 512 - pos, "%s", tmp);
    }
    if (pos == 0) strcpy(buf, "unknown error");
    return buf;
}

static int get_errno() {
    return errno;
}

static int is_hw_crypto_enabled() {
    return devcrypto_engine != NULL ? 1 : 0;
}

static const char* get_hw_crypto_engine_name() {
    if (devcrypto_engine == NULL) {
        return NULL;
    }
    return ENGINE_get_name(devcrypto_engine);
}
*/
import "C"

import (
	"fmt"
	"net"
	"os"
	"sync"
	"unsafe"
)

var initOnce sync.Once

func Init() {
	initOnce.Do(func() {
		C.openssl_init()
	})
}

type TLSConn struct {
	ssl    *C.SSL
	ctx    *C.SSL_CTX
	conn   net.Conn
	fd     int
	closed bool
	mu     sync.Mutex    // protects closed flag
	readMu sync.Mutex    // protects SSL_read
	writeMu sync.Mutex   // protects SSL_write
}

func UpgradeToTLS(conn net.Conn, useCert bool, certPEM, keyPEM string) (*TLSConn, error) {
	Init()

	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return nil, fmt.Errorf("connection must be TCP")
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return nil, fmt.Errorf("failed to get raw connection: %w", err)
	}

	var fd int
	err = rawConn.Control(func(fdPtr uintptr) {
		fd = int(fdPtr)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get fd: %w", err)
	}

	var ctx *C.SSL_CTX
	if useCert {
		certC := C.CString(certPEM)
		keyC := C.CString(keyPEM)
		defer C.free(unsafe.Pointer(certC))
		defer C.free(unsafe.Pointer(keyC))
		ctx = C.create_vnc_ssl_ctx(1, certC, keyC)
	} else {
		ctx = C.create_vnc_ssl_ctx(0, nil, nil)
	}

	if ctx == nil {
		errStr := C.get_ssl_error_string()
		defer C.free(unsafe.Pointer(errStr))
		return nil, fmt.Errorf("failed to create SSL context: %s", C.GoString(errStr))
	}

	ssl := C.SSL_new(ctx)
	if ssl == nil {
		C.SSL_CTX_free(ctx)
		errStr := C.get_ssl_error_string()
		defer C.free(unsafe.Pointer(errStr))
		return nil, fmt.Errorf("failed to create SSL connection: %s", C.GoString(errStr))
	}

	if C.set_blocking(C.int(fd)) != 0 {
		C.SSL_free(ssl)
		C.SSL_CTX_free(ctx)
		return nil, fmt.Errorf("failed to set fd to blocking mode")
	}

	if C.SSL_set_fd(ssl, C.int(fd)) != 1 {
		C.SSL_free(ssl)
		C.SSL_CTX_free(ctx)
		errStr := C.get_ssl_error_string()
		defer C.free(unsafe.Pointer(errStr))
		return nil, fmt.Errorf("failed to set SSL fd: %s", C.GoString(errStr))
	}

	ret := C.SSL_accept(ssl)
	if ret != 1 {
		sslErr := C.SSL_get_error(ssl, ret)
		errStr := C.get_ssl_error_string()
		errMsg := C.GoString(errStr)
		C.free(unsafe.Pointer(errStr))

		var errnoStr string
		if sslErr == C.SSL_ERROR_SYSCALL {
			errnoStr = fmt.Sprintf(", errno=%d", C.get_errno())
		}

		C.SSL_free(ssl)
		C.SSL_CTX_free(ctx)
		return nil, fmt.Errorf("TLS handshake failed: %s%s", errMsg, errnoStr)
	}

	return &TLSConn{
		ssl:  ssl,
		ctx:  ctx,
		conn: conn,
		fd:   fd,
	}, nil
}

func (c *TLSConn) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}

	// Check closed flag with main mutex
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return 0, os.ErrClosed
	}

	// Use separate read mutex to allow concurrent writes
	c.readMu.Lock()
	defer c.readMu.Unlock()

	n := C.SSL_read(c.ssl, unsafe.Pointer(&b[0]), C.int(len(b)))
	if n <= 0 {
		err := C.SSL_get_error(c.ssl, n)
		if err == C.SSL_ERROR_ZERO_RETURN {
			return 0, os.ErrClosed
		}
		errStr := C.get_ssl_error_string()
		defer C.free(unsafe.Pointer(errStr))
		return 0, fmt.Errorf("SSL read error: %s", C.GoString(errStr))
	}

	return int(n), nil
}

func (c *TLSConn) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}

	// Check closed flag with main mutex
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return 0, os.ErrClosed
	}

	// Use separate write mutex to allow concurrent reads
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	n := C.SSL_write(c.ssl, unsafe.Pointer(&b[0]), C.int(len(b)))
	if n <= 0 {
		errStr := C.get_ssl_error_string()
		defer C.free(unsafe.Pointer(errStr))
		return 0, fmt.Errorf("SSL write error: %s", C.GoString(errStr))
	}

	return int(n), nil
}

func (c *TLSConn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	// Acquire both read and write locks to ensure no operations in progress
	c.readMu.Lock()
	c.writeMu.Lock()
	defer c.readMu.Unlock()
	defer c.writeMu.Unlock()

	C.SSL_shutdown(c.ssl)
	C.SSL_free(c.ssl)
	C.SSL_CTX_free(c.ctx)

	return c.conn.Close()
}

func (c *TLSConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *TLSConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *TLSConn) GetCipherName() string {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return "unknown"
	}

	c.readMu.Lock()
	defer c.readMu.Unlock()

	cipher := C.SSL_get_current_cipher(c.ssl)
	if cipher == nil {
		return "unknown"
	}
	return C.GoString(C.SSL_CIPHER_get_name(cipher))
}

func (c *TLSConn) GetProtocolVersion() string {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return "unknown"
	}

	c.readMu.Lock()
	defer c.readMu.Unlock()

	return C.GoString(C.SSL_get_version(c.ssl))
}

// IsHardwareCryptoEnabled returns true if OpenSSL is using a hardware crypto engine
func IsHardwareCryptoEnabled() bool {
	return C.is_hw_crypto_enabled() != 0
}

// GetHardwareCryptoEngine returns the name of the hardware crypto engine in use
func GetHardwareCryptoEngine() string {
	name := C.get_hw_crypto_engine_name()
	if name == nil {
		return "none (software)"
	}
	return C.GoString(name)
}
