package db

type SessionStatus = string

const (
	SessionStatusQueued     SessionStatus = "queued"
	SessionStatusActiveIdle SessionStatus = "active.idle"
	SessionStatusCompleted  SessionStatus = "completed"
	SessionStatusFailed     SessionStatus = "failed"
	SessionStatusDeleted    SessionStatus = "deleted"
)
