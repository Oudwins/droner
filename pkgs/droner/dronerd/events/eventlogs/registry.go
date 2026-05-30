package eventlogs

import (
	"context"
	"fmt"

	sqlite3eventlog "github.com/Oudwins/droner/pkgs/droner/dronerd/events/backend/sqlite3"
	backenddb "github.com/Oudwins/droner/pkgs/droner/dronerd/events/backend/sqlite3/db"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/pullrequests/pullrequestslog"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/sessions/sessionslog"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

type Registry struct {
	backend *sqlite3eventlog.Backend
}

type SessionResetter struct {
	backend *sqlite3eventlog.Backend
}

func Open(dataDir string) (*Registry, error) {
	backend, err := sqlite3eventlog.New(sqlite3eventlog.Config{Path: backenddb.DBPath(dataDir)})
	if err != nil {
		return nil, fmt.Errorf("open eventlog sqlite backend: %w", err)
	}
	return &Registry{backend: backend}, nil
}

func (r *Registry) Sessions() (eventlog.EventLog, error) {
	return r.log(sessionslog.Topic)
}

func (r *Registry) PullRequests() (eventlog.EventLog, error) {
	return r.log(pullrequestslog.Topic)
}

func (r *Registry) SessionResetter() *SessionResetter {
	return &SessionResetter{backend: r.backend}
}

func (r *Registry) Close() error {
	if r == nil || r.backend == nil {
		return nil
	}
	return r.backend.Close()
}

func (r *Registry) log(topic eventlog.Topic) (eventlog.EventLog, error) {
	if r == nil || r.backend == nil {
		return nil, fmt.Errorf("eventlog registry is not open")
	}
	log, err := eventlog.New(eventlog.Config{Topic: topic}, r.backend)
	if err != nil {
		return nil, fmt.Errorf("create %s event log: %w", topic, err)
	}
	return log, nil
}

func (r *SessionResetter) ResetStreamToEvent(ctx context.Context, streamID eventlog.StreamID, eventID eventlog.EventID) (eventlog.Envelope, error) {
	if r == nil || r.backend == nil {
		return eventlog.Envelope{}, fmt.Errorf("session resetter unavailable")
	}
	return r.backend.ResetStreamToEvent(ctx, sessionslog.Topic, streamID, eventID)
}
