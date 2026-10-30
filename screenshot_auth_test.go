//go:build linux && arm

package kvm

import (
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func TestVerifyBasicAuthPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if config == nil {
		config = &Config{}
	}

	password := "supersecret123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	config.HashedPassword = string(hash)
	verifiedBasicAuthHash = ""
	verifiedBasicAuthSHA = [32]byte{}

	if !verifyBasicAuthPassword(password) {
		t.Fatalf("expected password to verify")
	}

	// Verify cached
	pwSHA := sha256.Sum256([]byte(password))
	if verifiedBasicAuthHash != string(hash) || verifiedBasicAuthSHA != pwSHA {
		t.Fatalf("expected cache to be populated with current hash and sha")
	}

	// Second verification should use fast path
	if !verifyBasicAuthPassword(password) {
		t.Fatalf("expected cached password to verify")
	}

	// Wrong password should fail
	if verifyBasicAuthPassword("wrongpassword") {
		t.Fatalf("expected wrong password to fail")
	}

	// Password change drops previous cache
	newPassword := "differentSecret456"
	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("failed to hash new password: %v", err)
	}
	config.HashedPassword = string(newHash)

	if verifyBasicAuthPassword(password) {
		t.Fatalf("expected old password to fail after config change")
	}
	if !verifyBasicAuthPassword(newPassword) {
		t.Fatalf("expected new password to verify")
	}
}

func TestApiAuthMiddlewareRateLimiting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if config == nil {
		config = &Config{}
	}

	password := "correctpassword"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	config.LocalAuthMode = "password"
	config.HashedPassword = string(hash)
	config.LocalAuthToken = "valid-token"

	router := gin.New()
	router.GET("/test-auth", apiAuthMiddleware(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	testIP := "192.0.2.100"

	// 5 failed attempts
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test-auth", nil)
		req.Header.Set("X-Forwarded-For", testIP)
		req.RemoteAddr = testIP + ":12345"
		req.SetBasicAuth("api", "wrong")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i+1, w.Code)
		}
	}

	// 6th attempt should be rate limited (429)
	req := httptest.NewRequest("GET", "/test-auth", nil)
	req.Header.Set("X-Forwarded-For", testIP)
	req.RemoteAddr = testIP + ":12345"
	req.SetBasicAuth("api", "wrong")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests, got %d", w.Code)
	}

	// Even correct password gets locked out when rate limited
	reqCorrect := httptest.NewRequest("GET", "/test-auth", nil)
	reqCorrect.Header.Set("X-Forwarded-For", testIP)
	reqCorrect.RemoteAddr = testIP + ":12345"
	reqCorrect.SetBasicAuth("api", password)
	wCorrect := httptest.NewRecorder()
	router.ServeHTTP(wCorrect, reqCorrect)

	if wCorrect.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests even with correct password while rate limited, got %d", wCorrect.Code)
	}
}
