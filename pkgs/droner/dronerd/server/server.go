package server

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core"
	"github.com/Oudwins/droner/pkgs/droner/internals/assert"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
	"github.com/Oudwins/droner/pkgs/droner/internals/timeouts"
	"github.com/Oudwins/droner/pkgs/droner/sdk"
)

type Server struct {
	Base         *core.BaseServer
	oauth        *oauthStateStore
	httpServer   *http.Server
	canceler     context.CancelFunc
	consumer     *tasky.Consumer[core.Jobs]
	shutdownOnce sync.Once
}

func New() *Server {
	base := core.New()

	storePath := filepath.Join(base.Config.Server.DataDir, "tasks", "tasks.db")
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		assert.AssertNil(err, "[SERVER] Failed to create data directory")
	}

	return &Server{
		Base:     base,
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
	return runErr
}

func (s *Server) Shutdown() {
	s.shutdownOnce.Do(func() {
		s.canceler()
		ctx, cancel := context.WithTimeout(context.Background(), timeouts.SecondLong)
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
	})
}
