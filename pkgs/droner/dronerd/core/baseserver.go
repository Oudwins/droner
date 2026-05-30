package core

import (
	"database/sql"
	"log/slog"
	"os"

	coredb "github.com/Oudwins/droner/pkgs/droner/dronerd/db"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/eventlogs"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/pullrequests/pullrequestevents"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/sessions/sessionevents"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/backends"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/env"
)

type BaseServer struct {
	Config       *conf.Config
	Env          *env.EnvStruct
	Logger       *slog.Logger
	LogFile      *os.File
	DB           *sql.DB
	Queries      *coredb.Queries
	EventLogs    *eventlogs.Registry
	Sessions     *sessionevents.SQLiteProjectionStore
	PRSessions   *pullrequestevents.SQLiteSessionLookupStore
	PRSnapshots  *pullrequestevents.SQLitePullRequestSnapshotStore
	BackendStore *backends.Store
}

func New() *BaseServer {
	env := env.Get()
	config := conf.GetConfig()

	logger, logFile := InitLogger(env)
	db, err := coredb.OpenSQLiteDB(coredb.DBPath(env.DATA_DIR))
	if err != nil {
		panic(err)
	}
	eventLogs, err := eventlogs.Open(env.DATA_DIR)
	if err != nil {
		_ = db.Close()
		panic(err)
	}
	queries := coredb.New(db)
	base := &BaseServer{
		Config:      config,
		Env:         env,
		Logger:      logger,
		LogFile:     logFile,
		DB:          db,
		Queries:     queries,
		EventLogs:   eventLogs,
		Sessions:    sessionevents.NewSQLiteProjectionStore(queries),
		PRSessions:  pullrequestevents.NewSQLiteSessionLookupStore(queries),
		PRSnapshots: pullrequestevents.NewSQLitePullRequestSnapshotStore(queries),
	}

	base.BackendStore = backends.NewStore(config)

	return base
}

func (b *BaseServer) Close() {
	if b.EventLogs != nil {
		_ = b.EventLogs.Close()
	}
	if b.DB != nil {
		_ = b.DB.Close()
	}
	if b.LogFile != nil {
		_ = b.LogFile.Close()
	}
}
