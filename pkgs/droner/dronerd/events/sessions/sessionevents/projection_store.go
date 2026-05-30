package sessionevents

import (
	"context"
	"database/sql"
	"strings"

	coredb "github.com/Oudwins/droner/pkgs/droner/dronerd/db"
)

type ProjectionStore interface {
	Upsert(ctx context.Context, mutation projectionMutation) error
	Delete(ctx context.Context, streamID string) error
	LoadStateByStreamID(ctx context.Context, streamID string) (sessionState, error)
	LoadCurrentByBranch(ctx context.Context, branch string) (SessionRef, error)
	LoadBlockedByRepoAndBranch(ctx context.Context, repoPath string, branch string) (SessionRef, error)
	LoadByWorktreePath(ctx context.Context, worktreePath string) (SessionRef, error)
	LoadLatestNavigationByBranch(ctx context.Context, branch string) (SessionRef, error)
	ListActiveRefs(ctx context.Context) ([]SessionRef, error)
	ListHydratableRefs(ctx context.Context) ([]SessionRef, error)
	ListReusableRefs(ctx context.Context, repoPath string, backendID string) ([]SessionRef, error)
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

func (s *SQLiteProjectionStore) Upsert(ctx context.Context, m projectionMutation) error {
	return s.queries.UpsertSessionProjection(ctx, coredb.UpsertSessionProjectionParams{
		StreamID:       m.StreamID,
		Harness:        m.Harness,
		Branch:         nullableString(m.Branch),
		BackendID:      m.BackendID,
		RepoPath:       m.RepoPath,
		WorktreePath:   nullableString(m.WorktreePath),
		RemoteUrl:      m.RemoteURL,
		AgentConfig:    m.AgentConfig,
		LifecycleState: m.LifecycleState,
		PublicState:    m.PublicState,
		LastError:      m.LastError,
		PrNumber:       nullableInt64(m.PRNumber),
		PrState:        nullableString(m.PRState),
		PrCiState:      nullableString(m.PRCIState),
		PrUpdatedAt:    nullableTime(m.PRUpdatedAt),
		CreatedAt:      m.CreatedAt.UTC(),
		UpdatedAt:      m.UpdatedAt.UTC(),
	})
}

func (s *SQLiteProjectionStore) Delete(ctx context.Context, streamID string) error {
	return s.queries.DeleteSessionProjection(ctx, streamID)
}

func (s *SQLiteProjectionStore) LoadStateByStreamID(ctx context.Context, streamID string) (sessionState, error) {
	row, err := s.queries.GetSessionProjectionByStreamID(ctx, streamID)
	if err != nil {
		return sessionState{}, err
	}
	return stateFromProjection(row), nil
}

func (s *SQLiteProjectionStore) LoadCurrentByBranch(ctx context.Context, branch string) (SessionRef, error) {
	row, err := s.queries.GetCurrentSessionProjectionByBranch(ctx, nullableString(branch))
	if err != nil {
		return SessionRef{}, err
	}
	return sessionRefFromRow(row), nil
}

func (s *SQLiteProjectionStore) LoadBlockedByRepoAndBranch(ctx context.Context, repoPath string, branch string) (SessionRef, error) {
	row, err := s.queries.GetBlockedSessionProjectionByRepoPathAndBranch(ctx, coredb.GetBlockedSessionProjectionByRepoPathAndBranchParams{RepoPath: repoPath, Branch: nullableString(branch)})
	if err != nil {
		return SessionRef{}, err
	}
	return sessionRefFromRow(row), nil
}

func (s *SQLiteProjectionStore) LoadByWorktreePath(ctx context.Context, worktreePath string) (SessionRef, error) {
	row, err := s.queries.GetSessionProjectionByWorktreePath(ctx, nullableString(worktreePath))
	if err != nil {
		return SessionRef{}, err
	}
	return sessionRefFromRow(row), nil
}

func (s *SQLiteProjectionStore) LoadLatestNavigationByBranch(ctx context.Context, branch string) (SessionRef, error) {
	row, err := s.queries.GetLatestNavigationSessionProjectionByBranch(ctx, nullableString(branch))
	if err != nil {
		return SessionRef{}, err
	}
	return sessionRefFromRow(row), nil
}

func (s *SQLiteProjectionStore) ListActiveRefs(ctx context.Context) ([]SessionRef, error) {
	rows, err := s.queries.ListActiveSessionProjectionRefs(ctx)
	return sessionRefsFromRows(rows, err)
}

func (s *SQLiteProjectionStore) ListHydratableRefs(ctx context.Context) ([]SessionRef, error) {
	rows, err := s.queries.ListHydratableSessionProjectionRefs(ctx)
	return sessionRefsFromRows(rows, err)
}

func (s *SQLiteProjectionStore) ListReusableRefs(ctx context.Context, repoPath string, backendID string) ([]SessionRef, error) {
	rows, err := s.queries.ListReusableSessionProjectionRefs(ctx, coredb.ListReusableSessionProjectionRefsParams{RepoPath: repoPath, BackendID: backendID})
	return sessionRefsFromRows(rows, err)
}

func (s *SQLiteProjectionStore) ListVisible(ctx context.Context) ([]ListItem, error) {
	rows, err := s.queries.ListVisibleSessionProjectionItems(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]ListItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, newListItem(row.StreamID, row.RepoPath, row.RemoteUrl, nullStringValue(row.Branch), PublicState(row.PublicState)))
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
		items = append(items, newListItem(row.StreamID, row.RepoPath, row.RemoteUrl, nullStringValue(row.Branch), PublicState(row.PublicState)))
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
		items = append(items, newListItem(row.StreamID, row.RepoPath, row.RemoteUrl, nullStringValue(row.Branch), PublicState(row.PublicState)))
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
		items = append(items, newListItem(row.StreamID, row.RepoPath, row.RemoteUrl, nullStringValue(row.Branch), PublicState(row.PublicState)))
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
		items = append(items, newListItem(row.StreamID, row.RepoPath, row.RemoteUrl, nullStringValue(row.Branch), PublicState(row.PublicState)))
	}
	return items, nil
}

func sessionRefsFromRows(rows []coredb.SessionProjection, err error) ([]SessionRef, error) {
	if err != nil {
		return nil, err
	}
	refs := make([]SessionRef, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, sessionRefFromRow(row))
	}
	return refs, nil
}

func sessionRefFromRow(row coredb.SessionProjection) SessionRef {
	return SessionRef{
		StreamID:       row.StreamID,
		Harness:        row.Harness,
		Branch:         nullStringValue(row.Branch),
		BackendID:      row.BackendID,
		RepoPath:       row.RepoPath,
		WorktreePath:   nullStringValue(row.WorktreePath),
		RemoteURL:      row.RemoteUrl,
		LifecycleState: LifecycleState(row.LifecycleState),
		PublicState:    PublicState(row.PublicState),
		LastError:      row.LastError,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func statusesValue(statuses []string) (string, sql.NullString) {
	statusesArg := ""
	if len(statuses) > 0 {
		statusesArg = strings.Join(statuses, ",")
	}
	return statusesArg, sql.NullString{String: statusesArg, Valid: statusesArg != ""}
}
