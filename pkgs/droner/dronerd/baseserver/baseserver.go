package baseserver

import (
	"log/slog"
	"os"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/env"
)

type BaseServer struct {
	Config *conf.Config
	Env    *env.EnvStruct
	Logger *slog.Logger
}

func New() *BaseServer {
	env := env.Get()
	config := conf.GetConfig()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	return &BaseServer{
		Config: config,
		Env:    env,
		Logger: logger,
	}
}
