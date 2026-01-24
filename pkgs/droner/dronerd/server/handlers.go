package server

import (
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

	"droner/internals/conf"
	"droner/internals/schemas"

	z "github.com/Oudwins/zog"
)

func (s *Server) HandlerVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(s.Config.VERSION))
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

	worktreeRoot, _ := expandPath(s.Config.WORKTREE_DIR)
	if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Failed to create worktree root", nil), Render.Status(http.StatusInternalServerError))
		return
	}

	baseName := filepath.Base(repoPath)
	if request.SessionID != "" {
		worktreePath := filepath.Join(worktreeRoot, baseName+"#"+request.SessionID)
		if _, err := os.Stat(worktreePath); err == nil {

			response := schemas.SessionCreateResponse{WorktreePath: worktreePath, SessionID: request.SessionID}

			RenderJSON(w, r, response)
			return
		}
	}

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

	if err := createGitWorktree(request.SessionID, repoPath, worktreePath); err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}

	if err := runWorktreeSetup(repoPath, worktreePath); err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}

	if err := createTmuxSession(worktreeName, worktreePath, request.Agent.Model, request.Agent.Prompt); err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}
	response := schemas.SessionCreateResponse{WorktreePath: worktreePath, SessionID: request.SessionID}
	RenderJSON(w, r, response)
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

	worktreeRoot, err := expandPath(s.Config.WORKTREE_DIR)
	if err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Worktree Root doesn't exist", nil), Render.Status(http.StatusInternalServerError))
		return
	}

	var worktreePath string
	if reqbody.Path != "" {
		worktreePath = filepath.Clean(reqbody.Path)
		if _, err := os.Stat(worktreePath); err != nil {
			if os.IsNotExist(err) {
				RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeNotFound, "Couldn't find the worktree", nil), Render.Status(http.StatusNotFound))
				return
			}
			RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, "Faild to read worktree", nil), Render.Status(http.StatusInternalServerError))
			return
		}
	} else {
		matchedPath, err := findWorktreeBySessionID(worktreeRoot, reqbody.SessionID)
		if err != nil {
			RenderJSON(w, r, JsonResponseError(JsonResponseErrorCodeNotFound, "Couldn't find the session", nil), Render.Status(http.StatusNotFound))
			return
		}
		worktreePath = matchedPath
	}

	worktreeName := filepath.Base(worktreePath)
	if reqbody.SessionID == "" {
		reqbody.SessionID = sessionIDFromName(worktreeName)
	}

	commonGitDir, err := gitCommonDirFromWorktree(worktreePath)
	if err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}

	if err := killTmuxSession(worktreeName); err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}

	if err := removeGitWorktree(worktreePath); err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}

	if err := deleteGitBranch(commonGitDir, reqbody.SessionID); err != nil {
		RenderJSON(w, r, JsonResponseError(JsonResponseErroCodeInternal, err.Error(), nil), Render.Status(http.StatusInternalServerError))
		return
	}

	RenderJSON(w, r, schemas.SessionDeleteResponse{WorktreePath: worktreePath, SessionID: reqbody.SessionID})
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
