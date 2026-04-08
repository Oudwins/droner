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
		r.Get("/_session/next", HandlerWithLogger(s.HandlerSessionNext))
		r.Get("/_session/prev", HandlerWithLogger(s.HandlerSessionPrev))
		r.Get("/sessions", HandlerWithLogger(s.HandlerListSessions))
		r.Post("/sessions", HandlerWithLogger(s.HandlerCreateSession))
		r.Delete("/sessions", HandlerWithLogger(s.HandlerDeleteSession))
		r.Post("/sessions/complete", HandlerWithLogger(s.HandlerCompleteSession))
		r.Post("/sessions/reset", HandlerWithLogger(s.HandlerResetSession))
		r.Post("/sessions/nuke", HandlerWithLogger(s.HandlerNukeSessions))
	})

	return r
}
