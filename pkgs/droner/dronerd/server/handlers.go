package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/tasks"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
	"github.com/Oudwins/droner/pkgs/droner/internals/workspace"
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

	errs := schemas.SessionCreateSchema.Parse(zhttp.Request(r), &payload, z.WithCtxValue("workspace", s.Workspace))
	if errs != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "Schema validation failed", z.Issues.Flatten(errs)), Render.Status(http.StatusBadRequest))
		return
	}

	worktreeRoot := s.Base.Config.Worktrees.Dir
	if err := s.Workspace.MkdirAll(worktreeRoot, 0o755); err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to create worktree root", nil), Render.Status(http.StatusInternalServerError))
		return
	}

	// LOGIC
	baseName := filepath.Base(payload.Path)
	if payload.SessionID == "" {
		generatedID, err := generateSessionID(s.Workspace, baseName, worktreeRoot)
		if err != nil || generatedID == "" {
			RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to generate session id", nil), Render.Status(http.StatusInternalServerError))
			return
		}
		payload.SessionID = generatedID
	}

	worktreeName := baseName + "#" + payload.SessionID
	worktreePath := filepath.Join(worktreeRoot, worktreeName)
	if _, err := s.Workspace.Stat(worktreePath); err != nil {
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
		TaskID:       taskId.(string),
	}
	RenderJSON(w, r, res, Render.Status(http.StatusAccepted))
}

func (s *Server) HandlerDeleteSession(w http.ResponseWriter, r *http.Request) {
	var reqbody schemas.SessionDeleteRequest
	errs := schemas.SessionDeleteSchema.Parse(zhttp.Request(r), &reqbody)
	if errs != nil {
		payload := JsonResponseError(JsonResponseErrorCodeValidationFailed, "Schema validation failed", z.Issues.Flatten(errs))
		RenderJSON(w, r, payload, Render.Status(http.StatusBadRequest))
		return
	}

	worktreeRoot := s.Base.Config.Worktrees.Dir

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

	// response, err := s.enqueueDeleteSession(reqbody, worktreePath)
	// if err != nil {
	// 	RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
	// 	return
	// }
	// response.Result = &schemas.TaskResult{SessionID: reqbody.SessionID, WorktreePath: worktreePath}
	// RenderJSON(w, r, response, Render.Status(http.StatusAccepted))
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

func (s *Server) runDeleteSession(ctx context.Context, payload schemas.SessionDeleteRequest, worktreePath string) (*schemas.TaskResult, error) {
	worktreeName := filepath.Base(worktreePath)
	if payload.SessionID == "" {
		payload.SessionID = sessionIDFromName(worktreeName)
	}

	commonGitDir, err := s.Workspace.GitCommonDirFromWorktree(worktreePath)
	if err != nil {
		return nil, err
	}

	if remoteURL, err := s.Workspace.GetRemoteURLFromWorktree(worktreePath); err == nil {
		if err := s.subs.unsubscribe(ctx, remoteURL, payload.SessionID, s.Base.Logger); err != nil {
			s.Base.Logger.Warn("Failed to unsubscribe from remote events",
				"error", err,
				"remote_url", remoteURL,
				"session_id", payload.SessionID,
			)
		}
	}

	if err := s.Workspace.KillTmuxSession(worktreeName); err != nil {
		return nil, err
	}

	if err := s.Workspace.RemoveGitWorktree(worktreePath); err != nil {
		return nil, err
	}

	if err := s.Workspace.DeleteGitBranch(commonGitDir, payload.SessionID); err != nil {
		return nil, err
	}

	return &schemas.TaskResult{SessionID: payload.SessionID, WorktreePath: worktreePath}, nil
}

func resolveDeleteWorktreePath(host workspace.Host, worktreeRoot string, payload schemas.SessionDeleteRequest) (string, error) {
	if payload.Path != "" {
		worktreePath := filepath.Clean(payload.Path)
		if _, err := host.Stat(worktreePath); err != nil {
			if os.IsNotExist(err) {
				return "", os.ErrNotExist
			}
			return "", fmt.Errorf("failed to read worktree")
		}
		return worktreePath, nil
	}

	matchedPath, err := findWorktreeBySessionID(host, worktreeRoot, payload.SessionID)
	if err != nil {
		return "", os.ErrNotExist
	}
	return matchedPath, nil
}
