package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed schemas/*.sql
var schemaFS embed.FS

const (
	DBFileName       = "droner.db"
	legacyDBFileName = "droner.new.db"
)

func DBPath(dataDir string) string {
	return filepath.Join(filepath.Clean(dataDir), "db", DBFileName)
}

func OpenSQLiteDB(dbPath string) (*sql.DB, error) {
	if filepath.Base(dbPath) == DBFileName {
		if err := migrateLegacyDBPath(dbPath); err != nil {
			return nil, err
		}
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}

	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)

	if _, err := conn.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		return nil, err
	}
	if _, err := conn.Exec("PRAGMA busy_timeout = 5000;"); err != nil {
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

	return conn, nil
}

func migrateLegacyDBPath(dbPath string) error {
	legacyPath := filepath.Join(filepath.Dir(dbPath), legacyDBFileName)
	if _, err := os.Stat(dbPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat db path: %w", err)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat legacy db path: %w", err)
	}

	for _, suffix := range []string{"", "-shm", "-wal"} {
		from := legacyPath + suffix
		to := dbPath + suffix
		if err := os.Rename(from, to); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("rename %s to %s: %w", from, to, err)
		}
	}

	return nil
}

func loadSchemas() ([]string, error) {
	paths, err := fs.Glob(schemaFS, "schemas/*.sql")
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
