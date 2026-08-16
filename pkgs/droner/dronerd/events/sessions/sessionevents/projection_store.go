package sessionevents

import (
	"context"
	"database/sql"
	"strings"

	coredb "github.com/Oudwins/droner/pkgs/droner/dronerd/db"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/sessions"
)

type ProjectionStore interface {
	Upsert(ctx context.Context, state sessions.State) error
	Delete(ctx context.Context, streamID string) error
	LoadStateByStreamID(ctx context.Context, streamID string) (sessions.State, error)
	LoadCurrentByBranch(ctx context.Context, branch string) (sessions.State, error)
	LoadBlockedByRepoAndBranch(ctx context.Context, repoPath string, branch string) (sessions.State, error)
	LoadByWorktreePath(ctx context.Context, worktreePath string) (sessions.State, error)
	LoadByTmuxSessionName(ctx context.Context, tmuxSessionName string) (sessions.State, error)
	LoadLatestNavigationByBranch(ctx context.Context, branch string) (sessions.State, error)
	ListActiveRefs(ctx context.Context) ([]sessions.State, error)
	ListHydratableRefs(ctx context.Context) ([]sessions.State, error)
	ListReusableRefs(ctx context.Context, repoPath string, backendID string) ([]sessions.State, error)
	ListVisible(ctx context.Context) ([]ListItem, error)
	ListAll(ctx context.Context) ([]ListItem, error)
	ListAfterCursor(ctx context.Context, statusesArg string, statusesValue sql.NullString, cursor string, limit int) ([]ListItem, error)
	ListBeforeCursor(ctx context.Context, statusesArg string, statusesValue sql.NullString, cursor string, limit int) ([]ListItem, error)
	ListOldest(ctx context.Context, statusesArg string, statusesValue sql.NullString, limit int) ([]ListItem, error)
}

type SQLiteProjectionStore struct {
	queries *coredb.Queries
}

func NewSQLiteProjectionStore(queries *coredb.Queries) *SQLiteProjectionStore {
	return &SQLiteProjectionStore{queries: queries}
}

func (s *SQLiteProjectionStore) Upsert(ctx context.Context, m sessions.State) error {
	return s.queries.UpsertSessionProjection(ctx, coredb.UpsertSessionProjectionParams{
		StreamID:        m.StreamID,
		Harness:         m.Harness,
		Branch:          nullableString(m.Branch),
		TmuxSessionName: nullableString(m.TmuxSessionName),
		BackendID:       m.BackendID,
		RepoPath:        m.RepoPath,
		WorktreePath:    nullableString(m.WorktreePath),
		RemoteUrl:       m.RemoteURL,
		AgentConfig:     m.AgentConfig,
		LifecycleState:  m.LifecycleState.String(),
		PublicState:     m.PublicState.String(),
		LastError:       m.LastError,
		PrNumber:        nullableInt64(m.PRNumber),
		PrState:         nullableString(m.PRState),
		PrCiState:       nullableString(m.PRCIState),
		PrUpdatedAt:     nullableTime(m.PRUpdatedAt),
		CreatedAt:       m.CreatedAt.UTC(),
		UpdatedAt:       m.UpdatedAt.UTC(),
	})
}

func (s *SQLiteProjectionStore) Delete(ctx context.Context, streamID string) error {
	return s.queries.DeleteSessionProjection(ctx, streamID)
}

func (s *SQLiteProjectionStore) LoadStateByStreamID(ctx context.Context, streamID string) (sessions.State, error) {
	row, err := s.queries.GetSessionProjectionByStreamID(ctx, streamID)
	if err != nil {
		return sessions.State{}, err
	}
	return stateFromProjection(row), nil
}

func (s *SQLiteProjectionStore) LoadCurrentByBranch(ctx context.Context, branch string) (sessions.State, error) {
	row, err := s.queries.GetCurrentSessionProjectionByBranch(ctx, nullableString(branch))
	if err != nil {
		return sessions.State{}, err
	}
	return stateFromProjection(row), nil
}

func (s *SQLiteProjectionStore) LoadBlockedByRepoAndBranch(ctx context.Context, repoPath string, branch string) (sessions.State, error) {
	row, err := s.queries.GetBlockedSessionProjectionByRepoPathAndBranch(ctx, coredb.GetBlockedSessionProjectionByRepoPathAndBranchParams{RepoPath: repoPath, Branch: nullableString(branch)})
	if err != nil {
		return sessions.State{}, err
	}
	return stateFromProjection(row), nil
}

func (s *SQLiteProjectionStore) LoadByWorktreePath(ctx context.Context, worktreePath string) (sessions.State, error) {
	row, err := s.queries.GetSessionProjectionByWorktreePath(ctx, nullableString(worktreePath))
	if err != nil {
		return sessions.State{}, err
	}
	return stateFromProjection(row), nil
}

func (s *SQLiteProjectionStore) LoadByTmuxSessionName(ctx context.Context, tmuxSessionName string) (sessions.State, error) {
	row, err := s.queries.GetSessionProjectionByTmuxSessionName(ctx, nullableString(tmuxSessionName))
	if err != nil {
		return sessions.State{}, err
	}
	return stateFromProjection(row), nil
}

func (s *SQLiteProjectionStore) LoadLatestNavigationByBranch(ctx context.Context, branch string) (sessions.State, error) {
	row, err := s.queries.GetLatestNavigationSessionProjectionByBranch(ctx, nullableString(branch))
	if err != nil {
		return sessions.State{}, err
	}
	return stateFromProjection(row), nil
}

func (s *SQLiteProjectionStore) ListActiveRefs(ctx context.Context) ([]sessions.State, error) {
	rows, err := s.queries.ListActiveSessionProjectionRefs(ctx)
	return statesFromRows(rows, err)
}

func (s *SQLiteProjectionStore) ListHydratableRefs(ctx context.Context) ([]sessions.State, error) {
	rows, err := s.queries.ListHydratableSessionProjectionRefs(ctx)
	return statesFromRows(rows, err)
}

func (s *SQLiteProjectionStore) ListReusableRefs(ctx context.Context, repoPath string, backendID string) ([]sessions.State, error) {
	rows, err := s.queries.ListReusableSessionProjectionRefs(ctx, coredb.ListReusableSessionProjectionRefsParams{RepoPath: repoPath, BackendID: backendID})
	return statesFromRows(rows, err)
}

func (s *SQLiteProjectionStore) ListVisible(ctx context.Context) ([]ListItem, error) {
	rows, err := s.queries.ListVisibleSessionProjectionItems(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]ListItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, newListItem(row.StreamID, row.RepoPath, row.RemoteUrl, nullStringValue(row.Branch), nullStringValue(row.TmuxSessionName), sessions.PublicState(row.PublicState)))
	}
	return items, nil
}

func (s *SQLiteProjectionStore) ListAll(ctx context.Context) ([]ListItem, error) {
	rows, err := s.queries.ListAllSessionProjectionItems(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]ListItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, newListItem(row.StreamID, row.RepoPath, row.RemoteUrl, nullStringValue(row.Branch), nullStringValue(row.TmuxSessionName), sessions.PublicState(row.PublicState)))
	}
	return items, nil
}

func (s *SQLiteProjectionStore) ListAfterCursor(ctx context.Context, statusesArg string, statusesValue sql.NullString, cursor string, limit int) ([]ListItem, error) {
	rows, err := s.queries.ListSessionProjectionItemsAfterCursorByStatuses(ctx, coredb.ListSessionProjectionItemsAfterCursorByStatusesParams{Column1: statusesArg, Column2: statusesValue, Column3: cursor, StreamID: cursor, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	items := make([]ListItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, newListItem(row.StreamID, row.RepoPath, row.RemoteUrl, nullStringValue(row.Branch), nullStringValue(row.TmuxSessionName), sessions.PublicState(row.PublicState)))
	}
	return items, nil
}

func (s *SQLiteProjectionStore) ListBeforeCursor(ctx context.Context, statusesArg string, statusesValue sql.NullString, cursor string, limit int) ([]ListItem, error) {
	rows, err := s.queries.ListSessionProjectionItemsBeforeCursorByStatuses(ctx, coredb.ListSessionProjectionItemsBeforeCursorByStatusesParams{Column1: statusesArg, Column2: statusesValue, Column3: cursor, StreamID: cursor, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	items := make([]ListItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, newListItem(row.StreamID, row.RepoPath, row.RemoteUrl, nullStringValue(row.Branch), nullStringValue(row.TmuxSessionName), sessions.PublicState(row.PublicState)))
	}
	return items, nil
}

func (s *SQLiteProjectionStore) ListOldest(ctx context.Context, statusesArg string, statusesValue sql.NullString, limit int) ([]ListItem, error) {
	rows, err := s.queries.ListSessionProjectionItemsOldestByStatuses(ctx, coredb.ListSessionProjectionItemsOldestByStatusesParams{Column1: statusesArg, Column2: statusesValue, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	items := make([]ListItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, newListItem(row.StreamID, row.RepoPath, row.RemoteUrl, nullStringValue(row.Branch), nullStringValue(row.TmuxSessionName), sessions.PublicState(row.PublicState)))
	}
	return items, nil
}

func statesFromRows(rows []coredb.SessionProjection, err error) ([]sessions.State, error) {
	if err != nil {
		return nil, err
	}
	states := make([]sessions.State, 0, len(rows))
	for _, row := range rows {
		states = append(states, stateFromProjection(row))
	}
	return states, nil
}

func statusesValue(statuses []string) (string, sql.NullString) {
	statusesArg := ""
	if len(statuses) > 0 {
		statusesArg = strings.Join(statuses, ",")
	}
	return statusesArg, sql.NullString{String: statusesArg, Valid: statusesArg != ""}
}
