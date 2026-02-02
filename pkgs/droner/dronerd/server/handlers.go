package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/core/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	sessionids "github.com/Oudwins/droner/pkgs/droner/internals/sessionIds"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
	"github.com/Oudwins/zog/zhttp"
	"github.com/google/uuid"

	z "github.com/Oudwins/zog"
)

func (s *Server) HandlerVersion(_ *slog.Logger, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(s.Base.Config.Version))
}

func (s *Server) HandlerShutdown(_ *slog.Logger, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	s.Shutdown()
	_, _ = w.Write([]byte("Shutdown"))
}

func (s *Server) HandlerCreateSession(logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	logger.Info("Creating session")
	var payload schemas.SessionCreateRequest

	errs := schemas.SessionCreateSchema.Parse(zhttp.Request(r), &payload, z.WithCtxValue("workspace", s.Base.Workspace))
	if errs != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "Schema validation failed", z.Issues.Flatten(errs)), Render.Status(http.StatusBadRequest))
		return
	}

	// TODO: This needs to be moved out of here
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
				worktreePath := filepath.Join(worktreeRoot, baseName+".."+id) // TODO: this conversion is done in multiple places. Brittle
				_, err := s.Base.Workspace.Stat(worktreePath)
				if err == nil {
					return errors.New("Session folder already exists")
				}

				return nil
			},
		})
		if err != nil {
			logger.Error("Failed to generate session id", slog.String("error", err.Error()))
			RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to generate session id", nil), Render.Status(http.StatusInternalServerError))
			return
		}
		if generatedID == "" {
			logger.Error("Generated empty session id")
			RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Generated ID that was empty", nil), Render.Status(http.StatusInternalServerError))
			return
		}
		payload.SessionID = generatedID
	}

	worktreeName := baseName + "..." + payload.SessionID // TODO: again duplicated logic here
	worktreePath := filepath.Join(worktreeRoot, worktreeName)
	if _, err := s.Base.Workspace.Stat(worktreePath); err != nil {
		logger.Error("Stat at worktree path failed")
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}

	sessionID, err := uuid.NewV7()
	if err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to generate session id", nil), Render.Status(http.StatusInternalServerError))
		return
	}

	payloadValue := sql.NullString{}
	if payload.Agent != nil {
		payloadBytes, err := json.Marshal(payload.Agent)
		if err != nil {
			logger.Error("Failed to serialize agent payload", slog.String("error", err.Error()))
			RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
			return
		}
		payloadValue = sql.NullString{String: string(payloadBytes), Valid: true}
	}

	_, err = s.Base.DB.CreateSession(context.Background(), db.CreateSessionParams{
		ID:           sessionID.String(),
		SimpleID:     payload.SessionID,
		Status:       db.SessionStatusQueued,
		RepoPath:     payload.Path,
		WorktreePath: worktreePath,
		Payload:      payloadValue,
		Error:        sql.NullString{},
	})
	if err != nil {
		logger.Error("Failed to create session record", slog.String("error", err.Error()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}

	// Enqueue task
	bytes, _ := json.Marshal(payload)
	taskId, err := s.Base.TaskQueue.Enqueue(context.Background(), tasky.NewTask(core.JobCreateSession, bytes))
	if err != nil {
		logger.Error("Failed to enque task", slog.String("error", err.Error()))
		_, updateErr := s.Base.DB.UpdateSessionStatusBySimpleID(context.Background(), db.UpdateSessionStatusBySimpleIDParams{
			SimpleID: payload.SessionID,
			Status:   db.SessionStatusFailed,
			Error:    sql.NullString{String: err.Error(), Valid: true},
		})
		if updateErr != nil {
			logger.Error("Failed to update session status", slog.String("error", updateErr.Error()))
		}
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

func (s *Server) HandlerDeleteSession(logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	var payload schemas.SessionDeleteRequest
	errs := schemas.SessionDeleteSchema.Parse(zhttp.Request(r), &payload)
	if errs != nil {
		payload := JsonResponseError(JsonResponseErrorCodeValidationFailed, "Schema validation failed", z.Issues.Flatten(errs))
		RenderJSON(w, r, payload, Render.Status(http.StatusBadRequest))
		return
	}

	bytes, _ := json.Marshal(payload)
	taskId, err := s.Base.TaskQueue.Enqueue(context.Background(), tasky.NewTask(core.JobDeleteSession, bytes))
	if err != nil {
		logger.Error("Failed to enque job", slog.String("error", err.Error()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to enque job"+err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}

	RenderJSON(w, r, schemas.SessionDeleteResponse{
		SessionID: payload.SessionID,
		TaskId:    taskId,
	}, Render.Status(http.StatusAccepted))
}
