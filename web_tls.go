package kvm

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	WebSecureListen                  = ":443"
	WebSecureSelfSignedDefaultDomain = "jetkvm.local"
	WebSecureSelfSignedDuration      = 365 * 24 * time.Hour
)

var (
	tlsCerts    = make(map[string]*tls.Certificate)
	tlsCertLock = &sync.Mutex{}
)

// RunWebSecureServer runs a web server with TLS.
func RunWebSecureServer() {
	r := setupRouter()

	server := &http.Server{
		Addr:    WebSecureListen,
		Handler: r,
		TLSConfig: &tls.Config{
			// TODO: cache certificate in persistent storage
			GetCertificate: func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
				var hostname string
				if info.ServerName != "" {
					hostname = info.ServerName
				} else {
					hostname = strings.Split(info.Conn.LocalAddr().String(), ":")[0]
				}

				logger.Infof("TLS handshake for %s, SupportedProtos: %v", hostname, info.SupportedProtos)

				cert := createSelfSignedCert(hostname)

				return cert, nil
			},
		},
	}
	logger.Infof("Starting websecure server on %s", RunWebSecureServer)
	err := server.ListenAndServeTLS("", "")
	if err != nil {
		panic(err)
	}
}

func createSelfSignedCert(hostname string) *tls.Certificate {
	if tlsCert := tlsCerts[hostname]; tlsCert != nil {
		return tlsCert
	}
	tlsCertLock.Lock()
	defer tlsCertLock.Unlock()

	logger.Infof("Creating self-signed certificate for %s", hostname)

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		logger.Errorf("Failed to generate private key: %v", err)
		os.Exit(1)
	}
	keyUsage := x509.KeyUsageDigitalSignature

	notBefore := time.Now()
	notAfter := notBefore.AddDate(1, 0, 0)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		logger.Errorf("Failed to generate serial number: %v", err)
	}

	dnsName := hostname
	ip := net.ParseIP(hostname)
	if ip != nil {
		dnsName = WebSecureSelfSignedDefaultDomain
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   hostname,
			Organization: []string{"JetKVM"},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		KeyUsage:              keyUsage,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,

		DNSNames:    []string{dnsName},
		IPAddresses: []net.IP{},
	}

	if ip != nil {
		template.IPAddresses = append(template.IPAddresses, ip)
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		logger.Errorf("Failed to create certificate: %v", err)
	}

	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	if cert == nil {
		logger.Errorf("Failed to encode certificate")
	}

	tlsCert := &tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}
	tlsCerts[hostname] = tlsCert

	return tlsCert
}
