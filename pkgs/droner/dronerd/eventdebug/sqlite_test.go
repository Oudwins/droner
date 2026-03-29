package eventdebug

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSQLiteStore(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE event_log (
			id TEXT PRIMARY KEY,
			stream_id TEXT NOT NULL,
			stream_version INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			schema_version INTEGER NOT NULL,
			occurred_at TEXT NOT NULL,
			causation_id TEXT,
			correlation_id TEXT,
			payload TEXT NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	ts1 := time.Date(2026, 3, 29, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	ts2 := time.Date(2026, 3, 29, 10, 1, 0, 0, time.UTC).Format(time.RFC3339Nano)
	_, err = db.Exec(`
		INSERT INTO event_log (id, stream_id, stream_version, event_type, schema_version, occurred_at, causation_id, correlation_id, payload)
		VALUES
			('evt-1', 'session/a', 1, 'session.queued', 1, ?, '', 'corr-1', '{"repoPath":"/tmp/repo"}'),
			('evt-2', 'session/a', 2, 'session.ready', 1, ?, 'evt-1', 'corr-1', '{"worktreePath":"/tmp/wt"}'),
			('evt-3', 'session/b', 1, 'session.queued', 1, ?, '', 'corr-2', '{"repoPath":"/tmp/repo2"}');
	`, ts1, ts2, ts2)
	if err != nil {
		t.Fatalf("insert rows: %v", err)
	}

	store := NewSQLiteStore(db, SQLiteStoreOptions{})
	streams, err := store.ListStreams(context.Background(), ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListStreams: %v", err)
	}
	if len(streams) != 2 {
		t.Fatalf("stream count = %d, want 2", len(streams))
	}

	stream, err := store.LoadStream(context.Background(), "session/a", StreamOptions{})
	if err != nil {
		t.Fatalf("LoadStream: %v", err)
	}
	if stream.Summary.EventCount != 2 {
		t.Fatalf("event count = %d, want 2", stream.Summary.EventCount)
	}
	if len(stream.Events) != 2 || stream.Events[1].EventType != "session.ready" {
		t.Fatalf("unexpected events: %#v", stream.Events)
	}
}
