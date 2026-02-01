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

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core"
	"github.com/Oudwins/droner/pkgs/droner/internals/assert"
	"github.com/Oudwins/droner/pkgs/droner/internals/logbuf"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
	"github.com/Oudwins/droner/pkgs/droner/sdk"
)

type Server struct {
	Base       *core.BaseServer
	Logbuf     *logbuf.Logger
	oauth      *oauthStateStore
	httpServer *http.Server
	canceler   context.CancelFunc
	consumer   *tasky.Consumer[core.Jobs]
}

func New() *Server {
	base := core.New()
	buffer := logbuf.New(
		slog.String("version", base.Config.Version),
		slog.Int("port", base.Env.PORT),
	)

	storePath := filepath.Join(base.Config.Server.DataDir, "tasks", "tasks.db")
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		assert.AssertNil(err, "[SERVER] Failed to create data directory")
	}

	return &Server{
		Base:     base,
		Logbuf:   buffer,
		oauth:    newOAuthStateStore(),
		canceler: func() {},
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

	ctx, cancel := context.WithCancel(context.Background())
	s.canceler = cancel
	consumer := tasky.NewConsumer(s.Base.TaskQueue, tasky.ConsumerOptions{Workers: 1})
	s.consumer = consumer
	consumer.Start(ctx)

	errCh := make(chan error, 2)
	go func() {
		errCh <- <-consumer.Err()
	}()
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	runErr := <-errCh
	s.Shutdown()
	secondErr := <-errCh
	if runErr == nil {
		runErr = secondErr
	}
	return runErr
}

func (s *Server) Shutdown() {
	s.canceler()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if s.consumer != nil {
		if err := s.consumer.Shutdown(ctx); err != nil {
			s.Base.Logger.Error("[shutdown] Consumer shutdown failed", "error", err)
		}
	}

	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				s.Base.Logger.Error("[shutdown] server shutdown failed", "error", err)
			}
		}
	}

	if s.Base != nil {
		s.Base.Close()
	}

}
