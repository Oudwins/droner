package pullrequestslog

import (
	"fmt"

	sqlite3eventlog "github.com/Oudwins/droner/pkgs/droner/dronerd/events/backend/sqlite3"
	backenddb "github.com/Oudwins/droner/pkgs/droner/dronerd/events/backend/sqlite3/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

const Topic = eventlog.Topic("pullrequests")

func DBPath(dataDir string) string {
	return backenddb.DBPath(dataDir)
}

func OpenBackend(dataDir string) (*sqlite3eventlog.Backend, error) {
	backend, err := sqlite3eventlog.New(sqlite3eventlog.Config{Path: DBPath(dataDir)})
	if err != nil {
		return nil, fmt.Errorf("open pullrequestslog sqlite backend: %w", err)
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
		return nil, fmt.Errorf("create pullrequestslog event log: %w", err)
	}
	return log, nil
}
