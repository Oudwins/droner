package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/core/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/repo"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	sessionids "github.com/Oudwins/droner/pkgs/droner/internals/sessionIds"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
	"github.com/Oudwins/droner/pkgs/droner/internals/timeouts"
	"github.com/Oudwins/zog/zhttp"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	z "github.com/Oudwins/zog"
)

func (s *Server) HandlerVersion(_ *slog.Logger, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(s.Base.Config.Version))
}

func (s *Server) HandlerShutdown(_ *slog.Logger, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	// Trigger shutdown asynchronously; calling http.Server.Shutdown from within
	// a handler can deadlock until the shutdown context times out.
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("Shutting down"))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		// Give the response a moment to flush before tearing down the server.
		time.Sleep(100 * time.Millisecond)
		s.Shutdown()
	}()
}

func (s *Server) HandlerCreateSession(logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	var request schemas.SessionCreateRequest

	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		logger.Info("Json decoding failed", slog.String("err", err.Error()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeInvalidJson, "Invalid json", nil), Render.Status(http.StatusBadRequest))
		return
	}

	errs := schemas.SessionCreateSchema.Validate(&request)
	if errs != nil {
		flattened := z.Issues.FlattenAndCollect(errs)
		logger.Info("Schema validation failed", slog.Any("errors", flattened))
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "Schema validation failed", flattened), Render.Status(http.StatusBadRequest))
		return
	}

	logger = logger.With(slog.Any("validated_payload", request))

	if err := repo.CheckRepo(request.Path); err != nil {
		logger.Info("Repo validation failed", slog.String("error", err.Error()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "Path is not a git repo", nil), Render.Status(http.StatusBadRequest))
		return
	}
	backend, err := s.Base.BackendStore.Get(request.BackendID)
	if err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, fmt.Sprintf("Backend '%s' is not registered", request.BackendID), nil), Render.Status(http.StatusBadRequest))
		return
	}

	logger = logger.With(slog.Any("request", request))
	logger.Debug("Successful validation")

	// LOGIC
	// NOTE: Parallel requests with the same ID are allowed by this behaviour. Can fix this later. Its safe because create session should fail at db level
	repoName := filepath.Base(request.Path)
	if request.SessionID == "" {
		generatedID, err := sessionids.New(repoName, &sessionids.GeneratorConfig{
			MaxAttempts: 100,
			IsValid: func(id string) error {
				return backend.ValidateSessionID(request.Path, id)
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

	if err := backend.ValidateSessionID(request.Path, request.SessionID.String()); err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "Session ID is not available", nil), Render.Status(http.StatusBadRequest))
		return
	}

	worktreePath, err := backend.WorktreePath(request.Path, request.SessionID.String())
	if err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to resolve worktree path", nil), Render.Status(http.StatusInternalServerError))
		return
	}

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
		BackendID:    request.BackendID.String(),
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
	taskId, err := s.Base.TaskQueue.Enqueue(context.Background(), tasky.NewTask(core.JobCreateSession, bytes))
	logger.Debug("Enqued create job session", slog.Bool("success", err == nil))
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
		return
	}

	// Response
	res := schemas.SessionCreateResponse{
		SessionID:    request.SessionID,
		SimpleID:     sessionData.SimpleID,
		BackendID:    request.BackendID,
		WorktreePath: worktreePath,
		TaskID:       taskId,
	}
	RenderJSON(w, r, res, Render.Status(http.StatusAccepted))
}

func (s *Server) HandlerDeleteSession(logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	var payload schemas.SessionDeleteRequest
	decodeErr := json.NewDecoder(r.Body).Decode(&payload)
	if decodeErr != nil {
		if errors.Is(decodeErr, io.EOF) {
			errs := schemas.SessionDeleteSchema.Parse(zhttp.Request(r), &payload)
			if errs != nil {
				logger.Error("Schema validation failed")
				payload := JsonResponseError(JsonResponseErrorCodeValidationFailed, "Schema validation failed", z.Issues.Flatten(errs))
				RenderJSON(w, r, payload, Render.Status(http.StatusBadRequest))
				return
			}
		} else {
			logger.Info("Json decoding failed", slog.String("err", decodeErr.Error()))
			RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeInvalidJson, "Invalid json", nil), Render.Status(http.StatusBadRequest))
			return
		}
	}

	errs := schemas.SessionDeleteSchema.Validate(&payload)
	if errs != nil {
		flattened := z.Issues.FlattenAndCollect(errs)
		logger.Info("Schema validation failed", slog.Any("errors", flattened))
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "Schema validation failed", flattened), Render.Status(http.StatusBadRequest))
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

	RenderJSON(w, r, schemas.TaskResponse{
		TaskID: taskId,
		Type:   "session_delete",
		Status: schemas.TaskStatusPending,
		Result: &schemas.TaskResult{SessionID: payload.SessionID.String()},
	}, Render.Status(http.StatusAccepted))
}

func (s *Server) HandlerCompleteSession(logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	var payload schemas.SessionCompleteRequest
	decodeErr := json.NewDecoder(r.Body).Decode(&payload)
	if decodeErr != nil {
		logger.Info("Json decoding failed", slog.String("err", decodeErr.Error()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeInvalidJson, "Invalid json", nil), Render.Status(http.StatusBadRequest))
		return
	}

	errs := schemas.SessionCompleteSchema.Validate(&payload)
	if errs != nil {
		flattened := z.Issues.FlattenAndCollect(errs)
		logger.Info("Schema validation failed", slog.Any("errors", flattened))
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "Schema validation failed", flattened), Render.Status(http.StatusBadRequest))
		return
	}

	session, err := s.Base.DB.GetSessionBySimpleIDAnyStatus(context.Background(), payload.SessionID.String())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeNotFound, "Session not found", nil), Render.Status(http.StatusNotFound))
			return
		}
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to load session", nil), Render.Status(http.StatusInternalServerError))
		return
	}

	if session.Status != db.SessionStatusRunning && session.Status != db.SessionStatusCompleted && session.Status != db.SessionStatusDeleted {
		logger.Error("Complete requested for non-running session", slog.String("status", string(session.Status)), slog.String("sessionId", payload.SessionID.String()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, fmt.Sprintf("Session is not running (status=%s)", session.Status), nil), Render.Status(http.StatusConflict))
		return
	}

	bytes, _ := json.Marshal(payload)
	logger.Debug("Enqueueing task", slog.Any("payload", payload))
	taskId, err := s.Base.TaskQueue.Enqueue(context.Background(), tasky.NewTask(core.JobCompleteSession, bytes))
	if err != nil {
		logger.Error("Failed to enque job", slog.String("error", err.Error()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to enque job"+err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}

	RenderJSON(w, r, schemas.TaskResponse{
		TaskID: taskId,
		Type:   "session_complete",
		Status: schemas.TaskStatusPending,
		Result: &schemas.TaskResult{SessionID: payload.SessionID.String(), WorktreePath: session.WorktreePath},
	}, Render.Status(http.StatusAccepted))
}

func (s *Server) HandlerTaskStatus(logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	if taskID == "" {
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "Task id is required", nil), Render.Status(http.StatusBadRequest))
		return
	}

	queueDBPath := filepath.Join(s.Base.Config.Server.DataDir, "queue", "queue.db")
	conn, err := sql.Open("sqlite", queueDBPath)
	if err != nil {
		logger.Error("Failed to open queue db", slog.String("error", err.Error()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to open task store", nil), Render.Status(http.StatusInternalServerError))
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(r.Context(), timeouts.SecondShort)
	defer cancel()

	var jobID string
	var rawStatus string
	var rawPayload []byte
	var createdAt int64
	var updatedAt int64
	var completedAt sql.NullInt64
	row := conn.QueryRowContext(ctx, `SELECT job_id, status, payload, created_at, updated_at, completed_at FROM droner_queue WHERE id = ?`, taskID)
	if err := row.Scan(&jobID, &rawStatus, &rawPayload, &createdAt, &updatedAt, &completedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeNotFound, "Task not found", nil), Render.Status(http.StatusNotFound))
			return
		}
		logger.Error("Failed to query queue db", slog.String("error", err.Error()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to load task", nil), Render.Status(http.StatusInternalServerError))
		return
	}

	status := schemas.TaskStatusPending
	switch rawStatus {
	case "pending":
		status = schemas.TaskStatusPending
	case "in_flight":
		status = schemas.TaskStatusRunning
	case "completed":
		status = schemas.TaskStatusSucceeded
	case "failed":
		status = schemas.TaskStatusFailed
	default:
		status = schemas.TaskStatusPending
	}

	res := schemas.TaskResponse{
		TaskID:    taskID,
		Type:      jobID,
		Status:    status,
		CreatedAt: time.Unix(0, createdAt).UTC().Format(time.RFC3339Nano),
	}
	if updatedAt > 0 && (status == schemas.TaskStatusRunning || status == schemas.TaskStatusSucceeded || status == schemas.TaskStatusFailed) {
		res.StartedAt = time.Unix(0, updatedAt).UTC().Format(time.RFC3339Nano)
	}
	if completedAt.Valid {
		res.FinishedAt = time.Unix(0, completedAt.Int64).UTC().Format(time.RFC3339Nano)
	} else if updatedAt > 0 && (status == schemas.TaskStatusSucceeded || status == schemas.TaskStatusFailed) {
		res.FinishedAt = time.Unix(0, updatedAt).UTC().Format(time.RFC3339Nano)
	}

	result := &schemas.TaskResult{}
	switch core.Jobs(jobID) {
	case core.JobCreateSession:
		var payload schemas.SessionCreateRequest
		if err := json.Unmarshal(rawPayload, &payload); err == nil {
			result.SessionID = payload.SessionID.String()
			backend, err := s.Base.BackendStore.Get(payload.BackendID)
			if err == nil {
				if wt, err := backend.WorktreePath(payload.Path, payload.SessionID.String()); err == nil {
					result.WorktreePath = wt
				}
			}
		}
	case core.JobDeleteSession:
		var payload schemas.SessionDeleteRequest
		if err := json.Unmarshal(rawPayload, &payload); err == nil {
			result.SessionID = payload.SessionID.String()
			if session, err := s.Base.DB.GetSessionBySimpleIDAnyStatus(context.Background(), payload.SessionID.String()); err == nil {
				result.WorktreePath = session.WorktreePath
			}
		}
	case core.JobCompleteSession:
		var payload schemas.SessionCompleteRequest
		if err := json.Unmarshal(rawPayload, &payload); err == nil {
			result.SessionID = payload.SessionID.String()
			if session, err := s.Base.DB.GetSessionBySimpleIDAnyStatus(context.Background(), payload.SessionID.String()); err == nil {
				result.WorktreePath = session.WorktreePath
			}
		}
	}
	if result.SessionID != "" || result.WorktreePath != "" {
		res.Result = result
	}

	RenderJSON(w, r, res)
}

func (s *Server) HandlerNukeSessions(logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	taskId, err := s.Base.TaskQueue.Enqueue(context.Background(), tasky.NewTask(core.JobDeleteAllSessions, []byte("{}")))
	if err != nil {
		logger.Error("Failed to enque job", slog.String("error", err.Error()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to enque job"+err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}

	RenderJSON(w, r, schemas.TaskResponse{
		TaskID: taskId,
		Type:   "session_nuke",
		Status: schemas.TaskStatusPending,
	}, Render.Status(http.StatusAccepted))
}

func (s *Server) HandlerListSessions(logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	all := false
	rawAll := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("all")))
	if rawAll == "1" || rawAll == "true" || rawAll == "yes" {
		all = true
	}

	var sessions []db.Session
	if all {
		rows, err := s.Base.DB.ListSessions(r.Context())
		if err != nil {
			logger.Error("Failed to list sessions", slog.String("error", err.Error()))
			RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to list sessions", nil), Render.Status(http.StatusInternalServerError))
			return
		}
		if len(rows) > 100 {
			rows = rows[:100]
		}
		sessions = rows
	} else {
		queued, err := s.Base.DB.ListSessionsByStatus(r.Context(), db.SessionStatusQueued)
		if err != nil {
			logger.Error("Failed to list queued sessions", slog.String("error", err.Error()))
			RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to list queued sessions", nil), Render.Status(http.StatusInternalServerError))
			return
		}

		running, err := s.Base.DB.ListSessionsByStatus(r.Context(), db.SessionStatusRunning)
		if err != nil {
			logger.Error("Failed to list running sessions", slog.String("error", err.Error()))
			RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to list running sessions", nil), Render.Status(http.StatusInternalServerError))
			return
		}

		sessions = append(queued, running...)
		sort.Slice(sessions, func(i, j int) bool {
			return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
		})
	}

	items := make([]schemas.SessionListItem, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, schemas.SessionListItem{
			SimpleID: schemas.NewSSessionID(session.SimpleID),
			State:    string(session.Status),
		})
	}

	RenderJSON(w, r, schemas.SessionListResponse{Sessions: items})
}
