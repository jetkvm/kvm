//go:build linux && arm

package kvm

import (
	"testing"

	"github.com/coreos/go-oidc/v3/oidc"
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
