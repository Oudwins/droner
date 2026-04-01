package sessionslog

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
	sqliteeventlog "github.com/Oudwins/droner/pkgs/droner/internals/eventlog/backends/sqlite"
)

const DBFileName = "droner.sessionslog.db"

const Topic = eventlog.Topic("sessions")

func DBPath(dataDir string) string {
	return filepath.Join(filepath.Clean(dataDir), "db", DBFileName)
}

func OpenBackend(dataDir string) (*sqliteeventlog.Backend, error) {
	path := DBPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create sessionslog db dir: %w", err)
	}
	backend, err := sqliteeventlog.New(sqliteeventlog.Config{Path: path})
	if err != nil {
		return nil, fmt.Errorf("open sessionslog sqlite backend: %w", err)
	}
	return backend, nil
}

func Open(dataDir string) (eventlog.EventLog, error) {
	backend, err := OpenBackend(dataDir)
	if err != nil {
		return nil, err
	}
	log, err := eventlog.New(eventlog.Config{Topic: Topic}, backend)
	if err != nil {
		_ = backend.Close()
		return nil, fmt.Errorf("create sessionslog event log: %w", err)
	}
	return log, nil
}
