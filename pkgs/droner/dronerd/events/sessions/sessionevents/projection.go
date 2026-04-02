package sessionevents

import (
	"context"
	"database/sql"
	"errors"
	"time"

	coredb "github.com/Oudwins/droner/pkgs/droner/dronerd/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

type projectionMutation struct {
	StreamID       string
	Harness        string
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

func (s *System) applyProjectionEvent(ctx context.Context, evt eventlog.Envelope) error {
	state, err := s.loadProjectionStateForUpdate(ctx, evt)
	if err != nil {
		return err
	}
	changed, err := state.Apply(evt)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return s.upsertProjection(ctx, state.projectionMutation())
}

func (s *System) upsertProjection(ctx context.Context, m projectionMutation) error {
	return s.queries.UpsertSessionProjection(ctx, coredb.UpsertSessionProjectionParams{
		StreamID:       m.StreamID,
		Harness:        m.Harness,
		Branch:         m.Branch,
		BackendID:      m.BackendID,
		RepoPath:       m.RepoPath,
		WorktreePath:   m.WorktreePath,
		RemoteUrl:      m.RemoteURL,
		AgentConfig:    m.AgentConfig,
		LifecycleState: m.LifecycleState,
		PublicState:    m.PublicState,
		LastError:      m.LastError,
		CreatedAt:      m.CreatedAt.UTC(),
		UpdatedAt:      m.UpdatedAt.UTC(),
	})
}

func (s *System) loadProjectionStateForUpdate(ctx context.Context, evt eventlog.Envelope) (sessionState, error) {
	row, err := s.queries.GetSessionProjectionByStreamID(ctx, string(evt.StreamID))
	if err == nil {
		return stateFromProjection(row), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return sessionState{}, err
	}
	state, _, err := s.loadSessionStateBeforeVersion(ctx, string(evt.StreamID), evt.StreamVersion)
	return state, err
}

func (s *System) loadProjectionByBranch(ctx context.Context, branch string) (SessionRef, error) {
	row, err := s.queries.GetSessionProjectionByBranch(ctx, branch)
	if err != nil {
		return SessionRef{}, err
	}
	return sessionRefFromRow(row), nil
}

func (s *System) loadLatestNavigationProjectionByBranch(ctx context.Context, branch string) (SessionRef, error) {
	row, err := s.queries.GetLatestNavigationSessionProjectionByBranch(ctx, branch)
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

func sessionRefFromRow(row coredb.SessionProjection) SessionRef {
	return SessionRef{
		StreamID:       row.StreamID,
		Harness:        row.Harness,
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
