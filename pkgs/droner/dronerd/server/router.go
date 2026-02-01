package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(s.MiddlewareLogger)
	r.Group(func(r chi.Router) {
		r.Get("/version", s.HandlerVersion)
		r.Post("/shutdown", s.HandlerShutdown)
	})
	r.Group(func(r chi.Router) {
		r.Get("/oauth/github/start", s.HandlerGitHubOAuthStart)
		r.Get("/oauth/github/status", s.HandlerGitHubOAuthStatus)
	})

	r.Group(func(r chi.Router) {
		r.Post("/sessions", s.HandlerCreateSession)
		r.Delete("/sessions", s.HandlerDeleteSession)
	})
	return r
}
