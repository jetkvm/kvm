//go:build linux && arm

package vnctls

/*
#include <openssl/ssl.h>
#include <openssl/err.h>
#include <openssl/dh.h>
#include <openssl/bn.h>
#include <openssl/engine.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>
#include <stdio.h>
#include <fcntl.h>
#include <unistd.h>
#include <sys/socket.h>
#include <sys/select.h>

// Set file descriptor to blocking mode (Go uses non-blocking sockets)
static int set_blocking(int fd) {
    int flags = fcntl(fd, F_GETFL, 0);
    if (flags == -1) {
        fprintf(stderr, "VNC-TLS: fcntl F_GETFL failed: %s\n", strerror(errno));
        return -1;
    }
    flags &= ~O_NONBLOCK;
    if (fcntl(fd, F_SETFL, flags) == -1) {
        fprintf(stderr, "VNC-TLS: fcntl F_SETFL failed: %s\n", strerror(errno));
        return -1;
    }
    fprintf(stderr, "VNC-TLS: set fd %d to blocking mode\n", fd);
    return 0;
}

// Check if data is available on fd using select with timeout
static int check_fd_readable(int fd, int timeout_ms) {
    fd_set readfds;
    struct timeval tv;

    FD_ZERO(&readfds);
    FD_SET(fd, &readfds);

    tv.tv_sec = timeout_ms / 1000;
    tv.tv_usec = (timeout_ms % 1000) * 1000;

    int ret = select(fd + 1, &readfds, NULL, NULL, &tv);
    if (ret < 0) {
        fprintf(stderr, "VNC-TLS: select failed: %s\n", strerror(errno));
        return -1;
    }
    if (ret == 0) {
        fprintf(stderr, "VNC-TLS: fd %d not readable (timeout after %dms)\n", fd, timeout_ms);
        return 0;
    }
    fprintf(stderr, "VNC-TLS: fd %d is readable\n", fd);
    return 1;
}

// Peek at socket to see what data is available
static void peek_socket_data(int fd) {
    unsigned char buf[64];
    ssize_t n = recv(fd, buf, sizeof(buf), MSG_PEEK);
    if (n < 0) {
        fprintf(stderr, "VNC-TLS: peek failed: %s (errno=%d)\n", strerror(errno), errno);
    } else if (n == 0) {
        fprintf(stderr, "VNC-TLS: peek returned 0 (EOF/connection closed)\n");
    } else {
        fprintf(stderr, "VNC-TLS: peek got %zd bytes: ", n);
        for (ssize_t i = 0; i < n && i < 32; i++) {
            fprintf(stderr, "%02x ", buf[i]);
        }
        fprintf(stderr, "\n");
        // Check for TLS ClientHello signature
        if (n >= 5 && buf[0] == 0x16 && buf[1] == 0x03) {
            fprintf(stderr, "VNC-TLS: looks like TLS ClientHello (record type=0x16, version=0x%02x%02x)\n", buf[1], buf[2]);
        }
    }
}

// Initialize OpenSSL library with hardware crypto acceleration
// Attempts to load devcrypto engine for Rockchip hardware AES/SHA acceleration
static int hw_crypto_initialized = 0;
static ENGINE *devcrypto_engine = NULL;

static void openssl_init() {
    if (hw_crypto_initialized) return;
    hw_crypto_initialized = 1;

    // Initialize OpenSSL
    OPENSSL_init_ssl(OPENSSL_INIT_LOAD_SSL_STRINGS | OPENSSL_INIT_LOAD_CRYPTO_STRINGS, NULL);

    // Load engine support
    OPENSSL_init_crypto(OPENSSL_INIT_ENGINE_ALL_BUILTIN, NULL);

    // Try to load devcrypto engine for hardware acceleration
    // This uses /dev/crypto which is backed by Rockchip crypto hardware
    devcrypto_engine = ENGINE_by_id("devcrypto");
    if (devcrypto_engine != NULL) {
        if (ENGINE_init(devcrypto_engine)) {
            // Set as default for symmetric ciphers and digests
            ENGINE_set_default_ciphers(devcrypto_engine);
            ENGINE_set_default_digests(devcrypto_engine);
            fprintf(stderr, "VNC-TLS: Hardware crypto acceleration enabled (devcrypto engine)\n");
        } else {
            fprintf(stderr, "VNC-TLS: Failed to init devcrypto engine: %s\n", ERR_reason_error_string(ERR_get_error()));
            ENGINE_free(devcrypto_engine);
            devcrypto_engine = NULL;
        }
    } else {
        // Try afalg engine as fallback (kernel AF_ALG interface)
        devcrypto_engine = ENGINE_by_id("afalg");
        if (devcrypto_engine != NULL && ENGINE_init(devcrypto_engine)) {
            ENGINE_set_default_ciphers(devcrypto_engine);
            ENGINE_set_default_digests(devcrypto_engine);
            fprintf(stderr, "VNC-TLS: Hardware crypto acceleration enabled (afalg engine)\n");
        } else {
            fprintf(stderr, "VNC-TLS: No hardware crypto engine available, using software crypto\n");
            if (devcrypto_engine) {
                ENGINE_free(devcrypto_engine);
                devcrypto_engine = NULL;
            }
        }
    }
}

// Create DH parameters for anonymous DH
// Using RFC 2409 1024-bit MODP Group 2 for maximum GnuTLS compatibility
// (Some GnuTLS versions have issues with 2048-bit DH)
static DH* create_dh_params() {
    DH *dh = DH_new();
    if (dh == NULL) return NULL;

    // RFC 2409 1024-bit MODP Group 2 - widely supported by GnuTLS
    static const unsigned char dh1024_p[] = {
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
        0x49, 0x28, 0x66, 0x51, 0xEC, 0xE6, 0x5B, 0x3D,
        0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF
    };
    static const unsigned char dh1024_g[] = { 0x02 };

    BIGNUM *p = BN_bin2bn(dh1024_p, sizeof(dh1024_p), NULL);
    BIGNUM *g = BN_bin2bn(dh1024_g, sizeof(dh1024_g), NULL);

    if (p == NULL || g == NULL || !DH_set0_pqg(dh, p, NULL, g)) {
        BN_free(p);
        BN_free(g);
        DH_free(dh);
        return NULL;
    }

    return dh;
}

// TLS info callback for debugging handshake state
static void ssl_info_callback(const SSL *ssl, int where, int ret) {
    const char *str;
    int w = where & ~SSL_ST_MASK;

    if (w & SSL_ST_CONNECT) str = "SSL_connect";
    else if (w & SSL_ST_ACCEPT) str = "SSL_accept";
    else str = "undefined";

    if (where & SSL_CB_LOOP) {
        fprintf(stderr, "VNC-TLS: %s: %s\n", str, SSL_state_string_long(ssl));
    } else if (where & SSL_CB_ALERT) {
        str = (where & SSL_CB_READ) ? "read" : "write";
        fprintf(stderr, "VNC-TLS: SSL3 alert %s: %s: %s\n",
                str,
                SSL_alert_type_string_long(ret),
                SSL_alert_desc_string_long(ret));
    } else if (where & SSL_CB_EXIT) {
        if (ret == 0) {
            fprintf(stderr, "VNC-TLS: %s: failed in %s\n", str, SSL_state_string_long(ssl));
        } else if (ret < 0) {
            fprintf(stderr, "VNC-TLS: %s: error in %s\n", str, SSL_state_string_long(ssl));
        }
    }
}

// Enable debug callbacks on SSL context
static void enable_ssl_debug(SSL_CTX *ctx) {
    SSL_CTX_set_info_callback(ctx, ssl_info_callback);
}

// Get the cipher list from SSL context as a string for debugging
static char* get_cipher_list_str(SSL_CTX *ctx) {
    SSL *ssl = SSL_new(ctx);
    if (ssl == NULL) return strdup("failed to create SSL");

    char *buf = (char*)malloc(4096);
    buf[0] = '\0';
    int pos = 0;

    STACK_OF(SSL_CIPHER) *ciphers = SSL_get1_supported_ciphers(ssl);
    if (ciphers == NULL) {
        SSL_free(ssl);
        return strdup("no ciphers available");
    }

    int num = sk_SSL_CIPHER_num(ciphers);
    for (int i = 0; i < num && pos < 3900; i++) {
        const SSL_CIPHER *c = sk_SSL_CIPHER_value(ciphers, i);
        if (i > 0) pos += snprintf(buf + pos, 4096 - pos, ", ");
        pos += snprintf(buf + pos, 4096 - pos, "%s", SSL_CIPHER_get_name(c));
    }

    sk_SSL_CIPHER_free(ciphers);
    SSL_free(ssl);
    return buf;
}

// Create SSL context configured for VNC with anonymous DH
static SSL_CTX* create_vnc_ssl_ctx(int use_cert, const char* cert_pem, const char* key_pem) {
    SSL_CTX *ctx = SSL_CTX_new(TLS_server_method());
    if (ctx == NULL) return NULL;

    // Security level 0 required for anonymous ciphers (ADH/AECDH)
    // These ciphers are disabled at security level 1+ in OpenSSL 1.1+
    SSL_CTX_set_security_level(ctx, 0);

    // Allow TLS 1.0 - 1.2 for anonymous DH compatibility
    // IMPORTANT: TLS 1.3 does NOT support anonymous cipher suites (ADH/AECDH)
    // They were deprecated and removed from TLS 1.3 spec
    SSL_CTX_set_min_proto_version(ctx, TLS1_VERSION);
    SSL_CTX_set_max_proto_version(ctx, TLS1_2_VERSION);

    // Configure cipher list - compatible with GnuTLS ANON-DH and ANON-ECDH
    // GnuTLS uses priority string "+ANON-ECDH:+ANON-DH" for anonymous TLS
    if (use_cert) {
        // With certificate: prefer authenticated ciphers, fallback to anonymous
        SSL_CTX_set_cipher_list(ctx,
            "ECDHE-RSA-AES256-GCM-SHA384:"
            "ECDHE-RSA-AES128-GCM-SHA256:"
            "DHE-RSA-AES256-GCM-SHA384:"
            "DHE-RSA-AES128-GCM-SHA256:"
            "AECDH-AES256-SHA:"
            "AECDH-AES128-SHA:"
            "ADH-AES256-GCM-SHA384:"
            "ADH-AES128-GCM-SHA256:"
            "ADH-AES256-SHA256:"
            "ADH-AES128-SHA256:"
            "ADH-AES256-SHA:"
            "ADH-AES128-SHA");

        // Load certificate
        if (SSL_CTX_use_certificate_chain_file(ctx, cert_pem) != 1) {
            SSL_CTX_free(ctx);
            return NULL;
        }
        if (SSL_CTX_use_PrivateKey_file(ctx, key_pem, SSL_FILETYPE_PEM) != 1) {
            SSL_CTX_free(ctx);
            return NULL;
        }
    } else {
        // No certificate: use only anonymous ciphers
        // Prioritize CBC ciphers for GnuTLS compatibility (GnuTLS's ANON-DH)
        // GnuTLS may not support GCM variants with anonymous DH
        SSL_CTX_set_cipher_list(ctx,
            "ADH-AES128-SHA:"
            "ADH-AES256-SHA:"
            "AECDH-AES128-SHA:"
            "AECDH-AES256-SHA:"
            "ADH-DES-CBC3-SHA:"
            "ADH-AES128-SHA256:"
            "ADH-AES256-SHA256");
    }

    // Set DH parameters for anonymous DH (RFC 2409 1024-bit MODP)
    DH *dh = create_dh_params();
    if (dh == NULL) {
        SSL_CTX_free(ctx);
        return NULL;
    }
    SSL_CTX_set_tmp_dh(ctx, dh);
    DH_free(dh);

    // Set up ECDH for anonymous ECDH ciphers
    SSL_CTX_set_ecdh_auto(ctx, 1);

    // No client certificate required
    SSL_CTX_set_verify(ctx, SSL_VERIFY_NONE, NULL);

    // Enable session caching
    SSL_CTX_set_session_cache_mode(ctx, SSL_SESS_CACHE_SERVER);

    // Enable debug callback for TLS handshake tracing
    enable_ssl_debug(ctx);

    // Log available ciphers for debugging
    char *ciphers = get_cipher_list_str(ctx);
    fprintf(stderr, "VNC-TLS: Available ciphers: %s\n", ciphers);
    free(ciphers);

    return ctx;
}

// Get all OpenSSL errors as a string
static char* get_ssl_error_string() {
    char *buf = (char*)malloc(1024);
    buf[0] = '\0';
    int pos = 0;
    unsigned long err;
    while ((err = ERR_get_error()) != 0 && pos < 900) {
        if (pos > 0) {
            pos += snprintf(buf + pos, 1024 - pos, "; ");
        }
        char tmp[256];
        ERR_error_string_n(err, tmp, sizeof(tmp));
        pos += snprintf(buf + pos, 1024 - pos, "%s", tmp);
    }
    if (pos == 0) {
        strcpy(buf, "no error in queue");
    }
    return buf;
}

// Get SSL_get_error result as string
static const char* ssl_error_name(int err) {
    switch (err) {
        case SSL_ERROR_NONE: return "SSL_ERROR_NONE";
        case SSL_ERROR_SSL: return "SSL_ERROR_SSL";
        case SSL_ERROR_WANT_READ: return "SSL_ERROR_WANT_READ";
        case SSL_ERROR_WANT_WRITE: return "SSL_ERROR_WANT_WRITE";
        case SSL_ERROR_WANT_X509_LOOKUP: return "SSL_ERROR_WANT_X509_LOOKUP";
        case SSL_ERROR_SYSCALL: return "SSL_ERROR_SYSCALL";
        case SSL_ERROR_ZERO_RETURN: return "SSL_ERROR_ZERO_RETURN";
        case SSL_ERROR_WANT_CONNECT: return "SSL_ERROR_WANT_CONNECT";
        case SSL_ERROR_WANT_ACCEPT: return "SSL_ERROR_WANT_ACCEPT";
        default: return "UNKNOWN";
    }
}

// Get current errno value
static int get_errno() {
    return errno;
}

// Wrapper to get file descriptor from SSL
static int ssl_get_fd(SSL *ssl) {
    return SSL_get_fd(ssl);
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

// Init initializes OpenSSL (call once at startup)
func Init() {
	initOnce.Do(func() {
		C.openssl_init()
	})
}

// TLSConn wraps an OpenSSL SSL connection
type TLSConn struct {
	ssl    *C.SSL
	ctx    *C.SSL_CTX
	conn   net.Conn
	fd     int
	closed bool
	mu     sync.Mutex
}

// UpgradeToTLS upgrades an existing net.Conn to TLS using OpenSSL
// If useCert is true, it loads certificate from certPEM and keyPEM file paths
// If useCert is false, it uses anonymous DH (no certificate needed)
func UpgradeToTLS(conn net.Conn, useCert bool, certPEM, keyPEM string) (*TLSConn, error) {
	Init()

	// Get the file descriptor directly from the connection
	// We use the original fd without duplication - OpenSSL will take over I/O
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return nil, fmt.Errorf("connection must be TCP")
	}

	// Get the raw fd using SyscallConn
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

	// Create SSL context
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

	// Create SSL connection
	ssl := C.SSL_new(ctx)
	if ssl == nil {
		C.SSL_CTX_free(ctx)
		errStr := C.get_ssl_error_string()
		defer C.free(unsafe.Pointer(errStr))
		return nil, fmt.Errorf("failed to create SSL connection: %s", C.GoString(errStr))
	}

	// Associate with file descriptor
	// Set the fd to blocking mode for OpenSSL
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

	// Debug: wait for and peek at incoming data before TLS handshake
	readable := C.check_fd_readable(C.int(fd), 5000) // 5 second timeout
	if readable <= 0 {
		C.SSL_free(ssl)
		C.SSL_CTX_free(ctx)
		return nil, fmt.Errorf("socket not readable before TLS handshake (readable=%d)", int(readable))
	}
	C.peek_socket_data(C.int(fd))

	// Perform TLS handshake
	ret := C.SSL_accept(ssl)
	if ret != 1 {
		sslErr := C.SSL_get_error(ssl, ret)
		sslErrName := C.GoString(C.ssl_error_name(sslErr))
		errStr := C.get_ssl_error_string()
		errMsg := C.GoString(errStr)
		C.free(unsafe.Pointer(errStr))

		// Get errno for SYSCALL errors
		var errnoStr string
		if sslErr == C.SSL_ERROR_SYSCALL {
			errnoStr = fmt.Sprintf(", errno=%d", C.get_errno())
		}

		C.SSL_free(ssl)
		C.SSL_CTX_free(ctx)
		return nil, fmt.Errorf("TLS handshake failed: %s, ssl_err=%s%s", errMsg, sslErrName, errnoStr)
	}

	return &TLSConn{
		ssl:  ssl,
		ctx:  ctx,
		conn: conn,
		fd:   fd,
	}, nil
}

// Read reads data from the TLS connection
func (c *TLSConn) Read(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return 0, os.ErrClosed
	}

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

// Write writes data to the TLS connection
func (c *TLSConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return 0, os.ErrClosed
	}

	n := C.SSL_write(c.ssl, unsafe.Pointer(&b[0]), C.int(len(b)))
	if n <= 0 {
		errStr := C.get_ssl_error_string()
		defer C.free(unsafe.Pointer(errStr))
		return 0, fmt.Errorf("SSL write error: %s", C.GoString(errStr))
	}

	return int(n), nil
}

// Close closes the TLS connection
func (c *TLSConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	C.SSL_shutdown(c.ssl)
	C.SSL_free(c.ssl)
	C.SSL_CTX_free(c.ctx)

	return c.conn.Close()
}

// LocalAddr returns the local network address
func (c *TLSConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

// RemoteAddr returns the remote network address
func (c *TLSConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// GetCipherName returns the name of the negotiated cipher
func (c *TLSConn) GetCipherName() string {
	cipher := C.SSL_get_current_cipher(c.ssl)
	if cipher == nil {
		return "unknown"
	}
	return C.GoString(C.SSL_CIPHER_get_name(cipher))
}

// GetProtocolVersion returns the negotiated TLS protocol version
func (c *TLSConn) GetProtocolVersion() string {
	return C.GoString(C.SSL_get_version(c.ssl))
}
