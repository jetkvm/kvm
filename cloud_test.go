//go:build linux && arm

package kvm

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"github.com/go-jose/go-jose/v4"
)

func TestCloudRegisterRequestOIDCFieldsPreferProviderAgnosticValues(t *testing.T) {
	req := CloudRegisterRequest{
		OIDCToken:    "generic-token",
		OIDCClientID: "generic-client",
		OIDCIssuer:   "https://auth.example.com/application/o/jetkvm/",
		OidcGoogle:   "legacy-token",
		ClientId:     "legacy-client",
	}

	if got := req.oidcToken(); got != "generic-token" {
		t.Fatalf("oidcToken() = %q, want generic token", got)
	}
	if got := req.oidcClientID(); got != "generic-client" {
		t.Fatalf("oidcClientID() = %q, want generic client", got)
	}
	if got := req.oidcIssuer(); got != "https://auth.example.com/application/o/jetkvm/" {
		t.Fatalf("oidcIssuer() = %q, want configured issuer", got)
	}
}

func TestCloudRegisterRequestOIDCFieldsFallbackToLegacyGoogleValues(t *testing.T) {
	req := CloudRegisterRequest{
		OidcGoogle: "legacy-token",
		ClientId:   "legacy-client",
	}

	if got := req.oidcToken(); got != "legacy-token" {
		t.Fatalf("oidcToken() = %q, want legacy token", got)
	}
	if got := req.oidcClientID(); got != "legacy-client" {
		t.Fatalf("oidcClientID() = %q, want legacy client", got)
	}
	if got := req.oidcIssuer(); got != "https://accounts.google.com" {
		t.Fatalf("oidcIssuer() = %q, want legacy Google issuer", got)
	}
}

func TestWebRTCSessionRequestOIDCTokenPrefersProviderAgnosticValue(t *testing.T) {
	req := WebRTCSessionRequest{
		OIDCToken:  "generic-token",
		OidcGoogle: "legacy-token",
	}

	if got := req.oidcToken(); got != "generic-token" {
		t.Fatalf("oidcToken() = %q, want generic token", got)
	}
}

func TestOIDCIdentityIncludesIssuerAudienceAndSubject(t *testing.T) {
	idToken := &oidc.IDToken{
		Issuer:   "https://auth.example.com/application/o/jetkvm/",
		Audience: []string{"jetkvm-cloud-api"},
		Subject:  "user-123",
	}

	got, err := oidcIdentity(idToken)
	if err != nil {
		t.Fatalf("oidcIdentity() returned error: %v", err)
	}
	want := "https://auth.example.com/application/o/jetkvm/:jetkvm-cloud-api:user-123"
	if got != want {
		t.Fatalf("oidcIdentity() = %q, want %q", got, want)
	}
}

func TestOIDCIdentityRejectsMissingClaims(t *testing.T) {
	tests := []struct {
		name    string
		idToken *oidc.IDToken
	}{
		{
			name: "missing issuer",
			idToken: &oidc.IDToken{
				Audience: []string{"jetkvm-cloud-api"},
				Subject:  "user-123",
			},
		},
		{
			name: "missing audience",
			idToken: &oidc.IDToken{
				Issuer:  "https://auth.example.com/application/o/jetkvm/",
				Subject: "user-123",
			},
		},
		{
			name: "missing subject",
			idToken: &oidc.IDToken{
				Issuer:   "https://auth.example.com/application/o/jetkvm/",
				Audience: []string{"jetkvm-cloud-api"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := oidcIdentity(tt.idToken); err == nil {
				t.Fatal("oidcIdentity() returned nil error, want missing claim error")
			}
		})
	}
}

func TestLegacyOIDCIdentityKeepsGoogleCompatibleFormat(t *testing.T) {
	idToken := &oidc.IDToken{
		Issuer:   "https://accounts.google.com",
		Audience: []string{"legacy-client"},
		Subject:  "google-user-123",
	}

	got, err := legacyOIDCIdentity(idToken)
	if err != nil {
		t.Fatalf("legacyOIDCIdentity() returned error: %v", err)
	}
	want := "legacy-client:google-user-123"
	if got != want {
		t.Fatalf("legacyOIDCIdentity() = %q, want %q", got, want)
	}
}

func TestHandleCloudRegisterCompletesProviderAgnosticFakeAdoption(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalConfig := config
	originalConfigPath := configPath
	t.Cleanup(func() {
		config = originalConfig
		configPath = originalConfigPath
	})

	oidcIssuer, oidcToken := newTestOIDCIssuer(t, "jetkvm-cloud-api", "user-123")
	cloudAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/devices/token" {
			t.Fatalf("unexpected cloud API request: %s %s", r.Method, r.URL.Path)
		}

		var req struct {
			TempToken string `json:"tempToken"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode cloud API token request: %v", err)
		}
		if req.TempToken != "temp-token" {
			t.Fatalf("tempToken = %q, want temp-token", req.TempToken)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"secretToken":"permanent-cloud-token"}`))
	}))
	t.Cleanup(cloudAPI.Close)

	defaultConfig := getDefaultConfig()
	config = &defaultConfig
	config.CloudURL = cloudAPI.URL
	configPath = filepath.Join(t.TempDir(), "kvm_config.json")

	body, err := json.Marshal(map[string]string{
		"token":        "temp-token",
		"oidcToken":    oidcToken,
		"oidcClientId": "jetkvm-cloud-api",
		"oidcIssuer":   oidcIssuer,
	})
	if err != nil {
		t.Fatalf("failed to marshal register request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/cloud/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	handleCloudRegister(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if config.CloudToken != "permanent-cloud-token" {
		t.Fatalf("CloudToken = %q, want permanent-cloud-token", config.CloudToken)
	}
	if config.OIDCIssuer != oidcIssuer {
		t.Fatalf("OIDCIssuer = %q, want %q", config.OIDCIssuer, oidcIssuer)
	}
	wantOIDCIdentity := oidcIssuer + ":jetkvm-cloud-api:user-123"
	if config.OIDCIdentity != wantOIDCIdentity {
		t.Fatalf("OIDCIdentity = %q, want %q", config.OIDCIdentity, wantOIDCIdentity)
	}
	if config.GoogleIdentity != "jetkvm-cloud-api:user-123" {
		t.Fatalf("GoogleIdentity = %q, want legacy-compatible identity", config.GoogleIdentity)
	}

	var saved Config
	rawSavedConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read saved config: %v", err)
	}
	if err := json.Unmarshal(rawSavedConfig, &saved); err != nil {
		t.Fatalf("failed to decode saved config: %v", err)
	}
	if saved.OIDCIssuer != oidcIssuer || saved.OIDCIdentity != wantOIDCIdentity {
		t.Fatalf("saved OIDC config = issuer %q identity %q", saved.OIDCIssuer, saved.OIDCIdentity)
	}
}

func newTestOIDCIssuer(t *testing.T, clientID string, subject string) (string, string) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate test RSA key: %v", err)
	}

	var issuerURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                issuerURL,
			"authorization_endpoint":                issuerURL + "/authorize",
			"token_endpoint":                        issuerURL + "/token",
			"jwks_uri":                              issuerURL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		key := jose.JSONWebKey{
			Key:       &privateKey.PublicKey,
			KeyID:     "test-key",
			Use:       "sig",
			Algorithm: string(jose.RS256),
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []jose.JSONWebKey{key}})
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	issuerURL = server.URL

	return issuerURL, signTestIDToken(t, privateKey, issuerURL, clientID, subject)
}

func signTestIDToken(
	t *testing.T,
	privateKey *rsa.PrivateKey,
	issuer string,
	clientID string,
	subject string,
) string {
	t.Helper()

	options := (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key")
	signer, err := jose.NewSigner(jose.SigningKey{
		Algorithm: jose.RS256,
		Key:       privateKey,
	}, options)
	if err != nil {
		t.Fatalf("failed to create JWT signer: %v", err)
	}

	now := time.Now()
	payload, err := json.Marshal(map[string]any{
		"iss": issuer,
		"sub": subject,
		"aud": clientID,
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("failed to marshal ID token claims: %v", err)
	}

	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatalf("failed to sign ID token: %v", err)
	}
	token, err := signed.CompactSerialize()
	if err != nil {
		t.Fatalf("failed to serialize ID token: %v", err)
	}
	return token
}
