package core

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"

	_ "modernc.org/sqlite"
)

//go:embed db/schemas/*.sql
var schemaFS embed.FS

func InitDB(config *conf.Config) (*db.Queries, error) {
	dbPath := filepath.Join(config.Server.DataDir, "db", "droner.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}

	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	if _, err := conn.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		return nil, err
	}
	if _, err := conn.Exec("PRAGMA synchronous = NORMAL;"); err != nil {
		return nil, err
	}

	schemas, err := loadSchemas()
	if err != nil {
		return nil, err
	}
	for _, schema := range schemas {
		if _, err := conn.Exec(schema); err != nil {
			return nil, err
		}
	}

	if err := ensureSessionsRemoteURLColumn(conn); err != nil {
		return nil, err
	}

	return db.New(conn), nil
}

func ensureSessionsRemoteURLColumn(conn *sql.DB) error {
	rows, err := conn.Query("PRAGMA table_info(sessions);")
	if err != nil {
		return fmt.Errorf("failed to query sessions table info: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("failed to scan sessions table info: %w", err)
		}
		if name == "remote_url" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to iterate sessions table info: %w", err)
	}

	if _, err := conn.Exec("ALTER TABLE sessions ADD COLUMN remote_url TEXT;"); err != nil {
		return fmt.Errorf("failed to add sessions.remote_url column: %w", err)
	}
	return nil
}

func loadSchemas() ([]string, error) {
	paths, err := fs.Glob(schemaFS, "db/schemas/*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fs.ErrNotExist
	}
	var schemas []string
	for _, schemaPath := range paths {
		contents, err := schemaFS.ReadFile(schemaPath)
		if err != nil {
			return nil, err
		}
		schema := strings.TrimSpace(string(contents))
		if schema == "" {
			continue
		}
		schemas = append(schemas, schema)
	}
	return schemas, nil
}
