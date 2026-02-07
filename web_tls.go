package kvm

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"

	hwcrypto "github.com/jetkvm/kvm/internal/crypto/tls"
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

	// Configure hardware RSA acceleration mode from config
	if mode := loadCfg().HardwareRSA; mode != "" {
		hwcrypto.SetHardwareRSAMode(mode)
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
	switch loadCfg().TLSMode {
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

func getTLSState() TLSState {
	s := TLSState{}
	switch loadCfg().TLSMode {
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
	var newTLSMode string
	currentTLSMode := loadCfg().TLSMode

	switch s.Mode {
	case "disabled":
		if currentTLSMode != "" {
			isChanged = true
		}
		newTLSMode = ""
	case "custom":
		if currentTLSMode == "" {
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
		newTLSMode = "custom"
	case "self-signed":
		if currentTLSMode == "" {
			isChanged = true
		}
		newTLSMode = "self-signed"
	default:
		return fmt.Errorf("invalid TLS mode: %s", s.Mode)
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.TLSMode = newTLSMode
	}); err != nil {
		return fmt.Errorf("failed to save TLS config: %w", err)
	}

	if !isChanged {
		websecureLogger.Info().Msg("TLS enabled state is not changed, not starting/stopping websecure server")
		return nil
	}

	if newTLSMode == "" {
		websecureLogger.Info().Msg("Stopping websecure server, as TLS mode is disabled")
		stopWebSecureServer()
	} else {
		websecureLogger.Info().Msg("Starting websecure server, as TLS mode is enabled")
		startWebSecureServer()
	}

	return nil
}

var (
	startTLS   = make(chan struct{})
	stopTLS    = make(chan struct{})
	tlsStarted atomic.Bool
)

// RunWebSecureServer runs a web server with hardware-accelerated TLS.
// Uses the same OpenSSL-based TLS as the RDP/VNC servers for hardware AES-GCM
// acceleration on the RV1106. On non-ARM platforms, falls back to software crypto.
func runWebSecureServer() {
	tlsStarted.Store(true)
	defer tlsStarted.Store(false)

	r := setupRouter()

	// Determine the binding address based on the config
	bindAddress := getBindAddress(443)

	ln, err := net.Listen("tcp", bindAddress)
	if err != nil {
		websecureLogger.Error().Err(err).Str("bindAddress", bindAddress).Msg("failed to listen")
		return
	}

	tlsListener := hwcrypto.NewListener(ln, &hwcrypto.Config{
		GetCertificate: getCertificate,
	})

	server := &http.Server{
		Handler: r,
	}
	websecureLogger.Info().Str("bindAddress", bindAddress).Bool("loopbackOnly", loadCfg().LocalLoopbackOnly).Msg("Starting websecure server")

	go func() {
		for range stopTLS {
			websecureLogger.Info().Msg("Shutting down websecure server")
			err := server.Shutdown(context.Background())
			if err != nil {
				websecureLogger.Error().Err(err).Msg("failed to shutdown websecure server")
			}
		}
	}()

	err = server.Serve(tlsListener)
	if !errors.Is(err, http.ErrServerClosed) {
		websecureLogger.Error().Err(err).Msg("websecure server error")
	}
}

func stopWebSecureServer() {
	if !tlsStarted.Load() {
		websecureLogger.Info().Msg("Websecure server is not running, not stopping it")
		return
	}
	stopTLS <- struct{}{}
}

func startWebSecureServer() {
	if tlsStarted.Load() {
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

// HardwareRSAState represents the RSA acceleration state for the UI.
type HardwareRSAState struct {
	Mode           string   `json:"mode"`           // Current mode: "openssl", "disabled"
	AvailableModes []string `json:"availableModes"` // Available mode options
}

// rpcGetHardwareRSAState returns the current RSA acceleration state.
func rpcGetHardwareRSAState() (HardwareRSAState, error) {
	mode := loadCfg().HardwareRSA
	if mode == "" {
		mode = "openssl"
	}

	return HardwareRSAState{
		Mode:           mode,
		AvailableModes: []string{"openssl", "disabled"},
	}, nil
}

// rpcSetHardwareRSAMode sets the RSA acceleration mode.
func rpcSetHardwareRSAMode(mode string) error {
	if mode != "openssl" && mode != "disabled" {
		return fmt.Errorf("invalid mode: %s (must be openssl or disabled)", mode)
	}

	if err := updateAndSaveConfig(func(cfg *Config) {
		cfg.HardwareRSA = mode
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	hwcrypto.SetHardwareRSAMode(mode)

	websecureLogger.Info().Str("mode", mode).Msg("RSA acceleration mode changed")
	return nil
}
