package core

import (
	"database/sql"
	"os"
	"path"
	"path/filepath"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"

	_ "modernc.org/sqlite"
)

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

	schemaPath := filepath.Join("dronerd", "core", "db", "schemas", "sessions.sql")
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, err
	}

	if _, err := conn.Exec(string(schema)); err != nil {
		return nil, err
	}

	return db.New(conn), nil
}
