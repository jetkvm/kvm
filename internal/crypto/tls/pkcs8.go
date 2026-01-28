//go:build cgo && linux && arm

package tls

import "crypto/x509"

// doMarshalPKCS8PrivateKey wraps x509.MarshalPKCS8PrivateKey.
// Handles OpenSSLRSASigner by extracting the underlying RSA key.
func doMarshalPKCS8PrivateKey(key any) ([]byte, error) {
	// If it's our OpenSSL signer, extract the underlying RSA key
	if signer, ok := key.(*OpenSSLRSASigner); ok {
		key = signer.RSAPrivateKey()
	}
	return x509.MarshalPKCS8PrivateKey(key)
}
