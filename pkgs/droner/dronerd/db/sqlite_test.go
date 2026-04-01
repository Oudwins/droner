package db

import (
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
