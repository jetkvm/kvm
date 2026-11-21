//go:build linux && arm

package kvm

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testAuthToken = "test-rpc-token-abc"

func setupTestRPCConfig(t *testing.T, cfg Config) {
	t.Helper()
	orig := config
	config = &cfg
	t.Cleanup(func() { config = orig })
}

func newRPCRequest(t *testing.T, path string, body []byte, withAuth bool) *http.Request {
	t.Helper()
	var req *http.Request
	if len(body) > 0 {
		req, _ = http.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, _ = http.NewRequest(http.MethodPost, path, nil)
	}
	if withAuth {
		req.AddCookie(&http.Cookie{Name: "authToken", Value: testAuthToken})
	}
	return req
}

func TestRPCHTTP_Unauthorized(t *testing.T) {
	setupTestRPCConfig(t, Config{LocalAuthToken: testAuthToken})
	r := setupRouter()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRPCRequest(t, "/rpc/getDeviceID", nil, false))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d: %s", http.StatusUnauthorized, w.Code, w.Body.String())
	}
}

func TestRPCHTTP_NonAllowlisted_404(t *testing.T) {
	setupTestRPCConfig(t, Config{LocalAuthMode: "noPassword"})
	r := setupRouter()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRPCRequest(t, "/rpc/reboot", nil, false))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected %d, got %d: %s", http.StatusNotFound, w.Code, w.Body.String())
	}
}

func TestRPCHTTP_GetDeviceID_Shape(t *testing.T) {
	setupTestRPCConfig(t, Config{LocalAuthMode: "noPassword"})
	r := setupRouter()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRPCRequest(t, "/rpc/getDeviceID", nil, false))

	if w.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
	var result string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response as string: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty device ID")
	}
}

func TestRPCHTTP_GetTLSState_Shape(t *testing.T) {
	setupTestRPCConfig(t, Config{LocalAuthMode: "noPassword"})
	r := setupRouter()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRPCRequest(t, "/rpc/getTLSState", nil, false))

	if w.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
	var result TLSState
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response as TLSState: %v", err)
	}
}

func TestRPCHTTP_SetTLSState_ValidBody_200(t *testing.T) {
	setupTestRPCConfig(t, Config{LocalAuthMode: "noPassword"})
	r := setupRouter()

	body, _ := json.Marshal(map[string]any{"state": map[string]any{"mode": "disabled"}})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRPCRequest(t, "/rpc/setTLSState", body, false))

	if w.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

func TestRPCHTTP_SetTLSState_MalformedJSON_400(t *testing.T) {
	setupTestRPCConfig(t, Config{LocalAuthMode: "noPassword"})
	r := setupRouter()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRPCRequest(t, "/rpc/setTLSState", []byte("{invalid"), false))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestRPCHTTP_SetTLSState_InvalidMode_500(t *testing.T) {
	setupTestRPCConfig(t, Config{LocalAuthMode: "noPassword"})
	r := setupRouter()

	body, _ := json.Marshal(map[string]any{"state": map[string]any{"mode": "invalid_mode"}})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRPCRequest(t, "/rpc/setTLSState", body, false))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected %d, got %d: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// TestRPCHTTP_Panic_Returns500 verifies that callRPCHandler's panic recovery
// (jsonrpc.go:526-537) is wired through the HTTP dispatch path.
// It forces a nil pointer dereference by setting certStore to nil while
// the TLS mode is "custom", causing getTLSState to panic on certStore.GetCertificate.
func TestRPCHTTP_Panic_Returns500(t *testing.T) {
	setupTestRPCConfig(t, Config{LocalAuthMode: "noPassword", TLSMode: "custom"})

	origCertStore := certStore
	certStore = nil
	t.Cleanup(func() { certStore = origCertStore })

	r := setupRouter()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, newRPCRequest(t, "/rpc/getTLSState", nil, false))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected %d, got %d: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}
