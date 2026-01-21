//go:build cgo && linux && arm

package tls

import "crypto/x509"

// doMarshalPKCS8PrivateKey wraps x509.MarshalPKCS8PrivateKey.
// This is in a separate file to work cleanly with CGO builds.
func doMarshalPKCS8PrivateKey(key any) ([]byte, error) {
	return x509.MarshalPKCS8PrivateKey(key)
}
