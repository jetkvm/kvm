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

static void openssl_init() {
    if (hw_crypto_initialized) return;
    hw_crypto_initialized = 1;

    OPENSSL_init_ssl(OPENSSL_INIT_LOAD_SSL_STRINGS | OPENSSL_INIT_LOAD_CRYPTO_STRINGS, NULL);
    OPENSSL_init_crypto(OPENSSL_INIT_ENGINE_ALL_BUILTIN, NULL);

    devcrypto_engine = ENGINE_by_id("devcrypto");
    if (devcrypto_engine != NULL) {
        if (ENGINE_init(devcrypto_engine)) {
            ENGINE_set_default_ciphers(devcrypto_engine);
            ENGINE_set_default_digests(devcrypto_engine);
        } else {
            ENGINE_free(devcrypto_engine);
            devcrypto_engine = NULL;
        }
    }

    if (devcrypto_engine == NULL) {
        devcrypto_engine = ENGINE_by_id("afalg");
        if (devcrypto_engine != NULL && ENGINE_init(devcrypto_engine)) {
            ENGINE_set_default_ciphers(devcrypto_engine);
            ENGINE_set_default_digests(devcrypto_engine);
        } else if (devcrypto_engine != NULL) {
            ENGINE_free(devcrypto_engine);
            devcrypto_engine = NULL;
        }
    }
}

static DH* create_dh_params() {
    DH *dh = DH_new();
    if (dh == NULL) return NULL;

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

static SSL_CTX* create_vnc_ssl_ctx(int use_cert, const char* cert_pem, const char* key_pem) {
    SSL_CTX *ctx = SSL_CTX_new(TLS_server_method());
    if (ctx == NULL) return NULL;

    SSL_CTX_set_security_level(ctx, 0);
    SSL_CTX_set_min_proto_version(ctx, TLS1_VERSION);
    SSL_CTX_set_max_proto_version(ctx, TLS1_2_VERSION);

    if (use_cert) {
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

        if (SSL_CTX_use_certificate_chain_file(ctx, cert_pem) != 1) {
            SSL_CTX_free(ctx);
            return NULL;
        }
        if (SSL_CTX_use_PrivateKey_file(ctx, key_pem, SSL_FILETYPE_PEM) != 1) {
            SSL_CTX_free(ctx);
            return NULL;
        }
    } else {
        SSL_CTX_set_cipher_list(ctx,
            "ADH-AES128-SHA:"
            "ADH-AES256-SHA:"
            "AECDH-AES128-SHA:"
            "AECDH-AES256-SHA:"
            "ADH-AES128-SHA256:"
            "ADH-AES256-SHA256");
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
	mu     sync.Mutex
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

func (c *TLSConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *TLSConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *TLSConn) GetCipherName() string {
	cipher := C.SSL_get_current_cipher(c.ssl)
	if cipher == nil {
		return "unknown"
	}
	return C.GoString(C.SSL_CIPHER_get_name(cipher))
}

func (c *TLSConn) GetProtocolVersion() string {
	return C.GoString(C.SSL_get_version(c.ssl))
}
