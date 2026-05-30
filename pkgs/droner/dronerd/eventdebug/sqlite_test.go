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
			topic TEXT NOT NULL,
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
		INSERT INTO event_log (topic, id, stream_id, stream_version, event_type, schema_version, occurred_at, causation_id, correlation_id, payload)
		VALUES
			('sessions', 'evt-1', 'session/a', 1, 'session.queued', 1, ?, '', 'corr-1', '{"repoPath":"/tmp/repo"}'),
			('sessions', 'evt-2', 'session/a', 2, 'session.ready', 1, ?, 'evt-1', 'corr-1', '{"worktreePath":"/tmp/wt"}'),
			('sessions', 'evt-3', 'session/b', 1, 'session.queued', 1, ?, '', 'corr-2', '{"repoPath":"/tmp/repo2"}'),
			('pullrequests', 'evt-4', 'github:owner/repo#1', 1, 'pr.observed', 1, ?, '', 'corr-3', '{"number":1}');
	`, ts1, ts2, ts2, ts2)
	if err != nil {
		t.Fatalf("insert rows: %v", err)
	}

	store := NewSQLiteStore(db, SQLiteStoreOptions{})
	streams, err := store.ListStreams(context.Background(), ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListStreams: %v", err)
	}
	if len(streams) != 3 {
		t.Fatalf("stream count = %d, want 3", len(streams))
	}
	if streams[0].Topic != topicPullRequests || streams[0].StreamID != "github:owner/repo#1" {
		t.Fatalf("first stream = %#v, want pull request stream", streams[0])
	}
	if streams[1].Topic != topicSessions || streams[1].StreamID != "session/a" {
		t.Fatalf("second stream = %#v, want session/a", streams[1])
	}
	if streams[2].Topic != topicSessions || streams[2].StreamID != "session/b" {
		t.Fatalf("third stream = %#v, want session/b", streams[2])
	}

	sessionStreams, err := store.ListStreams(context.Background(), ListOptions{Topics: []string{topicSessions}, Limit: 10})
	if err != nil {
		t.Fatalf("ListStreams sessions: %v", err)
	}
	if len(sessionStreams) != 2 {
		t.Fatalf("session stream count = %d, want 2", len(sessionStreams))
	}

	multiTopicStreams, err := store.ListStreams(context.Background(), ListOptions{Topics: []string{topicSessions, topicPullRequests}, Limit: 10})
	if err != nil {
		t.Fatalf("ListStreams multiple topics: %v", err)
	}
	if len(multiTopicStreams) != 3 {
		t.Fatalf("multi-topic stream count = %d, want 3", len(multiTopicStreams))
	}

	stream, err := store.LoadStream(context.Background(), "session/a", StreamOptions{Topic: topicSessions})
	if err != nil {
		t.Fatalf("LoadStream: %v", err)
	}
	if stream.Topic != topicSessions || stream.Summary.Topic != topicSessions {
		t.Fatalf("stream topic = %q summary topic = %q, want sessions", stream.Topic, stream.Summary.Topic)
	}
	if stream.Summary.EventCount != 2 {
		t.Fatalf("event count = %d, want 2", stream.Summary.EventCount)
	}
	if len(stream.Events) != 2 || stream.Events[1].EventType != "session.ready" {
		t.Fatalf("unexpected events: %#v", stream.Events)
	}
	if stream.Events[0].Topic != topicSessions {
		t.Fatalf("event topic = %q, want sessions", stream.Events[0].Topic)
	}
}

func TestSQLiteStoreLoadStreamAmbiguousTopic(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE event_log (
			topic TEXT NOT NULL,
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

	ts := time.Date(2026, 3, 29, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	_, err = db.Exec(`
		INSERT INTO event_log (topic, id, stream_id, stream_version, event_type, schema_version, occurred_at, causation_id, correlation_id, payload)
		VALUES
			('sessions', 'evt-1', 'shared', 1, 'session.queued', 1, ?, '', '', '{}'),
			('pullrequests', 'evt-2', 'shared', 1, 'pr.observed', 1, ?, '', '', '{}');
	`, ts, ts)
	if err != nil {
		t.Fatalf("insert rows: %v", err)
	}

	store := NewSQLiteStore(db, SQLiteStoreOptions{})
	_, err = store.LoadStream(context.Background(), "shared", StreamOptions{})
	if !isAmbiguousStreamError(err) {
		t.Fatalf("LoadStream err = %v, want ambiguous stream error", err)
	}

	stream, err := store.LoadStream(context.Background(), "shared", StreamOptions{Topic: topicPullRequests})
	if err != nil {
		t.Fatalf("LoadStream pullrequests: %v", err)
	}
	if stream.Topic != topicPullRequests {
		t.Fatalf("stream topic = %q, want pullrequests", stream.Topic)
	}
}
