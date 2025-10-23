package kvm

import (
	"time"
)

// emergencyPromotionContext holds context for emergency promotion attempts
type emergencyPromotionContext struct {
	triggerSessionID string
	triggerReason    string
	now              time.Time
}

// attemptEmergencyPromotion tries to promote a session using emergency or normal promotion logic
// Returns (promotedSessionID, isEmergency, shouldSkip)
func (sm *SessionManager) attemptEmergencyPromotion(ctx emergencyPromotionContext, excludeSessionID string) (string, bool, bool) {
	// Check if emergency promotion is needed
	if currentSessionSettings == nil || !currentSessionSettings.RequireApproval {
		// Normal promotion - reset consecutive counter
		sm.consecutiveEmergencyPromotions = 0
		promotedID := sm.findNextSessionToPromote()
		return promotedID, false, false
	}

	sm.emergencyWindowMutex.Lock()
	defer sm.emergencyWindowMutex.Unlock()

	// CRITICAL: Bypass all rate limits if no primary exists to prevent deadlock
	// System availability takes priority over DoS protection
	noPrimaryExists := (sm.primarySessionID == "")
	if noPrimaryExists {
		sm.logger.Info().
			Str("triggerSessionID", ctx.triggerSessionID).
			Str("triggerReason", ctx.triggerReason).
			Msg("Bypassing emergency promotion rate limits - no primary exists")

		// Find best session, excluding the specified session if provided
		var promotedSessionID string
		if excludeSessionID != "" {
			bestSessionID := ""
			bestScore := -1
			for id, session := range sm.sessions {
				if id != excludeSessionID &&
					!sm.isSessionBlacklisted(id) &&
					(session.Mode == SessionModeObserver || session.Mode == SessionModeQueued) {
					score := sm.getSessionTrustScore(id)
					if score > bestScore {
						bestScore = score
						bestSessionID = id
					}
				}
			}
			promotedSessionID = bestSessionID
		} else {
			promotedSessionID = sm.findMostTrustedSessionForEmergency()
		}
		return promotedSessionID, true, false
	}

	const slidingWindowDuration = 60 * time.Second
	const maxEmergencyPromotionsPerMinute = 3

	cutoff := ctx.now.Add(-slidingWindowDuration)
	validEntries := make([]time.Time, 0, len(sm.emergencyPromotionWindow))
	for _, t := range sm.emergencyPromotionWindow {
		if t.After(cutoff) {
			validEntries = append(validEntries, t)
		}
	}
	sm.emergencyPromotionWindow = validEntries

	if len(sm.emergencyPromotionWindow) >= maxEmergencyPromotionsPerMinute {
		sm.logger.Error().
			Str("triggerSessionID", ctx.triggerSessionID).
			Int("promotionsInLastMinute", len(sm.emergencyPromotionWindow)).
			Msg("Emergency promotion rate limit exceeded - potential attack")
		return "", false, true
	}

	if ctx.now.Sub(sm.lastEmergencyPromotion) < 10*time.Second {
		sm.logger.Warn().
			Str("triggerSessionID", ctx.triggerSessionID).
			Dur("timeSinceLastEmergency", ctx.now.Sub(sm.lastEmergencyPromotion)).
			Msg("Emergency promotion cooldown active")
		return "", false, true
	}

	if sm.consecutiveEmergencyPromotions >= 3 {
		sm.logger.Error().
			Str("triggerSessionID", ctx.triggerSessionID).
			Int("consecutiveCount", sm.consecutiveEmergencyPromotions).
			Msg("Too many consecutive emergency promotions - blocking")
		return "", false, true
	}

	// Find best session for emergency promotion
	var promotedSessionID string
	if excludeSessionID != "" {
		// Need to exclude a specific session (e.g., timed-out session)
		bestSessionID := ""
		bestScore := -1
		for id, session := range sm.sessions {
			if id != excludeSessionID &&
				!sm.isSessionBlacklisted(id) &&
				(session.Mode == SessionModeObserver || session.Mode == SessionModeQueued) {
				score := sm.getSessionTrustScore(id)
				if score > bestScore {
					bestScore = score
					bestSessionID = id
				}
			}
		}
		promotedSessionID = bestSessionID
	} else {
		promotedSessionID = sm.findMostTrustedSessionForEmergency()
	}

	return promotedSessionID, true, false
}

// handleGracePeriodExpiration checks and handles expired grace periods
// Returns true if any grace period expired
func (sm *SessionManager) handleGracePeriodExpiration(now time.Time) bool {
	gracePeriodExpired := false
	for sessionID, graceTime := range sm.reconnectGrace {
		if now.After(graceTime) {
			delete(sm.reconnectGrace, sessionID)
			gracePeriodExpired = true

			wasHoldingPrimarySlot := (sm.lastPrimaryID == sessionID)

			if wasHoldingPrimarySlot {
				sm.primarySessionID = ""
				sm.lastPrimaryID = ""

				sm.logger.Info().
					Str("expiredSessionID", sessionID).
					Msg("Primary session grace period expired - slot now available")

				// Promote next eligible session using emergency logic if needed
				sm.promoteAfterGraceExpiration(sessionID, now)
			} else {
				sm.logger.Debug().
					Str("expiredSessionID", sessionID).
					Msg("Non-primary session grace period expired")
			}

			delete(sm.reconnectInfo, sessionID)
		}
	}
	return gracePeriodExpired
}

// promoteAfterGraceExpiration handles promotion after grace period expiration
func (sm *SessionManager) promoteAfterGraceExpiration(expiredSessionID string, now time.Time) {
	ctx := emergencyPromotionContext{
		triggerSessionID: expiredSessionID,
		triggerReason:    "grace_expiration",
		now:              now,
	}

	promotedSessionID, isEmergency, shouldSkip := sm.attemptEmergencyPromotion(ctx, "")
	if shouldSkip {
		return
	}

	if promotedSessionID != "" {
		reason := "grace_expiration_promotion"
		if isEmergency {
			reason = "emergency_promotion_deadlock_prevention"
			sm.emergencyWindowMutex.Lock()
			sm.emergencyPromotionWindow = append(sm.emergencyPromotionWindow, now)
			sm.emergencyWindowMutex.Unlock()
			sm.lastEmergencyPromotion = now
			sm.consecutiveEmergencyPromotions++

			sm.logger.Warn().
				Str("expiredSessionID", expiredSessionID).
				Str("promotedSessionID", promotedSessionID).
				Bool("requireApproval", true).
				Int("consecutiveEmergencyPromotions", sm.consecutiveEmergencyPromotions).
				Int("trustScore", sm.getSessionTrustScore(promotedSessionID)).
				Msg("EMERGENCY: Bypassing approval requirement to prevent deadlock")
		}

		err := sm.transferPrimaryRole("", promotedSessionID, reason, "primary grace period expired")
		if err == nil {
			logEvent := sm.logger.Info()
			if isEmergency {
				logEvent = sm.logger.Warn()
			}
			logEvent.
				Str("expiredSessionID", expiredSessionID).
				Str("promotedSessionID", promotedSessionID).
				Str("reason", reason).
				Bool("isEmergencyPromotion", isEmergency).
				Msg("Auto-promoted session after primary grace period expiration")
		} else {
			sm.logger.Error().
				Err(err).
				Str("expiredSessionID", expiredSessionID).
				Str("promotedSessionID", promotedSessionID).
				Str("reason", reason).
				Bool("isEmergencyPromotion", isEmergency).
				Msg("Failed to promote session after grace period expiration")
		}
	} else {
		logLevel := sm.logger.Info()
		if isEmergency {
			logLevel = sm.logger.Error()
		}
		logLevel.
			Str("expiredSessionID", expiredSessionID).
			Bool("isEmergencyPromotion", isEmergency).
			Msg("Primary grace period expired but no eligible sessions to promote")
	}
}

// handlePendingSessionTimeout removes timed-out pending sessions (DoS protection)
// Returns true if any pending session was removed
func (sm *SessionManager) handlePendingSessionTimeout(now time.Time) bool {
	toDelete := make([]string, 0)
	for id, session := range sm.sessions {
		if session.Mode == SessionModePending &&
			now.Sub(session.CreatedAt) > defaultPendingSessionTimeout {
			websocketLogger.Debug().
				Str("sessionId", id).
				Dur("age", now.Sub(session.CreatedAt)).
				Msg("Removing timed-out pending session")
			toDelete = append(toDelete, id)
		}
	}
	for _, id := range toDelete {
		delete(sm.sessions, id)
	}
	return len(toDelete) > 0
}

// handleObserverSessionCleanup removes inactive observer sessions with closed RPC channels
// Returns true if any observer session was removed
func (sm *SessionManager) handleObserverSessionCleanup(now time.Time) bool {
	observerTimeout := defaultObserverSessionTimeout
	if currentSessionSettings != nil && currentSessionSettings.ObserverTimeout > 0 {
		observerTimeout = time.Duration(currentSessionSettings.ObserverTimeout) * time.Second
	}

	toDelete := make([]string, 0)
	for id, session := range sm.sessions {
		if session.Mode == SessionModeObserver {
			if session.RPCChannel == nil && now.Sub(session.LastActive) > observerTimeout {
				sm.logger.Debug().
					Str("sessionId", id).
					Dur("inactiveFor", now.Sub(session.LastActive)).
					Dur("observerTimeout", observerTimeout).
					Msg("Removing inactive observer session with closed RPC channel")
				toDelete = append(toDelete, id)
			}
		}
	}
	for _, id := range toDelete {
		delete(sm.sessions, id)
	}
	return len(toDelete) > 0
}

// handlePrimarySessionTimeout checks and handles primary session timeout
// Returns true if primary session was timed out and cleanup is needed
func (sm *SessionManager) handlePrimarySessionTimeout(now time.Time) bool {
	if sm.primarySessionID == "" {
		return false
	}

	primary, exists := sm.sessions[sm.primarySessionID]
	if !exists {
		sm.primarySessionID = ""
		return true
	}

	currentTimeout := sm.getCurrentPrimaryTimeout()
	if now.Sub(primary.LastActive) <= currentTimeout {
		return false
	}

	// Timeout detected - demote primary
	timedOutSessionID := primary.ID
	primary.Mode = SessionModeObserver
	sm.primarySessionID = ""

	sm.logger.Info().
		Str("sessionID", timedOutSessionID).
		Dur("inactiveFor", now.Sub(primary.LastActive)).
		Dur("timeout", currentTimeout).
		Msg("Primary session timed out due to inactivity - demoted to observer")

	ctx := emergencyPromotionContext{
		triggerSessionID: timedOutSessionID,
		triggerReason:    "timeout",
		now:              now,
	}

	promotedSessionID, isEmergency, shouldSkip := sm.attemptEmergencyPromotion(ctx, timedOutSessionID)
	if shouldSkip {
		sm.logger.Info().Msg("Promotion skipped after timeout - session demoted but no promotion")
		return true // Still need to broadcast the demotion
	}

	if promotedSessionID != "" {
		reason := "timeout_promotion"
		if isEmergency {
			reason = "emergency_timeout_promotion"
			sm.emergencyWindowMutex.Lock()
			sm.emergencyPromotionWindow = append(sm.emergencyPromotionWindow, now)
			sm.emergencyWindowMutex.Unlock()
			sm.lastEmergencyPromotion = now
			sm.consecutiveEmergencyPromotions++

			sm.logger.Warn().
				Str("timedOutSessionID", timedOutSessionID).
				Str("promotedSessionID", promotedSessionID).
				Bool("requireApproval", true).
				Int("trustScore", sm.getSessionTrustScore(promotedSessionID)).
				Msg("EMERGENCY: Timeout promotion bypassing approval requirement")
		}

		err := sm.transferPrimaryRole(timedOutSessionID, promotedSessionID, reason, "primary session timeout")
		if err == nil {
			logEvent := sm.logger.Info()
			if isEmergency {
				logEvent = sm.logger.Warn()
			}
			logEvent.
				Str("timedOutSessionID", timedOutSessionID).
				Str("promotedSessionID", promotedSessionID).
				Bool("isEmergencyPromotion", isEmergency).
				Msg("Auto-promoted session after primary timeout")
			return true
		} else {
			sm.logger.Error().Err(err).
				Str("timedOutSessionID", timedOutSessionID).
				Str("promotedSessionID", promotedSessionID).
				Msg("Failed to promote session after timeout - primary demoted")
			return true // Still broadcast the demotion even if promotion failed
		}
	}

	sm.logger.Info().
		Str("timedOutSessionID", timedOutSessionID).
		Msg("Primary session timed out - demoted to observer, no eligible sessions to promote")
	return true // Broadcast the demotion even if no promotion
}
