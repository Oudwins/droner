package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Get("/version", HandlerWithLogger(s.HandlerVersion))
		r.Post("/shutdown", HandlerWithLogger(s.HandlerShutdown))
	})
	r.Group(func(r chi.Router) {
		r.Get("/oauth/github/start", HandlerWithLogger(s.HandlerGitHubOAuthStart))
		r.Get("/oauth/github/status", HandlerWithLogger(s.HandlerGitHubOAuthStatus))
	})

	r.Group(func(r chi.Router) {
		r.Post("/sessions", HandlerWithLogger(s.HandlerCreateSession))
		r.Delete("/sessions", HandlerWithLogger(s.HandlerDeleteSession))
	})
	return r
}
