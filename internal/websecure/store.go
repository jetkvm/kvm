package websecure

import (
	"crypto/tls"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/rs/zerolog"
)

type CertStore struct {
	certificates map[string]*tls.Certificate
	certLock     *sync.Mutex
	storePath    string
}

func NewCertStore(storePath string) *CertStore {
	return &CertStore{
		certificates: make(map[string]*tls.Certificate),
		certLock:     &sync.Mutex{},
		storePath:    storePath,
	}
}

func (s *CertStore) ensureStorePath(logger *zerolog.Logger) error {
	// check if directory exists
	stat, err := os.Stat(s.storePath)
	if err == nil {
		if stat.IsDir() {
			return nil
		}

		return fmt.Errorf("TLS store path exists but is not a directory: %s", s.storePath)
	}

	if os.IsNotExist(err) {
		logger.Trace().Str("path", s.storePath).Msg("TLS store directory does not exist, creating directory")
		err = os.MkdirAll(s.storePath, 0755)
		if err != nil {
			return fmt.Errorf("failed to create TLS store path: %w", err)
		}
		return nil
	}

	return fmt.Errorf("failed to check TLS store path: %w", err)
}

func (s *CertStore) LoadCertificates(logger *zerolog.Logger) {
	scopedLogger := logger.With().Str("storePath", s.storePath).Logger()

	err := s.ensureStorePath(&scopedLogger)
	if err != nil {
		scopedLogger.Error().Err(err).Msg("Failed to ensure store path")
		return
	}

	files, err := os.ReadDir(s.storePath)
	if err != nil {
		scopedLogger.Error().Err(err).Msg("Failed to read TLS directory")
		return
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if strings.HasSuffix(file.Name(), ".crt") {
			hostname := strings.TrimSuffix(file.Name(), ".crt")
			s.loadCertificate(hostname, &scopedLogger)
		}
	}
}

func (s *CertStore) loadCertificate(hostname string, logger *zerolog.Logger) {
	s.certLock.Lock()
	defer s.certLock.Unlock()

	scopedLogger := logger.With().Str("hostname", hostname).Logger()

	keyFile := path.Join(s.storePath, hostname+".key")
	crtFile := path.Join(s.storePath, hostname+".crt")

	cert, err := tls.LoadX509KeyPair(crtFile, keyFile)
	if err != nil {
		scopedLogger.Error().Err(err).Msg("Failed to load certificate")
		return
	}

	s.certificates[hostname] = &cert

	if hostname == selfSignerCAMagicName {
		scopedLogger.Info().Msg("loaded CA certificate")
	} else {
		scopedLogger.Info().Msg("loaded certificate")
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
func (s *CertStore) ValidateAndSaveCertificate(hostname string, cert string, key string, ignoreWarning bool, logger *zerolog.Logger) (error, error) {
	scopedLogger := logger.With().Str("hostname", hostname).Str("cert", cert).Logger() // don't log the key for security reasons

	tlsCert, err := tls.X509KeyPair([]byte(cert), []byte(key))
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err), nil
	}

	// this can be skipped as current implementation supports one custom certificate only
	if tlsCert.Leaf != nil {
		// add recover to avoid panic
		defer func() {
			if r := recover(); r != nil {
				scopedLogger.Error().Interface("recovered", r).Msg("Failed to verify hostname")
			}
		}()

		if err = tlsCert.Leaf.VerifyHostname(hostname); err != nil {
			if !ignoreWarning {
				return nil, fmt.Errorf("certificate does not match hostname: %w", err)
			}
			scopedLogger.Warn().Err(err).Msg("certificate does not match hostname")
		}
	}

	s.certLock.Lock()
	s.certificates[hostname] = &tlsCert
	s.certLock.Unlock()

	s.saveCertificate(hostname, &scopedLogger)

	return nil, nil
}

func (s *CertStore) saveCertificate(hostname string, logger *zerolog.Logger) {
	// check if certificate already exists
	tlsCert := s.certificates[hostname]
	if tlsCert == nil {
		logger.Error().Msg("Certificate for hostname does not exist, skipping saving certificate")
		return
	}

	err := s.ensureStorePath(logger)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to ensure store path")
		return
	}

	keyFile := path.Join(s.storePath, hostname+".key")
	crtFile := path.Join(s.storePath, hostname+".crt")

	if err := keyToFile(tlsCert, keyFile); err != nil {
		logger.Error().Err(err).Msg("Failed to save key file")
		return
	}

	if err := certToFile(tlsCert, crtFile); err != nil {
		logger.Error().Err(err).Msg("Failed to save certificate")
		return
	}

	logger.Info().Msg("Saved certificate")
}
