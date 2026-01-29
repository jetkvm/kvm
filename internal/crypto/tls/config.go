package tls

import (
	"crypto/tls"
)

// DefaultConfig returns a default TLS configuration suitable for general use.
// It supports TLS 1.2 and 1.3 with modern cipher suites.
func DefaultConfig() *Config {
	return &Config{
		Mode:       ModeX509,
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS13,
	}
}

// RDPConfig returns a TLS configuration for RDP connections.
// It limits to TLS 1.2 because CredSSP/NLA requires TLS session binding
// which changed in TLS 1.3 and is not compatible.
func RDPConfig() *Config {
	return &Config{
		Mode:       ModeX509,
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS12, // CredSSP requires TLS 1.2
	}
}

// cipherSuitesX509 returns the preferred cipher suites for X.509 mode.
// These prioritize ECDHE-ECDSA for better performance on ARM without hardware RSA,
// and prefer AES-GCM for hardware acceleration.
func cipherSuitesX509() []uint16 {
	return []uint16{
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
	}
}

// cipherSuitesX509String returns the cipher suite string for OpenSSL X.509 mode.
func cipherSuitesX509String() string { //nolint:unused // Used only in ARM CGO build
	return "ECDHE-ECDSA-AES128-GCM-SHA256:" +
		"ECDHE-ECDSA-AES256-GCM-SHA384:" +
		"ECDHE-ECDSA-CHACHA20-POLY1305:" +
		"ECDHE-RSA-AES128-GCM-SHA256:" +
		"ECDHE-RSA-AES256-GCM-SHA384:" +
		"ECDHE-RSA-CHACHA20-POLY1305:" +
		"DHE-RSA-AES256-GCM-SHA384:" +
		"DHE-RSA-AES128-GCM-SHA256:" +
		"DHE-RSA-CHACHA20-POLY1305"
}

// curvePreferences returns the preferred curves for key exchange.
func curvePreferences() []tls.CurveID {
	return []tls.CurveID{
		tls.X25519, // Fastest curve, ~40% faster than P-256
		tls.CurveP256,
	}
}
