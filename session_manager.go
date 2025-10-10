package kvm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// SessionMode and constants are now imported from internal/session via session_permissions.go

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

// Broadcast throttling to prevent DoS
var (
	lastBroadcast  time.Time
	broadcastMutex sync.Mutex
	broadcastDelay = 100 * time.Millisecond // Min time between broadcasts

	// Pre-allocated event maps to reduce allocations
	modePrimaryEvent  = map[string]string{"mode": "primary"}
	modeObserverEvent = map[string]string{"mode": "observer"}
)

type SessionManager struct {
	mu                sync.RWMutex             // 24 bytes - place first for better alignment
	primaryTimeout    time.Duration            // 8 bytes
	logger            *zerolog.Logger          // 8 bytes
	sessions          map[string]*Session      // 8 bytes
	reconnectGrace    map[string]time.Time     // 8 bytes
	reconnectInfo     map[string]*SessionData  // 8 bytes
	transferBlacklist []TransferBlacklistEntry // Prevent demoted sessions from immediate re-promotion
	queueOrder        []string                 // 24 bytes (slice header)
	primarySessionID  string                   // 16 bytes
	lastPrimaryID     string                   // 16 bytes
	maxSessions       int                      // 8 bytes
	cleanupCancel     context.CancelFunc       // For stopping cleanup goroutine

	// Emergency promotion tracking for safety
	lastEmergencyPromotion         time.Time
	consecutiveEmergencyPromotions int
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
	if currentSessionSettings != nil && currentSessionSettings.PrimaryTimeout > 0 {
		primaryTimeout = time.Duration(currentSessionSettings.PrimaryTimeout) * time.Second
	}

	sm := &SessionManager{
		sessions:          make(map[string]*Session),
		reconnectGrace:    make(map[string]time.Time),
		reconnectInfo:     make(map[string]*SessionData),
		transferBlacklist: make([]TransferBlacklistEntry, 0),
		queueOrder:        make([]string, 0),
		logger:            logger,
		maxSessions:       maxSessions,
		primaryTimeout:    primaryTimeout,
	}

	// Start background cleanup of inactive sessions
	ctx, cancel := context.WithCancel(context.Background())
	sm.cleanupCancel = cancel
	go sm.cleanupInactiveSessions(ctx)

	return sm
}

func (sm *SessionManager) AddSession(session *Session, clientSettings *SessionSettings) error {
	// Basic input validation
	if session == nil {
		sm.logger.Error().Msg("AddSession: session is nil")
		return errors.New("session cannot be nil")
	}

	// Validate nickname if provided (matching frontend validation)
	if session.Nickname != "" {
		if len(session.Nickname) < 2 {
			return errors.New("nickname must be at least 2 characters")
		}
		if len(session.Nickname) > 30 {
			return errors.New("nickname must be 30 characters or less")
		}
		// Note: Pattern validation is done in RPC layer, not here for performance
	}
	if len(session.Identity) > 256 {
		return errors.New("identity too long")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

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

	// Check if a session with this ID already exists (reconnection)
	if existing, exists := sm.sessions[session.ID]; exists {
		// SECURITY: Verify identity matches to prevent session hijacking
		if existing.Identity != session.Identity || existing.Source != session.Source {
			return fmt.Errorf("session ID already in use by different user (identity mismatch)")
		}

		// Close old connection to prevent multiple active connections for same session ID
		if existing.peerConnection != nil {
			existing.peerConnection.Close()
		}

		// Update the existing session with new connection details
		existing.peerConnection = session.peerConnection
		existing.VideoTrack = session.VideoTrack
		existing.ControlChannel = session.ControlChannel
		existing.RPCChannel = session.RPCChannel
		existing.HidChannel = session.HidChannel
		existing.LastActive = time.Now()
		existing.flushCandidates = session.flushCandidates
		// Preserve existing mode and nickname
		session.Mode = existing.Mode
		session.Nickname = existing.Nickname
		session.CreatedAt = existing.CreatedAt

		// Ensure session has auto-generated nickname if needed
		sm.ensureNickname(session)

		sm.sessions[session.ID] = session

		// If this was the primary, try to restore primary status
		if existing.Mode == SessionModePrimary {
			isBlacklisted := sm.isSessionBlacklisted(session.ID)
			if sm.lastPrimaryID == session.ID && !isBlacklisted {
				sm.primarySessionID = session.ID
				sm.lastPrimaryID = ""
				delete(sm.reconnectGrace, session.ID)
			} else {
				// Grace period expired or another session took over
				session.Mode = SessionModeObserver
			}
		}

		go sm.broadcastSessionListUpdate()
		return nil
	}

	if len(sm.sessions) >= sm.maxSessions {
		return ErrMaxSessionsReached
	}

	// Generate ID if not set
	if session.ID == "" {
		session.ID = uuid.New().String()
	}

	// Set nickname from client settings if provided
	if clientSettings != nil && clientSettings.Nickname != "" {
		session.Nickname = clientSettings.Nickname
	}

	// Use global settings for requirements (not client-provided)
	globalSettings := currentSessionSettings

	primaryExists := sm.primarySessionID != "" && sm.sessions[sm.primarySessionID] != nil

	// Check if there's an active grace period for a primary session (different from this session)
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

	// Determine if this session should become primary
	// If there's no primary AND this is the ONLY session, ALWAYS promote regardless of blacklist
	isOnlySession := len(sm.sessions) == 0
	shouldBecomePrimary := (wasWithinGracePeriod && wasPreviouslyPrimary && !primaryExists && !hasActivePrimaryGracePeriod) ||
		(!wasWithinGracePeriod && !hasActivePrimaryGracePeriod && !primaryExists && (!isBlacklisted || isOnlySession))

	if shouldBecomePrimary {
		if sm.primarySessionID == "" || sm.sessions[sm.primarySessionID] == nil {
			session.Mode = SessionModePrimary
			sm.primarySessionID = session.ID
			sm.lastPrimaryID = ""

			// Clear all existing grace periods when a new primary is established
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

			// Only send approval request if nickname is not required OR already provided
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
			// If nickname is required and missing, the approval request will be sent
			// later when updateSessionNickname is called (see jsonrpc.go:232-242)
		}
	} else {
		// No primary exists and approval is required, OR approval is not required
		// In either case, this session becomes an observer
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

	// Ensure session has auto-generated nickname if needed
	sm.ensureNickname(session)

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

	sm.logger.Info().
		Str("sessionID", sessionID).
		Bool("wasPrimary", wasPrimary).
		Int("remainingSessions", len(sm.sessions)).
		Msg("Session removed from manager")

	// Remove from queue if present
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
		const maxGraceEntries = 10
		for len(sm.reconnectGrace) >= maxGraceEntries {
			var oldestID string
			var oldestTime time.Time
			for id, graceTime := range sm.reconnectGrace {
				if oldestTime.IsZero() || graceTime.Before(oldestTime) {
					oldestID = id
					oldestTime = graceTime
				}
			}
			if oldestID != "" {
				delete(sm.reconnectGrace, oldestID)
				delete(sm.reconnectInfo, oldestID)
			} else {
				break
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

	// Clean expired entries while we're here
	validEntries := make([]TransferBlacklistEntry, 0, len(sm.transferBlacklist))
	for _, entry := range sm.transferBlacklist {
		if now.Before(entry.ExpiresAt) {
			validEntries = append(validEntries, entry)
			if entry.SessionID == sessionID {
				return true // Found active blacklist entry
			}
		}
	}
	sm.transferBlacklist = validEntries // Update with only non-expired entries

	return false
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
	// Check if session has video view permission
	if !session.HasPermission(PermissionVideoView) {
		return false
	}

	// If nickname is required and session doesn't have one, block video
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
			writeJSONRPCEvent("modeChanged", modePrimaryEvent, session)
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
				writeJSONRPCEvent("modeChanged", modePrimaryEvent, promotedSession)
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
			writeJSONRPCEvent("modeChanged", modeObserverEvent, fromSession)
		}
	}()

	go func() {
		if toSession := sessionManager.GetSession(toID); toSession != nil {
			writeJSONRPCEvent("modeChanged", modePrimaryEvent, toSession)
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
			writeJSONRPCEvent("modeChanged", modeObserverEvent, demotedSession)
		}
	}()

	go func() {
		if promotedSession := sessionManager.GetSession(requesterID); promotedSession != nil {
			writeJSONRPCEvent("modeChanged", modePrimaryEvent, promotedSession)
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

// transferPrimaryRole is the centralized method for all primary role transfers
// It handles bidirectional blacklisting and logging consistently across all transfer types
func (sm *SessionManager) transferPrimaryRole(fromSessionID, toSessionID, transferType, context string) error {
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

	// SECURITY: Before promoting, verify there are no other primary sessions
	for id, sess := range sm.sessions {
		if id != toSessionID && sess.Mode == SessionModePrimary {
			sm.logger.Error().
				Str("existingPrimaryID", id).
				Str("targetPromotionID", toSessionID).
				Str("transferType", transferType).
				Msg("CRITICAL: Attempted to create second primary - blocking promotion")
			return fmt.Errorf("cannot promote: another primary session exists (%s)", id)
		}
	}

	// Promote target session
	toSession.Mode = SessionModePrimary
	toSession.hidRPCAvailable = false // Force re-handshake
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
	blacklistDuration := 60 * time.Second
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
					ExpiresAt: now.Add(blacklistDuration),
				})
				blacklistedCount++
			}
		}
	}

	// DON'T clear grace periods during transfers!
	// Grace periods and blacklisting serve different purposes:
	// - Grace periods: Allow disconnected sessions to reconnect and reclaim their role
	// - Blacklisting: Prevent recently demoted sessions from immediately taking primary again
	//
	// When a primary session is transferred to another session:
	// 1. The newly promoted session should be able to refresh its browser without losing primary
	// 2. When it refreshes, RemoveSession is called, which adds a grace period
	// 3. When it reconnects, it should find itself in lastPrimaryID and reclaim primary
	//
	// The blacklist prevents the OLD primary from immediately reclaiming control,
	// while the grace period allows the NEW primary to safely refresh its browser.
	// These mechanisms complement each other and should not interfere.

	sm.logger.Info().
		Str("fromSessionID", fromSessionID).
		Str("toSessionID", toSessionID).
		Str("transferType", transferType).
		Str("context", context).
		Int("blacklistedSessions", blacklistedCount).
		Dur("blacklistDuration", blacklistDuration).
		Msg("Primary role transferred with bidirectional protection")

	// DON'T validate here - causes recursive calls and map iteration issues
	// The caller (AddSession, RemoveSession, etc.) will validate after we return
	// sm.validateSinglePrimary()  // REMOVED to prevent recursion

	// Handle WebRTC connection state for promoted sessions
	// When a session changes from observer to primary, the existing WebRTC connection
	// was established for observer mode and needs to be re-negotiated for primary mode
	if toExists && (transferType == "emergency_timeout_promotion" || transferType == "emergency_auto_promotion") {
		go func() {
			// Small delay to ensure session mode changes are committed
			time.Sleep(100 * time.Millisecond)

			// Send connection reset signal to the promoted session
			writeJSONRPCEvent("connectionModeChanged", map[string]interface{}{
				"sessionId": toSessionID,
				"newMode":   string(toSession.Mode),
				"reason":    "session_promotion",
				"action":    "reconnect_required",
				"timestamp": time.Now().Unix(),
			}, toSession)

			sm.logger.Info().
				Str("sessionId", toSessionID).
				Str("newMode", string(toSession.Mode)).
				Str("transferType", transferType).
				Msg("Sent WebRTC reconnection signal to promoted session")
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
			// 0 means disabled - return a very large duration
			return 24 * time.Hour
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
		return -1000 // Session doesn't exist
	}

	score := 0
	now := time.Now()

	// Longer session duration = more trust (up to 100 points for 100+ minutes)
	sessionAge := now.Sub(session.CreatedAt)
	score += int(sessionAge.Minutes())
	if score > 100 {
		score = 100 // Cap age bonus at 100 points
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

	// First pass: try to find observers or queued sessions (preferred)
	for sessionID, session := range sm.sessions {
		// Skip if blacklisted, primary, or not eligible modes
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

	// If no observers/queued found, try pending sessions as last resort
	if bestSessionID == "" {
		for sessionID, session := range sm.sessions {
			if sm.isSessionBlacklisted(sessionID) || session.Mode == SessionModePrimary {
				continue
			}

			if session.Mode == SessionModePending {
				score := sm.getSessionTrustScore(sessionID)
				if score > bestScore {
					bestScore = score
					bestSessionID = sessionID
				}
			}
		}
	}

	// Log the selection decision for audit trail
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
	if strings.Contains(ua, "edg/") || strings.Contains(ua, "edge") {
		return &BrowserEdge
	}
	if strings.Contains(ua, "firefox") {
		return &BrowserFirefox
	}
	if strings.Contains(ua, "chrome") {
		return &BrowserChrome
	}
	if strings.Contains(ua, "safari") && !strings.Contains(ua, "chrome") {
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

func (sm *SessionManager) broadcastSessionListUpdate() {
	// Throttle broadcasts to prevent DoS
	broadcastMutex.Lock()
	if time.Since(lastBroadcast) < broadcastDelay {
		broadcastMutex.Unlock()
		return // Skip this broadcast to prevent storm
	}
	lastBroadcast = time.Now()
	broadcastMutex.Unlock()

	// Must be called in a goroutine to avoid deadlock
	// Get all sessions first - use read lock only, no validation during broadcasts
	sm.mu.RLock()

	// Build session infos and collect active sessions in one pass
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

		// Only collect sessions ready for broadcast
		if session.RPCChannel != nil {
			activeSessions = append(activeSessions, session)
		}
	}

	sm.mu.RUnlock()

	// Now send events without holding lock
	for _, session := range activeSessions {
		// Per-session throttling to prevent broadcast storms
		if time.Since(session.LastBroadcast) < 50*time.Millisecond {
			continue
		}
		session.LastBroadcast = time.Now()
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

	// Clean up all sessions
	for id := range sm.sessions {
		delete(sm.sessions, id)
	}
}

func (sm *SessionManager) cleanupInactiveSessions(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second) // Check every second for grace periods
	defer ticker.Stop()

	pendingTimeout := 1 * time.Minute // Reduced from 5 minutes to prevent DoS
	validationCounter := 0            // Counter for periodic validateSinglePrimary calls

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sm.mu.Lock()
			now := time.Now()
			needsBroadcast := false

			// Check for expired grace periods and promote if needed
			gracePeriodExpired := false
			for sessionID, graceTime := range sm.reconnectGrace {
				if now.After(graceTime) {
					delete(sm.reconnectGrace, sessionID)
					gracePeriodExpired = true

					wasHoldingPrimarySlot := (sm.lastPrimaryID == sessionID)

					// Check if this expired session was the primary holding the slot
					if wasHoldingPrimarySlot {
						// The primary didn't reconnect in time, now we can clear the slot and promote
						sm.primarySessionID = ""
						sm.lastPrimaryID = ""
						needsBroadcast = true

						sm.logger.Info().
							Str("expiredSessionID", sessionID).
							Msg("Primary session grace period expired - slot now available")

						// Always try to promote when possible - approval is only for new pending sessions
						// Use enhanced emergency promotion system for better security
						isEmergencyPromotion := false
						var promotedSessionID string

						// Check if this is an emergency scenario (RequireApproval enabled)
						if currentSessionSettings != nil && currentSessionSettings.RequireApproval {
							isEmergencyPromotion = true

							// CRITICAL: Ensure we ALWAYS have a primary session
							// If there's NO primary, bypass rate limits entirely
							hasPrimary := sm.primarySessionID != ""
							if !hasPrimary {
								sm.logger.Error().
									Str("expiredSessionID", sessionID).
									Msg("CRITICAL: No primary session exists - bypassing all rate limits")
							} else {
								// Rate limiting for emergency promotions (only when we have a primary)
								if now.Sub(sm.lastEmergencyPromotion) < 30*time.Second {
									sm.logger.Warn().
										Str("expiredSessionID", sessionID).
										Dur("timeSinceLastEmergency", now.Sub(sm.lastEmergencyPromotion)).
										Msg("Emergency promotion rate limit exceeded - potential attack")
									continue // Skip this grace period expiration
								}

								// Limit consecutive emergency promotions
								if sm.consecutiveEmergencyPromotions >= 3 {
									sm.logger.Error().
										Str("expiredSessionID", sessionID).
										Int("consecutiveCount", sm.consecutiveEmergencyPromotions).
										Msg("Too many consecutive emergency promotions - blocking for security")
									continue // Skip this grace period expiration
								}
							}

							promotedSessionID = sm.findMostTrustedSessionForEmergency()
						} else {
							// Normal promotion - reset consecutive counter
							sm.consecutiveEmergencyPromotions = 0
							promotedSessionID = sm.findNextSessionToPromote()
						}

						if promotedSessionID != "" {
							// Determine reason and log appropriately
							reason := "grace_expiration_promotion"
							if isEmergencyPromotion {
								reason = "emergency_promotion_deadlock_prevention"
								sm.lastEmergencyPromotion = now
								sm.consecutiveEmergencyPromotions++

								// Enhanced logging for emergency promotions
								sm.logger.Warn().
									Str("expiredSessionID", sessionID).
									Str("promotedSessionID", promotedSessionID).
									Bool("requireApproval", true).
									Int("consecutiveEmergencyPromotions", sm.consecutiveEmergencyPromotions).
									Int("trustScore", sm.getSessionTrustScore(promotedSessionID)).
									Msg("EMERGENCY: Bypassing approval requirement to prevent deadlock")
							}

							err := sm.transferPrimaryRole("", promotedSessionID, reason, "primary grace period expired")
							if err == nil {
								logEvent := sm.logger.Info()
								if isEmergencyPromotion {
									logEvent = sm.logger.Warn()
								}
								logEvent.
									Str("expiredSessionID", sessionID).
									Str("promotedSessionID", promotedSessionID).
									Str("reason", reason).
									Bool("isEmergencyPromotion", isEmergencyPromotion).
									Msg("Auto-promoted session after primary grace period expiration")
							} else {
								sm.logger.Error().
									Err(err).
									Str("expiredSessionID", sessionID).
									Str("promotedSessionID", promotedSessionID).
									Str("reason", reason).
									Bool("isEmergencyPromotion", isEmergencyPromotion).
									Msg("Failed to promote session after grace period expiration")
							}
						} else {
							logLevel := sm.logger.Info()
							if isEmergencyPromotion {
								logLevel = sm.logger.Error() // Emergency with no eligible sessions is critical
							}
							logLevel.
								Str("expiredSessionID", sessionID).
								Bool("isEmergencyPromotion", isEmergencyPromotion).
								Msg("Primary grace period expired but no eligible sessions to promote")
						}
					} else {
						// Non-primary session grace period expired - just cleanup
						sm.logger.Debug().
							Str("expiredSessionID", sessionID).
							Msg("Non-primary session grace period expired")
					}

					// Also clean up reconnect info for expired sessions
					delete(sm.reconnectInfo, sessionID)
				}
			}

			// Clean up pending sessions that have timed out (DoS protection)
			for id, session := range sm.sessions {
				if session.Mode == SessionModePending &&
					now.Sub(session.CreatedAt) > pendingTimeout {
					websocketLogger.Info().
						Str("sessionId", id).
						Dur("age", now.Sub(session.CreatedAt)).
						Msg("Removing timed-out pending session")
					delete(sm.sessions, id)
					needsBroadcast = true
				}
			}

			// Check primary session timeout (every 30 iterations = 30 seconds)
			if sm.primarySessionID != "" {
				if primary, exists := sm.sessions[sm.primarySessionID]; exists {
					currentTimeout := sm.getCurrentPrimaryTimeout()
					if now.Sub(primary.LastActive) > currentTimeout {
						timedOutSessionID := primary.ID
						primary.Mode = SessionModeObserver
						sm.primarySessionID = ""

						// Use enhanced emergency promotion system for timeout scenarios too
						isEmergencyPromotion := false
						var promotedSessionID string

						// Check if this requires emergency promotion due to approval requirements
						if currentSessionSettings != nil && currentSessionSettings.RequireApproval {
							isEmergencyPromotion = true

							// CRITICAL: Ensure we ALWAYS have a primary session
							// primarySessionID was just cleared above, so this will always be empty
							// But check anyway for completeness
							hasPrimary := sm.primarySessionID != ""
							if !hasPrimary {
								sm.logger.Error().
									Str("timedOutSessionID", timedOutSessionID).
									Msg("CRITICAL: No primary session after timeout - bypassing all rate limits")
							} else {
								// Rate limiting for emergency promotions (only when we have a primary)
								if now.Sub(sm.lastEmergencyPromotion) < 30*time.Second {
									sm.logger.Warn().
										Str("timedOutSessionID", timedOutSessionID).
										Dur("timeSinceLastEmergency", now.Sub(sm.lastEmergencyPromotion)).
										Msg("Emergency promotion rate limit exceeded during timeout - potential attack")
									continue // Skip this timeout
								}
							}

							// Use trust-based selection but exclude the timed-out session
							bestSessionID := ""
							bestScore := -1
							for id, session := range sm.sessions {
								if id != timedOutSessionID &&
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
							// Normal timeout promotion - find any observer except the timed-out one
							for id, session := range sm.sessions {
								if id != timedOutSessionID && session.Mode == SessionModeObserver && !sm.isSessionBlacklisted(id) {
									promotedSessionID = id
									break
								}
							}
						}

						// If found a session to promote
						if promotedSessionID != "" {
							reason := "timeout_promotion"
							if isEmergencyPromotion {
								reason = "emergency_timeout_promotion"
								sm.lastEmergencyPromotion = now
								sm.consecutiveEmergencyPromotions++

								// Enhanced logging for emergency timeout promotions
								sm.logger.Warn().
									Str("timedOutSessionID", timedOutSessionID).
									Str("promotedSessionID", promotedSessionID).
									Bool("requireApproval", true).
									Int("trustScore", sm.getSessionTrustScore(promotedSessionID)).
									Msg("EMERGENCY: Timeout promotion bypassing approval requirement")
							}

							err := sm.transferPrimaryRole(timedOutSessionID, promotedSessionID, reason, "primary session timeout")
							if err == nil {
								needsBroadcast = true
								logEvent := sm.logger.Info()
								if isEmergencyPromotion {
									logEvent = sm.logger.Warn()
								}
								logEvent.
									Str("timedOutSessionID", timedOutSessionID).
									Str("promotedSessionID", promotedSessionID).
									Bool("isEmergencyPromotion", isEmergencyPromotion).
									Msg("Auto-promoted session after primary timeout")
							}
						}
					}
				} else {
					// Primary session no longer exists, clear it
					sm.primarySessionID = ""
					needsBroadcast = true
				}
			}

			// Run validation immediately if a grace period expired, otherwise run periodically
			if gracePeriodExpired {
				sm.validateSinglePrimary()
			} else {
				// Periodic validateSinglePrimary to catch deadlock states
				validationCounter++
				if validationCounter >= 10 { // Every 10 seconds
					validationCounter = 0
					sm.validateSinglePrimary()
				}
			}

			sm.mu.Unlock()

			// Broadcast outside of lock if needed
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
