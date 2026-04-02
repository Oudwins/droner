package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const DBFileName = "droner.sessionslog.db"

func DBPath(dataDir string) string {
	return filepath.Join(filepath.Clean(dataDir), "db", DBFileName)
}

func OpenSQLiteDB(dbPath string) (*sql.DB, error) {
	conn, err := OpenSQLiteDBWithoutMigrations(dbPath)
	if err != nil {
		return nil, err
	}
	if err := EnsureMigrations(context.Background(), conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func OpenSQLiteDBWithoutMigrations(dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}

	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()
	if err := conn.Ping(); err != nil {
		return nil, err
	}
	if err := ConfigureSQLiteDB(conn); err != nil {
		return nil, err
	}
	return conn, nil
}

func ConfigureSQLiteDB(conn *sql.DB) error {
	// This backend is write-heavy and often has multiple goroutines sharing one
	// sqlite handle (process manager, projections, checkpoints). Serializing the
	// pool and waiting briefly on file locks avoids noisy SQLITE_BUSY failures.
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)

	if _, err := conn.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		return fmt.Errorf("set sqlite journal mode: %w", err)
	}
	if _, err := conn.Exec("PRAGMA busy_timeout = 5000;"); err != nil {
		return fmt.Errorf("set sqlite busy timeout: %w", err)
	}
	if _, err := conn.Exec("PRAGMA synchronous = NORMAL;"); err != nil {
		return fmt.Errorf("set sqlite synchronous mode: %w", err)
	}
	return nil
}
