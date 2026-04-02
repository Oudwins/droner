package sqliteeventlog

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pressly/goose/v3"

	_ "modernc.org/sqlite"
)

const (
	defaultEventTable      = "event_log"
	defaultCheckpointTable = "event_log_checkpoints"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func OpenDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = db.Close()
		}
	}()
	if err := db.Ping(); err != nil {
		return nil, err
	}
	if err := configureDB(db); err != nil {
		return nil, err
	}
	return db, nil
}

func NewMigrationProvider(conn *sql.DB) (*goose.Provider, error) {
	files, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("open sqlite eventlog migrations: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, conn, files)
	if err != nil {
		return nil, fmt.Errorf("create sqlite eventlog migration provider: %w", err)
	}
	return provider, nil
}

func ensureMigrations(ctx context.Context, conn *sql.DB) error {
	provider, err := NewMigrationProvider(conn)
	if err != nil {
		return err
	}
	sources := provider.ListSources()
	if len(sources) == 0 {
		return nil
	}
	current, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("get current sqlite eventlog migration version: %w", err)
	}
	target := sources[len(sources)-1].Version
	if current == target {
		return nil
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("run sqlite eventlog migrations: %w", err)
	}
	return nil
}

func configureDB(db *sql.DB) error {
	// This backend is write-heavy and often has multiple goroutines sharing one
	// sqlite handle (process manager, projections, checkpoints). Serializing the
	// pool and waiting briefly on file locks avoids noisy SQLITE_BUSY failures.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if _, err := db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		return err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000;`); err != nil {
		return err
	}
	if _, err := db.Exec(`PRAGMA synchronous = NORMAL;`); err != nil {
		return err
	}
	return nil
}
