package sessionevents

import (
	"context"
	"database/sql"
	"errors"
	"time"

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
	PRNumber       int64
	PRState        string
	PRCIState      string
	PRUpdatedAt    time.Time
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
	return s.projections.Upsert(ctx, m)
}

func nullableInt64(value int64) sql.NullInt64 {
	if value == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: value, Valid: true}
}

func nullableTime(value time.Time) sql.NullTime {
	if value.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: value.UTC(), Valid: true}
}

func nullInt64Value(value sql.NullInt64) int64 {
	if !value.Valid {
		return 0
	}
	return value.Int64
}

func nullTimeValue(value sql.NullTime) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time
}

func (s *System) loadProjectionStateForUpdate(ctx context.Context, evt eventlog.Envelope) (sessionState, error) {
	state, err := s.projections.LoadStateByStreamID(ctx, string(evt.StreamID))
	if err == nil {
		return state, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return sessionState{}, err
	}
	state, _, err = s.loadSessionStateBeforeVersion(ctx, string(evt.StreamID), evt.StreamVersion)
	return state, err
}

func (s *System) loadCurrentProjectionByBranch(ctx context.Context, branch string) (SessionRef, error) {
	return s.projections.LoadCurrentByBranch(ctx, branch)
}

func (s *System) loadBlockedProjectionByRepoAndBranch(ctx context.Context, repoPath string, branch string) (SessionRef, error) {
	return s.projections.LoadBlockedByRepoAndBranch(ctx, repoPath, branch)
}

func (s *System) loadProjectionByWorktreePath(ctx context.Context, worktreePath string) (SessionRef, error) {
	return s.projections.LoadByWorktreePath(ctx, worktreePath)
}

func (s *System) loadLatestNavigationProjectionByBranch(ctx context.Context, branch string) (SessionRef, error) {
	return s.projections.LoadLatestNavigationByBranch(ctx, branch)
}

func (s *System) listActiveProjectionRefs(ctx context.Context) ([]SessionRef, error) {
	return s.projections.ListActiveRefs(ctx)
}

func (s *System) listHydratableProjectionRefs(ctx context.Context) ([]SessionRef, error) {
	return s.projections.ListHydratableRefs(ctx)
}

func (s *System) listReusableProjectionRefs(ctx context.Context, repoPath string, backendID string) ([]SessionRef, error) {
	return s.projections.ListReusableRefs(ctx, repoPath, backendID)
}

func nullableString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
