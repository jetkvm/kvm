package websecure

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
)

var serialNumberLimit = new(big.Int).Lsh(big.NewInt(1), 4096)

func withSecretFile(filename string, f func(*os.File) error) error {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer file.Close()

	return f(file)
}

func keyToFile(cert *tls.Certificate, filename string) error {
	var keyBlock pem.Block
	switch k := cert.PrivateKey.(type) {
	case *rsa.PrivateKey:
		keyBlock = pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(k),
		}
	case *ecdsa.PrivateKey:
		b, e := x509.MarshalECPrivateKey(k)
		if e != nil {
			return fmt.Errorf("failed to marshal EC private key: %v", e)
		}

		keyBlock = pem.Block{
			Type:  "EC PRIVATE KEY",
			Bytes: b,
		}
	default:
		return fmt.Errorf("unknown private key type: %T", k)
	}

	err := withSecretFile(filename, func(file *os.File) error {
		return pem.Encode(file, &keyBlock)
	})

	if err != nil {
		return fmt.Errorf("failed to save private key: %w", err)
	}

	return nil
}

func certToFile(cert *tls.Certificate, filename string) error {
	return withSecretFile(filename, func(file *os.File) error {
		for _, c := range cert.Certificate {
			block := pem.Block{
				Type:  "CERTIFICATE",
				Bytes: c,
			}

			err := pem.Encode(file, &block)
			if err != nil {
				return fmt.Errorf("failed to save certificate: %w", err)
			}
		}

		return nil
	})
}

func generateSerialNumber() (*big.Int, error) {
	return rand.Int(rand.Reader, serialNumberLimit)
}
