package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"

	z "github.com/Oudwins/zog"
)

func (s *Server) HandlerVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(s.Config.VERSION))
}

func (s *Server) HandlerShutdown(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("shutting down"))

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if s.httpServer == nil {
			s.Logger.Error("shutdown failed", "error", errors.New("server not initialized"))
			return
		}
		if err := s.httpServer.Shutdown(ctx); err != nil {
			s.Logger.Error("shutdown failed", "error", err)
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
		request.Agent.Model = conf.GetConfig().DEFAULT_MODEL
	}

	repoPath := filepath.Clean(request.Path)
	info, err := os.Stat(repoPath)
	if err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "path not found", nil), Render.Status(http.StatusBadRequest))
		return
	}
	if !info.IsDir() {
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "path not a directory", nil), Render.Status(http.StatusBadRequest))
		return
	}

	if err := gitIsInsideWorkTree(repoPath); err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeValidationFailed, "path not to a git repo", nil), Render.Status(http.StatusBadRequest))
		return
	}

	worktreeRoot, err := expandPath(s.Config.WORKTREES_DIR)
	if err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to expand worktree root", nil), Render.Status(http.StatusInternalServerError))
		return
	}
	if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to create worktree root", nil), Render.Status(http.StatusInternalServerError))
		return
	}

	baseName := filepath.Base(repoPath)
	if request.SessionID == "" {
		generatedID, err := generateSessionID(baseName, worktreeRoot)
		if err != nil {
			RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to generate session id", nil), Render.Status(http.StatusInternalServerError))
			return
		}
		request.SessionID = generatedID
	}

	worktreeName := baseName + "#" + request.SessionID
	worktreePath := filepath.Join(worktreeRoot, worktreeName)

	if request.SessionID != "" {
		if _, err := os.Stat(worktreePath); err == nil {
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

	worktreeRoot, err := expandPath(s.Config.WORKTREES_DIR)
	if err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Worktree Root doesn't exist", nil), Render.Status(http.StatusInternalServerError))
		return
	}

	worktreePath, err := resolveDeleteWorktreePath(worktreeRoot, reqbody)
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

func gitIsInsideWorkTree(repoPath string) error {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--is-inside-work-tree")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git check failed: %s", strings.TrimSpace(string(output)))
	}
	if strings.TrimSpace(string(output)) != "true" {
		return fmt.Errorf("not a git worktree")
	}
	return nil
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

func generateSessionID(baseName string, worktreeRoot string) (string, error) {
	random := rand.New(rand.NewSource(time.Now().UnixNano()))
	letters := []rune("abcdefghijklmnopqrstuvwxyz")
	for range 100 {
		chars := make([]rune, 3)
		for i := range chars {
			chars[i] = letters[random.Intn(len(letters))]
		}
		candidate := fmt.Sprintf("%s-%02d", string(chars), random.Intn(100))
		worktreePath := filepath.Join(worktreeRoot, baseName+"#"+candidate)
		if _, err := os.Stat(worktreePath); err != nil {
			if os.IsNotExist(err) {
				return candidate, nil
			}
			return "", err
		}
	}
	return "", fmt.Errorf("no available session id")
}

func createGitWorktree(sessionId string, repoPath string, worktreePath string) error {
	cmd := exec.Command("git", "-C", repoPath, "worktree", "add", "-b", sessionId, worktreePath) // create worktree with branch name = sessionid
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create worktree: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func removeGitWorktree(worktreePath string) error {
	cmd := exec.Command("git", "-C", worktreePath, "worktree", "remove", "--force", worktreePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to remove worktree: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func gitCommonDirFromWorktree(worktreePath string) (string, error) {
	cmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--git-common-dir")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to determine git common dir: %s", strings.TrimSpace(string(output)))
	}
	commonDir := strings.TrimSpace(string(output))
	if commonDir == "" {
		return "", errors.New("failed to determine git common dir")
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(worktreePath, commonDir)
	}
	return commonDir, nil
}

func deleteGitBranch(commonGitDir string, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	check := exec.Command("git", "--git-dir", commonGitDir, "show-ref", "--verify", "--quiet", "refs/heads/"+sessionID)
	if err := check.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil
		}
		return fmt.Errorf("failed to check branch: %w", err)
	}
	cmd := exec.Command("git", "--git-dir", commonGitDir, "branch", "-D", sessionID)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to delete branch: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

type worktreeConfig struct {
	SetupWorktree []string `json:"setup-worktree"`
}

func runWorktreeSetup(repoPath string, worktreePath string) error {
	configPath := filepath.Join(repoPath, ".cursor", "worktrees.json")
	if _, err := os.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read worktree config")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read worktree config")
	}

	var config worktreeConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse worktree config")
	}

	for _, command := range config.SetupWorktree {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		cmd := exec.Command("sh", "-c", command)
		cmd.Dir = worktreePath
		cmd.Env = append(os.Environ(), fmt.Sprintf("ROOT_WORKTREE_PATH=%s", repoPath))
		output, err := cmd.CombinedOutput()
		if err != nil {
			message := strings.TrimSpace(string(output))
			if message != "" {
				return fmt.Errorf("setup command failed: %s: %s", command, message)
			}
			return fmt.Errorf("setup command failed: %s", command)
		}
	}

	return nil
}

func createTmuxSession(sessionName string, worktreePath string, model string, prompt string) error {
	newSession := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "-n", "nvim", "-c", worktreePath, "nvim")
	if output, err := newSession.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create tmux session: %s", strings.TrimSpace(string(output)))
	}

	opencodeArgs := []string{"new-window", "-t", sessionName, "-n", "opencode", "-c", worktreePath, "opencode"}
	if model != "" {
		opencodeArgs = append(opencodeArgs, "--model", model)
	}
	if prompt != "" {
		opencodeArgs = append(opencodeArgs, "--prompt", prompt)
	}

	newOpencode := exec.Command("tmux", opencodeArgs...)
	if output, err := newOpencode.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create tmux opencode window: %s", strings.TrimSpace(string(output)))
	}

	newTerminal := exec.Command("tmux", "new-window", "-t", sessionName, "-n", "terminal", "-c", worktreePath)
	if output, err := newTerminal.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create tmux terminal window: %s", strings.TrimSpace(string(output)))
	}

	return nil
}

func killTmuxSession(sessionName string) error {
	check := exec.Command("tmux", "has-session", "-t", sessionName)
	if err := check.Run(); err != nil {
		return nil
	}
	cmd := exec.Command("tmux", "kill-session", "-t", sessionName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to kill tmux session: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func sessionIDFromName(worktreeName string) string {
	parts := strings.SplitN(worktreeName, "#", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

func findWorktreeBySessionID(worktreeRoot string, sessionID string) (string, error) {
	entries, err := os.ReadDir(worktreeRoot)
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
	if err := createGitWorktree(request.SessionID, repoPath, worktreePath); err != nil {
		return nil, err
	}

	if err := runWorktreeSetup(repoPath, worktreePath); err != nil {
		return nil, err
	}

	if err := createTmuxSession(worktreeName, worktreePath, request.Agent.Model, request.Agent.Prompt); err != nil {
		return nil, err
	}

	if remoteURL, err := getRemoteURL(repoPath); err == nil {
		if err := s.subs.subscribe(ctx, remoteURL, request.SessionID, s.Logger, func(sessionID string) {
			s.deleteSessionBySessionID(sessionID)
		}); err != nil {
			s.Logger.Warn("Failed to subscribe to remote events",
				"error", err,
				"remote_url", remoteURL,
				"session_id", request.SessionID,
			)
		}
	} else {
		s.Logger.Warn("Failed to get remote URL, skipping event subscription",
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

	commonGitDir, err := gitCommonDirFromWorktree(worktreePath)
	if err != nil {
		return nil, err
	}

	if remoteURL, err := getRemoteURLFromWorktree(worktreePath); err == nil {
		if err := s.subs.unsubscribe(ctx, remoteURL, request.SessionID, s.Logger); err != nil {
			s.Logger.Warn("Failed to unsubscribe from remote events",
				"error", err,
				"remote_url", remoteURL,
				"session_id", request.SessionID,
			)
		}
	}

	if err := killTmuxSession(worktreeName); err != nil {
		return nil, err
	}

	if err := removeGitWorktree(worktreePath); err != nil {
		return nil, err
	}

	if err := deleteGitBranch(commonGitDir, request.SessionID); err != nil {
		return nil, err
	}

	return &schemas.TaskResult{SessionID: request.SessionID, WorktreePath: worktreePath}, nil
}

func resolveDeleteWorktreePath(worktreeRoot string, request schemas.SessionDeleteRequest) (string, error) {
	if request.Path != "" {
		worktreePath := filepath.Clean(request.Path)
		if _, err := os.Stat(worktreePath); err != nil {
			if os.IsNotExist(err) {
				return "", os.ErrNotExist
			}
			return "", fmt.Errorf("failed to read worktree")
		}
		return worktreePath, nil
	}

	matchedPath, err := findWorktreeBySessionID(worktreeRoot, request.SessionID)
	if err != nil {
		return "", os.ErrNotExist
	}
	return matchedPath, nil
}
