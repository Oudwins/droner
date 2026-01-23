package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(s.MiddlewareLogger)
	r.Get("/version", s.HandlerVersion)
	r.Get("/sum", s.HandlerSum)
	r.Post("/sessions", s.HandlerCreateSession)
	r.Delete("/sessions", s.HandlerDeleteSession)
	return r
}
