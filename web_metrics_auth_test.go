package kvm

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

const (
	metricsTestPassword = "correct horse"
	metricsTestToken    = "session-token"
)

type metricsRequest struct {
	password string
	cookie   string
}

func setupMetricsAuthTest(t *testing.T, authMode string) func(metricsRequest) *httptest.ResponseRecorder {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(metricsTestPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}

	previousConfig := config
	previousLimiter := passwordRateLimiter
	config = &Config{
		LocalAuthMode:  authMode,
		LocalAuthToken: metricsTestToken,
		HashedPassword: string(hash),
	}
	passwordRateLimiter = NewRateLimiter()
	t.Cleanup(func() {
		passwordRateLimiter.Stop()
		passwordRateLimiter = previousLimiter
		config = previousConfig
	})

	router := setupRouter()
	return func(mr metricsRequest) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		if mr.password != "" {
			req.SetBasicAuth("prometheus", mr.password)
		}
		if mr.cookie != "" {
			req.AddCookie(&http.Cookie{Name: "authToken", Value: mr.cookie})
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
}

func expectStatus(t *testing.T, rec *httptest.ResponseRecorder, want int, what string) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("%s: got status %d, want %d", what, rec.Code, want)
	}
}

func TestMetricsOpenInNoPasswordMode(t *testing.T) {
	get := setupMetricsAuthTest(t, "noPassword")
	expectStatus(t, get(metricsRequest{}), http.StatusOK, "no credentials")
}

func TestMetricsRequiresCredentialsInPasswordMode(t *testing.T) {
	get := setupMetricsAuthTest(t, "password")

	rec := get(metricsRequest{})
	expectStatus(t, rec, http.StatusUnauthorized, "no credentials")
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("no credentials: WWW-Authenticate header is missing")
	}

	expectStatus(t, get(metricsRequest{cookie: "wrong-token"}), http.StatusUnauthorized, "wrong cookie")
	expectStatus(t, get(metricsRequest{cookie: metricsTestToken}), http.StatusOK, "session cookie")
	expectStatus(t, get(metricsRequest{password: metricsTestPassword}), http.StatusOK, "correct password")
}

func TestMetricsRequestsWithoutPasswordDoNotCountAsFailures(t *testing.T) {
	get := setupMetricsAuthTest(t, "password")

	for i := 0; i < maxAttempts*2; i++ {
		expectStatus(t, get(metricsRequest{}), http.StatusUnauthorized, "no credentials")
		expectStatus(t, get(metricsRequest{cookie: "wrong-token"}), http.StatusUnauthorized, "wrong cookie")
	}
	expectStatus(t, get(metricsRequest{password: metricsTestPassword}), http.StatusOK, "correct password")
}

func TestMetricsLocksOutAfterRepeatedWrongPasswords(t *testing.T) {
	get := setupMetricsAuthTest(t, "password")

	for i := 0; i < maxAttempts; i++ {
		expectStatus(t, get(metricsRequest{password: "wrong"}), http.StatusUnauthorized, "wrong password")
	}

	rec := get(metricsRequest{password: metricsTestPassword})
	expectStatus(t, rec, http.StatusTooManyRequests, "correct password during lockout")
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("correct password during lockout: Retry-After header is missing")
	}

	expectStatus(t, get(metricsRequest{cookie: metricsTestToken}), http.StatusOK, "session cookie during lockout")
	expectStatus(t, get(metricsRequest{}), http.StatusUnauthorized, "no credentials during lockout")
}

func TestMetricsCorrectPasswordClearsEarlierFailures(t *testing.T) {
	get := setupMetricsAuthTest(t, "password")

	for round := 0; round < 2; round++ {
		for i := 0; i < maxAttempts-1; i++ {
			expectStatus(t, get(metricsRequest{password: "wrong"}), http.StatusUnauthorized, "wrong password")
		}
		expectStatus(t, get(metricsRequest{password: metricsTestPassword}), http.StatusOK, "correct password")
	}
}

func TestMetricsBasicAuthOnDeviceWithoutPasswordDoesNotLockOutSetup(t *testing.T) {
	get := setupMetricsAuthTest(t, "")
	config.HashedPassword = ""
	config.LocalAuthToken = ""

	for i := 0; i < maxAttempts*2; i++ {
		expectStatus(t, get(metricsRequest{password: "stale"}), http.StatusUnauthorized, "basic auth before setup")
	}

	req := httptest.NewRequest(http.MethodPost, "/device/setup", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	setupRouter().ServeHTTP(rec, req)
	expectStatus(t, rec, http.StatusBadRequest, "setup with an invalid body after the scrapes")
}
