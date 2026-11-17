//go:build linux && arm

package kvm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pion/webrtc/v4"

	"github.com/jetkvm/kvm/internal/hidrpc"
)

type sessionTimeoutTest struct {
	t      *testing.T
	router *gin.Engine
	now    time.Time
	token  string
}

func setupSessionTimeoutTest(t *testing.T, authMode string, idleMinutes int) *sessionTimeoutTest {
	t.Helper()
	setupMetricsAuthTest(t, authMode)

	previousPath := configPath
	previousClock := localAuthClock
	previousEpoch := localAuthEpoch
	previousSession := currentSession
	configPath = filepath.Join(t.TempDir(), "kvm_config.json")
	t.Cleanup(func() {
		configPath = previousPath
		localAuthClock = previousClock
		localAuthEpoch = previousEpoch
		currentSession = previousSession
		localAuthLastActivity.Store(0)
		expiredLocalAuthToken = ""
	})

	st := &sessionTimeoutTest{t: t, router: setupRouter(), now: time.Unix(1_800_000_000, 0)}
	localAuthClock = func() time.Time { return st.now }
	localAuthEpoch = st.now
	localAuthLastActivity.Store(0)
	expiredLocalAuthToken = ""
	currentSession = nil

	config.LocalAuthIdleMinutes = idleMinutes
	if authMode == "password" {
		st.token = "login-token"
		setLocalAuthToken(st.token)
	}
	return st
}

func (st *sessionTimeoutTest) advance(d time.Duration) {
	st.now = st.now.Add(d)
}

func (st *sessionTimeoutTest) request(path, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "authToken", Value: cookie})
	}
	rec := httptest.NewRecorder()
	st.router.ServeHTTP(rec, req)
	return rec
}

func (st *sessionTimeoutTest) expectDevice(want int, what string) *httptest.ResponseRecorder {
	st.t.Helper()
	rec := st.request("/device", st.token)
	expectStatus(st.t, rec, want, what)
	return rec
}

func expectSessionExpired(t *testing.T, rec *httptest.ResponseRecorder, what string) {
	t.Helper()
	expectStatus(t, rec, http.StatusUnauthorized, what)
	var body struct{ Code string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != sessionExpiredCode {
		t.Fatalf("%s: body %q does not report an expired session", what, rec.Body.String())
	}
}

func expectCookieCleared(t *testing.T, rec *httptest.ResponseRecorder, what string) {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == "authToken" && c.Value == "" && c.MaxAge < 0 {
			return
		}
	}
	t.Fatalf("%s: response does not clear the authToken cookie", what)
}

func savedLocalAuthToken(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved Config
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	return saved.LocalAuthToken
}

func (st *sessionTimeoutTest) openLocalSession() *Session {
	st.t.Helper()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		st.t.Fatal(err)
	}
	rpc, err := pc.CreateDataChannel("rpc", nil)
	if err != nil {
		st.t.Fatal(err)
	}
	session := &Session{
		peerConnection: pc,
		RPCChannel:     rpc,
		done:           make(chan struct{}),
		localAuthToken: st.token,
	}
	st.t.Cleanup(func() { _ = pc.Close() })
	currentSession = session
	return session
}

func waitForPeerClosed(t *testing.T, s *Session) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s.peerConnection.ConnectionState() == webrtc.PeerConnectionStateClosed {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the WebRTC session of the expired login is still open")
}

func sendHid(s *Session, t hidrpc.MessageType) {
	handler := getOnHidMessageHandler(s, webrtcLogger, "hidrpc")
	handler(webrtc.DataChannelMessage{Data: []byte{byte(t), 0, 0, 0, 0, 0, 0, 0, 0}})
}

func sendRPC(s *Session, method, params string) {
	body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":` + params + `}`
	onRPCMessage(webrtc.DataChannelMessage{IsString: true, Data: []byte(body)}, s)
}

func TestSessionTimeoutExpiresIdleLogin(t *testing.T) {
	st := setupSessionTimeoutTest(t, "password", 15)

	st.advance(14 * time.Minute)
	st.expectDevice(http.StatusOK, "request before the timeout")

	st.advance(15 * time.Minute)
	rec := st.expectDevice(http.StatusUnauthorized, "request after the timeout")
	expectSessionExpired(t, rec, "request after the timeout")
	expectCookieCleared(t, rec, "request after the timeout")
	if got := savedLocalAuthToken(t); got != "" {
		t.Fatalf("saved config still holds the expired token %q", got)
	}

	rec = st.expectDevice(http.StatusUnauthorized, "second request with the expired cookie")
	expectSessionExpired(t, rec, "second request with the expired cookie")
	expectStatus(t, st.request("/metrics", st.token), http.StatusUnauthorized, "metrics with the expired cookie")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.SetBasicAuth("prometheus", metricsTestPassword)
	rec = httptest.NewRecorder()
	st.router.ServeHTTP(rec, req)
	expectStatus(t, rec, http.StatusOK, "metrics with basic auth after the login expired")
}

func TestSessionTimeoutRequestsExtendLogin(t *testing.T) {
	st := setupSessionTimeoutTest(t, "password", 15)

	for i := 0; i < 6; i++ {
		st.advance(10 * time.Minute)
		st.expectDevice(http.StatusOK, "request every 10 minutes")
	}
	st.advance(15 * time.Minute)
	st.expectDevice(http.StatusUnauthorized, "request 15 minutes after the last one")
}

func TestSessionTimeoutClosesIdleWebRTCSession(t *testing.T) {
	st := setupSessionTimeoutTest(t, "password", 15)
	session := st.openLocalSession()

	for i := 0; i < 3; i++ {
		st.advance(5 * time.Minute)
		sendHid(session, hidrpc.TypeKeypressKeepAliveReport)
		sendRPC(session, "getLocalAuthSessionTimeout", "{}")
		sendRPC(session, "ping", "{}")
	}

	if !expireIdleLocalAuth() {
		t.Fatal("the sweeper did not expire a login with an open but idle WebRTC session")
	}
	waitForPeerClosed(t, session)
	expectSessionExpired(t, st.request("/device", st.token), "request after the sweeper expired the login")
}

func TestSessionTimeoutInputKeepsLoginAlive(t *testing.T) {
	inputs := map[string]func(*Session){
		"keyboard": func(s *Session) { sendHid(s, hidrpc.TypeKeypressReport) },
		"pointer":  func(s *Session) { sendHid(s, hidrpc.TypePointerReport) },
		"rpc call": func(s *Session) { sendRPC(s, "emitTestLog", `{"level":"DEBUG"}`) },
	}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			st := setupSessionTimeoutTest(t, "password", 15)
			session := st.openLocalSession()

			for i := 0; i < 6; i++ {
				st.advance(10 * time.Minute)
				input(session)
				if expireIdleLocalAuth() {
					t.Fatal("login expired 10 minutes after the last input")
				}
			}
			st.expectDevice(http.StatusOK, "request after an hour of regular input")
			if session.peerConnection.ConnectionState() == webrtc.PeerConnectionStateClosed {
				t.Fatal("the WebRTC session was closed although the user was active")
			}
		})
	}
}

func TestSessionTimeoutInputOfCloudSessionDoesNotKeepLocalLogin(t *testing.T) {
	st := setupSessionTimeoutTest(t, "password", 15)
	session := st.openLocalSession()
	session.localAuthToken = ""

	st.advance(10 * time.Minute)
	sendHid(session, hidrpc.TypeKeypressReport)
	st.advance(10 * time.Minute)

	if !expireIdleLocalAuth() {
		t.Fatal("cloud session input kept the local login alive")
	}
	time.Sleep(100 * time.Millisecond)
	if session.peerConnection.ConnectionState() == webrtc.PeerConnectionStateClosed {
		t.Fatal("the local login expiry closed a cloud session")
	}
}

func TestSessionTimeoutRestartInvalidatesCookie(t *testing.T) {
	st := setupSessionTimeoutTest(t, "password", 15)
	localAuthLastActivity.Store(0)

	expectSessionExpired(t, st.request("/device", st.token), "first request after a restart")
}

func TestSessionTimeoutOffKeepsCookieAcrossRestart(t *testing.T) {
	st := setupSessionTimeoutTest(t, "password", 0)
	localAuthLastActivity.Store(0)

	st.advance(365 * 24 * time.Hour)
	if expireIdleLocalAuth() {
		t.Fatal("the sweeper expired a login with the timeout off")
	}
	st.expectDevice(http.StatusOK, "request a year later with the timeout off")
}

func TestSessionTimeoutDoesNotApplyInNoPasswordMode(t *testing.T) {
	st := setupSessionTimeoutTest(t, "noPassword", 15)

	st.advance(24 * time.Hour)
	if expireIdleLocalAuth() {
		t.Fatal("the sweeper expired a login in noPassword mode")
	}
	expectStatus(t, st.request("/device", ""), http.StatusOK, "request in noPassword mode")
}

func TestSessionTimeoutChangeCountsFromTheChange(t *testing.T) {
	st := setupSessionTimeoutTest(t, "password", 0)
	session := st.openLocalSession()

	st.advance(2 * time.Hour)
	sendRPC(session, "setLocalAuthSessionTimeout", `{"timeoutMinutes":15}`)
	if config.LocalAuthIdleMinutes != 15 {
		t.Fatalf("timeout is %d minutes, want 15", config.LocalAuthIdleMinutes)
	}
	st.expectDevice(http.StatusOK, "request right after the timeout was turned on")

	st.advance(16 * time.Minute)
	st.expectDevice(http.StatusUnauthorized, "request 16 minutes after the last activity")
}

func TestSessionTimeoutRejectsOutOfRangeValues(t *testing.T) {
	setupSessionTimeoutTest(t, "password", 15)

	for _, minutes := range []int{-1, localAuthMaxIdleMinutes + 1} {
		if err := rpcSetLocalAuthSessionTimeout(minutes); err == nil {
			t.Fatalf("timeout of %d minutes was accepted", minutes)
		}
	}
	if config.LocalAuthIdleMinutes != 15 {
		t.Fatalf("rejected values changed the timeout to %d minutes", config.LocalAuthIdleMinutes)
	}
}
