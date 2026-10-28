//go:build linux && arm

package kvm

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func apiTestRouter(t *testing.T, mode, password string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	config = &Config{LocalAuthMode: mode, HashedPassword: string(hashed), LocalAuthToken: "cookie-token"}
	apiAuthCache.hash = ""
	passwordRateLimiter = NewRateLimiter()
	r := gin.New()
	r.POST("/api/rpc", apiAuthMiddleware(), handleAPIRPC)
	return r
}

func apiPost(r *gin.Engine, body string, auth func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/rpc", strings.NewReader(body))
	if auth != nil {
		auth(req)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAPIRPCAuth(t *testing.T) {
	r := apiTestRouter(t, "password", "secret")
	ping := `{"jsonrpc":"2.0","id":1,"method":"ping"}`

	if w := apiPost(r, ping, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("no credentials: status %d, want 401", w.Code)
	}
	if w := apiPost(r, ping, func(q *http.Request) { q.SetBasicAuth("api", "wrong") }); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: status %d, want 401", w.Code)
	}
	w := apiPost(r, ping, func(q *http.Request) { q.SetBasicAuth("api", "secret") })
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"result":"pong"`) {
		t.Fatalf("basic auth: status %d body %s", w.Code, w.Body.String())
	}
	w = apiPost(r, ping, func(q *http.Request) { q.AddCookie(&http.Cookie{Name: "authToken", Value: "cookie-token"}) })
	if w.Code != http.StatusOK {
		t.Fatalf("session cookie: status %d body %s", w.Code, w.Body.String())
	}

	open := apiTestRouter(t, "noPassword", "")
	if w := apiPost(open, ping, nil); w.Code != http.StatusOK {
		t.Fatalf("noPassword mode: status %d body %s", w.Code, w.Body.String())
	}
}

func TestAPIRPCRequests(t *testing.T) {
	r := apiTestRouter(t, "noPassword", "")

	if w := apiPost(r, `not json`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("bad json: status %d", w.Code)
	}
	w := apiPost(r, `{"jsonrpc":"2.0","id":7,"method":"noSuchMethod"}`, nil)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"id":7`) {
		t.Fatalf("unknown method: status %d body %s", w.Code, w.Body.String())
	}
	if w := apiPost(r, strings.Repeat("x", apiRPCMaxBody+1), nil); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: status %d", w.Code)
	}
}

func TestAPIPasswordCache(t *testing.T) {
	apiTestRouter(t, "password", "secret")
	if !apiPasswordOK("secret") || apiAuthCache.hash != config.HashedPassword {
		t.Fatal("first check must verify and fill the cache")
	}
	if apiPasswordOK("wrong") {
		t.Fatal("cache must not accept another password")
	}
	config.HashedPassword = "$2a$10$replaced"
	if apiPasswordOK("secret") {
		t.Fatal("a changed stored hash must invalidate the cache")
	}
}

func TestAPIRateLimit(t *testing.T) {
	r := apiTestRouter(t, "password", "secret")
	ping := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	wrong := func(q *http.Request) { q.SetBasicAuth("api", "wrong") }
	for i := 1; i <= 5; i++ {
		if w := apiPost(r, ping, wrong); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d, want 401", i, w.Code)
		}
	}
	w := apiPost(r, ping, func(q *http.Request) { q.SetBasicAuth("api", "secret") })
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("after five failures: status %d, want 429 with Retry-After, even for the right password", w.Code)
	}
}

func TestAPIPasswordChangeDuringCheck(t *testing.T) {
	slow, _ := bcrypt.GenerateFromPassword([]byte("old"), 12)
	fresh, _ := bcrypt.GenerateFromPassword([]byte("new"), bcrypt.MinCost)
	config = &Config{LocalAuthMode: "password", HashedPassword: string(slow)}
	apiAuthCache.hash = ""
	done := make(chan bool, 1)
	go func() { done <- apiPasswordOK("old") }()
	time.Sleep(20 * time.Millisecond) // the bcrypt check above is still running
	config.HashedPassword = string(fresh)
	if !<-done {
		t.Fatal("old password must verify against the hash it was checked with")
	}
	if apiPasswordOK("old") {
		t.Fatal("old password accepted after the change")
	}
	if !apiPasswordOK("new") {
		t.Fatal("new password rejected")
	}
}
