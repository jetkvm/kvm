package websecure

import (
	"os"
	"testing"
)

var (
	fixtureEd25519Certificate = `-----BEGIN CERTIFICATE-----
MIIBQDCB86ADAgECAhQdB4qB6dV0/u1lwhJofQgkmjjV1zAFBgMrZXAwLzELMAkG
A1UEBhMCREUxIDAeBgNVBAMMF2VkMjU1MTktdGVzdC5qZXRrdm0uY29tMB4XDTI1
MDUyMzEyNTkyN1oXDTI3MDQyMzEyNTkyN1owLzELMAkGA1UEBhMCREUxIDAeBgNV
BAMMF2VkMjU1MTktdGVzdC5qZXRrdm0uY29tMCowBQYDK2VwAyEA9tLyoulJn7Ev
bf8kuD1ZGdA092773pCRjFEDKpXHonyjITAfMB0GA1UdDgQWBBRkmrVMfsLY57iy
r/0POP0S4QxCADAFBgMrZXADQQBfTRvqavLHDYQiKQTgbGod+Yn+fIq2lE584+1U
C4wh9peIJDFocLBEAYTQpEMKxa4s0AIRxD+a7aCS5oz0e/0I
-----END CERTIFICATE-----`

	fixtureEd25519PrivateKey = `-----BEGIN PRIVATE KEY-----
MC4CAQAwBQYDK2VwBCIEIKV08xUsLRHBfMXqZwxVRzIbViOp8G7aQGjPvoRFjujB
-----END PRIVATE KEY-----`

	certStore  *CertStore
	certSigner *SelfSigner
)

func TestMain(m *testing.M) {
	tlsStorePath, err := os.MkdirTemp("", "jktls.*")
	if err != nil {
		defaultLogger.Fatal().Err(err).Msg("failed to create temp directory")
	}

	certStore = NewCertStore(tlsStorePath, nil)
	certStore.LoadCertificates()

	certSigner = NewSelfSigner(
		certStore,
		nil,
		"ci.jetkvm.com",
		"JetKVM",
		"JetKVM",
		"JetKVM",
	)

	m.Run()

	os.RemoveAll(tlsStorePath)
}

func TestSaveEd25519Certificate(t *testing.T) {
	err, _ := certStore.ValidateAndSaveCertificate("ed25519-test.jetkvm.com", fixtureEd25519Certificate, fixtureEd25519PrivateKey, true)
	if err != nil {
		t.Fatalf("failed to save certificate: %v", err)
	}
}
