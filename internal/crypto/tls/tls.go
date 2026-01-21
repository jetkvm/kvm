// Package tls provides hardware-accelerated TLS for VNC and RDP using OpenSSL
// with the RV1106's devcrypto engine on ARM Linux.
//
// On ARM Linux with CGO enabled, this package uses OpenSSL with hardware
// acceleration via the devcrypto or afalg engines for AES-GCM operations.
// On other platforms, it falls back to Go's software crypto/tls.
//
// Usage:
//
//	config := tls.VNCConfig()
//	config.CertPEM = certPEM
//	config.KeyPEM = keyPEM
//	tlsConn, err := tls.Server(conn, config)
package tls

import (
	"crypto/tls"
	"net"
)

// Mode specifies the TLS mode for VNC connections.
type Mode int

const (
	// ModeAnonymousDH uses anonymous Diffie-Hellman (VNC TLSVnc mode).
	// This provides encryption without server authentication.
	ModeAnonymousDH Mode = iota
	// ModeX509 uses X.509 certificates (VNC X509 mode and RDP).
	ModeX509
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

// Config holds configuration for TLS connections.
type Config struct {
	// Mode specifies anonymous DH vs X.509 certificate mode.
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
