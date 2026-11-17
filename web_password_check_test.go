package kvm

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func basicAuthRouteRequest(router *gin.Engine, password string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/developer/probe", nil)
	req.SetBasicAuth("developer", password)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func loginRequest(router *gin.Engine, password string) *httptest.ResponseRecorder {
	body := strings.NewReader(`{"password":"` + password + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/login-local", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestBasicAuthRouteLocksOutAfterRepeatedWrongPasswords(t *testing.T) {
	setupMetricsAuthTest(t, "password")

	router := gin.New()
	router.GET("/developer/probe", basicAuthProtectedMiddleware(false), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	expectStatus(t, basicAuthRouteRequest(router, metricsTestPassword), http.StatusNoContent, "correct password")

	for i := 0; i < maxAttempts; i++ {
		expectStatus(t, basicAuthRouteRequest(router, "wrong"), http.StatusUnauthorized, "wrong password")
	}

	rec := basicAuthRouteRequest(router, metricsTestPassword)
	expectStatus(t, rec, http.StatusTooManyRequests, "correct password during lockout")
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("correct password during lockout: Retry-After header is missing")
	}
}

func TestWrongPasswordsOnLoginAndMetricsShareOneLockout(t *testing.T) {
	getMetrics := setupMetricsAuthTest(t, "password")
	router := setupRouter()

	for i := 0; i < maxAttempts-2; i++ {
		expectStatus(t, loginRequest(router, "wrong"), http.StatusUnauthorized, "wrong password on login")
	}
	for i := 0; i < 2; i++ {
		expectStatus(t, getMetrics(metricsRequest{password: "wrong"}), http.StatusUnauthorized, "wrong password on metrics")
	}

	expectStatus(t, getMetrics(metricsRequest{password: metricsTestPassword}), http.StatusTooManyRequests, "correct password on metrics")
	expectStatus(t, loginRequest(router, "wrong"), http.StatusTooManyRequests, "login during lockout")
}
