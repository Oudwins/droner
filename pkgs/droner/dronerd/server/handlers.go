package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/tasks"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	sessionids "github.com/Oudwins/droner/pkgs/droner/internals/sessionIds"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
	"github.com/Oudwins/zog/zhttp"

	z "github.com/Oudwins/zog"
)

func (s *Server) HandlerVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(s.Base.Config.Version))
}

func (s *Server) HandlerShutdown(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	s.Shutdown()
	_, _ = w.Write([]byte("Shutdown"))
}

func (s *Server) HandlerCreateSession(w http.ResponseWriter, r *http.Request) {
	var payload schemas.SessionCreateRequest

	errs := schemas.SessionCreateSchema.Parse(zhttp.Request(r), &payload, z.WithCtxValue("workspace", s.Base.Workspace))
	if errs != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "Schema validation failed", z.Issues.Flatten(errs)), Render.Status(http.StatusBadRequest))
		return
	}

	worktreeRoot := s.Base.Config.Worktrees.Dir
	if err := s.Base.Workspace.MkdirAll(worktreeRoot, 0o755); err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to create worktree root", nil), Render.Status(http.StatusInternalServerError))
		return
	}

	// LOGIC
	baseName := filepath.Base(payload.Path)
	if payload.SessionID == "" {
		generatedID, err := sessionids.New(baseName, &sessionids.GeneratorConfig{
			MaxAttempts: 100,
			IsValid: func(id string) error {
				worktreePath := filepath.Join(worktreeRoot, baseName+"#"+id)
				_, err := s.Base.Workspace.Stat(worktreePath)
				return err
			},
		})
		if err != nil || generatedID == "" {
			RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to generate session id", nil), Render.Status(http.StatusInternalServerError))
			return
		}
		payload.SessionID = generatedID
	}

	worktreeName := baseName + "#" + payload.SessionID
	worktreePath := filepath.Join(worktreeRoot, worktreeName)
	if _, err := s.Base.Workspace.Stat(worktreePath); err != nil {
		s.Logbuf.Error("Stat at worktree path failed")
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}

	// Enqueue task
	bytes, _ := json.Marshal(payload)
	taskId, err := s.tasky.Enqueue(context.Background(), tasky.NewTask(tasks.JobCreateSession, bytes))
	if err != nil {
		s.Logbuf.Error("Failed to enque task", slog.String("error", err.Error()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
	}

	// Response
	res := schemas.SessionCreateResponse{
		WorktreePath: worktreePath,
		SessionID:    payload.SessionID,
		TaskID:       taskId,
	}
	RenderJSON(w, r, res, Render.Status(http.StatusAccepted))
}

func (s *Server) HandlerDeleteSession(w http.ResponseWriter, r *http.Request) {
	var payload schemas.SessionDeleteRequest
	errs := schemas.SessionDeleteSchema.Parse(zhttp.Request(r), &payload)
	if errs != nil {
		payload := JsonResponseError(JsonResponseErrorCodeValidationFailed, "Schema validation failed", z.Issues.Flatten(errs))
		RenderJSON(w, r, payload, Render.Status(http.StatusBadRequest))
		return
	}

	bytes, _ := json.Marshal(payload)
	taskId, err := s.tasky.Enqueue(context.Background(), tasky.NewTask(tasks.JobDeleteSession, bytes))
	if err != nil {
		s.Base.Logger.Error("Failed to enque job", slog.String("error", err.Error()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to enque job"+err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}

	RenderJSON(w, r, schemas.SessionDeleteResponse{
		SessionID: payload.SessionID,
		TaskId:    taskId,
	}, Render.Status(http.StatusAccepted))
}
