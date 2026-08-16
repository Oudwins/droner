package db

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestSessionEnrichmentTmuxSessionNameMigrationUpdatesEventsInPlace(t *testing.T) {
	ctx := context.Background()
	conn, err := OpenSQLiteDBWithoutMigrations(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatalf("OpenSQLiteDBWithoutMigrations: %v", err)
	}
	defer conn.Close()

	provider, err := NewMigrationProvider(conn)
	if err != nil {
		t.Fatalf("NewMigrationProvider: %v", err)
	}
	if _, err := provider.UpTo(ctx, 1); err != nil {
		t.Fatalf("migrate to version 1: %v", err)
	}

	insertEvent := func(sequence int, id string, streamID string, streamVersion int, eventType string, payload string) {
		t.Helper()
		_, err := conn.Exec(`
			INSERT INTO event_log (
				topic, sequence, id, stream_id, stream_version, event_type,
				schema_version, occurred_at, payload
			) VALUES ('sessions', ?, ?, ?, ?, ?, 1, CURRENT_TIMESTAMP, ?)
		`, sequence, id, streamID, streamVersion, eventType, []byte(payload))
		if err != nil {
			t.Fatalf("insert event %s: %v", id, err)
		}
	}

	insertEvent(1, "queued-old", "old", 1, "session.queued", `{"repoPath":"/tmp/work/repo"}`)
	insertEvent(2, "enriched-old", "old", 2, "session.enrichment.succeeded", `{"branch":"feature/foo","worktreePath":"/tmp/worktrees/repo..feature/foo"}`)
	insertEvent(3, "queued-current", "current", 1, "session.queued", `{"repoPath":"/tmp/work/repo"}`)
	insertEvent(4, "enriched-current", "current", 2, "session.enrichment.succeeded", `{"branch":"existing","tmuxSessionName":"repo#existing","worktreePath":"/tmp/worktrees/repo..existing"}`)

	_, err = conn.Exec(`
		INSERT INTO event_log_checkpoints (topic, subscriber_id, last_sequence, updated_at)
		VALUES ('sessions', 'session_projection', 4, CURRENT_TIMESTAMP)
	`)
	if err != nil {
		t.Fatalf("seed projection checkpoint: %v", err)
	}

	if _, err := provider.UpTo(ctx, 2); err != nil {
		t.Fatalf("migrate to version 2: %v", err)
	}

	readTmuxSessionName := func(eventID string) string {
		t.Helper()
		var payload []byte
		if err := conn.QueryRow(`SELECT payload FROM event_log WHERE id = ?`, eventID).Scan(&payload); err != nil {
			t.Fatalf("read event %s: %v", eventID, err)
		}
		var value struct {
			TmuxSessionName string `json:"tmuxSessionName"`
		}
		if err := json.Unmarshal(payload, &value); err != nil {
			t.Fatalf("decode event %s: %v", eventID, err)
		}
		return value.TmuxSessionName
	}

	if got := readTmuxSessionName("enriched-old"); got != "repo#feature/foo" {
		t.Fatalf("migrated tmux session name = %q, want repo#feature/foo", got)
	}
	if got := readTmuxSessionName("enriched-current"); got != "repo#existing" {
		t.Fatalf("existing tmux session name = %q, want repo#existing", got)
	}

	var checkpoint int
	if err := conn.QueryRow(`
		SELECT last_sequence
		FROM event_log_checkpoints
		WHERE topic = 'sessions' AND subscriber_id = 'session_projection'
	`).Scan(&checkpoint); err != nil {
		t.Fatalf("read projection checkpoint: %v", err)
	}
	if checkpoint != 4 {
		t.Fatalf("projection checkpoint = %d, want 4", checkpoint)
	}
}
