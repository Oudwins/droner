package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/hooks"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/pullrequests/pullrequestevents"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/sessions/sessionevents"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/assert"
	"github.com/Oudwins/droner/pkgs/droner/internals/timeouts"
)

type Server struct {
	Base         *core.BaseServer
	httpServer   *http.Server
	canceler     context.CancelFunc
	events       *sessionevents.System
	prs          *pullrequestevents.System
	hooks        *hooks.System
	shutdownOnce sync.Once
}

func New() *Server {
	base := core.New()
	sessionsLog, err := base.EventLogs.Sessions()
	assert.AssertNil(err, "[SERVER] Failed to initialize sessions event log")
	pullRequestsLog, err := base.EventLogs.PullRequests()
	assert.AssertNil(err, "[SERVER] Failed to initialize pull requests event log")

	return &Server{
		Base:     base,
		canceler: func() {},
		events:   sessionevents.New(sessionsLog, base.Sessions, base.EventLogs.SessionResetter(), base.Logger, base.Config, base.BackendStore),
		prs:      pullrequestevents.New(pullRequestsLog, sessionsLog, base.PRSessions, base.PRSnapshots, base.Logger),
		hooks:    hooks.New(sessionsLog, pullRequestsLog, base.Hooks, base.Logger),
	}
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
	s.events.Start(ctx)
	s.prs.Start(ctx)
	s.hooks.Start(ctx)

	errCh := make(chan error, 1)
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

		if s.httpServer != nil {
			if err := s.httpServer.Shutdown(ctx); err != nil {
				if !errors.Is(err, http.ErrServerClosed) {
					s.Base.Logger.Error("[shutdown] server shutdown failed", "error", err)
				}
			}
		}

		if err := s.events.Close(); err != nil {
			s.Base.Logger.Error("[shutdown] event system shutdown failed", "error", err)
		}
		if err := s.prs.Close(); err != nil {
			s.Base.Logger.Error("[shutdown] pull request event system shutdown failed", "error", err)
		}
		if err := s.hooks.Close(); err != nil {
			s.Base.Logger.Error("[shutdown] hooks event system shutdown failed", "error", err)
		}
		if s.Base != nil {
			s.Base.Close()
		}
	})
}
