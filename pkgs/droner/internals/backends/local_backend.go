package backends

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
)

type commandFunc func(name string, args ...string) *exec.Cmd

var execCommand commandFunc = exec.Command

func (l LocalBackend) WorktreePath(repoPath string, sessionID string) (string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return "", errors.New("session id is required")
	}
	repoName := filepath.Base(repoPath)
	worktreeName := fmt.Sprintf("%s..%s", repoName, sessionID)
	return filepath.Join(l.config.WorktreeDir, worktreeName), nil
}

func (l LocalBackend) ValidateSessionID(repoPath string, sessionID string) error {
	worktreePath, err := l.WorktreePath(repoPath, sessionID)
	if err != nil {
		return err
	}
	if _, err := os.Stat(worktreePath); err == nil {
		return errors.New("session folder already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat worktree path: %w", err)
	}
	return nil
}

func (l LocalBackend) CreateSession(ctx context.Context, repoPath string, worktreePath string, sessionID string, agentConfig AgentConfig) error {
	if err := os.MkdirAll(l.config.WorktreeDir, 0o755); err != nil {
		return fmt.Errorf("failed to create worktree root: %w", err)
	}
	if err := l.createGitWorktree(repoPath, worktreePath, sessionID); err != nil {
		return err
	}
	if err := l.createTmuxBaseSession(sessionID, worktreePath); err != nil {
		return err
	}
	opencodeConfig := agentConfig.Opencode
	if err := l.ensureOpencodeServer(ctx, sessionID, worktreePath, opencodeConfig); err != nil {
		return err
	}
	opencodeSessionID, err := l.createOpencodeSession(ctx, opencodeConfig)
	if err != nil {
		return err
	}
	if agentConfig.Message != nil && len(agentConfig.Message.Parts) > 0 {
		if err := l.seedOpencodeMessage(ctx, opencodeConfig, opencodeSessionID, agentConfig.Model, agentConfig.Message); err != nil {
			return err
		}
	}
	agentConfig.Opencode = opencodeConfig
	if err := l.createTmuxOpencodeWindow(sessionID, worktreePath, agentConfig, opencodeSessionID); err != nil {
		return err
	}
	if err := l.createTmuxTerminalWindow(sessionID, worktreePath); err != nil {
		return err
	}
	return nil
}

func (l LocalBackend) DeleteSession(_ context.Context, worktreePath string, sessionID string) error {
	commonGitDir, err := l.gitCommonDirFromWorktree(worktreePath)
	if err != nil {
		return err
	}
	if err := l.killTmuxSession(sessionID); err != nil {
		return err
	}
	if err := l.removeGitWorktree(worktreePath); err != nil {
		return err
	}
	if err := l.deleteGitBranch(commonGitDir, sessionID); err != nil {
		return err
	}
	return nil
}

func (l LocalBackend) createGitWorktree(repoPath string, worktreePath string, branchName string) error {
	cmd := execCommand("git", "-C", repoPath, "worktree", "add", "-b", branchName, worktreePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create worktree: %s: %s", err.Error(), strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) removeGitWorktree(worktreePath string) error {
	cmd := execCommand("git", "-C", worktreePath, "worktree", "remove", "--force", worktreePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to remove worktree: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) gitCommonDirFromWorktree(worktreePath string) (string, error) {
	cmd := execCommand("git", "-C", worktreePath, "rev-parse", "--git-common-dir")
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

func (l LocalBackend) deleteGitBranch(commonGitDir string, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	check := execCommand("git", "--git-dir", commonGitDir, "show-ref", "--verify", "--quiet", "refs/heads/"+sessionID)
	if err := check.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil
		}
		return fmt.Errorf("failed to check branch: %w", err)
	}
	cmd := execCommand("git", "--git-dir", commonGitDir, "branch", "-D", sessionID)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to delete branch: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) createTmuxBaseSession(sessionName string, worktreePath string) error {
	newSession := execCommand("tmux", "new-session", "-d", "-s", sessionName, "-n", "nvim", "-c", worktreePath, "nvim")
	if output, err := newSession.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create tmux session: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) createTmuxOpencodeWindow(sessionName string, worktreePath string, agentConfig AgentConfig, opencodeSessionID string) error {
	opencodeURL := fmt.Sprintf("http://%s:%d", agentConfig.Opencode.Hostname, agentConfig.Opencode.Port)
	opencodeArgs := []string{"new-window", "-t", sessionName, "-n", "opencode", "-c", worktreePath, "opencode", "attach", opencodeURL, "--session", opencodeSessionID}
	newOpencode := execCommand("tmux", opencodeArgs...)
	if output, err := newOpencode.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create tmux opencode window: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) createTmuxTerminalWindow(sessionName string, worktreePath string) error {
	newTerminal := execCommand("tmux", "new-window", "-t", sessionName, "-n", "terminal", "-c", worktreePath)
	if output, err := newTerminal.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create tmux terminal window: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) killTmuxSession(sessionName string) error {
	check := execCommand("tmux", "has-session", "-t", sessionName)
	if err := check.Run(); err != nil {
		return nil
	}
	cmd := execCommand("tmux", "kill-session", "-t", sessionName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to kill tmux session: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) ensureOpencodeServer(ctx context.Context, sessionName string, worktreePath string, config conf.OpenCodeConfig) error {
	if l.opencodeHealthy(ctx, config) {
		return nil
	}
	serveArgs := []string{"new-window", "-t", sessionName, "-n", "opencode-server", "-c", worktreePath, "opencode", "serve", "--hostname", config.Hostname, "--port", fmt.Sprintf("%d", config.Port)}
	startServer := execCommand("tmux", serveArgs...)
	if output, err := startServer.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start opencode server: %s", strings.TrimSpace(string(output)))
	}
	return l.waitForOpencode(ctx, config, 20*time.Second)
}

func (l LocalBackend) opencodeHealthy(ctx context.Context, config conf.OpenCodeConfig) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://%s:%d/global/health", config.Hostname, config.Port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (l LocalBackend) waitForOpencode(ctx context.Context, config conf.OpenCodeConfig, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if l.opencodeHealthy(ctx, config) {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for opencode server at %s:%d", config.Hostname, config.Port)
}

type opencodeSessionResponse struct {
	ID string `json:"id"`
}

type opencodeModel struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

type opencodeMessageRequest struct {
	NoReply bool                   `json:"noReply"`
	Parts   []messages.MessagePart `json:"parts"`
	Model   *opencodeModel         `json:"model,omitempty"`
}

func (l LocalBackend) createOpencodeSession(ctx context.Context, config conf.OpenCodeConfig) (string, error) {
	url := fmt.Sprintf("http://%s:%d/session", config.Hostname, config.Port)
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("unexpected opencode session status: %s", resp.Status)
	}
	var payload opencodeSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.ID) == "" {
		return "", errors.New("opencode session id missing from response")
	}
	return payload.ID, nil
}

func (l LocalBackend) seedOpencodeMessage(ctx context.Context, config conf.OpenCodeConfig, sessionID string, model string, message *messages.Message) error {
	if message == nil || len(message.Parts) == 0 {
		return nil
	}
	url := fmt.Sprintf("http://%s:%d/session/%s/message", config.Hostname, config.Port, sessionID)
	request := opencodeMessageRequest{NoReply: true, Parts: message.Parts}
	if parsed := parseOpencodeModel(model); parsed != nil {
		request.Model = parsed
	}
	body, err := json.Marshal(request)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected opencode message status: %s", resp.Status)
	}
	return nil
}

func parseOpencodeModel(raw string) *opencodeModel {
	parts := strings.SplitN(strings.TrimSpace(raw), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil
	}
	return &opencodeModel{ProviderID: parts[0], ModelID: parts[1]}
}
