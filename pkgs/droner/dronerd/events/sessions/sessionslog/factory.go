package sessionslog

import (
	"fmt"

	sqlite3eventlog "github.com/Oudwins/droner/pkgs/droner/dronerd/events/backend/sqlite3"
	backenddb "github.com/Oudwins/droner/pkgs/droner/dronerd/events/backend/sqlite3/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

const DBFileName = "droner.sessionslog.db"

const Topic = eventlog.Topic("sessions")

func DBPath(dataDir string) string {
	return backenddb.DBPath(dataDir)
}

func OpenBackend(dataDir string) (*sqlite3eventlog.Backend, error) {
	backend, err := sqlite3eventlog.New(sqlite3eventlog.Config{Path: DBPath(dataDir)})
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
