package sessionevents

import (
	"context"
	"time"

	coredb "github.com/Oudwins/droner/pkgs/droner/dronerd/core/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
)

type sessionProjection struct {
	StreamID       string
	SimpleID       string
	BackendID      string
	RepoPath       string
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

func (p sessionProjection) taskTimes(taskType string) (schemas.TaskStatus, time.Time, time.Time) {
	status := schemas.TaskStatusPending
	startedAt := p.CreatedAt
	finishedAt := time.Time{}

	switch taskType {
	case "session_create":
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
	case "session_complete":
		switch p.LifecycleState {
		case string(eventTypeSessionCompletionSuccess):
			status = schemas.TaskStatusSucceeded
			finishedAt = p.UpdatedAt
		case string(eventTypeSessionCleanupFailed), string(eventTypeSessionFailed):
			status = schemas.TaskStatusFailed
			finishedAt = p.UpdatedAt
		case string(eventTypeSessionCompletionRequested), string(eventTypeSessionCompletionStarted):
			status = schemas.TaskStatusRunning
			startedAt = p.UpdatedAt
		}
	case "session_delete":
		switch p.LifecycleState {
		case string(eventTypeSessionDeletionSuccess):
			status = schemas.TaskStatusSucceeded
			finishedAt = p.UpdatedAt
		case string(eventTypeSessionCleanupFailed), string(eventTypeSessionFailed):
			status = schemas.TaskStatusFailed
			finishedAt = p.UpdatedAt
		case string(eventTypeSessionDeletionRequested):
			status = schemas.TaskStatusRunning
			startedAt = p.UpdatedAt
		}
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
	case eventTypeSessionCompletionRequested:
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionCompletionRequested), "running", "", evt.OccurredAt)
	case eventTypeSessionCompletionStarted:
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionCompletionStarted), "completing", "", evt.OccurredAt)
	case eventTypeSessionCompletionSuccess:
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionCompletionSuccess), "completed", "", evt.OccurredAt)
	case eventTypeSessionDeletionRequested:
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionDeletionRequested), "deleting", "", evt.OccurredAt)
	case eventTypeSessionDeletionSuccess:
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionDeletionSuccess), "deleted", "", evt.OccurredAt)
	case eventTypeSessionCleanupFailed:
		payload, err := decodeFailedPayload(evt)
		if err != nil {
			return err
		}
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionCleanupFailed), "failed", payload.Error, evt.OccurredAt)
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
	return s.queries.UpsertSessionProjection(ctx, coredb.UpsertSessionProjectionParams{
		StreamID:       m.StreamID,
		SimpleID:       m.SimpleID,
		BackendID:      m.BackendID,
		RepoPath:       m.RepoPath,
		WorktreePath:   m.WorktreePath,
		RemoteUrl:      m.RemoteURL,
		AgentConfig:    m.AgentConfig,
		LifecycleState: m.LifecycleState,
		PublicState:    m.PublicState,
		LastError:      m.LastError,
		CreatedAt:      m.OccurredAt.UTC(),
		UpdatedAt:      m.OccurredAt.UTC(),
	})
}

func (s *System) patchProjection(ctx context.Context, streamID, lifecycleState, publicState, lastError string, occurredAt time.Time) error {
	return s.queries.PatchSessionProjection(ctx, coredb.PatchSessionProjectionParams{
		LifecycleState: lifecycleState,
		PublicState:    publicState,
		LastError:      lastError,
		UpdatedAt:      occurredAt.UTC(),
		StreamID:       streamID,
	})
}

func (s *System) loadProjection(ctx context.Context, streamID string) (sessionProjection, error) {
	row, err := s.queries.GetSessionProjectionByStreamID(ctx, streamID)
	if err != nil {
		return sessionProjection{}, err
	}
	return projectionFromRow(row), nil
}

func (s *System) loadProjectionBySimpleID(ctx context.Context, simpleID string) (SessionRef, error) {
	row, err := s.queries.GetSessionProjectionBySimpleID(ctx, simpleID)
	if err != nil {
		return SessionRef{}, err
	}
	return sessionRefFromRow(row), nil
}

func (s *System) listActiveProjectionRefs(ctx context.Context) ([]SessionRef, error) {
	rows, err := s.queries.ListActiveSessionProjectionRefs(ctx)
	if err != nil {
		return nil, err
	}
	refs := []SessionRef{}
	for _, row := range rows {
		refs = append(refs, sessionRefFromRow(row))
	}
	return refs, nil
}

func projectionFromRow(row coredb.SessionProjection) sessionProjection {
	return sessionProjection{
		StreamID:       row.StreamID,
		SimpleID:       row.SimpleID,
		BackendID:      row.BackendID,
		RepoPath:       row.RepoPath,
		WorktreePath:   row.WorktreePath,
		LifecycleState: row.LifecycleState,
		PublicState:    row.PublicState,
		LastError:      row.LastError,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func sessionRefFromRow(row coredb.SessionProjection) SessionRef {
	return SessionRef{
		StreamID:       row.StreamID,
		SimpleID:       row.SimpleID,
		BackendID:      row.BackendID,
		RepoPath:       row.RepoPath,
		WorktreePath:   row.WorktreePath,
		LifecycleState: row.LifecycleState,
		PublicState:    row.PublicState,
		LastError:      row.LastError,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}
