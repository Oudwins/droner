package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/internals/workspace"

	z "github.com/Oudwins/zog"
)

func (s *Server) HandlerVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(s.Base.Config.Version))
}

func (s *Server) HandlerShutdown(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("shutting down"))

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if s.httpServer == nil {
			s.Base.Logger.Error("shutdown failed", "error", errors.New("server not initialized"))
			return
		}
		if err := s.httpServer.Shutdown(ctx); err != nil {
			s.Base.Logger.Error("shutdown failed", "error", err)
		}
	}()
}

func (s *Server) HandlerCreateSession(w http.ResponseWriter, r *http.Request) {
	var request schemas.SessionCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeInvalidJson, "Invalid JSON", nil), Render.Status(http.StatusBadRequest))
		return
	}

	request.Path = strings.TrimSpace(request.Path)
	request.SessionID = strings.TrimSpace(request.SessionID)
	if request.Path == "" {
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "path is required", nil), Render.Status(http.StatusBadRequest))
		return
	}
	if request.Agent == nil {
		request.Agent = &schemas.SessionAgentConfig{}
	}
	request.Agent.Model = strings.TrimSpace(request.Agent.Model)
	request.Agent.Prompt = strings.TrimSpace(request.Agent.Prompt)
	if request.Agent.Model == "" {
		request.Agent.Model = conf.GetConfig().Agent.DefaultModel
	}

	repoPath := filepath.Clean(request.Path)
	info, err := s.Workspace.Stat(repoPath)
	if err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "path not found", nil), Render.Status(http.StatusBadRequest))
		return
	}
	if !info.IsDir() {
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "path not a directory", nil), Render.Status(http.StatusBadRequest))
		return
	}

	if err := s.Workspace.GitIsInsideWorkTree(repoPath); err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "path not to a git repo", nil), Render.Status(http.StatusBadRequest))
		return
	}

	worktreeRoot, _ := expandPath(s.Base.Config.Worktrees.Dir)
	if err := s.Workspace.MkdirAll(worktreeRoot, 0o755); err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to create worktree root", nil), Render.Status(http.StatusInternalServerError))
		return
	}

	baseName := filepath.Base(repoPath)
	if request.SessionID == "" {
		generatedID, err := generateSessionID(s.Workspace, baseName, worktreeRoot)
		if err != nil {
			RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to generate session id", nil), Render.Status(http.StatusInternalServerError))
			return
		}
		request.SessionID = generatedID
	}

	worktreeName := baseName + "#" + request.SessionID
	worktreePath := filepath.Join(worktreeRoot, worktreeName)

	if request.SessionID != "" {
		if _, err := s.Workspace.Stat(worktreePath); err == nil {
			response, err := s.enqueueCreateSession(request, repoPath, worktreePath, true)
			if err != nil {
				RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
				return
			}
			response.Result = &schemas.TaskResult{SessionID: request.SessionID, WorktreePath: worktreePath}
			RenderJSON(w, r, response, Render.Status(http.StatusAccepted))
			return
		}
	}

	response, err := s.enqueueCreateSession(request, repoPath, worktreePath, false)
	if err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}
	response.Result = &schemas.TaskResult{SessionID: request.SessionID, WorktreePath: worktreePath}
	RenderJSON(w, r, response, Render.Status(http.StatusAccepted))
}

func (s *Server) HandlerDeleteSession(w http.ResponseWriter, r *http.Request) {
	var reqbody schemas.SessionDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&reqbody); err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeInvalidJson, "Invalid JSON", nil), Render.Status(http.StatusBadRequest))
		return
	}

	if issues := schemas.SessionDeleteSchema.Validate(&reqbody); len(issues) > 0 {
		payload := JsonResponseError(JsonResponseErrorCodeValidationFailed, "Schema validation failed", z.Issues.Flatten(issues))
		RenderJSON(w, r, payload, Render.Status(http.StatusBadRequest))
		return
	}

	worktreeRoot, err := expandPath(s.Base.Config.Worktrees.Dir)
	if err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Worktree Root doesn't exist", nil), Render.Status(http.StatusInternalServerError))
		return
	}

	worktreePath, err := resolveDeleteWorktreePath(s.Workspace, worktreeRoot, reqbody)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeNotFound, "Couldn't find the worktree", nil), Render.Status(http.StatusNotFound))
			return
		}
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}
	worktreeName := filepath.Base(worktreePath)
	if reqbody.SessionID == "" {
		reqbody.SessionID = sessionIDFromName(worktreeName)
	}

	response, err := s.enqueueDeleteSession(reqbody, worktreePath)
	if err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}
	response.Result = &schemas.TaskResult{SessionID: reqbody.SessionID, WorktreePath: worktreePath}
	RenderJSON(w, r, response, Render.Status(http.StatusAccepted))
}

func expandPath(path string) (string, error) {
	if path == "" {
		return path, nil
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}

func generateSessionID(host workspace.Host, baseName string, worktreeRoot string) (string, error) {
	random := rand.New(rand.NewSource(time.Now().UnixNano()))
	letters := []rune("abcdefghijklmnopqrstuvwxyz")
	for range 100 {
		chars := make([]rune, 3)
		for i := range chars {
			chars[i] = letters[random.Intn(len(letters))]
		}
		candidate := fmt.Sprintf("%s-%02d", string(chars), random.Intn(100))
		worktreePath := filepath.Join(worktreeRoot, baseName+"#"+candidate)
		if _, err := host.Stat(worktreePath); err != nil {
			if os.IsNotExist(err) {
				return candidate, nil
			}
			return "", err
		}
	}
	return "", fmt.Errorf("no available session id")
}

func sessionIDFromName(worktreeName string) string {
	parts := strings.SplitN(worktreeName, "#", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

func findWorktreeBySessionID(host workspace.Host, worktreeRoot string, sessionID string) (string, error) {
	entries, err := host.ReadDir(worktreeRoot)
	if err != nil {
		return "", fmt.Errorf("failed to read worktree root")
	}

	var matches []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, "#"+sessionID) {
			matches = append(matches, filepath.Join(worktreeRoot, name))
		}
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("worktree not found")
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple worktrees matched session id")
	}
	return matches[0], nil
}

type createSessionPayload struct {
	RepoPath     string `json:"repo_path"`
	WorktreePath string `json:"worktree_path"`
	SessionID    string `json:"session_id"`
	Model        string `json:"model"`
	Prompt       string `json:"prompt"`
}

type deleteSessionPayload struct {
	WorktreePath string `json:"worktree_path"`
	SessionID    string `json:"session_id"`
}

func (s *Server) enqueueCreateSession(request schemas.SessionCreateRequest, repoPath string, worktreePath string, existing bool) (*schemas.TaskResponse, error) {
	payload := createSessionPayload{
		RepoPath:     repoPath,
		WorktreePath: worktreePath,
		SessionID:    request.SessionID,
		Model:        request.Agent.Model,
		Prompt:       request.Agent.Prompt,
	}
	return s.tasks.Enqueue(taskTypeSessionCreate, payload, func(ctx context.Context) (any, error) {
		if existing {
			return &schemas.TaskResult{SessionID: request.SessionID, WorktreePath: worktreePath}, nil
		}
		return s.runCreateSession(ctx, request, repoPath, worktreePath)
	})
}

func (s *Server) enqueueDeleteSession(request schemas.SessionDeleteRequest, worktreePath string) (*schemas.TaskResponse, error) {
	payload := deleteSessionPayload{
		WorktreePath: worktreePath,
		SessionID:    request.SessionID,
	}
	return s.tasks.Enqueue(taskTypeSessionDelete, payload, func(ctx context.Context) (any, error) {
		return s.runDeleteSession(ctx, request, worktreePath)
	})
}

func (s *Server) runCreateSession(ctx context.Context, request schemas.SessionCreateRequest, repoPath string, worktreePath string) (*schemas.TaskResult, error) {
	worktreeName := filepath.Base(worktreePath)
	if err := s.Workspace.CreateGitWorktree(request.SessionID, repoPath, worktreePath); err != nil {
		return nil, err
	}

	if err := s.Workspace.RunWorktreeSetup(repoPath, worktreePath); err != nil {
		return nil, err
	}

	if err := s.Workspace.CreateTmuxSession(worktreeName, worktreePath, request.Agent.Model, request.Agent.Prompt); err != nil {
		return nil, err
	}

	if remoteURL, err := s.Workspace.GetRemoteURL(repoPath); err == nil {
		if err := s.subs.subscribe(ctx, remoteURL, request.SessionID, s.Base.Logger, func(sessionID string) {
			s.deleteSessionBySessionID(sessionID)
		}); err != nil {
			s.Base.Logger.Warn("Failed to subscribe to remote events",
				"error", err,
				"remote_url", remoteURL,
				"session_id", request.SessionID,
			)
		}
	} else {
		s.Base.Logger.Warn("Failed to get remote URL, skipping event subscription",
			"error", err,
			"session_id", request.SessionID,
		)
	}

	return &schemas.TaskResult{SessionID: request.SessionID, WorktreePath: worktreePath}, nil
}

func (s *Server) runDeleteSession(ctx context.Context, request schemas.SessionDeleteRequest, worktreePath string) (*schemas.TaskResult, error) {
	worktreeName := filepath.Base(worktreePath)
	if request.SessionID == "" {
		request.SessionID = sessionIDFromName(worktreeName)
	}

	commonGitDir, err := s.Workspace.GitCommonDirFromWorktree(worktreePath)
	if err != nil {
		return nil, err
	}

	if remoteURL, err := s.Workspace.GetRemoteURLFromWorktree(worktreePath); err == nil {
		if err := s.subs.unsubscribe(ctx, remoteURL, request.SessionID, s.Base.Logger); err != nil {
			s.Base.Logger.Warn("Failed to unsubscribe from remote events",
				"error", err,
				"remote_url", remoteURL,
				"session_id", request.SessionID,
			)
		}
	}

	if err := s.Workspace.KillTmuxSession(worktreeName); err != nil {
		return nil, err
	}

	if err := s.Workspace.RemoveGitWorktree(worktreePath); err != nil {
		return nil, err
	}

	if err := s.Workspace.DeleteGitBranch(commonGitDir, request.SessionID); err != nil {
		return nil, err
	}

	return &schemas.TaskResult{SessionID: request.SessionID, WorktreePath: worktreePath}, nil
}

func resolveDeleteWorktreePath(host workspace.Host, worktreeRoot string, request schemas.SessionDeleteRequest) (string, error) {
	if request.Path != "" {
		worktreePath := filepath.Clean(request.Path)
		if _, err := host.Stat(worktreePath); err != nil {
			if os.IsNotExist(err) {
				return "", os.ErrNotExist
			}
			return "", fmt.Errorf("failed to read worktree")
		}
		return worktreePath, nil
	}

	matchedPath, err := findWorktreeBySessionID(host, worktreeRoot, request.SessionID)
	if err != nil {
		return "", os.ErrNotExist
	}
	return matchedPath, nil
}
