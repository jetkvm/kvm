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
	"net"
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

// Listener wraps a net.Listener to provide TLS connections using hardware
// acceleration when available. Each accepted connection is upgraded to TLS.
type Listener struct {
	inner  net.Listener
	config *Config
}

// NewListener creates a TLS listener that wraps the given net.Listener.
// All accepted connections will be upgraded to TLS using the provided config.
// On ARM Linux with CGO, this uses OpenSSL with hardware acceleration.
func NewListener(inner net.Listener, config *Config) *Listener {
	// Initialize TLS subsystem early to avoid delay on first connection
	Init()
	return &Listener{
		inner:  inner,
		config: config,
	}
}

// Accept accepts a connection and performs the TLS handshake.
// If the TLS handshake fails (e.g., client rejects certificate, time not synced),
// the connection is closed and Accept retries with the next connection.
// This prevents http.Server.Serve() from exiting on transient TLS errors.
func (l *Listener) Accept() (net.Conn, error) {
	for {
		conn, err := l.inner.Accept()
		if err != nil {
			// Listener error (e.g., closed) - propagate to caller
			return nil, err
		}

		tlsConn, err := Server(conn, l.config)
		if err != nil {
			// TLS handshake failed - close this connection and try the next one.
			// Common causes:
			// - Client rejected our certificate (self-signed, untrusted CA)
			// - Certificate not available (time not synced for self-signed)
			// - Client disconnected during handshake
			// - Protocol mismatch
			// These are all transient per-connection errors, not listener failures.
			conn.Close()
			continue
		}

		return tlsConn, nil
	}
}

// Close closes the underlying listener.
func (l *Listener) Close() error {
	return l.inner.Close()
}

// Addr returns the listener's network address.
func (l *Listener) Addr() net.Addr {
	return l.inner.Addr()
}
