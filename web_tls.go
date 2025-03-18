package kvm

import (
	"crypto/tls"
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
)

var (
	certStore  *websecure.CertStore
	certSigner *websecure.SelfSigner
)

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

// RunWebSecureServer runs a web server with TLS.
func RunWebSecureServer() {
	r := setupRouter()

	server := &http.Server{
		Addr:    webSecureListen,
		Handler: r,
		TLSConfig: &tls.Config{
			MaxVersion:       tls.VersionTLS13,
			CurvePreferences: []tls.CurveID{},
			GetCertificate:   certSigner.GetCertificate,
		},
	}
	logger.Infof("Starting websecure server on %s", RunWebSecureServer)
	err := server.ListenAndServeTLS("", "")
	if err != nil {
		panic(err)
	}
	return
}
