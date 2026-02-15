// Package tls provides hardware-accelerated TLS for VNC and RDP using OpenSSL
// with the RV1106's devcrypto engine on ARM Linux.
//
// On ARM Linux with CGO enabled, this package uses OpenSSL with hardware
// acceleration via the devcrypto or afalg engines for AES-GCM operations.
// On other platforms, it falls back to Go's software crypto/tls.
//
// Usage:
//
//	config := tls.DefaultConfig()
//	config.GetCertificate = getCertificate
//	tlsConn, err := tls.Server(conn, config)
package tls

import (
	"crypto/tls"
	"io"
	"net"
	"sync"
)

// Mode specifies the TLS mode for VNC connections.
type Mode int

const (
	// ModeX509 uses X.509 certificates for TLS (VNC and RDP).
	ModeX509 Mode = iota
)

// Conn represents a TLS connection with hardware acceleration info.
type Conn interface {
	net.Conn
	// GetCipherName returns the name of the negotiated cipher suite.
	GetCipherName() string
	// GetProtocolVersion returns the negotiated TLS version string.
	GetProtocolVersion() string
	// IsHardwareAccelerated returns true if hardware crypto is being used.
	IsHardwareAccelerated() bool
}

// KTLSConn extends Conn with kernel TLS (kTLS) capabilities.
// When kTLS is enabled, the kernel handles TLS encryption, enabling:
// - Zero-copy scatter-gather writes via sendmsg()
// - Reduced context switches (encryption in kernel space)
// - Hardware crypto offload via kernel crypto API (e.g., RV1106 crypto accelerator)
type KTLSConn interface {
	Conn
	// IsKTLSSendEnabled returns true if kernel TLS is enabled for sending.
	IsKTLSSendEnabled() bool
	// IsKTLSRecvEnabled returns true if kernel TLS is enabled for receiving.
	IsKTLSRecvEnabled() bool
	// GetFD returns the underlying socket file descriptor for scatter-gather I/O.
	GetFD() int
}

// Config holds configuration for TLS connections.
type Config struct {
	// Mode specifies the TLS certificate mode (currently only ModeX509).
	Mode Mode

	// CertPEM is the PEM-encoded certificate chain (for ModeX509).
	CertPEM string

	// KeyPEM is the PEM-encoded private key (for ModeX509).
	KeyPEM string

	// GetCertificate returns a certificate for the given ClientHelloInfo.
	// If set, this is used instead of CertPEM/KeyPEM.
	// This allows dynamic certificate selection (e.g., SNI, ACME).
	GetCertificate func(*tls.ClientHelloInfo) (*tls.Certificate, error)

	// MinVersion is the minimum TLS version (default: TLS 1.2).
	MinVersion uint16

	// MaxVersion is the maximum TLS version (default: TLS 1.3, or 1.2 for RDP).
	MaxVersion uint16

	// KeyLogWriter optionally receives TLS master secrets in NSS key log format.
	// This enables Wireshark decryption of captured TLS traffic.
	// Set to a file writer (e.g., os.OpenFile("sslkeys.log", ...)) for debugging.
	// Must be nil in production.
	KeyLogWriter io.Writer
}

// Server upgrades a net.Conn to a TLS server connection.
// It automatically uses hardware acceleration on ARM Linux if available,
// falling back to Go's crypto/tls otherwise.
func Server(conn net.Conn, config *Config) (Conn, error) {
	return serverImpl(conn, config)
}

// IsHardwareAvailable returns true if hardware crypto acceleration is available.
func IsHardwareAvailable() bool {
	return isHardwareAvailable()
}

// HardwareEngine returns the name of the hardware crypto engine in use,
// or "none (software)" if hardware acceleration is not available.
func HardwareEngine() string {
	return hardwareEngine()
}

// Init initializes the TLS subsystem.
// This is called automatically by Server(), but can be called early
// to check hardware crypto availability at startup.
func Init() {
	initImpl()
}

// sharedCtx is a platform-specific shared TLS context that enables efficient
// multi-connection TLS serving: shared session cache, cached certificates,
// and amortized context setup costs.
type sharedCtx interface {
	// serverConn creates a TLS server connection using the shared context.
	serverConn(conn net.Conn) (Conn, error)
	// close releases any cached resources.
	close()
}

// Listener wraps a net.Listener to provide TLS connections using hardware
// acceleration when available. TLS handshakes run concurrently so that a slow
// handshake on one connection does not block acceptance of new connections.
// A shared TLS context is cached across connections for session resumption
// and to avoid per-connection setup overhead.
type Listener struct {
	inner  net.Listener
	config *Config
	connCh chan acceptResult
	once   sync.Once
	done   chan struct{}
	wg     sync.WaitGroup // tracks in-flight handshake goroutines
	sctx   sharedCtx      // cached TLS context shared across connections
}

type acceptResult struct {
	conn net.Conn
	err  error
}

// NewListener creates a TLS listener that wraps the given net.Listener.
// All accepted connections will be upgraded to TLS using the provided config.
// On ARM Linux with CGO, this uses OpenSSL with hardware acceleration.
// A shared TLS context is created and reused across connections for
// session resumption and reduced per-connection overhead.
func NewListener(inner net.Listener, config *Config) *Listener {
	// Initialize TLS subsystem early to avoid delay on first connection
	Init()
	return &Listener{
		inner:  inner,
		config: config,
		connCh: make(chan acceptResult, 16),
		done:   make(chan struct{}),
		sctx:   newSharedCtxImpl(config),
	}
}

// acceptLoop runs in its own goroutine, accepting TCP connections from the
// inner listener and spawning a goroutine per connection for the TLS handshake.
// Completed connections are delivered via connCh; handshake failures are silently
// retried (the failed connection is closed).
func (l *Listener) acceptLoop() {
	for {
		conn, err := l.inner.Accept()
		if err != nil {
			// Listener closed or fatal error — propagate to caller via channel.
			select {
			case l.connCh <- acceptResult{nil, err}:
			case <-l.done:
			}
			return
		}

		l.wg.Add(1)
		go l.handshake(conn)
	}
}

func (l *Listener) handshake(conn net.Conn) {
	defer l.wg.Done()

	tlsConn, err := l.sctx.serverConn(conn)
	if err != nil {
		conn.Close()
		return
	}

	select {
	case l.connCh <- acceptResult{tlsConn, nil}:
	case <-l.done:
		tlsConn.Close()
	}
}

// Accept returns the next TLS-upgraded connection. TLS handshakes run
// concurrently in the background, so Accept never blocks on a single slow
// handshake.
func (l *Listener) Accept() (net.Conn, error) {
	l.once.Do(func() { go l.acceptLoop() })

	select {
	case result := <-l.connCh:
		return result.conn, result.err
	case <-l.done:
		return nil, net.ErrClosed
	}
}

// Close closes the underlying listener, waits for in-flight handshakes to
// finish, drains buffered connections, and releases the shared TLS context.
func (l *Listener) Close() error {
	select {
	case <-l.done:
		return l.inner.Close()
	default:
		close(l.done)
	}

	// Close the inner listener first — unblocks acceptLoop and prevents
	// new TCP connections from being accepted.
	err := l.inner.Close()

	// Wait for all in-flight handshake goroutines to finish. Each will
	// either send to connCh or see done closed and close its connection.
	l.wg.Wait()

	// Drain any successfully-handshaked connections that were buffered in
	// connCh but never consumed by Accept().
	for {
		select {
		case result := <-l.connCh:
			if result.conn != nil {
				result.conn.Close()
			}
		default:
			// Release the shared TLS context (frees cached SSL_CTX on OpenSSL).
			l.sctx.close()
			return err
		}
	}
}

// Addr returns the listener's network address.
func (l *Listener) Addr() net.Addr {
	return l.inner.Addr()
}
