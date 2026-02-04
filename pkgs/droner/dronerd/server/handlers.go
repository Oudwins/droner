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
	var request schemas.SessionCreateRequest

	errs := schemas.SessionCreateSchema.Parse(zhttp.Request(r), &request, z.WithCtxValue("workspace", s.Base.Workspace))
	if errs != nil {
		logger.Info("Schema validation failed", "errors", errs)
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
	repoName := filepath.Base(request.Path)
	if request.SessionID == "" {
		generatedID, err := sessionids.New(repoName, &sessionids.GeneratorConfig{
			MaxAttempts: 100,
			IsValid: func(id string) error {
				sid := schemas.NewSSessionID(id)
				worktreePath := filepath.Join(worktreeRoot, sid.SessionWorktreeName(repoName)) // TODO: this conversion is done in multiple places. Brittle
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
		request.SessionID = schemas.NewSSessionID(generatedID)
	}

	worktreeName := request.SessionID.SessionWorktreeName(repoName)
	worktreePath := filepath.Join(worktreeRoot, worktreeName)

	sessionID, err := uuid.NewV7()
	if err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to generate session id", nil), Render.Status(http.StatusInternalServerError))
		return
	}

	agentConfigValue := sql.NullString{}
	if request.AgentConfig != nil {
		agentConfigBytes, err := json.Marshal(request.AgentConfig)
		if err != nil {
			logger.Error("Failed to serialize agent config", slog.String("error", err.Error()))
			RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
			return
		}
		agentConfigValue = sql.NullString{String: string(agentConfigBytes), Valid: true}
	}
	sessionData := db.CreateSessionParams{
		ID:           sessionID.String(),
		SimpleID:     request.SessionID.String(),
		Status:       db.SessionStatusQueued,
		RepoPath:     request.Path,
		WorktreePath: worktreePath,
		AgentConfig:  agentConfigValue,
		Error:        sql.NullString{},
	}
	_, err = s.Base.DB.CreateSession(context.Background(), sessionData)
	if err != nil {
		logger.Error("Failed to create session record", slog.String("error", err.Error()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}

	// Enqueue task
	bytes, _ := json.Marshal(request)
	logger.Debug("Enequeued", slog.Any("request", request))
	taskId, err := s.Base.TaskQueue.Enqueue(context.Background(), tasky.NewTask(core.JobCreateSession, bytes))
	if err != nil {
		logger.Error("Failed to enque task", slog.String("error", err.Error()))
		_, updateErr := s.Base.DB.UpdateSessionStatusBySimpleID(context.Background(), db.UpdateSessionStatusBySimpleIDParams{
			SimpleID: request.SessionID.String(),
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
		SessionID:    request.SessionID,
		SimpleID:     sessionData.ID,
		WorktreePath: worktreePath,
		TaskID:       taskId,
	}
	RenderJSON(w, r, res, Render.Status(http.StatusAccepted))
}

func (s *Server) HandlerDeleteSession(logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	var payload schemas.SessionDeleteRequest
	errs := schemas.SessionDeleteSchema.Parse(zhttp.Request(r), &payload)
	if errs != nil {
		logger.Error("Schema validation failed")
		payload := JsonResponseError(JsonResponseErrorCodeValidationFailed, "Schema validation failed", z.Issues.Flatten(errs))
		RenderJSON(w, r, payload, Render.Status(http.StatusBadRequest))
		return
	}

	bytes, _ := json.Marshal(payload)
	logger.Debug("Enqueueing task", slog.Any("payload", payload))
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
