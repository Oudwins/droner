package core

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/assert"
	"github.com/Oudwins/droner/pkgs/droner/internals/backends"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/env"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
)

type BaseServer struct {
	Config        *conf.Config
	Env           *env.EnvStruct
	Logger        *slog.Logger
	LogFile       *os.File
	BackendStore  *backends.Store
	TaskQueue     *tasky.Queue[Jobs]
	Subscriptions *subscriptionManager
	DB            *db.Queries
}

func New() *BaseServer {
	env := env.Get()
	config := conf.GetConfig()
	dataDir := config.Server.DataDir
	if dataDir != "" {
		dataDir = filepath.Clean(dataDir)
		config.Server.DataDir = dataDir
	}

	logger, logFile := InitLogger(config)
	backendStore := backends.NewStore(config.Sessions)

	base := &BaseServer{
		Config:       config,
		Env:          env,
		Logger:       logger,
		LogFile:      logFile,
		BackendStore: backendStore,
	}

	queries, err := InitDB(config)
	assert.AssertNil(err, "[CORE] Failed to initialize DB")
	base.DB = queries

	queue, err := NewQueue(base)
	assert.AssertNil(err, "[CORE] Failed to initialize queue")
	base.TaskQueue = queue
	base.Subscriptions = newSubscriptionManager(base)

	return base
}

func (b *BaseServer) Close() {
	if b.LogFile != nil {
		_ = b.LogFile.Close()
	}
}
