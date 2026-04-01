package core

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Oudwins/droner/pkgs/droner/internals/backends"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/env"
)

type BaseServer struct {
	Config       *conf.Config
	Env          *env.EnvStruct
	Logger       *slog.Logger
	LogFile      *os.File
	BackendStore *backends.Store
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
	base := &BaseServer{
		Config:  config,
		Env:     env,
		Logger:  logger,
		LogFile: logFile,
	}

	base.BackendStore = backends.NewStore(config)

	return base
}

func (b *BaseServer) Close() {
	if b.LogFile != nil {
		_ = b.LogFile.Close()
	}
}
