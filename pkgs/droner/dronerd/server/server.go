package server

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/baseserver"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/tasks"
	"github.com/Oudwins/droner/pkgs/droner/internals/assert"
	"github.com/Oudwins/droner/pkgs/droner/internals/logbuf"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
	"github.com/Oudwins/droner/pkgs/droner/internals/workspace"
	"github.com/Oudwins/droner/pkgs/droner/sdk"
)

type Server struct {
	Base       *baseserver.BaseServer
	Logbuf     *logbuf.Logger
	subs       *subscriptionManager
	oauth      *oauthStateStore
	tasks      *taskManager
	httpServer *http.Server
	tasky      *tasky.Queue[tasks.Jobs]
	Workspace  workspace.Host
}

func New() *Server {
	base := baseserver.New()
	dataDir, err := expandPath(base.Config.Server.DataDir)
	assert.AssertNil(err, "[SERVER] Failed to expand data dir")
	if dataDir != "" {
		dataDir = filepath.Clean(dataDir)
		base.Config.Server.DataDir = dataDir
	}
	buffer := logbuf.New(
		slog.String("version", base.Config.Version),
		slog.Int("port", base.Env.PORT),
	)

	storePath := filepath.Join(base.Config.Server.DataDir, "tasks", "tasks.db")
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		assert.AssertNil(err, "[SERVER] Failed to create data directory")
	}
	store, err := newTaskStore(storePath)
	assert.AssertNil(err, "[SERVER] Failed to initialize task store")
	manager := newTaskManager(store, base.Logger)

	q, err := tasks.NewQueue(base)
	assert.AssertNil(err, "[SERVER] Failed to initialize queue")

	return &Server{
		Base:      base,
		Logbuf:    buffer,
		subs:      newSubscriptionManager(),
		oauth:     newOAuthStateStore(),
		tasks:     manager,
		tasky:     q,
		Workspace: workspace.NewLocalHost(),
	}
}

func (s *Server) SafeStart() error {
	if sdk.IsRunning(s.Base.Env.BASE_URL) {
		return nil
	}

	// TODO: Start the queue & the subscription manager
	go func() {
		s.Base.Logger.Info("starting server")
		err := s.Start()
		if err != nil {
			log.Fatal("[Droner] Failed to start server: " + err.Error())
		}
	}()

	if sdk.WaitForStart(s.Base.Env.BASE_URL, s.Base.Logger) {
		return nil
	}

	return errors.New("Couldn't start server")
}

func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.Base.Env.LISTEN_ADDR)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler: s.Router(),
	}
	s.httpServer = server
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown() {
	// TODO: Handle graceful shutdown of all componets

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if s.httpServer == nil {
			s.Base.Logger.Error("shutdown failed", "error", errors.New("server not initialized"))
			return
		}
		if err := s.httpServer.Shutdown(ctx); err != nil {
			s.Base.Logger.Error("shutdown failed", "error", err)
		}
	}()

}
