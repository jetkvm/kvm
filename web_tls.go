package kvm

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/jetkvm/kvm/internal/logging"
	"github.com/jetkvm/kvm/internal/websecure"
	"github.com/rs/zerolog"
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

func initCertStore(websecureLogger *zerolog.Logger) {
	certStore = websecure.NewCertStore(tlsStorePath)
	certStore.LoadCertificates(websecureLogger)

	certSigner = websecure.NewSelfSigner(
		certStore,
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

	logging.GetSubsystemLogger("web-secure").Info().Msg("TLS mode is disabled but WebSecure is running, returning nil")
	return nil, nil
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
	logger := logging.GetSubsystemLogger("web-secure")

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
			initCertStore(logger)
		}
		err, _ := certStore.ValidateAndSaveCertificate(webSecureCustomCertificateName, s.Certificate, s.PrivateKey, true, logger)
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
		logger.Info().Msg("TLS enabled state is not changed, not starting/stopping websecure server")
		return nil
	}

	if config.TLSMode == "" {
		logger.Info().Msg("Stopping websecure server, as TLS mode is disabled")
		stopWebSecureServer()
	} else {
		logger.Info().Msg("Starting websecure server, as TLS mode is enabled")
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

	server := &http.Server{
		Addr:    webSecureListen,
		Handler: r,
		TLSConfig: &tls.Config{
			MaxVersion:       tls.VersionTLS13,
			CurvePreferences: []tls.CurveID{},
			GetCertificate:   getCertificate,
		},
	}

	go func() {
		for range stopTLS {
			logger := logging.GetSubsystemLogger("web-secure")
			logger.Info().Msg("Shutting down websecure server")
			err := server.Shutdown(context.Background())
			if err != nil {
				logger.Error().Err(err).Msg("failed to shutdown websecure server")
			}
		}
	}()

	logging.GetSubsystemLogger("web-secure").Info().Str("listen", webSecureListen).Msg("Starting websecure server")

	err := server.ListenAndServeTLS("", "")
	if !errors.Is(err, http.ErrServerClosed) {
		panic(err)
	}
}

func stopWebSecureServer() {
	if !tlsStarted {
		logging.GetSubsystemLogger("web-secure").Info().Msg("Websecure server is not running, not stopping it")
		return
	}
	stopTLS <- struct{}{}
}

func startWebSecureServer() {
	if tlsStarted {
		logging.GetSubsystemLogger("web-secure").Info().Msg("Websecure server is already running, not starting it again")
		return
	}
	startTLS <- struct{}{}
}

func RunWebSecureServer() {
	for range startTLS {
		websecureLogger := logging.GetSubsystemLogger("web-secure")
		websecureLogger.Info().Msg("Starting websecure server, as we have received a start signal")

		if certStore == nil {
			initCertStore(websecureLogger)
		}
		go runWebSecureServer()
	}
}
