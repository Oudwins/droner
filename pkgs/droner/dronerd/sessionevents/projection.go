package sessionevents

import (
	"context"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
)

type sessionProjection struct {
	StreamID       string
	SimpleID       string
	WorktreePath   string
	LifecycleState string
	PublicState    string
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type projectionMutation struct {
	StreamID       string
	SimpleID       string
	BackendID      string
	RepoPath       string
	WorktreePath   string
	RemoteURL      string
	AgentConfig    string
	LifecycleState string
	PublicState    string
	LastError      string
	OccurredAt     time.Time
}

func (p sessionProjection) taskTimes() (schemas.TaskStatus, time.Time, time.Time) {
	status := schemas.TaskStatusPending
	startedAt := p.CreatedAt
	finishedAt := time.Time{}

	switch p.LifecycleState {
	case string(eventTypeSessionReady):
		status = schemas.TaskStatusSucceeded
		finishedAt = p.UpdatedAt
	case string(eventTypeSessionFailed):
		status = schemas.TaskStatusFailed
		finishedAt = p.UpdatedAt
	case string(eventTypeSessionEnvironmentProvisioningStarted), string(eventTypeSessionEnvironmentProvisioned), string(eventTypeSessionRuntimeStarted):
		status = schemas.TaskStatusRunning
		startedAt = p.UpdatedAt
	}

	return status, startedAt, finishedAt
}

func (s *System) applyProjectionEvent(ctx context.Context, evt eventlog.Envelope) error {
	switch evt.Type {
	case eventTypeSessionQueued:
		payload, err := decodeQueuedPayload(evt)
		if err != nil {
			return err
		}
		return s.upsertProjection(ctx, projectionMutation{
			StreamID:       string(evt.StreamID),
			SimpleID:       payload.SimpleID,
			BackendID:      payload.BackendID,
			RepoPath:       payload.RepoPath,
			WorktreePath:   payload.WorktreePath,
			RemoteURL:      payload.RemoteURL,
			AgentConfig:    payload.AgentConfigJSON,
			LifecycleState: string(eventTypeSessionQueued),
			PublicState:    "queued",
			OccurredAt:     evt.OccurredAt,
		})
	case eventTypeSessionEnvironmentProvisioningStarted:
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionEnvironmentProvisioningStarted), "queued", "", evt.OccurredAt)
	case eventTypeSessionEnvironmentProvisioned:
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionEnvironmentProvisioned), "queued", "", evt.OccurredAt)
	case eventTypeSessionRuntimeStarted:
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionRuntimeStarted), "queued", "", evt.OccurredAt)
	case eventTypeSessionReady:
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionReady), "running", "", evt.OccurredAt)
	case eventTypeSessionFailed:
		payload, err := decodeFailedPayload(evt)
		if err != nil {
			return err
		}
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionFailed), "failed", payload.Error, evt.OccurredAt)
	default:
		return nil
	}
}

func (s *System) upsertProjection(ctx context.Context, m projectionMutation) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO session_projection (
			stream_id, simple_id, backend_id, repo_path, worktree_path, remote_url, agent_config,
			lifecycle_state, public_state, last_error, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(stream_id) DO UPDATE SET
			simple_id = excluded.simple_id,
			backend_id = excluded.backend_id,
			repo_path = excluded.repo_path,
			worktree_path = excluded.worktree_path,
			remote_url = excluded.remote_url,
			agent_config = excluded.agent_config,
			lifecycle_state = excluded.lifecycle_state,
			public_state = excluded.public_state,
			last_error = excluded.last_error,
			updated_at = excluded.updated_at
	`, m.StreamID, m.SimpleID, m.BackendID, m.RepoPath, m.WorktreePath, m.RemoteURL, m.AgentConfig, m.LifecycleState, m.PublicState, m.LastError, formatTime(m.OccurredAt), formatTime(m.OccurredAt))
	return err
}

func (s *System) patchProjection(ctx context.Context, streamID, lifecycleState, publicState, lastError string, occurredAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE session_projection
		SET lifecycle_state = ?, public_state = ?, last_error = ?, updated_at = ?
		WHERE stream_id = ?
	`, lifecycleState, publicState, lastError, formatTime(occurredAt), streamID)
	return err
}

func (s *System) loadProjection(ctx context.Context, streamID string) (sessionProjection, error) {
	var projection sessionProjection
	var createdAt string
	var updatedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT stream_id, simple_id, worktree_path, lifecycle_state, public_state, last_error, created_at, updated_at
		FROM session_projection WHERE stream_id = ?
	`, streamID).Scan(
		&projection.StreamID,
		&projection.SimpleID,
		&projection.WorktreePath,
		&projection.LifecycleState,
		&projection.PublicState,
		&projection.LastError,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return sessionProjection{}, err
	}
	projection.CreatedAt = parseTime(createdAt)
	projection.UpdatedAt = parseTime(updatedAt)
	return projection, nil
}

var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS session_projection (
		stream_id TEXT PRIMARY KEY,
		simple_id TEXT NOT NULL UNIQUE,
		backend_id TEXT NOT NULL,
		repo_path TEXT NOT NULL,
		worktree_path TEXT NOT NULL,
		remote_url TEXT NOT NULL DEFAULT '',
		agent_config TEXT NOT NULL DEFAULT '',
		lifecycle_state TEXT NOT NULL,
		public_state TEXT NOT NULL,
		last_error TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`,
	`CREATE INDEX IF NOT EXISTS session_projection_public_state_idx ON session_projection(public_state, updated_at DESC);`,
}
