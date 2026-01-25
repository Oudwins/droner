package server

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) HandlerTaskStatus(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	if taskID == "" {
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "task id is required", nil), Render.Status(http.StatusBadRequest))
		return
	}

	response, err := s.tasks.Get(taskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeNotFound, "task not found", nil), Render.Status(http.StatusNotFound))
			return
		}
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to read task status", nil), Render.Status(http.StatusInternalServerError))
		return
	}

	RenderJSON(w, r, response)
}
