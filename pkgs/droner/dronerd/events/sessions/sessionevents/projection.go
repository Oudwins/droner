package sessionevents

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/sessions"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

func (s *System) applyProjectionEvent(ctx context.Context, evt eventlog.Envelope) error {
	state, err := s.loadProjectionStateForUpdate(ctx, evt)
	if err != nil {
		return err
	}
	changed, err := applySessionEvent(&state, evt)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return s.upsertProjection(ctx, state)
}

func (s *System) upsertProjection(ctx context.Context, state sessions.State) error {
	return s.projections.Upsert(ctx, state)
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

func (s *System) loadProjectionStateForUpdate(ctx context.Context, evt eventlog.Envelope) (sessions.State, error) {
	state, err := s.projections.LoadStateByStreamID(ctx, string(evt.StreamID))
	if err == nil {
		return state, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return sessions.State{}, err
	}
	state, _, err = s.loadSessionStateBeforeVersion(ctx, string(evt.StreamID), evt.StreamVersion)
	return state, err
}

func (s *System) loadCurrentProjectionByBranch(ctx context.Context, branch string) (sessions.State, error) {
	return s.projections.LoadCurrentByBranch(ctx, branch)
}

func (s *System) loadBlockedProjectionByRepoAndBranch(ctx context.Context, repoPath string, branch string) (sessions.State, error) {
	return s.projections.LoadBlockedByRepoAndBranch(ctx, repoPath, branch)
}

func (s *System) loadProjectionByWorktreePath(ctx context.Context, worktreePath string) (sessions.State, error) {
	return s.projections.LoadByWorktreePath(ctx, worktreePath)
}

func (s *System) loadProjectionByTmuxSessionName(ctx context.Context, tmuxSessionName string) (sessions.State, error) {
	return s.projections.LoadByTmuxSessionName(ctx, tmuxSessionName)
}

func (s *System) loadLatestNavigationProjectionByBranch(ctx context.Context, branch string) (sessions.State, error) {
	return s.projections.LoadLatestNavigationByBranch(ctx, branch)
}

func (s *System) listActiveProjectionRefs(ctx context.Context) ([]sessions.State, error) {
	return s.projections.ListActiveRefs(ctx)
}

func (s *System) listHydratableProjectionRefs(ctx context.Context) ([]sessions.State, error) {
	return s.projections.ListHydratableRefs(ctx)
}

func (s *System) listReusableProjectionRefs(ctx context.Context, repoPath string, backendID string) ([]sessions.State, error) {
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
