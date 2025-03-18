package websecure

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"log"
	"net"
	"strings"
	"time"

	"github.com/pion/logging"
	"golang.org/x/net/idna"
)

const selfSignerCAMagicName = "__ca__"

type SelfSigner struct {
	store *CertStore
	log   logging.LeveledLogger

	caInfo pkix.Name

	DefaultDomain string
	DefaultOrg    string
	DefaultOU     string
}

func NewSelfSigner(store *CertStore, log logging.LeveledLogger, defaultDomain, defaultOrg, defaultOU, caName string) *SelfSigner {
	return &SelfSigner{
		store:         store,
		log:           log,
		DefaultDomain: defaultDomain,
		DefaultOrg:    defaultOrg,
		DefaultOU:     defaultOU,
		caInfo: pkix.Name{
			CommonName:         caName,
			Organization:       []string{defaultOrg},
			OrganizationalUnit: []string{defaultOU},
		},
	}
}

func (s *SelfSigner) getCA() *tls.Certificate {
	return s.createSelfSignedCert(selfSignerCAMagicName)
}

func (s *SelfSigner) createSelfSignedCert(hostname string) *tls.Certificate {
	if tlsCert := s.store.certificates[hostname]; tlsCert != nil {
		return tlsCert
	}

	// check if hostname is the CA magic name
	var ca *tls.Certificate
	if hostname != selfSignerCAMagicName {
		ca = s.getCA()
		if ca == nil {
			s.log.Errorf("Failed to get CA certificate")
			return nil
		}
	}

	s.log.Infof("Creating self-signed certificate for %s", hostname)

	// lock the store while creating the certificate (do not move upwards)
	s.store.certLock.Lock()
	defer s.store.certLock.Unlock()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalf("Failed to generate private key: %v", err)
	}

	notBefore := time.Now()
	notAfter := notBefore.AddDate(1, 0, 0)

	serialNumber, err := generateSerialNumber()
	if err != nil {
		s.log.Errorf("Failed to generate serial number: %v", err)
	}

	dnsName := hostname
	ip := net.ParseIP(hostname)
	if ip != nil {
		dnsName = s.DefaultDomain
	}

	// set up CSR
	isCA := hostname == selfSignerCAMagicName
	subject := pkix.Name{
		CommonName:         hostname,
		Organization:       []string{s.DefaultOrg},
		OrganizationalUnit: []string{s.DefaultOU},
	}
	keyUsage := x509.KeyUsageDigitalSignature
	extKeyUsage := []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}

	// check if hostname is the CA magic name, and if so, set the subject to the CA info
	if isCA {
		subject = s.caInfo
		keyUsage |= x509.KeyUsageCertSign
		extKeyUsage = append(extKeyUsage, x509.ExtKeyUsageClientAuth)
		notAfter = notBefore.AddDate(10, 0, 0)
	}

	cert := x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               subject,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  isCA,
		KeyUsage:              keyUsage,
		ExtKeyUsage:           extKeyUsage,
		BasicConstraintsValid: true,
	}

	// set up DNS names and IP addresses
	if !isCA {
		cert.DNSNames = []string{dnsName}
		if ip != nil {
			cert.IPAddresses = []net.IP{ip}
		}
	}

	// set up parent certificate
	parent := &cert
	parentPriv := priv
	if ca != nil {
		parent, err = x509.ParseCertificate(ca.Certificate[0])
		if err != nil {
			s.log.Errorf("Failed to parse parent certificate: %v", err)
			return nil
		}
		parentPriv = ca.PrivateKey.(*ecdsa.PrivateKey)
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, &cert, parent, &priv.PublicKey, parentPriv)
	if err != nil {
		s.log.Errorf("Failed to create certificate: %v", err)
		return nil
	}

	tlsCert := &tls.Certificate{
		Certificate: [][]byte{certBytes},
		PrivateKey:  priv,
	}
	if ca != nil {
		tlsCert.Certificate = append(tlsCert.Certificate, ca.Certificate...)
	}

	s.store.certificates[hostname] = tlsCert
	s.store.saveCertificate(hostname)

	return tlsCert
}

func (s *SelfSigner) GetCertificate(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
	hostname := s.DefaultDomain
	if info.ServerName != "" && info.ServerName != selfSignerCAMagicName {
		hostname = info.ServerName
	} else {
		hostname = strings.Split(info.Conn.LocalAddr().String(), ":")[0]
	}

	s.log.Infof("TLS handshake for %s, SupportedProtos: %v", hostname, info.SupportedProtos)

	// convert hostname to punycode
	h, err := idna.Lookup.ToASCII(hostname)
	if err != nil {
		s.log.Warnf("Hostname %s is not valid: %w, from %s", hostname, err, info.Conn.RemoteAddr())
		hostname = s.DefaultDomain
	} else {
		hostname = h
	}

	cert := s.createSelfSignedCert(hostname)

	return cert, nil
}
