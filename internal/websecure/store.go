package websecure

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/jetkvm/kvm/internal/sync"

	"github.com/rs/zerolog"
)

type CertStore struct {
	certificates map[string]*tls.Certificate
	certLock     *sync.Mutex

	storePath string

	log *zerolog.Logger
}

func NewCertStore(storePath string, log *zerolog.Logger) *CertStore {
	if log == nil {
		log = &defaultLogger
	}

	return &CertStore{
		certificates: make(map[string]*tls.Certificate),
		certLock:     &sync.Mutex{},

		storePath: storePath,
		log:       log,
	}
}

func (s *CertStore) ensureStorePath() error {
	// check if directory exists
	stat, err := os.Stat(s.storePath)
	if err == nil {
		if stat.IsDir() {
			return nil
		}

		return fmt.Errorf("TLS store path exists but is not a directory: %s", s.storePath)
	}

	if os.IsNotExist(err) {
		s.log.Trace().Str("path", s.storePath).Msg("TLS store directory does not exist, creating directory")
		err = os.MkdirAll(s.storePath, 0755)
		if err != nil {
			return fmt.Errorf("failed to create TLS store path: %w", err)
		}
		return nil
	}

	return fmt.Errorf("failed to check TLS store path: %w", err)
}

func (s *CertStore) LoadCertificates() {
	err := s.ensureStorePath()
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to ensure store path")
		return
	}

	files, err := os.ReadDir(s.storePath)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to read TLS directory")
		return
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if strings.HasSuffix(file.Name(), ".crt") {
			s.loadCertificate(strings.TrimSuffix(file.Name(), ".crt"))
		}
	}

	s.migrateOversizedCAIfNeeded()
}

// migrateOversizedCAIfNeeded drops the self-signed CA — and any leaf
// certificates issued by it — when the CA's serial number is wider
// than RFC 5280 §4.1.2.2 allows (20 octets). Apple's DER parser
// rejects such certificates with "Unknown format in import" before
// any trust evaluation runs, so without this migration every device
// that already baked an oversized CA would keep failing on
// macOS/iOS/tvOS clients forever. User-supplied custom certificates
// have a different issuer and are left untouched.
func (s *CertStore) migrateOversizedCAIfNeeded() {
	s.certLock.Lock()
	defer s.certLock.Unlock()

	ca := s.certificates[selfSignerCAMagicName]
	if ca == nil || len(ca.Certificate) == 0 {
		return
	}

	caCert, err := x509.ParseCertificate(ca.Certificate[0])
	if err != nil {
		s.log.Warn().Err(err).Msg("Failed to parse stored CA certificate during migration check")
		return
	}

	if caCert.SerialNumber.BitLen() <= maxValidSerialBits {
		return
	}

	s.log.Warn().
		Int("serial_bits", caCert.SerialNumber.BitLen()).
		Msg("Stored self-signed CA has an oversized serial number; regenerating CA and any leaves it issued")

	toRemove := []string{selfSignerCAMagicName}
	for hostname, cert := range s.certificates {
		if hostname == selfSignerCAMagicName || len(cert.Certificate) == 0 {
			continue
		}
		leaf, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			continue
		}
		if !bytes.Equal(leaf.RawIssuer, caCert.RawSubject) {
			continue
		}
		toRemove = append(toRemove, hostname)
	}

	for _, hostname := range toRemove {
		delete(s.certificates, hostname)
		keyFile := path.Join(s.storePath, hostname+".key")
		crtFile := path.Join(s.storePath, hostname+".crt")
		if err := os.Remove(keyFile); err != nil && !os.IsNotExist(err) {
			s.log.Warn().Err(err).Str("file", keyFile).Msg("Failed to remove stale key file")
		}
		if err := os.Remove(crtFile); err != nil && !os.IsNotExist(err) {
			s.log.Warn().Err(err).Str("file", crtFile).Msg("Failed to remove stale certificate file")
		}
	}
}

func (s *CertStore) loadCertificate(hostname string) {
	s.certLock.Lock()
	defer s.certLock.Unlock()

	keyFile := path.Join(s.storePath, hostname+".key")
	crtFile := path.Join(s.storePath, hostname+".crt")

	cert, err := tls.LoadX509KeyPair(crtFile, keyFile)
	if err != nil {
		s.log.Error().Err(err).Str("hostname", hostname).Msg("Failed to load certificate")
		return
	}

	s.certificates[hostname] = &cert

	if hostname == selfSignerCAMagicName {
		s.log.Info().Msg("loaded CA certificate")
	} else {
		s.log.Info().Str("hostname", hostname).Msg("loaded certificate")
	}
}

// GetCertificate returns the certificate for the given hostname
// returns nil if the certificate is not found
func (s *CertStore) GetCertificate(hostname string) *tls.Certificate {
	s.certLock.Lock()
	defer s.certLock.Unlock()

	return s.certificates[hostname]
}

// ValidateAndSaveCertificate validates the certificate and saves it to the store
// returns are:
// - error: if the certificate is invalid or if there's any error during saving the certificate
// - error: if there's any warning or error during saving the certificate
func (s *CertStore) ValidateAndSaveCertificate(hostname string, cert string, key string, ignoreWarning bool) (error, error) {
	tlsCert, err := tls.X509KeyPair([]byte(cert), []byte(key))
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err), nil
	}

	// this can be skipped as current implementation supports one custom certificate only
	if tlsCert.Leaf != nil {
		// add recover to avoid panic
		defer func() {
			if r := recover(); r != nil {
				s.log.Error().Interface("recovered", r).Msg("Failed to verify hostname")
			}
		}()

		if err = tlsCert.Leaf.VerifyHostname(hostname); err != nil {
			if !ignoreWarning {
				return nil, fmt.Errorf("certificate does not match hostname: %w", err)
			}
			s.log.Warn().Err(err).Msg("certificate does not match hostname")
		}
	}

	s.certLock.Lock()
	s.certificates[hostname] = &tlsCert
	s.certLock.Unlock()

	s.saveCertificate(hostname)

	return nil, nil
}

func (s *CertStore) saveCertificate(hostname string) {
	// check if certificate already exists
	tlsCert := s.certificates[hostname]
	if tlsCert == nil {
		s.log.Error().Str("hostname", hostname).Msg("Certificate for hostname does not exist, skipping saving certificate")
		return
	}

	err := s.ensureStorePath()
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to ensure store path")
		return
	}

	keyFile := path.Join(s.storePath, hostname+".key")
	crtFile := path.Join(s.storePath, hostname+".crt")

	if err := keyToFile(tlsCert, keyFile); err != nil {
		s.log.Error().Err(err).Msg("Failed to save key file")
		return
	}

	if err := certToFile(tlsCert, crtFile); err != nil {
		s.log.Error().Err(err).Msg("Failed to save certificate")
		return
	}

	s.log.Info().Str("hostname", hostname).Msg("Saved certificate")
}
