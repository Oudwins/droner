package db

import (
	"database/sql"
	"time"
)

type Session struct {
	ID           string
	Branch       string
	Status       SessionStatus
	BackendID    string
	RepoPath     string
	RemoteUrl    sql.NullString
	WorktreePath string
	AgentConfig  sql.NullString
	Error        sql.NullString
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
