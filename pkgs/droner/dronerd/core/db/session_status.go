package db

type SessionStatus = string

const (
	SessionStatusQueued    SessionStatus = "queued"
	SessionStatusRunning   SessionStatus = "running"
	SessionStatusCompleted SessionStatus = "completed"
	SessionStatusFailed    SessionStatus = "failed"
	SessionStatusDeleted   SessionStatus = "deleted"
)
