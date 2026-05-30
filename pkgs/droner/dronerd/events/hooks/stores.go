package hooks

import (
	"context"
	"database/sql"

	coredb "github.com/Oudwins/droner/pkgs/droner/dronerd/db"
)

type SessionRef struct {
	Branch         string
	LifecycleState string
}

type SessionLookupStore interface {
	LoadByStreamID(ctx context.Context, streamID string) (SessionRef, error)
}

type SQLiteSessionLookupStore struct {
	queries *coredb.Queries
}

func NewSQLiteSessionLookupStore(queries *coredb.Queries) *SQLiteSessionLookupStore {
	return &SQLiteSessionLookupStore{queries: queries}
}

func (s *SQLiteSessionLookupStore) LoadByStreamID(ctx context.Context, streamID string) (SessionRef, error) {
	row, err := s.queries.GetSessionProjectionByStreamID(ctx, streamID)
	if err != nil {
		return SessionRef{}, err
	}
	return SessionRef{Branch: nullStringValue(row.Branch), LifecycleState: row.LifecycleState}, nil
}

func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
