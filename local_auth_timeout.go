package kvm

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/jetkvm/kvm/internal/hidrpc"
)

const (
	localAuthMaxIdleMinutes                            = 24 * 60
	localAuthExpirySweepInterval                       = 10 * time.Second
	signalingStatusSessionExpired websocket.StatusCode = 4003
	sessionExpiredCode                                 = "session_expired"
)

var (
	localAuthClock = time.Now
	localAuthEpoch = time.Now()

	localAuthLastActivity atomic.Int64

	localAuthMu           sync.Mutex
	expiredLocalAuthToken string
)

type localAuthCookieState int

const (
	localAuthCookieInvalid localAuthCookieState = iota
	localAuthCookieValid
	localAuthCookieExpired
)

func localAuthSinceEpoch() time.Duration {
	return localAuthClock().Sub(localAuthEpoch)
}

func recordLocalAuthActivity() {
	localAuthLastActivity.Store(int64(localAuthSinceEpoch()) + 1)
}

func localAuthIdleTimeout() time.Duration {
	if config.LocalAuthMode != "password" {
		return 0
	}
	return time.Duration(config.LocalAuthIdleMinutes) * time.Minute
}

func localAuthIdleLocked() bool {
	timeout := localAuthIdleTimeout()
	if timeout <= 0 || config.LocalAuthToken == "" {
		return false
	}
	last := localAuthLastActivity.Load()
	if last == 0 {
		return true
	}
	return localAuthSinceEpoch()-time.Duration(last-1) >= timeout
}

func setLocalAuthToken(token string) {
	localAuthMu.Lock()
	defer localAuthMu.Unlock()
	if token != "" {
		recordLocalAuthActivity()
	}
	config.LocalAuthToken = token
}

func expireIdleLocalAuthLocked() bool {
	if !localAuthIdleLocked() {
		return false
	}
	expiredLocalAuthToken = config.LocalAuthToken
	config.LocalAuthToken = ""
	return true
}

func finishLocalAuthExpiry() {
	logger.Info().Msg("local session expired after inactivity")
	if err := SaveConfig(); err != nil {
		logger.Warn().Err(err).Msg("failed to save config after the local session expired")
	}
	if s := currentSession; s != nil {
		s.endForExpiredLogin()
	}
}

func expireIdleLocalAuth() bool {
	localAuthMu.Lock()
	expired := expireIdleLocalAuthLocked()
	localAuthMu.Unlock()
	if expired {
		finishLocalAuthExpiry()
	}
	return expired
}

func runLocalAuthExpirySweeper() {
	ticker := time.NewTicker(localAuthExpirySweepInterval)
	defer ticker.Stop()
	for range ticker.C {
		expireIdleLocalAuth()
	}
}

func checkLocalAuthCookie(c *gin.Context) localAuthCookieState {
	token, err := c.Cookie("authToken")
	if err != nil || token == "" {
		return localAuthCookieInvalid
	}

	localAuthMu.Lock()
	if token == expiredLocalAuthToken {
		localAuthMu.Unlock()
		return localAuthCookieExpired
	}
	if token != config.LocalAuthToken {
		localAuthMu.Unlock()
		return localAuthCookieInvalid
	}
	expired := expireIdleLocalAuthLocked()
	localAuthMu.Unlock()

	if expired {
		finishLocalAuthExpiry()
		return localAuthCookieExpired
	}
	return localAuthCookieValid
}

func localLoginActive() bool {
	localAuthMu.Lock()
	defer localAuthMu.Unlock()
	if config.LocalAuthMode != "password" {
		return true
	}
	return config.LocalAuthToken != "" && !localAuthIdleLocked()
}

func sendSessionExpiredThenAbort(c *gin.Context) {
	c.SetCookie("authToken", "", -1, "/", "", false, true)
	c.JSON(http.StatusUnauthorized, gin.H{"error": "Session expired", "code": sessionExpiredCode})
	c.Abort()
}

func rpcCountsAsLocalAuthActivity(method string) bool {
	for _, prefix := range []string{"get", "list", "check", "is"} {
		if strings.HasPrefix(method, prefix) {
			return false
		}
	}
	return method != "ping"
}

func hidMessageCountsAsLocalAuthActivity(t hidrpc.MessageType) bool {
	switch t {
	case hidrpc.TypeKeyboardReport,
		hidrpc.TypeKeypressReport,
		hidrpc.TypePointerReport,
		hidrpc.TypeMouseReport,
		hidrpc.TypeWheelReport,
		hidrpc.TypeKeyboardMacroReport,
		hidrpc.TypeCancelKeyboardMacroReport:
		return true
	}
	return false
}

func (s *Session) recordLocalAuthActivity() {
	if s != nil && s.local {
		recordLocalAuthActivity()
	}
}

func (s *Session) endForExpiredLogin() {
	if !s.local {
		return
	}
	go func() {
		if s.signaling != nil {
			_ = s.signaling.Close(signalingStatusSessionExpired, "session expired")
		}
		_ = s.peerConnection.Close()
	}()
}

func rpcGetLocalAuthSessionTimeout() int {
	return config.LocalAuthIdleMinutes
}

func rpcSetLocalAuthSessionTimeout(timeoutMinutes int) error {
	if timeoutMinutes < 0 || timeoutMinutes > localAuthMaxIdleMinutes {
		return fmt.Errorf("session timeout must be between 0 and %d minutes", localAuthMaxIdleMinutes)
	}
	localAuthMu.Lock()
	recordLocalAuthActivity()
	config.LocalAuthIdleMinutes = timeoutMinutes
	localAuthMu.Unlock()
	return SaveConfig()
}
