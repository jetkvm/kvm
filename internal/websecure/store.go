package websecure

import (
	"crypto/tls"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/pion/logging"
)

type CertStore struct {
	certificates map[string]*tls.Certificate
	certLock     *sync.Mutex

	storePath string

	log logging.LeveledLogger
}

func NewCertStore(storePath string) *CertStore {
	return &CertStore{
		certificates: make(map[string]*tls.Certificate),
		certLock:     &sync.Mutex{},

		storePath: storePath,
		log:       defaultLogger,
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
		s.log.Tracef("TLS store directory does not exist, creating directory")
		err = os.MkdirAll(s.storePath, 0755)
		if err != nil {
			return fmt.Errorf("Failed to create TLS store path: %w", err)
		}
		return nil
	}

	return fmt.Errorf("Failed to check TLS store path: %w", err)
}

func (s *CertStore) LoadCertificates() {
	err := s.ensureStorePath()
	if err != nil {
		s.log.Errorf(err.Error())
		return
	}

	files, err := os.ReadDir(s.storePath)
	if err != nil {
		s.log.Errorf("Failed to read TLS directory: %v", err)
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
}

func (s *CertStore) loadCertificate(hostname string) {
	s.certLock.Lock()
	defer s.certLock.Unlock()

	keyFile := path.Join(s.storePath, hostname+".key")
	crtFile := path.Join(s.storePath, hostname+".crt")

	cert, err := tls.LoadX509KeyPair(crtFile, keyFile)
	if err != nil {
		s.log.Errorf("Failed to load certificate for %s: %w", hostname, err)
		return
	}

	s.certificates[hostname] = &cert

	s.log.Infof("Loaded certificate for %s", hostname)
}

func (s *CertStore) saveCertificate(hostname string) {
	// check if certificate already exists
	tlsCert := s.certificates[hostname]
	if tlsCert == nil {
		s.log.Errorf("Certificate for %s does not exist, skipping saving certificate", hostname)
		return
	}

	err := s.ensureStorePath()
	if err != nil {
		s.log.Errorf(err.Error())
		return
	}

	keyFile := path.Join(s.storePath, hostname+".key")
	crtFile := path.Join(s.storePath, hostname+".crt")

	if keyToFile(tlsCert, keyFile); err != nil {
		s.log.Errorf(err.Error())
		return
	}

	if certToFile(tlsCert, crtFile); err != nil {
		s.log.Errorf(err.Error())
		return
	}

	s.log.Infof("Saved certificate for %s", hostname)
}
