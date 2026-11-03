//go:build linux && arm

package kvm

import (
	"testing"
	"time"
)

func TestLocalAuthSessionExpired(t *testing.T) {
	ensureConfigLoaded()
	defer func() {
		authTimeNow = time.Now
		lastAuthActivity.Store(0)
		config.LocalAuthSessionTimeoutMinutes = 0
	}()

	now := time.Unix(1_700_000_000, 0)
	authTimeNow = func() time.Time { return now }

	t.Run("disabled timeout never expires", func(t *testing.T) {
		config.LocalAuthSessionTimeoutMinutes = 0
		touchAuthActivity()
		now = now.Add(365 * 24 * time.Hour)
		if localAuthSessionExpired() {
			t.Error("session expired although the timeout is disabled")
		}
	})

	t.Run("no recorded activity does not expire", func(t *testing.T) {
		config.LocalAuthSessionTimeoutMinutes = 30
		lastAuthActivity.Store(0)
		if localAuthSessionExpired() {
			t.Error("session expired without any recorded activity")
		}
	})

	t.Run("fresh activity keeps the session alive", func(t *testing.T) {
		config.LocalAuthSessionTimeoutMinutes = 30
		touchAuthActivity()
		now = now.Add(29 * time.Minute)
		if localAuthSessionExpired() {
			t.Error("session expired before the timeout elapsed")
		}
	})

	t.Run("elapsed timeout expires", func(t *testing.T) {
		config.LocalAuthSessionTimeoutMinutes = 30
		touchAuthActivity()
		now = now.Add(31 * time.Minute)
		if !localAuthSessionExpired() {
			t.Error("session did not expire after the timeout elapsed")
		}
	})

	t.Run("activity resets the timer", func(t *testing.T) {
		config.LocalAuthSessionTimeoutMinutes = 30
		touchAuthActivity()
		now = now.Add(25 * time.Minute)
		touchAuthActivity()
		now = now.Add(25 * time.Minute)
		if localAuthSessionExpired() {
			t.Error("session expired although activity reset the timer")
		}
	})
}
