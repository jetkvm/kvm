package session

// SessionMode represents the role/mode of a session
type SessionMode string

const (
	SessionModePrimary  SessionMode = "primary"
	SessionModeObserver SessionMode = "observer"
	SessionModeQueued   SessionMode = "queued"
	SessionModePending  SessionMode = "pending"
)
