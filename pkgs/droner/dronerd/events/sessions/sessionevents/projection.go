package sessionevents

import (
	"context"
	"time"

	coredb "github.com/Oudwins/droner/pkgs/droner/dronerd/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

type sessionProjection struct {
	StreamID       string
	Branch         string
	BackendID      string
	RepoPath       string
	WorktreePath   string
	RemoteURL      string
	AgentConfig    string
	LifecycleState string
	PublicState    string
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type projectionMutation struct {
	StreamID       string
	Branch         string
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

func (s *System) applyProjectionEvent(ctx context.Context, evt eventlog.Envelope) error {
	switch evt.Type {
	case eventTypeSessionQueued:
		payload, err := decodeQueuedPayload(evt)
		if err != nil {
			return err
		}
		return s.upsertProjection(ctx, projectionMutation{
			StreamID:       string(evt.StreamID),
			Branch:         payload.Branch,
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
	case eventTypeSessionEnvironmentProvisioningSuccess:
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionEnvironmentProvisioningSuccess), "queued", "", evt.OccurredAt)
	case eventTypeSessionReady:
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionReady), "running", "", evt.OccurredAt)
	case eventTypeSessionEnvironmentProvisioningFailed:
		payload, err := decodeFailedPayload(evt)
		if err != nil {
			return err
		}
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionEnvironmentProvisioningFailed), "failed", payload.Error, evt.OccurredAt)
	case eventTypeSessionCompletionRequested:
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionCompletionRequested), "running", "", evt.OccurredAt)
	case eventTypeSessionCompletionStarted:
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionCompletionStarted), "completing", "", evt.OccurredAt)
	case eventTypeSessionCompletionSuccess:
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionCompletionSuccess), "completed", "", evt.OccurredAt)
	case eventTypeSessionCompletionFailed:
		payload, err := decodeFailedPayload(evt)
		if err != nil {
			return err
		}
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionCompletionFailed), "failed", payload.Error, evt.OccurredAt)
	case eventTypeSessionDeletionRequested:
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionDeletionRequested), "deleting", "", evt.OccurredAt)
	case eventTypeSessionDeletionStarted:
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionDeletionStarted), "deleting", "", evt.OccurredAt)
	case eventTypeSessionDeletionSuccess:
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionDeletionSuccess), "deleted", "", evt.OccurredAt)
	case eventTypeSessionDeletionFailed:
		payload, err := decodeFailedPayload(evt)
		if err != nil {
			return err
		}
		return s.patchProjection(ctx, string(evt.StreamID), string(eventTypeSessionDeletionFailed), "failed", payload.Error, evt.OccurredAt)
	default:
		return nil
	}
}

func (s *System) upsertProjection(ctx context.Context, m projectionMutation) error {
	return s.queries.UpsertSessionProjection(ctx, coredb.UpsertSessionProjectionParams{
		StreamID:       m.StreamID,
		Branch:         m.Branch,
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

func (s *System) loadProjectionByBranch(ctx context.Context, branch string) (SessionRef, error) {
	row, err := s.queries.GetSessionProjectionByBranch(ctx, branch)
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

func (s *System) listHydratableProjectionRefs(ctx context.Context) ([]SessionRef, error) {
	rows, err := s.queries.ListHydratableSessionProjectionRefs(ctx)
	if err != nil {
		return nil, err
	}
	refs := []SessionRef{}
	for _, row := range rows {
		refs = append(refs, sessionRefFromRow(row))
	}
	return refs, nil
}

func (s *System) listReusableProjectionRefs(ctx context.Context, repoPath string, backendID string) ([]SessionRef, error) {
	rows, err := s.queries.ListReusableSessionProjectionRefs(ctx, coredb.ListReusableSessionProjectionRefsParams{
		RepoPath:  repoPath,
		BackendID: backendID,
	})
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
		Branch:         row.Branch,
		BackendID:      row.BackendID,
		RepoPath:       row.RepoPath,
		WorktreePath:   row.WorktreePath,
		RemoteURL:      row.RemoteUrl,
		AgentConfig:    row.AgentConfig,
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
		Branch:         row.Branch,
		BackendID:      row.BackendID,
		RepoPath:       row.RepoPath,
		WorktreePath:   row.WorktreePath,
		RemoteURL:      row.RemoteUrl,
		LifecycleState: row.LifecycleState,
		PublicState:    row.PublicState,
		LastError:      row.LastError,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}
