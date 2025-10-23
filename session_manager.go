package kvm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// SessionMode and constants are now imported from internal/session via session_permissions.go

// Session validation constants
const (
	minNicknameLength = 2
	maxNicknameLength = 30
	maxIdentityLength = 256
)

// Timing constants for session management
const (
	// Broadcast throttling (DoS protection)
	globalBroadcastDelay   = 100 * time.Millisecond // Minimum time between global session broadcasts
	sessionBroadcastDelay  = 50 * time.Millisecond  // Minimum time between broadcasts to a single session
	broadcastQueueCapacity = 100                    // Maximum pending broadcasts before drops occur

	// Session timeout defaults
	defaultPendingSessionTimeout  = 1 * time.Minute // Timeout for pending sessions (DoS protection)
	defaultObserverSessionTimeout = 2 * time.Minute // Timeout for inactive observer sessions
	disabledTimeoutValue          = 24 * time.Hour  // Value used when timeout is disabled (0 setting)

	// Transfer and blacklist settings
	transferBlacklistDuration = 60 * time.Second // Duration to blacklist sessions after manual transfer

	// Grace period limits
	maxGracePeriodEntries = 10 // Maximum number of grace period entries to prevent memory exhaustion

	// Emergency promotion limits (DoS protection)
	emergencyWindowDuration            = 60 * time.Second // Sliding window duration for emergency promotion rate limiting
	maxEmergencyPromotionsPerMinute    = 3                // Maximum emergency promotions allowed within the sliding window
	emergencyPromotionCooldown         = 10 * time.Second // Minimum time between individual emergency promotions
	maxConsecutiveEmergencyPromotions  = 3                // Maximum consecutive emergency promotions before blocking
	emergencyPromotionWindowCleanupAge = 60 * time.Second // Age at which emergency window entries are cleaned up

	// Trust scoring constants
	invalidSessionTrustScore = -1000 // Trust score for non-existent sessions
)

var (
	ErrMaxSessionsReached = errors.New("maximum number of sessions reached")
)

type SessionData struct {
	ID         string      `json:"id"`
	Mode       SessionMode `json:"mode"`
	Source     string      `json:"source"`
	Identity   string      `json:"identity"`
	Nickname   string      `json:"nickname,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	LastActive time.Time   `json:"last_active"`
}

// Event types for JSON-RPC notifications
type (
	SessionsUpdateEvent struct {
		Sessions []SessionData `json:"sessions"`
		YourMode SessionMode   `json:"yourMode"`
	}

	NewSessionPendingEvent struct {
		SessionID string `json:"sessionId"`
		Source    string `json:"source"`
		Identity  string `json:"identity"`
		Nickname  string `json:"nickname,omitempty"`
	}

	PrimaryRequestEvent struct {
		RequestID string `json:"requestId"`
		Source    string `json:"source"`
		Identity  string `json:"identity"`
		Nickname  string `json:"nickname,omitempty"`
	}
)

// TransferBlacklistEntry prevents recently demoted sessions from immediately becoming primary again
type TransferBlacklistEntry struct {
	SessionID string
	ExpiresAt time.Time
}

type SessionManager struct {
	mu                   sync.RWMutex
	primaryPromotionLock sync.Mutex
	primaryTimeout       time.Duration
	logger               *zerolog.Logger
	sessions             map[string]*Session
	nicknameIndex        map[string]*Session
	reconnectGrace       map[string]time.Time
	reconnectInfo        map[string]*SessionData
	transferBlacklist    []TransferBlacklistEntry
	queueOrder           []string
	primarySessionID     string
	lastPrimaryID        string
	maxSessions          int
	cleanupCancel        context.CancelFunc

	lastEmergencyPromotion         time.Time
	consecutiveEmergencyPromotions int
	emergencyPromotionWindow       []time.Time
	emergencyWindowMutex           sync.Mutex

	lastBroadcast    time.Time
	broadcastMutex   sync.Mutex
	broadcastQueue   chan struct{}
	broadcastPending atomic.Bool
}

// NewSessionManager creates a new session manager
func NewSessionManager(logger *zerolog.Logger) *SessionManager {
	// Use configuration values if available
	maxSessions := 10
	primaryTimeout := 5 * time.Minute

	if config != nil && config.MultiSession != nil {
		if config.MultiSession.MaxSessions > 0 {
			maxSessions = config.MultiSession.MaxSessions
		}
		if config.MultiSession.PrimaryTimeout > 0 {
			primaryTimeout = time.Duration(config.MultiSession.PrimaryTimeout) * time.Second
		}
	}

	// Override with session settings if available
	if currentSessionSettings != nil {
		if currentSessionSettings.PrimaryTimeout > 0 {
			primaryTimeout = time.Duration(currentSessionSettings.PrimaryTimeout) * time.Second
		}
		if currentSessionSettings.MaxSessions > 0 {
			maxSessions = currentSessionSettings.MaxSessions
		}
	}

	sm := &SessionManager{
		sessions:          make(map[string]*Session),
		nicknameIndex:     make(map[string]*Session),
		reconnectGrace:    make(map[string]time.Time),
		reconnectInfo:     make(map[string]*SessionData),
		transferBlacklist: make([]TransferBlacklistEntry, 0),
		queueOrder:        make([]string, 0),
		logger:            logger,
		maxSessions:       maxSessions,
		primaryTimeout:    primaryTimeout,
		broadcastQueue:    make(chan struct{}, broadcastQueueCapacity),
	}

	ctx, cancel := context.WithCancel(context.Background())
	sm.cleanupCancel = cancel
	go sm.cleanupInactiveSessions(ctx)
	go sm.broadcastWorker(ctx)

	return sm
}

func (sm *SessionManager) AddSession(session *Session, clientSettings *SessionSettings) error {
	if session == nil {
		sm.logger.Error().Msg("AddSession: session is nil")
		return errors.New("session cannot be nil")
	}

	if session.Nickname != "" {
		if err := sm.validateNickname(session.Nickname); err != nil {
			return err
		}
	}
	if len(session.Identity) > maxIdentityLength {
		return fmt.Errorf("identity too long (max %d characters)", maxIdentityLength)
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	nicknameReserved := false
	defer func() {
		if r := recover(); r != nil {
			if nicknameReserved && session.Nickname != "" {
				if sm.nicknameIndex[session.Nickname] == session {
					delete(sm.nicknameIndex, session.Nickname)
				}
			}
			sm.logger.Error().Interface("panic", r).Str("sessionID", session.ID).Msg("Recovered from panic in AddSession")
		}
	}()

	if session.Nickname != "" {
		if existingSession, exists := sm.nicknameIndex[session.Nickname]; exists {
			if existingSession.ID != session.ID {
				return fmt.Errorf("nickname '%s' is already in use by another session", session.Nickname)
			}
		}
		sm.nicknameIndex[session.Nickname] = session
		nicknameReserved = true
	}

	wasWithinGracePeriod := false
	wasPreviouslyPrimary := false
	wasPreviouslyPending := false
	if graceTime, exists := sm.reconnectGrace[session.ID]; exists {
		if time.Now().Before(graceTime) {
			wasWithinGracePeriod = true
			wasPreviouslyPrimary = (sm.lastPrimaryID == session.ID)
			if reconnectInfo, hasInfo := sm.reconnectInfo[session.ID]; hasInfo {
				wasPreviouslyPending = (reconnectInfo.Mode == SessionModePending)
			}
		}
	}

	if existing, exists := sm.sessions[session.ID]; exists {
		if existing.Identity != session.Identity || existing.Source != session.Source {
			return fmt.Errorf("session ID already in use by different user (identity mismatch)")
		}

		if existing.peerConnection != nil {
			existing.peerConnection.Close()
		}

		existing.peerConnection = session.peerConnection
		existing.VideoTrack = session.VideoTrack
		existing.ControlChannel = session.ControlChannel
		existing.RPCChannel = session.RPCChannel
		existing.HidChannel = session.HidChannel
		existing.flushCandidates = session.flushCandidates
		session.Mode = existing.Mode
		session.Nickname = existing.Nickname
		session.CreatedAt = existing.CreatedAt

		sm.ensureNickname(session)

		if !nicknameReserved && session.Nickname != "" {
			sm.nicknameIndex[session.Nickname] = session
		}

		sm.sessions[session.ID] = session

		if existing.Mode == SessionModePrimary {
			isBlacklisted := sm.isSessionBlacklisted(session.ID)
			// SECURITY: Prevent dual-primary - check actual mode, not just existence
			primaryExists := false
			if sm.primarySessionID != "" {
				if existingPrimary, ok := sm.sessions[sm.primarySessionID]; ok && existingPrimary.Mode == SessionModePrimary {
					primaryExists = true
				}
			}
			if sm.lastPrimaryID == session.ID && !isBlacklisted && !primaryExists {
				sm.primarySessionID = session.ID
				sm.lastPrimaryID = ""
				delete(sm.reconnectGrace, session.ID)
			} else {
				session.Mode = SessionModeObserver
			}
		}

		go sm.broadcastSessionListUpdate()
		return nil
	}

	if len(sm.sessions) >= sm.maxSessions {
		return ErrMaxSessionsReached
	}

	if session.ID == "" {
		session.ID = uuid.New().String()
	}

	if clientSettings != nil && clientSettings.Nickname != "" {
		session.Nickname = clientSettings.Nickname
	}

	globalSettings := currentSessionSettings

	primaryExists := sm.primarySessionID != "" && sm.sessions[sm.primarySessionID] != nil

	hasActivePrimaryGracePeriod := false
	if sm.lastPrimaryID != "" && sm.lastPrimaryID != session.ID {
		if graceTime, exists := sm.reconnectGrace[sm.lastPrimaryID]; exists {
			if time.Now().Before(graceTime) {
				if reconnectInfo, hasInfo := sm.reconnectInfo[sm.lastPrimaryID]; hasInfo {
					if reconnectInfo.Mode == SessionModePrimary {
						hasActivePrimaryGracePeriod = true
					}
				}
			}
		}
	}

	isBlacklisted := sm.isSessionBlacklisted(session.ID)
	isOnlySession := len(sm.sessions) == 0

	canBecomePrimary := !primaryExists && !hasActivePrimaryGracePeriod
	isReconnectingPrimary := wasWithinGracePeriod && wasPreviouslyPrimary
	isNewEligibleSession := !wasWithinGracePeriod && (!isBlacklisted || isOnlySession)

	shouldBecomePrimary := canBecomePrimary && (isReconnectingPrimary || isNewEligibleSession)

	if shouldBecomePrimary {
		if sm.primarySessionID == "" || sm.sessions[sm.primarySessionID] == nil {
			session.Mode = SessionModePrimary
			sm.primarySessionID = session.ID
			sm.lastPrimaryID = ""

			// Clear grace periods when new primary is established
			for oldSessionID := range sm.reconnectGrace {
				delete(sm.reconnectGrace, oldSessionID)
			}
			for oldSessionID := range sm.reconnectInfo {
				delete(sm.reconnectInfo, oldSessionID)
			}

			session.hidRPCAvailable = false
		} else {
			session.Mode = SessionModeObserver
		}
	} else if wasPreviouslyPending {
		session.Mode = SessionModePending
	} else if globalSettings != nil && globalSettings.RequireApproval && primaryExists && !wasWithinGracePeriod {
		session.Mode = SessionModePending
		// Notify primary about the pending session, but only if nickname is not required OR already provided
		if primary := sm.sessions[sm.primarySessionID]; primary != nil {
			// Check if nickname is required and missing
			requiresNickname := globalSettings.RequireNickname
			hasNickname := session.Nickname != "" && len(session.Nickname) > 0

			if !requiresNickname || hasNickname {
				go func() {
					writeJSONRPCEvent("newSessionPending", map[string]interface{}{
						"sessionId": session.ID,
						"source":    session.Source,
						"identity":  session.Identity,
						"nickname":  session.Nickname,
					}, primary)
				}()
			}
		}
	} else {
		session.Mode = SessionModeObserver
	}

	session.CreatedAt = time.Now()
	session.LastActive = time.Now()

	sm.sessions[session.ID] = session

	sm.logger.Info().
		Str("sessionID", session.ID).
		Str("mode", string(session.Mode)).
		Int("totalSessions", len(sm.sessions)).
		Msg("Session added to manager")

	sm.ensureNickname(session)

	if !nicknameReserved && session.Nickname != "" {
		sm.nicknameIndex[session.Nickname] = session
	}

	sm.validateSinglePrimary()

	// Clean up grace period after validation completes
	if wasWithinGracePeriod {
		delete(sm.reconnectGrace, session.ID)
		delete(sm.reconnectInfo, session.ID)
	}

	// Notify all sessions about the new connection
	go sm.broadcastSessionListUpdate()

	return nil
}

// RemoveSession removes a session from the manager
func (sm *SessionManager) RemoveSession(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return
	}

	wasPrimary := session.Mode == SessionModePrimary
	delete(sm.sessions, sessionID)

	if session.Nickname != "" {
		if sm.nicknameIndex[session.Nickname] == session {
			delete(sm.nicknameIndex, session.Nickname)
		}
	}

	sm.logger.Info().
		Str("sessionID", sessionID).
		Bool("wasPrimary", wasPrimary).
		Int("remainingSessions", len(sm.sessions)).
		Msg("Session removed from manager")

	sm.removeFromQueue(sessionID)

	// Check if this session was marked for immediate removal (intentional logout)
	isIntentionalLogout := false
	if graceTime, exists := sm.reconnectGrace[sessionID]; exists {
		if time.Now().After(graceTime) {
			isIntentionalLogout = true
			delete(sm.reconnectGrace, sessionID)
			delete(sm.reconnectInfo, sessionID)
		}
	}

	// Determine grace period duration (used for logging even if intentional logout)
	gracePeriod := 10
	if currentSessionSettings != nil && currentSessionSettings.ReconnectGrace > 0 {
		gracePeriod = currentSessionSettings.ReconnectGrace
	}

	// Only add grace period if this is NOT an intentional logout
	if !isIntentionalLogout {
		// Limit grace period entries to prevent memory exhaustion
		// Evict entries ONLY when full, and only evict one entry
		if len(sm.reconnectGrace) >= maxGracePeriodEntries {
			var evictID string
			var earliestExpiration time.Time
			for id, graceTime := range sm.reconnectGrace {
				// Find the grace period that expires first (earliest time)
				if earliestExpiration.IsZero() || graceTime.Before(earliestExpiration) {
					evictID = id
					earliestExpiration = graceTime
				}
			}
			if evictID != "" {
				delete(sm.reconnectGrace, evictID)
				delete(sm.reconnectInfo, evictID)
				sm.logger.Debug().
					Str("evictedSessionID", evictID).
					Msg("Evicted oldest grace period entry due to limit")
			} else {
				// Defensive: if we couldn't evict, don't add grace period
				sm.logger.Error().
					Int("graceCount", len(sm.reconnectGrace)).
					Msg("Failed to evict grace period entry, skipping grace period for this session")
				goto skipGracePeriod
			}
		}

		sm.reconnectGrace[sessionID] = time.Now().Add(time.Duration(gracePeriod) * time.Second)

		// Store session info for potential reconnection
		sm.reconnectInfo[sessionID] = &SessionData{
			ID:        session.ID,
			Mode:      session.Mode,
			Source:    session.Source,
			Identity:  session.Identity,
			Nickname:  session.Nickname,
			CreatedAt: session.CreatedAt,
		}
	}

skipGracePeriod:

	// If this was the primary session, clear primary slot and track for grace period
	if wasPrimary {
		if isIntentionalLogout {
			// Intentional logout: clear immediately and promote right away
			sm.primarySessionID = ""
			sm.lastPrimaryID = ""
			sm.logger.Info().
				Str("sessionID", sessionID).
				Int("remainingSessions", len(sm.sessions)).
				Msg("Primary session removed via intentional logout - immediate promotion")
		} else {
			// Accidental disconnect: use grace period
			sm.lastPrimaryID = sessionID // Remember this was the primary for grace period
			sm.primarySessionID = ""     // Clear primary slot so other sessions can be promoted

			// Clear all blacklists to allow promotion after grace period expires
			if len(sm.transferBlacklist) > 0 {
				sm.transferBlacklist = make([]TransferBlacklistEntry, 0)
			}

			sm.logger.Info().
				Str("sessionID", sessionID).
				Dur("gracePeriod", time.Duration(gracePeriod)*time.Second).
				Int("remainingSessions", len(sm.sessions)).
				Msg("Primary session removed, grace period active")
		}

		// Trigger validation for potential promotion
		if len(sm.sessions) > 0 {
			sm.validateSinglePrimary()
		}
	}

	// Notify remaining sessions
	go sm.broadcastSessionListUpdate()
}

// GetSession returns a session by ID
func (sm *SessionManager) GetSession(sessionID string) *Session {
	sm.mu.RLock()
	session := sm.sessions[sessionID]
	sm.mu.RUnlock()
	return session
}

// IsValidReconnection checks if a session ID can be reused for reconnection
func (sm *SessionManager) IsValidReconnection(sessionID, source, identity string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Check if session is in reconnect grace period
	if info, exists := sm.reconnectInfo[sessionID]; exists {
		// Verify the source and identity match
		return info.Source == source && info.Identity == identity
	}

	return false
}

// IsInGracePeriod checks if a session ID is within the reconnection grace period
func (sm *SessionManager) IsInGracePeriod(sessionID string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if graceTime, exists := sm.reconnectGrace[sessionID]; exists {
		return time.Now().Before(graceTime)
	}
	return false
}

// ClearGracePeriod removes the grace period for a session (for intentional logout/disconnect)
// This marks the session for immediate removal without grace period protection
// Actual promotion will happen in RemoveSession when it detects no grace period
func (sm *SessionManager) ClearGracePeriod(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Clear grace period and reconnect info to prevent grace period from being added
	delete(sm.reconnectGrace, sessionID)
	delete(sm.reconnectInfo, sessionID)

	// Mark this session with a special "immediate removal" grace period (already expired)
	// This signals to RemoveSession that this was intentional and should skip grace period
	sm.reconnectGrace[sessionID] = time.Now().Add(-1 * time.Second) // Already expired

	sm.logger.Info().
		Str("sessionID", sessionID).
		Str("lastPrimaryID", sm.lastPrimaryID).
		Str("primarySessionID", sm.primarySessionID).
		Msg("Marked session for immediate removal (intentional logout)")
}

// isSessionBlacklisted checks if a session was recently demoted via transfer and should not become primary
func (sm *SessionManager) isSessionBlacklisted(sessionID string) bool {
	now := time.Now()
	isBlacklisted := false

	// Clean expired entries in-place (zero allocations)
	writeIndex := 0
	for readIndex := 0; readIndex < len(sm.transferBlacklist); readIndex++ {
		entry := sm.transferBlacklist[readIndex]
		if now.Before(entry.ExpiresAt) {
			// Keep this entry - still valid
			sm.transferBlacklist[writeIndex] = entry
			writeIndex++
			if entry.SessionID == sessionID {
				isBlacklisted = true
			}
		}
		// Expired entries are automatically skipped (not copied forward)
	}
	// Truncate to only valid entries
	sm.transferBlacklist = sm.transferBlacklist[:writeIndex]

	return isBlacklisted
}

// GetPrimarySession returns the current primary session
func (sm *SessionManager) GetPrimarySession() *Session {
	sm.mu.RLock()
	if sm.primarySessionID == "" {
		sm.mu.RUnlock()
		return nil
	}
	session := sm.sessions[sm.primarySessionID]
	sm.mu.RUnlock()
	return session
}

// SetPrimarySession sets a session as primary
func (sm *SessionManager) SetPrimarySession(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return ErrSessionNotFound
	}

	session.Mode = SessionModePrimary
	sm.primarySessionID = sessionID
	sm.lastPrimaryID = ""
	return nil
}

// CanReceiveVideo checks if a session is allowed to receive video
// Sessions in pending state cannot receive video
// Sessions that require nickname but don't have one also cannot receive video (if enforced)
func (sm *SessionManager) CanReceiveVideo(session *Session, settings *SessionSettings) bool {
	if !session.HasPermission(PermissionVideoView) {
		return false
	}

	if settings != nil && settings.RequireNickname && session.Nickname == "" {
		return false
	}

	return true
}

// GetAllSessions returns information about all active sessions
func (sm *SessionManager) GetAllSessions() []SessionData {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Don't run validation on every getSessions call
	// This was causing immediate demotion during transfers and page refreshes
	// Validation should only run during state changes, not data queries

	infos := make([]SessionData, 0, len(sm.sessions))
	for _, session := range sm.sessions {
		infos = append(infos, SessionData{
			ID:         session.ID,
			Mode:       session.Mode,
			Source:     session.Source,
			Identity:   session.Identity,
			Nickname:   session.Nickname,
			CreatedAt:  session.CreatedAt,
			LastActive: session.LastActive,
		})
	}
	return infos
}

// RequestPrimary requests primary control for a session
func (sm *SessionManager) RequestPrimary(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return ErrSessionNotFound
	}

	// If already primary, nothing to do
	if session.Mode == SessionModePrimary {
		return nil
	}

	// Check if there's a primary in grace period before promoting
	if sm.primarySessionID == "" {
		// Don't promote immediately if there's a primary waiting in grace period
		if sm.lastPrimaryID != "" {
			// Check if grace period is still active
			if graceTime, exists := sm.reconnectGrace[sm.lastPrimaryID]; exists {
				if time.Now().Before(graceTime) {
					// Primary is in grace period, queue this request instead
					sm.queueOrder = append(sm.queueOrder, sessionID)
					session.Mode = SessionModeQueued
					sm.logger.Info().
						Str("sessionID", sessionID).
						Str("gracePrimaryID", sm.lastPrimaryID).
						Msg("Request queued - primary session in grace period")
					go sm.broadcastSessionListUpdate()
					return nil
				}
			}
		}

		// No grace period conflict, promote immediately using centralized system
		err := sm.transferPrimaryRole("", sessionID, "initial_promotion", "first session auto-promotion")
		if err == nil {
			// Send mode change event after promoting
			writeJSONRPCEvent("modeChanged", map[string]string{"mode": "primary"}, session)
			go sm.broadcastSessionListUpdate()
		}
		return err
	}

	// Notify the primary session about the request
	if primarySession, exists := sm.sessions[sm.primarySessionID]; exists {
		event := PrimaryRequestEvent{
			RequestID: sessionID,
			Identity:  session.Identity,
			Source:    session.Source,
			Nickname:  session.Nickname,
		}
		writeJSONRPCEvent("primaryControlRequested", event, primarySession)
	}

	// Add to queue if not already there
	if session.Mode != SessionModeQueued {
		session.Mode = SessionModeQueued
		sm.queueOrder = append(sm.queueOrder, sessionID)
	}

	// Broadcast update in goroutine to avoid deadlock
	go sm.broadcastSessionListUpdate()
	return nil
}

// ReleasePrimary releases primary control from a session
func (sm *SessionManager) ReleasePrimary(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return ErrSessionNotFound
	}

	if session.Mode != SessionModePrimary {
		return nil
	}

	// Check if there are other sessions that could take control
	hasOtherEligibleSessions := false
	for id, s := range sm.sessions {
		if id != sessionID && (s.Mode == SessionModeObserver || s.Mode == SessionModeQueued) {
			hasOtherEligibleSessions = true
			break
		}
	}

	// Don't allow releasing primary if no one else can take control
	if !hasOtherEligibleSessions {
		return errors.New("cannot release primary control - no other sessions available")
	}

	// Demote to observer
	session.Mode = SessionModeObserver
	sm.primarySessionID = ""

	// Clear any active input state
	sm.clearInputState()

	// Find the next session to promote (excluding the current primary)
	// For voluntary releases, ignore blacklisting since this is user-initiated
	promotedSessionID := sm.findNextSessionToPromoteExcludingIgnoreBlacklist(sessionID)

	// If we found someone to promote, use centralized transfer
	if promotedSessionID != "" {
		err := sm.transferPrimaryRole(sessionID, promotedSessionID, "release_transfer", "primary release and auto-promotion")
		if err != nil {
			sm.logger.Error().
				Str("error", err.Error()).
				Str("releasedBySessionID", sessionID).
				Str("promotedSessionID", promotedSessionID).
				Msg("Failed to transfer primary role after release")
			return err
		}

		sm.logger.Info().
			Str("releasedBySessionID", sessionID).
			Str("promotedSessionID", promotedSessionID).
			Msg("Primary control released and transferred to observer")

		// Send mode change event for promoted session
		go func() {
			if promotedSession := sessionManager.GetSession(promotedSessionID); promotedSession != nil {
				writeJSONRPCEvent("modeChanged", map[string]string{"mode": "primary"}, promotedSession)
			}
		}()
	} else {
		sm.logger.Warn().
			Str("releasedBySessionID", sessionID).
			Msg("Primary control released but no eligible sessions found for promotion")
	}

	// Broadcast update in goroutine to avoid deadlock
	go sm.broadcastSessionListUpdate()
	return nil
}

// TransferPrimary transfers primary control from one session to another
func (sm *SessionManager) TransferPrimary(fromID, toID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// SECURITY: Verify fromID is the actual current primary
	if sm.primarySessionID != fromID {
		return fmt.Errorf("transfer denied: %s is not the current primary (current primary: %s)", fromID, sm.primarySessionID)
	}

	fromSession, exists := sm.sessions[fromID]
	if !exists {
		return ErrSessionNotFound
	}

	if fromSession.Mode != SessionModePrimary {
		return errors.New("transfer denied: from session is not in primary mode")
	}

	// Use centralized transfer method
	err := sm.transferPrimaryRole(fromID, toID, "direct_transfer", "manual transfer request")
	if err != nil {
		return err
	}

	// Send events in goroutines to avoid holding lock
	go func() {
		if fromSession := sessionManager.GetSession(fromID); fromSession != nil {
			writeJSONRPCEvent("modeChanged", map[string]string{"mode": "observer"}, fromSession)
		}
	}()

	go func() {
		if toSession := sessionManager.GetSession(toID); toSession != nil {
			writeJSONRPCEvent("modeChanged", map[string]string{"mode": "primary"}, toSession)
		}
		sm.broadcastSessionListUpdate()
	}()

	return nil
}

// ApprovePrimaryRequest approves a pending primary control request
func (sm *SessionManager) ApprovePrimaryRequest(currentPrimaryID, requesterID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Log the approval request
	sm.logger.Info().
		Str("currentPrimaryID", currentPrimaryID).
		Str("requesterID", requesterID).
		Str("actualPrimaryID", sm.primarySessionID).
		Msg("ApprovePrimaryRequest called")

	// Verify current primary is correct
	if sm.primarySessionID != currentPrimaryID {
		sm.logger.Error().
			Str("currentPrimaryID", currentPrimaryID).
			Str("actualPrimaryID", sm.primarySessionID).
			Msg("Not the primary session")
		return errors.New("not the primary session")
	}

	// SECURITY: Verify requester session exists and is in Queued mode
	requesterSession, exists := sm.sessions[requesterID]
	if !exists {
		sm.logger.Error().
			Str("requesterID", requesterID).
			Msg("Requester session not found")
		return errors.New("requester session not found")
	}

	if requesterSession.Mode != SessionModeQueued {
		sm.logger.Error().
			Str("requesterID", requesterID).
			Str("actualMode", string(requesterSession.Mode)).
			Msg("Requester session is not in queued mode")
		return fmt.Errorf("requester session is not in queued mode (current mode: %s)", requesterSession.Mode)
	}

	// Remove requester from queue
	sm.removeFromQueue(requesterID)

	// Use centralized transfer method
	err := sm.transferPrimaryRole(currentPrimaryID, requesterID, "approval_transfer", "primary approval request")
	if err != nil {
		return err
	}

	// Send events after releasing lock to avoid deadlock
	go func() {
		if demotedSession := sessionManager.GetSession(currentPrimaryID); demotedSession != nil {
			writeJSONRPCEvent("modeChanged", map[string]string{"mode": "observer"}, demotedSession)
		}
	}()

	go func() {
		if promotedSession := sessionManager.GetSession(requesterID); promotedSession != nil {
			writeJSONRPCEvent("modeChanged", map[string]string{"mode": "primary"}, promotedSession)
		}
		sm.broadcastSessionListUpdate()
	}()

	return nil
}

// DenyPrimaryRequest denies a pending primary control request
func (sm *SessionManager) DenyPrimaryRequest(currentPrimaryID, requesterID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Verify current primary is correct
	if sm.primarySessionID != currentPrimaryID {
		return errors.New("not the primary session")
	}

	requester, exists := sm.sessions[requesterID]
	if !exists {
		return ErrSessionNotFound
	}

	// Move requester back to observer
	requester.Mode = SessionModeObserver
	sm.removeFromQueue(requesterID)

	// Validate session consistency after mode change
	sm.validateSinglePrimary()

	// Notify requester of denial in goroutine
	go func() {
		writeJSONRPCEvent("primaryControlDenied", map[string]interface{}{}, requester)
		sm.broadcastSessionListUpdate()
	}()

	return nil
}

// ApproveSession approves a pending session (thread-safe)
func (sm *SessionManager) ApproveSession(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return ErrSessionNotFound
	}

	if session.Mode != SessionModePending {
		return errors.New("session is not in pending mode")
	}

	// Promote session to observer
	session.Mode = SessionModeObserver

	sm.logger.Info().
		Str("sessionID", sessionID).
		Msg("Session approved and promoted to observer")

	return nil
}

// DenySession denies a pending session (thread-safe)
func (sm *SessionManager) DenySession(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return ErrSessionNotFound
	}

	if session.Mode != SessionModePending {
		return errors.New("session is not in pending mode")
	}

	sm.logger.Info().
		Str("sessionID", sessionID).
		Msg("Session denied - notifying session")

	return nil
}

// ForEachSession executes a function for each active session
func (sm *SessionManager) ForEachSession(fn func(*Session)) {
	sm.mu.RLock()
	// Create a copy of sessions to avoid holding lock during callbacks
	sessionsCopy := make([]*Session, 0, len(sm.sessions))
	for _, session := range sm.sessions {
		sessionsCopy = append(sessionsCopy, session)
	}
	sm.mu.RUnlock()

	// Call function outside of lock to prevent deadlocks
	for _, session := range sessionsCopy {
		fn(session)
	}
}

// UpdateLastActive updates the last active time for a session
func (sm *SessionManager) UpdateLastActive(sessionID string) {
	sm.mu.Lock()
	if session, exists := sm.sessions[sessionID]; exists {
		session.LastActive = time.Now()
	}
	sm.mu.Unlock()
}

// UpdateSessionNickname atomically updates a session's nickname with uniqueness check
func (sm *SessionManager) UpdateSessionNickname(sessionID, nickname string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	targetSession, exists := sm.sessions[sessionID]
	if !exists {
		return errors.New("session not found")
	}

	// Check nickname uniqueness under lock
	if existingSession, nicknameInUse := sm.nicknameIndex[nickname]; nicknameInUse {
		if existingSession.ID != sessionID {
			return fmt.Errorf("nickname '%s' is already in use by another session", nickname)
		}
	}

	// Remove old nickname from index
	if targetSession.Nickname != "" {
		delete(sm.nicknameIndex, targetSession.Nickname)
	}

	// Update nickname and index atomically
	targetSession.Nickname = nickname
	sm.nicknameIndex[nickname] = targetSession

	return nil
}

// Internal helper methods

// validateSinglePrimary ensures there's only one primary session and fixes any inconsistencies
func (sm *SessionManager) validateSinglePrimary() {
	primarySessions := make([]*Session, 0)

	// Find all sessions that think they're primary
	for _, session := range sm.sessions {
		if session.Mode == SessionModePrimary {
			primarySessions = append(primarySessions, session)
		}
	}

	// If we have multiple primaries, fix it
	if len(primarySessions) > 1 {
		sm.logger.Error().
			Int("primaryCount", len(primarySessions)).
			Msg("Multiple primary sessions detected, fixing")

		// Keep the first one as primary, demote the rest
		for i, session := range primarySessions {
			if i == 0 {
				sm.primarySessionID = session.ID
			} else {
				session.Mode = SessionModeObserver
			}
		}
	}

	// Ensure manager's primarySessionID matches reality
	if len(primarySessions) == 1 && sm.primarySessionID != primarySessions[0].ID {
		sm.logger.Warn().
			Str("managerPrimaryID", sm.primarySessionID).
			Str("actualPrimaryID", primarySessions[0].ID).
			Msg("Manager primary ID mismatch, fixing...")
		sm.primarySessionID = primarySessions[0].ID
	}

	// Don't clear primary slot if there's a grace period active
	if len(primarySessions) == 0 && sm.primarySessionID != "" {
		if sm.lastPrimaryID == sm.primarySessionID {
			if graceTime, exists := sm.reconnectGrace[sm.primarySessionID]; exists {
				if time.Now().Before(graceTime) {
					return // Keep primary slot reserved during grace period
				}
			}
		}
		sm.primarySessionID = ""
	}

	// Check if there's an active grace period for any primary session
	hasActivePrimaryGracePeriod := false
	for sessionID, graceTime := range sm.reconnectGrace {
		if time.Now().Before(graceTime) {
			if reconnectInfo, hasInfo := sm.reconnectInfo[sessionID]; hasInfo {
				if reconnectInfo.Mode == SessionModePrimary {
					hasActivePrimaryGracePeriod = true
					break
				}
			}
		}
	}

	// Auto-promote if there are NO primary sessions at all AND no active grace period
	if len(primarySessions) == 0 && sm.primarySessionID == "" && len(sm.sessions) > 0 && !hasActivePrimaryGracePeriod {
		// Find a session to promote to primary
		nextSessionID := sm.findNextSessionToPromote()
		if nextSessionID != "" {
			sm.logger.Info().
				Str("promotedSessionID", nextSessionID).
				Msg("Auto-promoting observer to primary - no primary sessions exist and no grace period active")

			// Use the centralized promotion logic
			err := sm.transferPrimaryRole("", nextSessionID, "emergency_auto_promotion", "no primary sessions detected")
			if err != nil {
				sm.logger.Error().
					Err(err).
					Str("sessionID", nextSessionID).
					Msg("Failed to auto-promote session to primary")
			}
		} else {
			sm.logger.Warn().
				Msg("No eligible session found for emergency auto-promotion")
		}
	}
}

func (sm *SessionManager) transferPrimaryRole(fromSessionID, toSessionID, transferType, context string) error {
	sm.primaryPromotionLock.Lock()
	defer sm.primaryPromotionLock.Unlock()

	// Validate sessions exist
	toSession, toExists := sm.sessions[toSessionID]
	if !toExists {
		return ErrSessionNotFound
	}

	// SECURITY: Prevent promoting a session that's already primary
	if toSession.Mode == SessionModePrimary {
		sm.logger.Warn().
			Str("sessionID", toSessionID).
			Str("transferType", transferType).
			Msg("Attempted to promote session that is already primary")
		return errors.New("target session is already primary")
	}

	var fromSession *Session
	var fromExists bool
	if fromSessionID != "" {
		fromSession, fromExists = sm.sessions[fromSessionID]
		if !fromExists {
			return ErrSessionNotFound
		}
	}

	// Demote existing primary if specified
	if fromExists && fromSession.Mode == SessionModePrimary {
		fromSession.Mode = SessionModeObserver
		fromSession.hidRPCAvailable = false

		// Always delete grace period when demoting - no exceptions
		// If a session times out or is manually transferred, it should not auto-reclaim primary
		delete(sm.reconnectGrace, fromSessionID)
		delete(sm.reconnectInfo, fromSessionID)

		sm.logger.Info().
			Str("demotedSessionID", fromSessionID).
			Str("transferType", transferType).
			Str("context", context).
			Msg("Demoted existing primary session")
	}

	primaryCount := 0
	var existingPrimaryID string
	for id, sess := range sm.sessions {
		if sess.Mode == SessionModePrimary {
			primaryCount++
			if id != toSessionID {
				existingPrimaryID = id
			}
		}
	}

	if primaryCount > 1 || (primaryCount == 1 && existingPrimaryID != "" && existingPrimaryID != sm.primarySessionID) {
		sm.logger.Error().
			Int("primaryCount", primaryCount).
			Str("existingPrimaryID", existingPrimaryID).
			Str("targetPromotionID", toSessionID).
			Str("managerPrimaryID", sm.primarySessionID).
			Str("transferType", transferType).
			Msg("CRITICAL: Dual-primary corruption detected - forcing fix")

		for id, sess := range sm.sessions {
			if sess.Mode == SessionModePrimary {
				if id != sm.primarySessionID && id != toSessionID {
					sess.Mode = SessionModeObserver
					sm.logger.Warn().
						Str("demotedSessionID", id).
						Msg("Force-demoted session due to dual-primary corruption")
				}
			}
		}

		if sm.primarySessionID != "" && sm.sessions[sm.primarySessionID] != nil {
			if sm.sessions[sm.primarySessionID].Mode != SessionModePrimary {
				sm.primarySessionID = ""
			}
		}

		existingPrimaryID = ""
		for id, sess := range sm.sessions {
			if id != toSessionID && sess.Mode == SessionModePrimary {
				existingPrimaryID = id
				break
			}
		}

		if existingPrimaryID != "" {
			sm.logger.Error().
				Str("existingPrimaryID", existingPrimaryID).
				Str("targetPromotionID", toSessionID).
				Msg("CRITICAL: Cannot fix dual-primary corruption - blocking promotion")
			return fmt.Errorf("cannot promote: dual-primary corruption detected and fix failed (%s)", existingPrimaryID)
		}
	} else if existingPrimaryID != "" {
		sm.logger.Error().
			Str("existingPrimaryID", existingPrimaryID).
			Str("targetPromotionID", toSessionID).
			Str("transferType", transferType).
			Msg("CRITICAL: Attempted to create second primary - blocking promotion")
		return fmt.Errorf("cannot promote: another primary session exists (%s)", existingPrimaryID)
	}

	toSession.Mode = SessionModePrimary
	toSession.hidRPCAvailable = false
	if transferType == "emergency_timeout_promotion" {
		toSession.LastActive = time.Now()
	}
	sm.primarySessionID = toSessionID

	// ALWAYS set lastPrimaryID to the new primary to support WebRTC reconnections
	// This allows the newly promoted session to handle page refreshes correctly
	// The blacklist system prevents unwanted takeovers during manual transfers
	sm.lastPrimaryID = toSessionID

	// Clear input state
	sm.clearInputState()

	// Reset consecutive emergency promotion counter on successful manual transfer
	if fromSessionID != "" && transferType != "emergency_promotion_deadlock_prevention" && transferType != "emergency_timeout_promotion" {
		sm.consecutiveEmergencyPromotions = 0
	}

	// Apply bidirectional blacklisting - protect newly promoted session
	// Only apply blacklisting for MANUAL transfers, not emergency promotions
	// Emergency promotions need to happen immediately without blacklist interference
	isManualTransfer := (transferType == "direct_transfer" || transferType == "approval_transfer" || transferType == "release_transfer")
	now := time.Now()
	blacklistedCount := 0

	if isManualTransfer {
		// First, clear any existing blacklist entries for the newly promoted session
		cleanedBlacklist := make([]TransferBlacklistEntry, 0)
		for _, entry := range sm.transferBlacklist {
			if entry.SessionID != toSessionID { // Remove any old blacklist entries for the new primary
				cleanedBlacklist = append(cleanedBlacklist, entry)
			}
		}
		sm.transferBlacklist = cleanedBlacklist

		// Then blacklist all other sessions
		for sessionID := range sm.sessions {
			if sessionID != toSessionID { // Don't blacklist the newly promoted session
				sm.transferBlacklist = append(sm.transferBlacklist, TransferBlacklistEntry{
					SessionID: sessionID,
					ExpiresAt: now.Add(transferBlacklistDuration),
				})
				blacklistedCount++
			}
		}
	}

	// Grace periods are cleared for demoted sessions (line 519-520) to prevent them from
	// auto-reclaiming primary after manual transfer. New grace periods are created when
	// sessions reconnect via RemoveSession. The blacklist provides additional protection
	// during the transfer window, while lastPrimaryID allows the newly promoted session
	// to safely handle browser refreshes and reclaim primary if disconnected.

	sm.logger.Info().
		Str("fromSessionID", fromSessionID).
		Str("toSessionID", toSessionID).
		Str("transferType", transferType).
		Str("context", context).
		Int("blacklistedSessions", blacklistedCount).
		Dur("blacklistDuration", transferBlacklistDuration).
		Msg("Primary role transferred with bidirectional protection")

	// DON'T validate here - causes recursive calls and map iteration issues
	// The caller (AddSession, RemoveSession, etc.) will validate after we return
	// sm.validateSinglePrimary()  // REMOVED to prevent recursion

	// Send reconnection signal for emergency promotions via WebSocket (more reliable than RPC when channel is stale)
	if toExists && (transferType == "emergency_timeout_promotion" || transferType == "emergency_auto_promotion") {
		go func() {
			time.Sleep(globalBroadcastDelay)

			eventData := map[string]interface{}{
				"sessionId": toSessionID,
				"newMode":   string(toSession.Mode),
				"reason":    "session_promotion",
				"action":    "reconnect_required",
				"timestamp": time.Now().Unix(),
			}

			err := toSession.sendWebSocketSignal("connectionModeChanged", eventData)
			if err != nil {
				sm.logger.Warn().Err(err).Str("sessionId", toSessionID).Msg("WebSocket signal failed, using RPC")
				writeJSONRPCEvent("connectionModeChanged", eventData, toSession)
			}

			sm.logger.Info().Str("sessionId", toSessionID).Str("transferType", transferType).Msg("Sent reconnection signal")
		}()
	}

	return nil
}

// findNextSessionToPromote finds the next eligible session for promotion
// Replicates the logic from promoteNextSession but just returns the session ID
func (sm *SessionManager) findNextSessionToPromote() string {
	return sm.findNextSessionToPromoteExcluding("", true)
}

func (sm *SessionManager) findNextSessionToPromoteExcluding(excludeSessionID string, checkBlacklist bool) string {
	// First, check if there are queued sessions (excluding the specified session)
	if len(sm.queueOrder) > 0 {
		nextID := sm.queueOrder[0]
		if nextID != excludeSessionID {
			if _, exists := sm.sessions[nextID]; exists {
				if !checkBlacklist || !sm.isSessionBlacklisted(nextID) {
					return nextID
				}
			}
		}
	}

	// Otherwise, find any observer session (excluding the specified session)
	for id, session := range sm.sessions {
		if id != excludeSessionID && session.Mode == SessionModeObserver {
			if !checkBlacklist || !sm.isSessionBlacklisted(id) {
				return id
			}
		}
	}

	// If still no primary and there are pending sessions (edge case: all sessions are pending)
	// This can happen if RequireApproval was enabled but primary left
	for id, session := range sm.sessions {
		if id != excludeSessionID && session.Mode == SessionModePending {
			if !checkBlacklist || !sm.isSessionBlacklisted(id) {
				return id
			}
		}
	}

	return "" // No eligible session found
}

func (sm *SessionManager) findNextSessionToPromoteExcludingIgnoreBlacklist(excludeSessionID string) string {
	return sm.findNextSessionToPromoteExcluding(excludeSessionID, false)
}

func (sm *SessionManager) removeFromQueue(sessionID string) {
	// In-place removal is more efficient
	for i, id := range sm.queueOrder {
		if id == sessionID {
			sm.queueOrder = append(sm.queueOrder[:i], sm.queueOrder[i+1:]...)
			return
		}
	}
}

func (sm *SessionManager) clearInputState() {
	// Clear keyboard state
	if gadget != nil {
		_ = gadget.KeyboardReport(0, []byte{0, 0, 0, 0, 0, 0})
	}
}

// getCurrentPrimaryTimeout returns the current primary timeout duration
func (sm *SessionManager) getCurrentPrimaryTimeout() time.Duration {
	// Use session settings if available
	if currentSessionSettings != nil {
		if currentSessionSettings.PrimaryTimeout == 0 {
			return disabledTimeoutValue
		} else if currentSessionSettings.PrimaryTimeout > 0 {
			return time.Duration(currentSessionSettings.PrimaryTimeout) * time.Second
		}
	}
	// Fall back to config or default
	return sm.primaryTimeout
}

// getSessionTrustScore calculates a trust score for session selection during emergency promotion
func (sm *SessionManager) getSessionTrustScore(sessionID string) int {
	session, exists := sm.sessions[sessionID]
	if !exists {
		return invalidSessionTrustScore
	}

	score := 0
	now := time.Now()

	// Longer session duration = more trust (up to 100 points for 100+ minutes)
	sessionAge := now.Sub(session.CreatedAt)
	sessionAgeMinutes := sessionAge.Minutes()
	if sessionAgeMinutes > 100 {
		score += 100
	} else {
		score += int(sessionAgeMinutes)
	}

	// Recently successful primary sessions get higher trust
	if sm.lastPrimaryID == sessionID {
		score += 50
	}

	// Observer mode is more trustworthy than queued/pending for emergency promotion
	switch session.Mode {
	case SessionModeObserver:
		score += 20
	case SessionModeQueued:
		score += 10
	case SessionModePending:
		// Pending sessions get no bonus and are less preferred
		score += 0
	}

	// Check if session has nickname when required (shows engagement)
	if currentSessionSettings != nil && currentSessionSettings.RequireNickname {
		if session.Nickname != "" {
			score += 15
		} else {
			score -= 30 // Penalize sessions without required nickname
		}
	}

	return score
}

// findMostTrustedSessionForEmergency finds the most trustworthy session for emergency promotion
func (sm *SessionManager) findMostTrustedSessionForEmergency() string {
	bestSessionID := ""
	bestScore := -1

	for sessionID, session := range sm.sessions {
		if sm.isSessionBlacklisted(sessionID) ||
			session.Mode == SessionModePrimary ||
			(session.Mode != SessionModeObserver && session.Mode != SessionModeQueued) {
			continue
		}

		score := sm.getSessionTrustScore(sessionID)
		if score > bestScore {
			bestScore = score
			bestSessionID = sessionID
		}
	}

	if bestSessionID != "" {
		sm.logger.Info().
			Str("selectedSession", bestSessionID).
			Int("trustScore", bestScore).
			Msg("Selected most trusted session for emergency promotion")
	}

	return bestSessionID
}

// extractBrowserFromUserAgent extracts browser name from user agent string
func extractBrowserFromUserAgent(userAgent string) *string {
	ua := strings.ToLower(userAgent)

	// Check for common browsers (order matters - Chrome contains Safari, etc.)
	// Optimize Safari check by caching Chrome detection
	hasChrome := strings.Contains(ua, "chrome")

	if strings.Contains(ua, "edg/") || strings.Contains(ua, "edge") {
		return &BrowserEdge
	}
	if strings.Contains(ua, "firefox") {
		return &BrowserFirefox
	}
	if hasChrome {
		return &BrowserChrome
	}
	if strings.Contains(ua, "safari") {
		return &BrowserSafari
	}
	if strings.Contains(ua, "opera") || strings.Contains(ua, "opr/") {
		return &BrowserOpera
	}

	return &BrowserUnknown
}

// generateAutoNickname creates a user-friendly auto-generated nickname
func generateAutoNickname(session *Session) string {
	// Use browser type from session, fallback to "user" if not set
	browser := "user"
	if session.Browser != nil {
		browser = *session.Browser
	}

	// Use last 4 chars of session ID for uniqueness (lowercase)
	sessionID := strings.ToLower(session.ID)
	shortID := sessionID[len(sessionID)-4:]

	// Generate contextual lowercase nickname
	return fmt.Sprintf("u-%s-%s", browser, shortID)
}

// generateNicknameFromUserAgent creates a nickname from user agent (for frontend use)
func generateNicknameFromUserAgent(userAgent string) string {
	// Extract browser info
	browserPtr := extractBrowserFromUserAgent(userAgent)
	browser := "user"
	if browserPtr != nil {
		browser = *browserPtr
	}

	// Generate a random 4-character ID (lowercase)
	shortID := strings.ToLower(fmt.Sprintf("%04x", time.Now().UnixNano()%0xFFFF))

	// Generate contextual lowercase nickname
	return fmt.Sprintf("u-%s-%s", browser, shortID)
}

// ensureNickname ensures session has a nickname, auto-generating if needed
func (sm *SessionManager) validateNickname(nickname string) error {
	if len(nickname) < minNicknameLength {
		return fmt.Errorf("nickname must be at least %d characters", minNicknameLength)
	}
	if len(nickname) > maxNicknameLength {
		return fmt.Errorf("nickname must be %d characters or less", maxNicknameLength)
	}
	if !isValidNickname(nickname) {
		return errors.New("nickname can only contain letters, numbers, spaces, and - _ . @")
	}

	for i, r := range nickname {
		if r < 32 || r == 127 {
			return fmt.Errorf("nickname contains control character at position %d", i)
		}
		if r >= 0x200B && r <= 0x200D {
			return errors.New("nickname contains zero-width character")
		}
	}

	trimmed := ""
	for _, r := range nickname {
		trimmed += string(r)
	}
	if trimmed != nickname {
		return errors.New("nickname contains disallowed unicode")
	}

	return nil
}

func (sm *SessionManager) ensureNickname(session *Session) {
	// Skip if session already has a nickname
	if session.Nickname != "" {
		return
	}

	// Skip if nickname is required (user must set manually)
	if currentSessionSettings != nil && currentSessionSettings.RequireNickname {
		return
	}

	// Auto-generate nickname
	session.Nickname = generateAutoNickname(session)

	sm.logger.Debug().
		Str("sessionID", session.ID).
		Str("autoNickname", session.Nickname).
		Msg("Auto-generated nickname for session")
}

// updateAllSessionNicknames updates nicknames for all sessions when settings change
func (sm *SessionManager) updateAllSessionNicknames() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	updated := 0
	for _, session := range sm.sessions {
		oldNickname := session.Nickname
		sm.ensureNickname(session)
		if session.Nickname != oldNickname {
			updated++
		}
	}

	if updated > 0 {
		sm.logger.Info().
			Int("updatedSessions", updated).
			Msg("Auto-generated nicknames for sessions after settings change")

		// Broadcast the update
		go sm.broadcastSessionListUpdate()
	}
}

func (sm *SessionManager) broadcastWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-sm.broadcastQueue:
			sm.broadcastPending.Store(false)
			sm.executeBroadcast()
		}
	}
}

func (sm *SessionManager) broadcastSessionListUpdate() {
	if sm.broadcastPending.CompareAndSwap(false, true) {
		select {
		case sm.broadcastQueue <- struct{}{}:
		default:
			sm.logger.Warn().
				Int("queueLen", len(sm.broadcastQueue)).
				Int("queueCap", cap(sm.broadcastQueue)).
				Msg("Broadcast queue full, dropping update")
			sm.broadcastPending.Store(false)
		}
	}
}

func (sm *SessionManager) executeBroadcast() {
	sm.broadcastMutex.Lock()
	if time.Since(sm.lastBroadcast) < globalBroadcastDelay {
		sm.broadcastMutex.Unlock()
		return
	}
	sm.lastBroadcast = time.Now()
	sm.broadcastMutex.Unlock()

	sm.mu.RLock()
	infos := make([]SessionData, 0, len(sm.sessions))
	activeSessions := make([]*Session, 0, len(sm.sessions))

	for _, session := range sm.sessions {
		infos = append(infos, SessionData{
			ID:         session.ID,
			Mode:       session.Mode,
			Source:     session.Source,
			Identity:   session.Identity,
			Nickname:   session.Nickname,
			CreatedAt:  session.CreatedAt,
			LastActive: session.LastActive,
		})

		if session.RPCChannel != nil {
			activeSessions = append(activeSessions, session)
		}
	}
	sm.mu.RUnlock()

	for _, session := range activeSessions {
		session.lastBroadcastMu.Lock()
		shouldSkip := time.Since(session.LastBroadcast) < sessionBroadcastDelay
		if !shouldSkip {
			session.LastBroadcast = time.Now()
		}
		session.lastBroadcastMu.Unlock()

		if shouldSkip {
			continue
		}

		event := SessionsUpdateEvent{
			Sessions: infos,
			YourMode: session.Mode,
		}
		writeJSONRPCEvent("sessionsUpdated", event, session)
	}
}

// Shutdown stops the session manager and cleans up resources
func (sm *SessionManager) Shutdown() {
	if sm.cleanupCancel != nil {
		sm.cleanupCancel()
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	close(sm.broadcastQueue)

	for id := range sm.sessions {
		delete(sm.sessions, id)
	}
}

func (sm *SessionManager) cleanupInactiveSessions(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	validationCounter := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sm.mu.Lock()
			now := time.Now()
			needsBroadcast := false

			// Clean up expired emergency promotion window entries
			sm.emergencyWindowMutex.Lock()
			cutoff := now.Add(-emergencyPromotionWindowCleanupAge)
			validEntries := make([]time.Time, 0, len(sm.emergencyPromotionWindow))
			for _, t := range sm.emergencyPromotionWindow {
				if t.After(cutoff) {
					validEntries = append(validEntries, t)
				}
			}
			sm.emergencyPromotionWindow = validEntries
			sm.emergencyWindowMutex.Unlock()

			// Handle expired grace periods
			gracePeriodExpired := sm.handleGracePeriodExpiration(now)
			if gracePeriodExpired {
				needsBroadcast = true
			}

			// Clean up timed-out pending sessions (DoS protection)
			if sm.handlePendingSessionTimeout(now) {
				needsBroadcast = true
			}

			// Clean up inactive observer sessions
			if sm.handleObserverSessionCleanup(now) {
				needsBroadcast = true
			}

			// Handle primary session timeout
			if sm.handlePrimarySessionTimeout(now) {
				needsBroadcast = true
			}

			// Run validation immediately if grace period expired, otherwise periodically
			if gracePeriodExpired {
				sm.validateSinglePrimary()
			} else {
				validationCounter++
				if validationCounter >= 10 {
					validationCounter = 0
					sm.validateSinglePrimary()
				}
			}

			sm.mu.Unlock()

			if needsBroadcast {
				go sm.broadcastSessionListUpdate()
			}
		}
	}
}

// Global session manager instance
var (
	sessionManager     *SessionManager
	sessionManagerOnce sync.Once
)

func initSessionManager() {
	sessionManagerOnce.Do(func() {
		sessionManager = NewSessionManager(websocketLogger)
	})
}

// Global session settings - references config.SessionSettings for persistence
var currentSessionSettings *SessionSettings
