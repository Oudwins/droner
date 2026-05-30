package pullrequestevents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	coredb "github.com/Oudwins/droner/pkgs/droner/dronerd/db"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/remote"
)

type SessionRef struct {
	RemoteURL string
	Branch    string
}

type SessionLookupStore interface {
	LoadByStreamID(ctx context.Context, streamID string) (SessionRef, error)
}

type PullRequestSnapshotStore interface {
	Load(ctx context.Context, streamID string) (remote.PullRequestSnapshot, bool, error)
	Upsert(ctx context.Context, snapshot remote.PullRequestSnapshot, observedAt time.Time) error
}

type SQLiteSessionLookupStore struct {
	queries *coredb.Queries
}

type SQLitePullRequestSnapshotStore struct {
	queries *coredb.Queries
}

func NewSQLiteSessionLookupStore(queries *coredb.Queries) *SQLiteSessionLookupStore {
	return &SQLiteSessionLookupStore{queries: queries}
}

func NewSQLitePullRequestSnapshotStore(queries *coredb.Queries) *SQLitePullRequestSnapshotStore {
	return &SQLitePullRequestSnapshotStore{queries: queries}
}

func (s *SQLiteSessionLookupStore) LoadByStreamID(ctx context.Context, streamID string) (SessionRef, error) {
	row, err := s.queries.GetSessionProjectionByStreamID(ctx, streamID)
	if err != nil {
		return SessionRef{}, err
	}
	return SessionRef{RemoteURL: row.RemoteUrl, Branch: nullStringValue(row.Branch)}, nil
}

func (s *SQLitePullRequestSnapshotStore) Load(ctx context.Context, streamID string) (remote.PullRequestSnapshot, bool, error) {
	row, err := s.queries.GetPRLatestSnapshotByStreamID(ctx, streamID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return remote.PullRequestSnapshot{}, false, nil
		}
		return remote.PullRequestSnapshot{}, false, err
	}
	var snapshot remote.PullRequestSnapshot
	if err := json.Unmarshal([]byte(row.SnapshotJson), &snapshot); err != nil {
		return remote.PullRequestSnapshot{}, false, err
	}
	return snapshot, true, nil
}

func (s *SQLitePullRequestSnapshotStore) Upsert(ctx context.Context, snapshot remote.PullRequestSnapshot, observedAt time.Time) error {
	snapshotJSON, err := stableSnapshotJSON(snapshot)
	if err != nil {
		return err
	}
	return s.queries.UpsertPRLatestSnapshot(ctx, coredb.UpsertPRLatestSnapshotParams{
		StreamID:     prStreamID(snapshot),
		Provider:     snapshot.Provider,
		RemoteUrl:    snapshot.RemoteURL,
		RepoOwner:    snapshot.RepoOwner,
		RepoName:     snapshot.RepoName,
		Number:       int64(snapshot.Number),
		HeadRef:      snapshot.HeadRef,
		HeadSha:      snapshot.HeadSHA,
		ObservedAt:   observedAt.UTC(),
		SnapshotJson: string(snapshotJSON),
	})
}

func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
