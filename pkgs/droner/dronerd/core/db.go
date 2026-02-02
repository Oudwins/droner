package core

import (
	"database/sql"
	"embed"
	"io/fs"
	"os"
	"path"
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
	dbPath := filepath.Join(config.Server.DataDir, "db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}

	conn, err := sql.Open("sqlite", path.Join(dbPath, "droner.db"))
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

	return db.New(conn), nil
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
