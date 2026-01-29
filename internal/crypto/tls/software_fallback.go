//go:build !cgo || !linux || !arm

package tls

import (
	"crypto/tls"
	"fmt"
	"net"
)

// softwareConn wraps Go's tls.Conn to implement the Conn interface.
type softwareConn struct {
	*tls.Conn
}

func (c *softwareConn) GetCipherName() string {
	return tls.CipherSuiteName(c.ConnectionState().CipherSuite)
}

func (c *softwareConn) GetProtocolVersion() string {
	switch c.ConnectionState().Version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("unknown (0x%04x)", c.ConnectionState().Version)
	}
}

func (c *softwareConn) IsHardwareAccelerated() bool {
	return false
}

func initImpl() {
	// No initialization needed for software TLS
}

func isHardwareAvailable() bool {
	return false
}

func hardwareEngine() string {
	return "none (software)"
}

// SetCLogLevel is a no-op on non-ARM platforms.
func SetCLogLevel(level int) {}

func serverImpl(conn net.Conn, config *Config) (Conn, error) {
	goConfig := &tls.Config{
		MinVersion:       config.MinVersion,
		MaxVersion:       config.MaxVersion,
		CipherSuites:     cipherSuitesX509(),
		CurvePreferences: curvePreferences(),
	}

	if config.GetCertificate != nil {
		goConfig.GetCertificate = config.GetCertificate
	} else if config.CertPEM != "" && config.KeyPEM != "" {
		cert, err := tls.X509KeyPair([]byte(config.CertPEM), []byte(config.KeyPEM))
		if err != nil {
			return nil, fmt.Errorf("failed to parse certificate: %w", err)
		}
		goConfig.Certificates = []tls.Certificate{cert}
	} else {
		return nil, fmt.Errorf("no certificate provided: set CertPEM/KeyPEM or GetCertificate")
	}

	tlsConn := tls.Server(conn, goConfig)
	if err := tlsConn.Handshake(); err != nil {
		return nil, fmt.Errorf("TLS handshake failed: %w", err)
	}

	return &softwareConn{Conn: tlsConn}, nil
}
