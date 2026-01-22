package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/version", s.HandlerVersion)
	r.Get("/sum", s.HandlerSum)
	return r
}
