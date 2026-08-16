package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenSQLiteDBMigratesLegacyDronerNewDB(t *testing.T) {
	dataDir := t.TempDir()
	legacyPath := filepath.Join(dataDir, "db", legacyDBFileName)
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("MkdirAll legacy db dir: %v", err)
	}

	legacyConn, err := sql.Open("sqlite", legacyPath)
	if err != nil {
		t.Fatalf("sql.Open legacy db: %v", err)
	}
	t.Cleanup(func() {
		_ = legacyConn.Close()
	})

	if _, err := legacyConn.Exec(`CREATE TABLE marker (value TEXT NOT NULL); INSERT INTO marker(value) VALUES ('migrated');`); err != nil {
		t.Fatalf("seed legacy db: %v", err)
	}
	if err := legacyConn.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	conn, err := OpenSQLiteDB(DBPath(dataDir))
	if err != nil {
		t.Fatalf("OpenSQLiteDB: %v", err)
	}
	defer conn.Close()

	var value string
	if err := conn.QueryRow(`SELECT value FROM marker`).Scan(&value); err != nil {
		t.Fatalf("read migrated marker: %v", err)
	}
	if value != "migrated" {
		t.Fatalf("marker value = %q, want migrated", value)
	}

	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy path stat err = %v, want not-exist", err)
	}
	if _, err := os.Stat(DBPath(dataDir)); err != nil {
		t.Fatalf("stat migrated db path: %v", err)
	}

	var sessionProjectionCount int
	if err := conn.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'session_projection'`).Scan(&sessionProjectionCount); err != nil {
		t.Fatalf("count session_projection table: %v", err)
	}
	if sessionProjectionCount != 1 {
		t.Fatalf("session_projection table count = %d, want 1", sessionProjectionCount)
	}
}

func TestSessionProjectionTmuxSessionNameMigrationBackfillsInPlace(t *testing.T) {
	ctx := context.Background()
	conn, err := OpenSQLiteDBWithoutMigrations(filepath.Join(t.TempDir(), "droner.db"))
	if err != nil {
		t.Fatalf("OpenSQLiteDBWithoutMigrations: %v", err)
	}
	defer conn.Close()

	provider, err := NewMigrationProvider(conn)
	if err != nil {
		t.Fatalf("NewMigrationProvider: %v", err)
	}
	if _, err := provider.UpTo(ctx, 4); err != nil {
		t.Fatalf("migrate to version 4: %v", err)
	}

	_, err = conn.Exec(`
		INSERT INTO session_projection (
			stream_id, harness, branch, backend_id, repo_path, worktree_path,
			lifecycle_state, public_state, created_at, updated_at
		) VALUES
			('active', 'opencode', 'feature/foo', 'local', '/tmp/work/repo', '/tmp/worktrees/repo..feature/foo', 'session.ready', 'active.idle', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
			('queued', 'opencode', NULL, 'local', '/tmp/work/repo', NULL, 'session.queued', 'queued', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`)
	if err != nil {
		t.Fatalf("seed session projections: %v", err)
	}

	if _, err := provider.UpTo(ctx, 5); err != nil {
		t.Fatalf("migrate to version 5: %v", err)
	}

	var tmuxSessionName sql.NullString
	if err := conn.QueryRow(`SELECT tmux_session_name FROM session_projection WHERE stream_id = 'active'`).Scan(&tmuxSessionName); err != nil {
		t.Fatalf("read active tmux session name: %v", err)
	}
	if !tmuxSessionName.Valid || tmuxSessionName.String != "repo#feature/foo" {
		t.Fatalf("active tmux session name = %#v, want repo#feature/foo", tmuxSessionName)
	}

	if err := conn.QueryRow(`SELECT tmux_session_name FROM session_projection WHERE stream_id = 'queued'`).Scan(&tmuxSessionName); err != nil {
		t.Fatalf("read queued tmux session name: %v", err)
	}
	if tmuxSessionName.Valid {
		t.Fatalf("queued tmux session name = %#v, want NULL", tmuxSessionName)
	}

	var count int
	if err := conn.QueryRow(`SELECT count(*) FROM session_projection`).Scan(&count); err != nil {
		t.Fatalf("count session projections: %v", err)
	}
	if count != 2 {
		t.Fatalf("session projection count = %d, want 2", count)
	}
}
