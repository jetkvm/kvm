package kvm

import (
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"net/http"

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
	certStore = websecure.NewCertStore(tlsStorePath)
	certStore.LoadCertificates()

	certSigner = websecure.NewSelfSigner(
		certStore,
		logger,
		webSecureSelfSignedDefaultDomain,
		webSecureSelfSignedOrganization,
		webSecureSelfSignedOU,
		webSecureSelfSignedCAName,
	)
}

func getCertificate(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if config.TLSMode == "self-signed" {
		if isTimeSyncNeeded() || !timeSyncSuccess {
			return nil, fmt.Errorf("time is not synced")
		}
		return certSigner.GetCertificate(info)
	} else if config.TLSMode == "custom" {
		return certStore.GetCertificate(webSecureCustomCertificateName), nil
	}

	logger.Infof("TLS mode is disabled but WebSecure is running, returning nil")
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
	switch s.Mode {
	case "disabled":
		config.TLSMode = ""
	case "custom":
		// parse pem to cert and key
		err, _ := certStore.ValidateAndSaveCertificate(webSecureCustomCertificateName, s.Certificate, s.PrivateKey, true)
		// warn doesn't matter as ... we don't know the hostname yet
		if err != nil {
			return fmt.Errorf("Failed to save certificate: %w", err)
		}
		config.TLSMode = "custom"
	case "self-signed":
		config.TLSMode = "self-signed"
	}
	return nil
}

// RunWebSecureServer runs a web server with TLS.
func RunWebSecureServer() {
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
	logger.Infof("Starting websecure server on %s", webSecureListen)
	err := server.ListenAndServeTLS("", "")
	if err != nil {
		panic(err)
	}
	return
}
