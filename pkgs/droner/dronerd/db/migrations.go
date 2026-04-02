package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func NewMigrationProvider(conn *sql.DB) (*goose.Provider, error) {
	files, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("open db migrations: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, conn, files)
	if err != nil {
		return nil, fmt.Errorf("create db migration provider: %w", err)
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
		return fmt.Errorf("get current db migration version: %w", err)
	}
	target := sources[len(sources)-1].Version
	if current == target {
		return nil
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("run db migrations: %w", err)
	}
	return nil
}
