package core

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Oudwins/droner/pkgs/droner/internals/assert"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/env"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
	"github.com/Oudwins/droner/pkgs/droner/internals/workspace"
)

type BaseServer struct {
	Config        *conf.Config
	Env           *env.EnvStruct
	Logger        *slog.Logger
	Workspace     workspace.Host
	TaskQueue     *tasky.Queue[Jobs]
	Subscriptions *subscriptionManager
}

func New() *BaseServer {
	env := env.Get()
	config := conf.GetConfig()
	dataDir := config.Server.DataDir
	if dataDir != "" {
		dataDir = filepath.Clean(dataDir)
		config.Server.DataDir = dataDir
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	w := workspace.NewLocalHost()

	base := &BaseServer{
		Config:    config,
		Env:       env,
		Logger:    logger,
		Workspace: w,
	}

	queue, err := NewQueue(base)
	assert.AssertNil(err, "[CORE] Failed to initialize queue")
	base.TaskQueue = queue
	base.Subscriptions = newSubscriptionManager(base)

	return base
}
