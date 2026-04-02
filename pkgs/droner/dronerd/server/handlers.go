package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/sessions/sessionevents"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/repo"
	sessionids "github.com/Oudwins/droner/pkgs/droner/dronerd/internals/sessionIds"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
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
	remoteURL, err := repo.GetRemoteURL(request.Path)
	if err != nil {
		logger.Warn("Failed to resolve repo remote URL; continuing without subscriptions", slog.String("error", err.Error()))
		remoteURL = ""
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
	if request.Branch == "" {
		description := ""
		if request.AgentConfig != nil {
			description = request.AgentConfig.ToDescription()
		}

		generatedID, err := sessionids.NewForCreateSession(r.Context(), sessionids.CreateSessionIDOptions{
			RepoPath:    request.Path,
			Naming:      s.Base.Config.Sessions.Naming,
			Description: description,
			MaxAttempts: 100,
			IsValid: func(id string) error {
				return backend.ValidateSessionID(request.Path, id)
			},
			OnNamingError: func(err error) {
				logger.Info("OpenCode naming failed; falling back to random", slog.String("error", err.Error()))
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
		request.Branch = schemas.NewSBranch(generatedID)
	}

	if err := backend.ValidateSessionID(request.Path, request.Branch.String()); err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "Branch is not available", nil), Render.Status(http.StatusBadRequest))
		return
	}

	worktreePath, err := backend.WorktreePath(request.Path, request.Branch.String())
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
	result, err := s.events.CreateSession(r.Context(), sessionevents.CreateSessionInput{
		StreamID:        sessionID.String(),
		Harness:         request.Harness,
		Branch:          request.Branch.String(),
		BackendID:       request.BackendID,
		RepoPath:        request.Path,
		WorktreePath:    worktreePath,
		RemoteURL:       remoteURL,
		AgentConfigJSON: agentConfigValue.String,
	})
	if err != nil {
		logger.Error("Failed to append session event", slog.String("error", err.Error()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}

	res := schemas.SessionCreateResponse{
		ID:           sessionID.String(),
		Harness:      request.Harness,
		Branch:       request.Branch,
		BackendID:    request.BackendID,
		WorktreePath: worktreePath,
		TaskID:       result.TaskID,
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

	result, err := s.events.RequestDeletion(r.Context(), payload.Branch.String())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeNotFound, "Session not found", nil), Render.Status(http.StatusNotFound))
			return
		}
		logger.Error("Failed to append deletion request", slog.String("error", err.Error()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to request session deletion", nil), Render.Status(http.StatusInternalServerError))
		return
	}

	RenderJSON(w, r, schemas.TaskResponse{
		TaskID: result.TaskID,
		Type:   "session_delete",
		Status: schemas.TaskStatusPending,
		Result: &schemas.TaskResult{Branch: payload.Branch.String()},
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

	ref, err := s.events.LookupSessionByBranch(r.Context(), payload.Branch.String())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeNotFound, "Session not found", nil), Render.Status(http.StatusNotFound))
			return
		}
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to load session", nil), Render.Status(http.StatusInternalServerError))
		return
	}

	if !ref.PublicState.IsActive() && ref.PublicState != sessionevents.PublicStateCompleted && ref.PublicState != sessionevents.PublicStateDeleted {
		logger.Error("Complete requested for non-active session", slog.String("status", ref.PublicState.String()), slog.String("branch", payload.Branch.String()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, fmt.Sprintf("Session is not active (status=%s)", ref.PublicState), nil), Render.Status(http.StatusConflict))
		return
	}

	result, err := s.events.RequestCompletion(r.Context(), payload.Branch.String())
	if err != nil {
		logger.Error("Failed to append completion request", slog.String("error", err.Error()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to request session completion", nil), Render.Status(http.StatusInternalServerError))
		return
	}

	RenderJSON(w, r, schemas.TaskResponse{
		TaskID: result.TaskID,
		Type:   "session_complete",
		Status: schemas.TaskStatusPending,
		Result: &schemas.TaskResult{Branch: payload.Branch.String(), WorktreePath: ref.WorktreePath},
	}, Render.Status(http.StatusAccepted))
}

func (s *Server) HandlerNukeSessions(logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	result, err := s.events.NukeSessions(r.Context())
	if err != nil {
		logger.Error("Failed to request session nuke", slog.String("error", err.Error()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to nuke sessions", nil), Render.Status(http.StatusInternalServerError))
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)

	RenderJSON(w, r, schemas.TaskResponse{
		TaskID:     "",
		Type:       "session_nuke",
		Status:     schemas.TaskStatusSucceeded,
		CreatedAt:  now,
		FinishedAt: now,
		Result:     &schemas.TaskResult{Requested: fmt.Sprintf("%d", result.Requested)},
	}, Render.Status(http.StatusAccepted))
}

func (s *Server) HandlerListSessions(logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	// Use zog schema to parse and validate query params.
	var q schemas.SessionListQuery
	if errs := schemas.SessionListQuerySchema.Parse(zhttp.Request(r), &q); errs != nil {
		flattened := z.Issues.FlattenAndCollect(errs)
		logger.Info("Query validation failed", slog.Any("errors", flattened))
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "Query validation failed", flattened), Render.Status(http.StatusBadRequest))
		return
	}

	// Ensure non-negative limit
	if q.Limit < 0 {
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "limit must be non-negative", nil), Render.Status(http.StatusBadRequest))
		return
	}

	statuses := make([]string, 0, len(q.Status))
	for _, status := range q.Status {
		statuses = append(statuses, string(status))
	}

	items, err := s.events.ListSessionProjections(r.Context(), statuses, q.Limit, q.Cursor, string(q.Direction))
	if err != nil {
		logger.Error("Failed to list session projections", slog.String("error", err.Error()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to list sessions", nil), Render.Status(http.StatusInternalServerError))
		return
	}
	renderSessionListResponse(w, r, items)
}

func (s *Server) HandlerSessionNext(logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	s.handleSessionNavigation(logger, w, r, string(schemas.SessionListDirectionAfter))
}

func (s *Server) HandlerSessionPrev(logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	s.handleSessionNavigation(logger, w, r, string(schemas.SessionListDirectionBefore))
}

func (s *Server) handleSessionNavigation(logger *slog.Logger, w http.ResponseWriter, r *http.Request, direction string) {
	var q schemas.SessionNavigationQuery
	if errs := schemas.SessionNavigationQuerySchema.Parse(zhttp.Request(r), &q); errs != nil {
		flattened := z.Issues.FlattenAndCollect(errs)
		logger.Info("Query validation failed", slog.Any("errors", flattened))
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "Query validation failed", flattened), Render.Status(http.StatusBadRequest))
		return
	}

	cursor := strings.TrimSpace(q.ID)
	if cursor == "" {
		branch := navigationBranchFromTmuxSession(q.TmuxSession)
		if branch != "" {
			ref, err := s.events.LookupLatestNavigationSessionByBranch(r.Context(), branch)
			if err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					logger.Error("Failed to resolve navigation branch", slog.String("branch", branch), slog.String("error", err.Error()))
					RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to resolve session branch", nil), Render.Status(http.StatusInternalServerError))
					return
				}
			} else {
				cursor = ref.StreamID
			}
		}
	}

	items, err := s.events.ListSessionProjections(r.Context(), []string{string(schemas.SessionPublicStateActiveIdle)}, 1, cursor, direction)
	if err != nil {
		logger.Error("Failed to navigate session projections", slog.String("direction", direction), slog.String("error", err.Error()))
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to navigate sessions", nil), Render.Status(http.StatusInternalServerError))
		return
	}

	renderSessionListResponse(w, r, items)
}

func navigationBranchFromTmuxSession(tmuxSession string) string {
	parts := strings.Split(strings.TrimSpace(tmuxSession), "#")
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func renderSessionListResponse(w http.ResponseWriter, r *http.Request, items []sessionevents.ListItem) {
	responseItems := make([]schemas.SessionListItem, 0, len(items))
	for _, item := range items {
		tmuxSession := ""
		if item.Repo != "" && item.Branch != "" {
			tmuxSession = item.Repo + "#" + item.Branch
		}

		responseItems = append(responseItems, schemas.SessionListItem{
			ID:          item.ID,
			Repo:        item.Repo,
			RemoteURL:   item.RemoteURL,
			TmuxSession: tmuxSession,
			Branch:      schemas.NewSBranch(item.Branch),
			State:       schemas.SessionPublicState(item.State),
		})
	}
	RenderJSON(w, r, schemas.SessionListResponse{Sessions: responseItems})
}
