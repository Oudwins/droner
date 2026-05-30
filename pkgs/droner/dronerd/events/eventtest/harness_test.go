package eventtest

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"

	coredb "github.com/Oudwins/droner/pkgs/droner/dronerd/db"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/eventlogs"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/pullrequests/pullrequestevents"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/sessions/sessionevents"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/backends"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

type Harness struct {
	DataDir string
	DB      *sql.DB
	Queries *coredb.Queries
	Logs    *eventlogs.Registry

	SessionsLog     eventlog.EventLog
	PullRequestsLog eventlog.EventLog

	SessionStore  *sessionevents.SQLiteProjectionStore
	PRSessions    *pullrequestevents.SQLiteSessionLookupStore
	PRSnapshots   *pullrequestevents.SQLitePullRequestSnapshotStore
	SessionEvents *sessionevents.System
	PullRequests  *pullrequestevents.System
}

type Options struct {
	Config       *conf.Config
	BackendStore *backends.Store
	Logger       *slog.Logger
	Start        bool
}

func NewHarness(t testing.TB, opts Options) *Harness {
	t.Helper()
	dataDir := t.TempDir()
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	config := opts.Config
	if config == nil {
		config = &conf.Config{}
	}
	backendStore := opts.BackendStore
	if backendStore == nil {
		backendStore = backends.NewStore(config)
	}
	db, err := coredb.OpenSQLiteDB(coredb.DBPath(dataDir))
	if err != nil {
		t.Fatalf("OpenSQLiteDB: %v", err)
	}
	queries := coredb.New(db)
	logs, err := eventlogs.Open(dataDir)
	if err != nil {
		_ = db.Close()
		t.Fatalf("eventlogs.Open: %v", err)
	}
	sessionsLog, err := logs.Sessions()
	if err != nil {
		_ = logs.Close()
		_ = db.Close()
		t.Fatalf("Sessions log: %v", err)
	}
	pullRequestsLog, err := logs.PullRequests()
	if err != nil {
		_ = logs.Close()
		_ = db.Close()
		t.Fatalf("PullRequests log: %v", err)
	}
	sessionStore := sessionevents.NewSQLiteProjectionStore(queries)
	prSessions := pullrequestevents.NewSQLiteSessionLookupStore(queries)
	prSnapshots := pullrequestevents.NewSQLitePullRequestSnapshotStore(queries)
	h := &Harness{
		DataDir:         dataDir,
		DB:              db,
		Queries:         queries,
		Logs:            logs,
		SessionsLog:     sessionsLog,
		PullRequestsLog: pullRequestsLog,
		SessionStore:    sessionStore,
		PRSessions:      prSessions,
		PRSnapshots:     prSnapshots,
	}
	h.SessionEvents = sessionevents.New(sessionsLog, sessionStore, logs.SessionResetter(), logger, config, backendStore)
	h.PullRequests = pullrequestevents.New(pullRequestsLog, sessionsLog, prSessions, prSnapshots, logger)
	var cancel context.CancelFunc
	if opts.Start {
		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		h.SessionEvents.Start(ctx)
		h.PullRequests.Start(ctx)
	}
	t.Cleanup(func() {
		if cancel != nil {
			cancel()
		}
		_ = h.SessionEvents.Close()
		_ = h.PullRequests.Close()
		_ = logs.Close()
		_ = db.Close()
	})
	return h
}
