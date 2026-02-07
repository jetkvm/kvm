//go:build cgo && linux && arm

// Package tls provides hardware-accelerated TLS using OpenSSL via CGO.
//
// OpenSSL 3.0 Deprecation Notes:
// This implementation uses the ENGINE API for hardware crypto acceleration
// (devcrypto/AF_ALG for Rockchip RV1106). The ENGINE API is deprecated in
// OpenSSL 3.0 in favor of the Provider architecture, but:
//
//  1. The Provider API for hardware crypto is not yet mature
//  2. No production-ready devcrypto/AF_ALG provider exists as of OpenSSL 3.3
//  3. ENGINE API still functions in OpenSSL 3.x (deprecated != removed)
//
// The DH API (DH_new, DH_set0_pqg, etc.) is also deprecated. This code uses
// version-conditional compilation:
//   - OpenSSL 3.0+: Uses EVP_PKEY_fromdata() for DH parameters (modern API)
//   - OpenSSL 1.x:  Uses legacy DH_* functions
//
// Future migration path when OpenSSL Provider API matures:
//  1. Replace ENGINE_by_id("devcrypto") with OSSL_PROVIDER_load(NULL, "devcrypto")
//  2. Engine initialization becomes provider configuration
//  3. The rest of the code (SSL_CTX, SSL) remains unchanged
//
// References:
//   - OpenSSL 3.0 Migration: https://www.openssl.org/docs/man3.0/man7/migration_guide.html
//   - Provider concept: https://www.openssl.org/docs/man3.0/man7/provider.html

package tls

/*
// Suppress ENGINE API deprecation warnings.
// The ENGINE API is deprecated in OpenSSL 3.0 in favor of Providers, but:
// 1. The Provider API for hardware crypto (devcrypto, AF_ALG) is not yet mature
// 2. ENGINE API still works in OpenSSL 3.x, it's just marked deprecated
// 3. Hardware crypto acceleration on Rockchip RV1106 requires ENGINE for now
// TODO: Migrate to Provider API when OpenSSL's hardware crypto providers mature
#define OPENSSL_SUPPRESS_DEPRECATED

#include <openssl/ssl.h>
#include <openssl/err.h>
#include <openssl/bn.h>
#include <openssl/engine.h>
#include <openssl/bio.h>
#include <openssl/pem.h>
#include <openssl/x509.h>
#include <openssl/evp.h>

// OpenSSL 3.0+ uses EVP_PKEY for DH instead of deprecated DH API
#if OPENSSL_VERSION_NUMBER >= 0x30000000L
#include <openssl/param_build.h>
#include <openssl/core_names.h>
#else
#include <openssl/dh.h>
#endif
#include <stdlib.h>
#include <string.h>
#include <errno.h>
#include <fcntl.h>
#include <unistd.h>
#include <sys/socket.h>

// Buffer sizes for error handling
#define SSL_ERROR_BUF_SIZE 512
#define SSL_ERROR_MAX_POS 400
#define SSL_ERROR_TMP_SIZE 128
#define ENGINE_ERROR_BUF_SIZE 256

// kTLS support detection - SSL_OP_ENABLE_KTLS added in OpenSSL 3.0
#ifndef SSL_OP_ENABLE_KTLS
#define SSL_OP_ENABLE_KTLS 0
#define KTLS_NOT_AVAILABLE 1
#else
#define KTLS_NOT_AVAILABLE 0
#endif

// kTLS send/recv detection - added in OpenSSL 3.0
#if OPENSSL_VERSION_NUMBER >= 0x30000000L
#define HAS_KTLS_FUNCTIONS 1
#else
#define HAS_KTLS_FUNCTIONS 0
#endif

// set_blocking switches a socket fd from non-blocking (Go default) to blocking mode.
// This is required for OpenSSL: SSL_read/SSL_write call read()/write() on the raw fd
// via BIO. In non-blocking mode these would return EAGAIN/SSL_ERROR_WANT_READ which
// our code doesn't retry. Blocking mode lets OpenSSL complete I/O normally.
// Deadlines are enforced via SO_SNDTIMEO/SO_RCVTIMEO set in SetWriteDeadline/SetReadDeadline.
static int set_blocking(int fd) {
    int flags = fcntl(fd, F_GETFL, 0);
    if (flags == -1) return -1;
    flags &= ~O_NONBLOCK;
    return fcntl(fd, F_SETFL, flags);
}

// set_send_timeout sets SO_SNDTIMEO on a blocking socket.
// sec=0,usec=0 clears the timeout (infinite wait).
static int set_send_timeout(int fd, long sec, long usec) {
    struct timeval tv;
    tv.tv_sec = sec;
    tv.tv_usec = usec;
    return setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &tv, sizeof(tv));
}

// set_recv_timeout sets SO_RCVTIMEO on a blocking socket.
// sec=0,usec=0 clears the timeout (infinite wait).
static int set_recv_timeout(int fd, long sec, long usec) {
    struct timeval tv;
    tv.tv_sec = sec;
    tv.tv_usec = usec;
    return setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof(tv));
}

static int hw_crypto_initialized = 0;
static ENGINE *devcrypto_engine = NULL;

// Log level for OpenSSL info messages: 1=INFO, 2=WARN (default), 3=ERROR
// Using volatile for single-core RV1106 (no cache coherency issues).
// Static to avoid duplicate symbols when CGO generates _cgo_export.c.
static volatile int openssl_log_level = 2;

// Set the OpenSSL log level from Go.
// Clamps to valid range: TRACE=-1 to DISABLE=6 (zerolog convention).
// Static because it's only called from this file's Go code via C.openssl_set_log_level().
static void openssl_set_log_level(int level) {
    if (level < -1) level = -1;
    if (level > 6) level = 6;
    openssl_log_level = level;
}

// Getter for Go code — Go can't access static variables directly.
static int openssl_get_log_level() {
    return openssl_log_level;
}

// List available engines for debugging
static void list_engines() {
    if (openssl_log_level > 1) return;  // Skip if log level > INFO
    ENGINE *e;
    fprintf(stderr, "INFO: OpenSSL crypto/tls: Available engines:\n");
    for (e = ENGINE_get_first(); e != NULL; e = ENGINE_get_next(e)) {
        fprintf(stderr, "INFO:   - %s (%s)\n", ENGINE_get_id(e), ENGINE_get_name(e));
    }
}

// Try to load engine by ID with detailed error reporting
static ENGINE* try_load_engine(const char* engine_id) {
    ENGINE *e = ENGINE_by_id(engine_id);
    if (e == NULL) {
        if (openssl_log_level <= 1) fprintf(stderr, "INFO: OpenSSL crypto/tls: Engine '%s' not found\n", engine_id);
        return NULL;
    }

    if (openssl_log_level <= 1) fprintf(stderr, "INFO: OpenSSL crypto/tls: Found engine '%s' (%s)\n",
            ENGINE_get_id(e), ENGINE_get_name(e));

    if (!ENGINE_init(e)) {
        unsigned long err = ERR_get_error();
        char err_buf[ENGINE_ERROR_BUF_SIZE];
        ERR_error_string_n(err, err_buf, sizeof(err_buf));
        if (openssl_log_level <= 1) fprintf(stderr, "INFO: OpenSSL crypto/tls: Engine '%s' init FAILED: %s\n", engine_id, err_buf);
        ENGINE_free(e);
        return NULL;
    }

    // Set as default for ciphers, digests, and asymmetric crypto (if available)
    if (!ENGINE_set_default_ciphers(e)) {
        if (openssl_log_level <= 1) fprintf(stderr, "INFO: OpenSSL crypto/tls: Engine '%s' set_default_ciphers failed\n", engine_id);
    }
    if (!ENGINE_set_default_digests(e)) {
        if (openssl_log_level <= 1) fprintf(stderr, "INFO: OpenSSL crypto/tls: Engine '%s' set_default_digests failed\n", engine_id);
    }
    // Try to enable RSA acceleration if available
    ENGINE_set_default_RSA(e);
    // Try to enable EC acceleration if available
    ENGINE_set_default_EC(e);
    // Use hardware RNG if available
    ENGINE_set_default_RAND(e);

    if (openssl_log_level <= 1) fprintf(stderr, "INFO: OpenSSL crypto/tls: Engine '%s' ENABLED for hardware crypto\n", engine_id);
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

    if (openssl_log_level <= 1) {
        fprintf(stderr, "INFO: OpenSSL crypto/tls: OpenSSL %s initialized\n", OPENSSL_VERSION_TEXT);
        fprintf(stderr, "INFO: OpenSSL crypto/tls: Attempting to load hardware crypto engine...\n");
    }

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
        if (openssl_log_level <= 1) fprintf(stderr, "INFO: OpenSSL crypto/tls: Trying dynamic engine load...\n");
        ENGINE *dyn = ENGINE_by_id("dynamic");
        if (dyn != NULL) {
            if (ENGINE_ctrl_cmd_string(dyn, "SO_PATH", "devcrypto", 0) &&
                ENGINE_ctrl_cmd_string(dyn, "LOAD", NULL, 0)) {
                devcrypto_engine = dyn;
                if (ENGINE_init(devcrypto_engine)) {
                    ENGINE_set_default_ciphers(devcrypto_engine);
                    if (openssl_log_level <= 1) fprintf(stderr, "INFO: OpenSSL crypto/tls: Dynamic devcrypto engine loaded\n");
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
        if (openssl_log_level <= 1) {
            fprintf(stderr, "INFO: OpenSSL crypto/tls: NO hardware crypto engine available, using software AES\n");
            fprintf(stderr, "INFO: OpenSSL crypto/tls: For better performance, ensure /dev/crypto is available\n");
        }
    }
}

// RFC 3526 MODP Group 14 (2048-bit) - for DHE key exchange
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

#if OPENSSL_VERSION_NUMBER >= 0x30000000L
// OpenSSL 3.0+ uses EVP_PKEY API instead of deprecated DH functions
static EVP_PKEY* create_dh_pkey() {
    BIGNUM *p = BN_bin2bn(dh2048_p, sizeof(dh2048_p), NULL);
    BIGNUM *g = BN_bin2bn(dh2048_g, sizeof(dh2048_g), NULL);
    if (p == NULL || g == NULL) {
        BN_free(p);
        BN_free(g);
        return NULL;
    }

    OSSL_PARAM_BLD *bld = OSSL_PARAM_BLD_new();
    if (bld == NULL) {
        BN_free(p);
        BN_free(g);
        return NULL;
    }

    if (!OSSL_PARAM_BLD_push_BN(bld, OSSL_PKEY_PARAM_FFC_P, p) ||
        !OSSL_PARAM_BLD_push_BN(bld, OSSL_PKEY_PARAM_FFC_G, g)) {
        OSSL_PARAM_BLD_free(bld);
        BN_free(p);
        BN_free(g);
        return NULL;
    }

    OSSL_PARAM *params = OSSL_PARAM_BLD_to_param(bld);
    OSSL_PARAM_BLD_free(bld);
    BN_free(p);
    BN_free(g);

    if (params == NULL) {
        return NULL;
    }

    EVP_PKEY_CTX *pctx = EVP_PKEY_CTX_new_from_name(NULL, "DH", NULL);
    if (pctx == NULL) {
        OSSL_PARAM_free(params);
        return NULL;
    }

    EVP_PKEY *pkey = NULL;
    if (EVP_PKEY_fromdata_init(pctx) <= 0 ||
        EVP_PKEY_fromdata(pctx, &pkey, EVP_PKEY_KEY_PARAMETERS, params) <= 0) {
        EVP_PKEY_CTX_free(pctx);
        OSSL_PARAM_free(params);
        return NULL;
    }

    EVP_PKEY_CTX_free(pctx);
    OSSL_PARAM_free(params);
    return pkey;
}
#else
// OpenSSL 1.x uses legacy DH API
static DH* create_dh_params() {
    DH *dh = DH_new();
    if (dh == NULL) return NULL;

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
#endif

static SSL_CTX* create_ssl_ctx(int use_cert, const char* cert_pem, const char* key_pem,
                               const char* cipher_list, int min_version, int max_version) {
    SSL_CTX *ctx = SSL_CTX_new(TLS_server_method());
    if (ctx == NULL) return NULL;

    // Security level 2 for X.509 certificate mode
    SSL_CTX_set_security_level(ctx, use_cert ? 2 : 1);
    SSL_CTX_set_min_proto_version(ctx, min_version);
    SSL_CTX_set_max_proto_version(ctx, max_version);

    // Set cipher list
    SSL_CTX_set_cipher_list(ctx, cipher_list);

    if (use_cert) {
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
    }

    // Set up DH parameters for key exchange
#if OPENSSL_VERSION_NUMBER >= 0x30000000L
    EVP_PKEY *dhpkey = create_dh_pkey();
    if (dhpkey == NULL) {
        SSL_CTX_free(ctx);
        return NULL;
    }
    // SSL_CTX_set0_tmp_dh_pkey takes ownership, don't free
    if (SSL_CTX_set0_tmp_dh_pkey(ctx, dhpkey) != 1) {
        EVP_PKEY_free(dhpkey);
        SSL_CTX_free(ctx);
        return NULL;
    }
#else
    DH *dh = create_dh_params();
    if (dh == NULL) {
        SSL_CTX_free(ctx);
        return NULL;
    }
    SSL_CTX_set_tmp_dh(ctx, dh);
    DH_free(dh);
#endif

    SSL_CTX_set_ecdh_auto(ctx, 1);
    SSL_CTX_set_verify(ctx, SSL_VERIFY_NONE, NULL);
    SSL_CTX_set_session_cache_mode(ctx, SSL_SESS_CACHE_SERVER);

    // kTLS (kernel TLS) disabled for now - causes handshake issues on ARM
    // The devcrypto hardware acceleration is already active for bulk encryption.
    // kTLS would add zero-copy scatter-gather but isn't critical for performance.
    // TODO: Investigate kTLS handshake issues (SSL_ERROR_WANT_READ loop too slow)
#if 0 && !KTLS_NOT_AVAILABLE
    SSL_CTX_set_options(ctx, SSL_OP_ENABLE_KTLS);
    if (openssl_log_level <= 1) fprintf(stderr, "INFO: OpenSSL crypto/tls: kTLS (kernel TLS) ENABLED in SSL context\n");
#else
    if (openssl_log_level <= 1) fprintf(stderr, "INFO: OpenSSL crypto/tls: kTLS disabled (hardware crypto via devcrypto still active)\n");
#endif

    return ctx;
}

static char* get_ssl_error_string() {
    char *buf = (char*)malloc(SSL_ERROR_BUF_SIZE);
    buf[0] = '\0';
    int pos = 0;
    unsigned long err;
    while ((err = ERR_get_error()) != 0 && pos < SSL_ERROR_MAX_POS) {
        if (pos > 0) pos += snprintf(buf + pos, SSL_ERROR_BUF_SIZE - pos, "; ");
        char tmp[SSL_ERROR_TMP_SIZE];
        ERR_error_string_n(err, tmp, sizeof(tmp));
        pos += snprintf(buf + pos, SSL_ERROR_BUF_SIZE - pos, "%s", tmp);
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

// Check if kTLS is enabled for sending on this SSL connection
static int is_ktls_send_enabled(SSL *ssl) {
#if HAS_KTLS_FUNCTIONS
    return BIO_get_ktls_send(SSL_get_wbio(ssl));
#else
    (void)ssl;
    return 0;
#endif
}

// Check if kTLS is enabled for receiving on this SSL connection
static int is_ktls_recv_enabled(SSL *ssl) {
#if HAS_KTLS_FUNCTIONS
    return BIO_get_ktls_recv(SSL_get_rbio(ssl));
#else
    (void)ssl;
    return 0;
#endif
}

// Get the underlying socket fd for scatter-gather writes
static int get_ssl_fd(SSL *ssl) {
    return SSL_get_fd(ssl);
}

// TLS key log callback for Wireshark decryption (NSS key log format).
// SSL_CTX_set_keylog_callback was added in OpenSSL 1.1.1.
// Note: CGO generates the extern prototype from //export goSSLKeyLogCallback
// with signature `void goSSLKeyLogCallback(char* line)` (no const).
// We must match that signature here to avoid "conflicting types" errors.
extern void goSSLKeyLogCallback(char *line);
static void ssl_keylog_callback(const SSL *ssl, const char *line) {
    (void)ssl;
    goSSLKeyLogCallback((char*)line);
}
static void enable_keylog(SSL_CTX *ctx) {
    SSL_CTX_set_keylog_callback(ctx, ssl_keylog_callback);
}
*/
import "C"

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"sync"
	"time"
	"unsafe"
)

var (
	initOnce sync.Once

	// keyLogWriter is the global writer for TLS key log output (NSS format).
	// Protected by keyLogMu. Set via Config.KeyLogWriter before connections.
	keyLogWriter io.Writer
	keyLogMu     sync.Mutex
)

//export goSSLKeyLogCallback
func goSSLKeyLogCallback(line *C.char) {
	keyLogMu.Lock()
	w := keyLogWriter
	keyLogMu.Unlock()
	if w == nil {
		return
	}
	goLine := C.GoString(line)
	// NSS key log format: one line per secret, terminated by newline
	_, _ = fmt.Fprintln(w, goLine)
}

func initImpl() {
	initOnce.Do(func() {
		C.openssl_init()
	})
}

// SetCLogLevel sets the log level for OpenSSL C code.
// Maps zerolog levels: TRACE=-1, DEBUG=0, INFO=1, WARN=2, ERROR=3, FATAL=4, PANIC=5
func SetCLogLevel(level int) {
	C.openssl_set_log_level(C.int(level))
}

func isHardwareAvailable() bool {
	initImpl()
	return C.is_hw_crypto_enabled() != 0
}

func hardwareEngine() string {
	initImpl()
	name := C.get_hw_crypto_engine_name()
	if name == nil {
		return "none (software)"
	}
	return C.GoString(name)
}

// opensslConn wraps an OpenSSL connection to implement the Conn interface.
type opensslConn struct {
	ssl     *C.SSL
	ctx     *C.SSL_CTX
	conn    net.Conn
	fd      int
	closed  bool
	mu      sync.Mutex // protects closed flag
	readMu  sync.Mutex // protects SSL_read
	writeMu sync.Mutex // protects SSL_write
}

func (c *opensslConn) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}

	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return 0, os.ErrClosed
	}

	c.readMu.Lock()
	defer c.readMu.Unlock()

	n := C.SSL_read(c.ssl, unsafe.Pointer(&b[0]), C.int(len(b)))

	// Yield to Go scheduler after CGO call to prevent starving other goroutines
	// (e.g., HTTPS TLS handshakes) on single-core ARM systems
	runtime.Gosched()

	if n <= 0 {
		sslErr := C.SSL_get_error(c.ssl, n)
		switch sslErr {
		case C.SSL_ERROR_ZERO_RETURN:
			// Peer sent TLS close_notify — clean shutdown
			return 0, io.EOF
		case C.SSL_ERROR_SYSCALL:
			// Peer disconnected without TLS shutdown alert (e.g., TCP RST, process killed).
			// When the OpenSSL error queue is empty, get_ssl_error_string() returns "unknown error".
			errStr := C.get_ssl_error_string()
			errMsg := C.GoString(errStr)
			C.free(unsafe.Pointer(errStr))
			if errMsg == "unknown error" {
				return 0, io.EOF
			}
			return 0, fmt.Errorf("SSL read error: %s", errMsg)
		default:
			errStr := C.get_ssl_error_string()
			defer C.free(unsafe.Pointer(errStr))
			return 0, fmt.Errorf("SSL read error: %s", C.GoString(errStr))
		}
	}

	return int(n), nil
}

func (c *opensslConn) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}

	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return 0, os.ErrClosed
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	n := C.SSL_write(c.ssl, unsafe.Pointer(&b[0]), C.int(len(b)))

	// Yield to Go scheduler after CGO call to prevent starving other goroutines
	// (e.g., HTTPS TLS handshakes) on single-core ARM systems
	runtime.Gosched()

	if n <= 0 {
		sslErr := C.SSL_get_error(c.ssl, n)
		switch sslErr {
		case C.SSL_ERROR_ZERO_RETURN:
			return 0, io.EOF
		case C.SSL_ERROR_SYSCALL:
			errStr := C.get_ssl_error_string()
			errMsg := C.GoString(errStr)
			C.free(unsafe.Pointer(errStr))
			if errMsg == "unknown error" {
				return 0, io.EOF
			}
			return 0, fmt.Errorf("SSL write error: %s", errMsg)
		default:
			errStr := C.get_ssl_error_string()
			defer C.free(unsafe.Pointer(errStr))
			return 0, fmt.Errorf("SSL write error: %s", C.GoString(errStr))
		}
	}

	return int(n), nil
}

func (c *opensslConn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	// Close underlying connection first to unblock any in-progress
	// SSL_read/SSL_write blocking on the fd. After this, blocking
	// operations will return with an error, allowing us to acquire
	// the read/write locks without deadlocking.
	c.conn.Close()

	c.readMu.Lock()
	c.writeMu.Lock()
	defer c.readMu.Unlock()
	defer c.writeMu.Unlock()

	C.SSL_free(c.ssl)
	if c.ctx != nil {
		C.SSL_CTX_free(c.ctx)
	}

	return nil
}

func (c *opensslConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *opensslConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// SetDeadline sets both read and write deadlines using kernel socket timeouts.
// Go's net.Conn.SetDeadline relies on non-blocking I/O + epoll, which doesn't work
// here because the fd is in blocking mode for OpenSSL (see set_blocking).
func (c *opensslConn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}

// SetReadDeadline sets SO_RCVTIMEO on the underlying socket.
// This provides a kernel-level read timeout for blocking SSL_read calls.
// A zero time clears the timeout (infinite wait).
func (c *opensslConn) SetReadDeadline(t time.Time) error {
	sec, usec := deadlineToTimeval(t)
	if C.set_recv_timeout(C.int(c.fd), C.long(sec), C.long(usec)) != 0 {
		return fmt.Errorf("setsockopt SO_RCVTIMEO failed: errno=%d", C.get_errno())
	}
	return nil
}

// SetWriteDeadline sets SO_SNDTIMEO on the underlying socket.
// This provides a kernel-level write timeout for blocking SSL_write calls.
// A zero time clears the timeout (infinite wait).
func (c *opensslConn) SetWriteDeadline(t time.Time) error {
	sec, usec := deadlineToTimeval(t)
	if C.set_send_timeout(C.int(c.fd), C.long(sec), C.long(usec)) != 0 {
		return fmt.Errorf("setsockopt SO_SNDTIMEO failed: errno=%d", C.get_errno())
	}
	return nil
}

// deadlineToTimeval converts a Go deadline (absolute time) to seconds and microseconds
// for SO_SNDTIMEO/SO_RCVTIMEO socket options which expect a duration.
func deadlineToTimeval(t time.Time) (sec, usec C.long) {
	if t.IsZero() {
		return 0, 0 // Clear timeout (infinite wait)
	}
	d := time.Until(t)
	if d <= 0 {
		return 0, 1000 // Deadline already passed: 1ms minimal timeout
	}
	return C.long(d / time.Second), C.long((d % time.Second) / time.Microsecond)
}

func (c *opensslConn) GetCipherName() string {
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

func (c *opensslConn) GetProtocolVersion() string {
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

func (c *opensslConn) IsHardwareAccelerated() bool {
	return isHardwareAvailable()
}

// IsKTLSSendEnabled returns true if kernel TLS is enabled for sending.
// When enabled, writes can use scatter-gather I/O for zero-copy encryption.
func (c *opensslConn) IsKTLSSendEnabled() bool {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return false
	}

	return C.is_ktls_send_enabled(c.ssl) != 0
}

// IsKTLSRecvEnabled returns true if kernel TLS is enabled for receiving.
func (c *opensslConn) IsKTLSRecvEnabled() bool {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return false
	}

	return C.is_ktls_recv_enabled(c.ssl) != 0
}

// GetFD returns the underlying socket file descriptor.
// This can be used for scatter-gather writes when kTLS is enabled.
func (c *opensslConn) GetFD() int {
	return c.fd
}

func serverImpl(conn net.Conn, config *Config) (Conn, error) {
	initImpl()

	fd, err := getTCPFd(conn)
	if err != nil {
		return nil, err
	}

	// Determine certificate and key
	var certPEM, keyPEM string
	if config.GetCertificate != nil {
		cert, err := config.GetCertificate(nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get certificate: %w", err)
		}
		if cert == nil {
			return nil, fmt.Errorf("GetCertificate returned nil certificate")
		}
		certPEM, keyPEM, err = certToPEM(cert)
		if err != nil {
			return nil, fmt.Errorf("failed to encode certificate: %w", err)
		}
	} else {
		certPEM = config.CertPEM
		keyPEM = config.KeyPEM
	}

	cipherList := cipherSuitesX509String()
	if certPEM == "" || keyPEM == "" {
		return nil, fmt.Errorf("X.509 mode requires certificate and key")
	}

	minVersion := config.MinVersion
	if minVersion == 0 {
		minVersion = tls.VersionTLS12
	}
	maxVersion := config.MaxVersion
	if maxVersion == 0 {
		maxVersion = tls.VersionTLS13
	}

	certC := C.CString(certPEM)
	keyC := C.CString(keyPEM)
	cipherC := C.CString(cipherList)
	defer C.free(unsafe.Pointer(certC))
	defer C.free(unsafe.Pointer(keyC))
	defer C.free(unsafe.Pointer(cipherC))
	ctx := C.create_ssl_ctx(1, certC, keyC, cipherC,
		C.int(opensslVersion(minVersion)), C.int(opensslVersion(maxVersion)))

	if ctx == nil {
		errStr := C.get_ssl_error_string()
		defer C.free(unsafe.Pointer(errStr))
		return nil, fmt.Errorf("failed to create SSL context: %s", C.GoString(errStr))
	}

	if config.KeyLogWriter != nil {
		keyLogMu.Lock()
		keyLogWriter = config.KeyLogWriter
		keyLogMu.Unlock()
		C.enable_keylog(ctx)
	}

	ssl, err := newSSLFromCtx(ctx, fd)
	if err != nil {
		C.SSL_CTX_free(ctx)
		return nil, err
	}

	if err := sslAccept(ssl); err != nil {
		C.SSL_free(ssl)
		C.SSL_CTX_free(ctx)
		return nil, err
	}

	logHandshake(ssl)

	return &opensslConn{
		ssl:  ssl,
		ctx:  ctx,
		conn: conn,
		fd:   fd,
	}, nil
}

// opensslVersion converts Go TLS version constants to OpenSSL constants.
func opensslVersion(v uint16) int {
	switch v {
	case tls.VersionTLS10:
		return 0x0301
	case tls.VersionTLS11:
		return 0x0302
	case tls.VersionTLS12:
		return 0x0303
	case tls.VersionTLS13:
		return 0x0304
	default:
		return 0x0303 // Default to TLS 1.2
	}
}

// certToPEM converts a tls.Certificate to PEM-encoded strings.
func certToPEM(cert *tls.Certificate) (certPEM, keyPEM string, err error) {
	if len(cert.Certificate) == 0 {
		return "", "", fmt.Errorf("certificate has no data")
	}

	// Build certificate PEM (including chain)
	var certBuilder []byte
	for _, certDER := range cert.Certificate {
		block := "-----BEGIN CERTIFICATE-----\n"
		block += base64Encode(certDER)
		block += "\n-----END CERTIFICATE-----\n"
		certBuilder = append(certBuilder, []byte(block)...)
	}
	certPEM = string(certBuilder)

	// Get private key PEM
	if cert.PrivateKey == nil {
		return "", "", fmt.Errorf("certificate has no private key")
	}

	keyDER, err := encodePrivateKey(cert.PrivateKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to encode private key: %w", err)
	}

	keyPEM = "-----BEGIN PRIVATE KEY-----\n"
	keyPEM += base64Encode(keyDER)
	keyPEM += "\n-----END PRIVATE KEY-----\n"

	return certPEM, keyPEM, nil
}

// base64Encode encodes data to base64 with PEM line wrapping (64 chars per line).
func base64Encode(data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)

	const lineLength = 64
	var result []byte
	for i := 0; i < len(encoded); i += lineLength {
		end := i + lineLength
		if end > len(encoded) {
			end = len(encoded)
		}
		result = append(result, encoded[i:end]...)
		if end < len(encoded) {
			result = append(result, '\n')
		}
	}
	return string(result)
}

// encodePrivateKey encodes a private key to PKCS#8 DER format.
func encodePrivateKey(key any) ([]byte, error) {
	return doMarshalPKCS8PrivateKey(key)
}

// --- Helper functions shared by serverImpl and opensslSharedCtx ---

// getTCPFd extracts the file descriptor from a TCP connection.
func getTCPFd(conn net.Conn) (int, error) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return 0, fmt.Errorf("connection must be TCP")
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("failed to get raw connection: %w", err)
	}

	var fd int
	err = rawConn.Control(func(fdPtr uintptr) {
		fd = int(fdPtr)
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get fd: %w", err)
	}
	return fd, nil
}

// newSSLFromCtx creates an SSL object from an existing SSL_CTX, sets the fd
// to blocking mode, and binds it. Caller must SSL_free on error.
func newSSLFromCtx(ctx *C.SSL_CTX, fd int) (*C.SSL, error) {
	ssl := C.SSL_new(ctx)
	if ssl == nil {
		errStr := C.get_ssl_error_string()
		defer C.free(unsafe.Pointer(errStr))
		return nil, fmt.Errorf("failed to create SSL connection: %s", C.GoString(errStr))
	}

	if C.set_blocking(C.int(fd)) != 0 {
		C.SSL_free(ssl)
		return nil, fmt.Errorf("failed to set fd to blocking mode")
	}

	if C.SSL_set_fd(ssl, C.int(fd)) != 1 {
		C.SSL_free(ssl)
		errStr := C.get_ssl_error_string()
		defer C.free(unsafe.Pointer(errStr))
		return nil, fmt.Errorf("failed to set SSL fd: %s", C.GoString(errStr))
	}

	return ssl, nil
}

// sslAccept performs the TLS handshake and returns a descriptive error on failure.
// The caller is responsible for freeing the SSL on error.
func sslAccept(ssl *C.SSL) error {
	ret := C.SSL_accept(ssl)
	if ret == 1 {
		return nil
	}

	sslErr := C.SSL_get_error(ssl, ret)
	errStr := C.get_ssl_error_string()
	errMsg := C.GoString(errStr)
	C.free(unsafe.Pointer(errStr))

	savedErrno := C.get_errno()

	var details string
	switch sslErr {
	case C.SSL_ERROR_NONE:
		details = "SSL_ERROR_NONE"
	case C.SSL_ERROR_SSL:
		details = "SSL_ERROR_SSL"
	case C.SSL_ERROR_WANT_READ:
		details = "SSL_ERROR_WANT_READ (socket not ready)"
	case C.SSL_ERROR_WANT_WRITE:
		details = "SSL_ERROR_WANT_WRITE (socket not ready)"
	case C.SSL_ERROR_SYSCALL:
		if savedErrno == 0 {
			details = "SSL_ERROR_SYSCALL (EOF/connection closed)"
		} else {
			details = fmt.Sprintf("SSL_ERROR_SYSCALL errno=%d", savedErrno)
		}
	case C.SSL_ERROR_ZERO_RETURN:
		details = "SSL_ERROR_ZERO_RETURN (clean shutdown)"
	default:
		details = fmt.Sprintf("SSL_ERROR_%d", sslErr)
	}

	if errMsg == "" || errMsg == "unknown error" {
		return fmt.Errorf("TLS handshake failed: %s", details)
	}
	return fmt.Errorf("TLS handshake failed: %s (%s)", errMsg, details)
}

// logHandshake logs the TLS handshake result at INFO level.
func logHandshake(ssl *C.SSL) {
	if C.openssl_get_log_level() <= 1 {
		ktlsSend := C.is_ktls_send_enabled(ssl) != 0
		ktlsRecv := C.is_ktls_recv_enabled(ssl) != 0
		cipher := C.SSL_get_current_cipher(ssl)
		var cipherName string
		if cipher != nil {
			cipherName = C.GoString(C.SSL_CIPHER_get_name(cipher))
		}
		tlsVersion := C.GoString(C.SSL_get_version(ssl))
		fmt.Fprintf(os.Stderr, "INFO: OpenSSL TLS handshake complete: version=%s cipher=%s kTLS_send=%v kTLS_recv=%v ktls_available=%d\n",
			tlsVersion, cipherName, ktlsSend, ktlsRecv, 1-C.KTLS_NOT_AVAILABLE)
	}
}

// --- Shared SSL_CTX for Listener (multi-connection reuse) ---

// opensslSharedCtx caches an SSL_CTX for reuse across connections. The CTX is
// lazily created on the first connection and automatically recreated when the
// certificate changes (detected by comparing the leaf cert DER bytes).
// SSL_CTX's internal session cache provides TLS session resumption.
type opensslSharedCtx struct {
	mu      sync.RWMutex
	ctx     *C.SSL_CTX
	certDER []byte // cached leaf cert DER for change detection
	config  *Config
}

func newSharedCtxImpl(config *Config) sharedCtx {
	return &opensslSharedCtx{config: config}
}

// getOrCreateCtx returns a cached SSL_CTX, creating or replacing it as needed.
func (s *opensslSharedCtx) getOrCreateCtx() (*C.SSL_CTX, error) {
	if s.config.GetCertificate == nil {
		return nil, fmt.Errorf("GetCertificate not set")
	}

	cert, err := s.config.GetCertificate(nil)
	if err != nil {
		return nil, err
	}
	if cert == nil || len(cert.Certificate) == 0 {
		return nil, fmt.Errorf("no certificate available")
	}

	leafDER := cert.Certificate[0]

	// Fast path: cert unchanged, reuse CTX.
	s.mu.RLock()
	if s.ctx != nil && bytes.Equal(s.certDER, leafDER) {
		ctx := s.ctx
		s.mu.RUnlock()
		return ctx, nil
	}
	s.mu.RUnlock()

	// Slow path: cert changed (or first use), (re)create CTX.
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock.
	if s.ctx != nil && bytes.Equal(s.certDER, leafDER) {
		return s.ctx, nil
	}

	certPEM, keyPEM, err := certToPEM(cert)
	if err != nil {
		return nil, err
	}

	cipherList := cipherSuitesX509String()

	minVersion := s.config.MinVersion
	if minVersion == 0 {
		minVersion = tls.VersionTLS12
	}
	maxVersion := s.config.MaxVersion
	if maxVersion == 0 {
		maxVersion = tls.VersionTLS13
	}

	certC := C.CString(certPEM)
	keyC := C.CString(keyPEM)
	cipherC := C.CString(cipherList)
	defer C.free(unsafe.Pointer(certC))
	defer C.free(unsafe.Pointer(keyC))
	defer C.free(unsafe.Pointer(cipherC))

	newCtx := C.create_ssl_ctx(1, certC, keyC, cipherC,
		C.int(opensslVersion(minVersion)), C.int(opensslVersion(maxVersion)))
	if newCtx == nil {
		errStr := C.get_ssl_error_string()
		defer C.free(unsafe.Pointer(errStr))
		return nil, fmt.Errorf("failed to create SSL context: %s", C.GoString(errStr))
	}

	if s.config.KeyLogWriter != nil {
		keyLogMu.Lock()
		keyLogWriter = s.config.KeyLogWriter
		keyLogMu.Unlock()
		C.enable_keylog(newCtx)
	}

	// SSL_CTX uses reference counting: existing SSL objects hold their own
	// reference to the old CTX, so it stays alive until they are freed.
	oldCtx := s.ctx
	s.ctx = newCtx
	s.certDER = append([]byte(nil), leafDER...)

	if oldCtx != nil {
		C.SSL_CTX_free(oldCtx)
	}

	return newCtx, nil
}

func (s *opensslSharedCtx) serverConn(conn net.Conn) (Conn, error) {
	initImpl()

	ctx, err := s.getOrCreateCtx()
	if err != nil {
		return nil, err
	}

	fd, err := getTCPFd(conn)
	if err != nil {
		return nil, err
	}

	ssl, err := newSSLFromCtx(ctx, fd)
	if err != nil {
		return nil, err
	}

	if err := sslAccept(ssl); err != nil {
		C.SSL_free(ssl)
		return nil, err
	}

	logHandshake(ssl)

	return &opensslConn{
		ssl:  ssl,
		ctx:  nil, // shared context manages CTX lifecycle
		conn: conn,
		fd:   fd,
	}, nil
}

func (s *opensslSharedCtx) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx != nil {
		C.SSL_CTX_free(s.ctx)
		s.ctx = nil
		s.certDER = nil
	}
}
