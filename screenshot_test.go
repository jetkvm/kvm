//go:build linux && arm

package kvm

import (
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/jetkvm/kvm/internal/native"
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

// recordingVideoNative notes the capture calls and serves frames.
type recordingVideoNative struct {
	native.EmptyNativeInterface
	calls []string
}

func (n *recordingVideoNative) VideoStart() error { n.calls = append(n.calls, "start"); return nil }
func (n *recordingVideoNative) VideoStop() error  { n.calls = append(n.calls, "stop"); return nil }
func (n *recordingVideoNative) VideoGetSnapshot() ([]byte, error) {
	n.calls = append(n.calls, "snapshot")
	return []byte("jpeg"), nil
}

func TestScreenshotLeavesAPausedStreamPausedAfterTheLease(t *testing.T) {
	fake := &recordingVideoNative{}
	original := nativeInstance
	nativeInstance = fake
	screenshotLeaseMu.Lock()
	screenshotLeaseUntil = time.Time{}
	screenshotLeaseMu.Unlock()
	videoPauseMu.Lock()
	videoPausedBy = &Session{}
	videoPauseMu.Unlock()
	defer func() {
		nativeInstance = original
		videoPauseMu.Lock()
		videoPausedBy = nil
		videoPauseMu.Unlock()
	}()

	frame, err := captureScreenshot()
	if err != nil || len(frame) == 0 {
		t.Fatalf("frame=%d err=%v", len(frame), err)
	}
	// A cold start skips the first frame, and capture is left running for
	// the lease instead of being stopped at once.
	if want := []string{"start", "snapshot", "snapshot"}; len(fake.calls) != len(want) {
		t.Fatalf("calls = %v, want %v", fake.calls, want)
	}
	for i, c := range fake.calls {
		if want := []string{"start", "snapshot", "snapshot"}; c != want[i] {
			t.Fatalf("call %d = %s, want %s", i, c, want[i])
		}
	}
	// A second screenshot inside the lease takes a single, fresh frame without restarting start.
	if _, err := captureScreenshot(); err != nil {
		t.Fatal(err)
	}
	if got := fake.calls[len(fake.calls)-1]; got != "snapshot" || fake.calls[len(fake.calls)-2] != "start" {
		t.Fatalf("calls = %v", fake.calls)
	}

	// Once the lease is over, capture stops because the pause is held.
	screenshotLeaseMu.Lock()
	screenshotLeaseUntil = time.Now().Add(-time.Second)
	if screenshotLeaseTimer != nil {
		screenshotLeaseTimer.Stop()
	}
	screenshotLeaseMu.Unlock()
	releaseScreenshotLease()
	if got := fake.calls[len(fake.calls)-1]; got != "stop" {
		t.Fatalf("capture must stop after the lease while a session holds the pause, calls=%v", fake.calls)
	}
}

// pauseCheckingNative records whether the pause lock was held when capture
// was started.
type pauseCheckingNative struct {
	native.EmptyNativeInterface
	startedUnlocked bool
}

func (n *pauseCheckingNative) VideoStart() error {
	if videoPauseMu.TryLock() {
		n.startedUnlocked = true
		videoPauseMu.Unlock()
	}
	return nil
}

func (n *pauseCheckingNative) VideoGetSnapshot() ([]byte, error) { return []byte("jpeg"), nil }

func TestScreenshotStartsCaptureUnderPauseLock(t *testing.T) {
	fake := &pauseCheckingNative{}
	original := nativeInstance
	nativeInstance = fake
	defer func() { nativeInstance = original }()

	if _, err := captureScreenshot(); err != nil {
		t.Fatal(err)
	}
	if fake.startedUnlocked {
		t.Fatal("capture was started without the pause lock, so a pause could slip in and be undone")
	}
}

