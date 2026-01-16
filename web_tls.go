package kvm

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/jetkvm/kvm/internal/websecure"
)

const (
	tlsStorePath                     = "/userdata/jetkvm/tls"
	webSecureListen                  = ":443"
	webSecureSelfSignedDefaultDomain = "jetkvm.local"
	webSecureSelfSignedCAName        = "JetKVM Self-Signed CA"
	webSecureSelfSignedOrganization  = "JetKVM"
	webSecureSelfSignedOU            = "JetKVM Self-Signed"
	webSecureCustomCertificateName   = "user-defined"
)

var (
	certStore  *websecure.CertStore
	certSigner *websecure.SelfSigner
)

type TLSState struct {
	Mode        string `json:"mode"`
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"privateKey"`
}

func initCertStore() {
	if certStore != nil {
		websecureLogger.Warn().Msg("TLS store already initialized, it should not be initialized again")
		return
	}
	certStore = websecure.NewCertStore(tlsStorePath, websecureLogger)
	certStore.LoadCertificates()

	certSigner = websecure.NewSelfSigner(
		certStore,
		websecureLogger,
		webSecureSelfSignedDefaultDomain,
		webSecureSelfSignedOrganization,
		webSecureSelfSignedOU,
		webSecureSelfSignedCAName,
	)
}

func getCertificate(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
	switch config.TLSMode {
	case "self-signed":
		if isTimeSyncNeeded() || !timeSync.IsSyncSuccess() {
			return nil, fmt.Errorf("time is not synced")
		}
		return certSigner.GetCertificate(info)
	case "custom":
		return certStore.GetCertificate(webSecureCustomCertificateName), nil
	}

	websecureLogger.Info().Msg("TLS mode is disabled but WebSecure is running, returning nil")
	return nil, nil
}

// getVNCCertificatePEM returns the certificate and private key as PEM strings
// for use with OpenSSL-based VNC TLS. This allows hardware-accelerated crypto.
func getVNCCertificatePEM() (certPEM string, keyPEM string, err error) {
	var tlsCert *tls.Certificate

	switch config.TLSMode {
	case "self-signed":
		if isTimeSyncNeeded() || !timeSync.IsSyncSuccess() {
			return "", "", fmt.Errorf("time is not synced")
		}
		// For self-signed, use the default domain certificate
		// Create a mock ClientHelloInfo to trigger certificate generation
		tlsCert, err = certSigner.GetCertificate(&tls.ClientHelloInfo{
			ServerName: webSecureSelfSignedDefaultDomain,
		})
		if err != nil {
			return "", "", fmt.Errorf("failed to get self-signed certificate: %w", err)
		}
	case "custom":
		tlsCert = certStore.GetCertificate(webSecureCustomCertificateName)
		if tlsCert == nil {
			return "", "", fmt.Errorf("custom certificate not found")
		}
	default:
		return "", "", fmt.Errorf("TLS mode is disabled")
	}

	if tlsCert == nil {
		return "", "", fmt.Errorf("no certificate available")
	}

	// Convert certificate chain to PEM
	var certPEMBytes []byte
	for _, certDER := range tlsCert.Certificate {
		block := pem.Block{
			Type:  "CERTIFICATE",
			Bytes: certDER,
		}
		certPEMBytes = append(certPEMBytes, pem.EncodeToMemory(&block)...)
	}

	// Convert private key to PEM
	var keyBlock pem.Block
	switch k := tlsCert.PrivateKey.(type) {
	case *rsa.PrivateKey:
		keyBlock = pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(k),
		}
	case *ecdsa.PrivateKey:
		b, e := x509.MarshalECPrivateKey(k)
		if e != nil {
			return "", "", fmt.Errorf("failed to marshal EC private key: %v", e)
		}
		keyBlock = pem.Block{
			Type:  "EC PRIVATE KEY",
			Bytes: b,
		}
	case ed25519.PrivateKey:
		keyBlock = pem.Block{
			Type:  "ED25519 PRIVATE KEY",
			Bytes: k,
		}
	default:
		return "", "", fmt.Errorf("unknown private key type: %T", tlsCert.PrivateKey)
	}

	return string(certPEMBytes), string(pem.EncodeToMemory(&keyBlock)), nil
}

func getTLSState() TLSState {
	s := TLSState{}
	switch config.TLSMode {
	case "disabled":
		s.Mode = "disabled"
	case "custom":
		s.Mode = "custom"
		cert := certStore.GetCertificate(webSecureCustomCertificateName)
		if cert != nil {
			var certPEM []byte
			// convert to pem format
			for _, c := range cert.Certificate {
				block := pem.Block{
					Type:  "CERTIFICATE",
					Bytes: c,
				}

				certPEM = append(certPEM, pem.EncodeToMemory(&block)...)
			}
			s.Certificate = string(certPEM)
		}
	case "self-signed":
		s.Mode = "self-signed"
	}

	return s
}

func setTLSState(s TLSState) error {
	var isChanged = false

	switch s.Mode {
	case "disabled":
		if config.TLSMode != "" {
			isChanged = true
		}
		config.TLSMode = ""
	case "custom":
		if config.TLSMode == "" {
			isChanged = true
		}
		// parse pem to cert and key
		if certStore == nil {
			initCertStore()
		}
		err, _ := certStore.ValidateAndSaveCertificate(webSecureCustomCertificateName, s.Certificate, s.PrivateKey, true)
		// warn doesn't matter as ... we don't know the hostname yet
		if err != nil {
			return fmt.Errorf("failed to save certificate: %w", err)
		}
		config.TLSMode = "custom"
	case "self-signed":
		if config.TLSMode == "" {
			isChanged = true
		}
		config.TLSMode = "self-signed"
	default:
		return fmt.Errorf("invalid TLS mode: %s", s.Mode)
	}

	if !isChanged {
		websecureLogger.Info().Msg("TLS enabled state is not changed, not starting/stopping websecure server")
		return nil
	}

	if config.TLSMode == "" {
		websecureLogger.Info().Msg("Stopping websecure server, as TLS mode is disabled")
		stopWebSecureServer()
	} else {
		websecureLogger.Info().Msg("Starting websecure server, as TLS mode is enabled")
		startWebSecureServer()
	}

	return nil
}

var (
	startTLS       = make(chan struct{})
	stopTLS        = make(chan struct{})
	tlsServiceLock = sync.Mutex{}
	tlsStarted     = false
)

// RunWebSecureServer runs a web server with TLS.
func runWebSecureServer() {
	tlsServiceLock.Lock()
	defer tlsServiceLock.Unlock()

	tlsStarted = true
	defer func() {
		tlsStarted = false
	}()

	r := setupRouter()

	// Determine the binding address based on the config
	bindAddress := getBindAddress(443)

	server := &http.Server{
		Addr:    bindAddress,
		Handler: r,
		TLSConfig: &tls.Config{
			MaxVersion:       tls.VersionTLS13,
			CurvePreferences: []tls.CurveID{},
			GetCertificate:   getCertificate,
		},
	}
	websecureLogger.Info().Str("bindAddress", bindAddress).Bool("loopbackOnly", config.LocalLoopbackOnly).Msg("Starting websecure server")

	go func() {
		for range stopTLS {
			websecureLogger.Info().Msg("Shutting down websecure server")
			err := server.Shutdown(context.Background())
			if err != nil {
				websecureLogger.Error().Err(err).Msg("failed to shutdown websecure server")
			}
		}
	}()

	err := server.ListenAndServeTLS("", "")
	if !errors.Is(err, http.ErrServerClosed) {
		panic(err)
	}
}

func stopWebSecureServer() {
	if !tlsStarted {
		websecureLogger.Info().Msg("Websecure server is not running, not stopping it")
		return
	}
	stopTLS <- struct{}{}
}

func startWebSecureServer() {
	if tlsStarted {
		websecureLogger.Info().Msg("Websecure server is already running, not starting it again")
		return
	}
	startTLS <- struct{}{}
}

func RunWebSecureServer() {
	for range startTLS {
		websecureLogger.Info().Msg("Starting websecure server, as we have received a start signal")
		if certStore == nil {
			initCertStore()
		}
		go runWebSecureServer()
	}
}
