package backends

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
	"github.com/Oudwins/droner/pkgs/droner/internals/timeouts"
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

func tmuxSessionName(repoPath string, sessionID string) string {
	repoName := filepath.Base(repoPath)
	return fmt.Sprintf("%s#%s", repoName, sessionID)
}

func tmuxSessionNameFromWorktreePath(worktreePath string) string {
	worktreeName := filepath.Base(worktreePath)
	parts := strings.Split(worktreeName, "..")
	if len(parts) != 2 {
		return worktreeName
	}
	return fmt.Sprintf("%s#%s", parts[0], parts[1])
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

func (l LocalBackend) CreateSession(ctx context.Context, repoPath string, worktreePath string, sessionID string, agentConfig AgentConfig) (retErr error) {
	sessionName := tmuxSessionName(repoPath, sessionID)

	defer func() {
		if retErr == nil {
			return
		}

		// Best-effort cleanup. We prefer to leave no partial state behind.
		_ = l.killTmuxSession(sessionName)

		cleanRoot := filepath.Clean(l.config.WorktreeDir)
		cleanWorktree := filepath.Clean(worktreePath)
		if cleanRoot != "" && cleanWorktree != "" {
			if rel, err := filepath.Rel(cleanRoot, cleanWorktree); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				_ = l.removeGitWorktreeFromRepo(repoPath, cleanWorktree)
				_ = os.RemoveAll(cleanWorktree)
			}
		}

		if commonGitDir, err := l.gitCommonDirFromRepo(repoPath); err == nil {
			_ = l.deleteGitBranch(commonGitDir, sessionID)
		}
	}()

	if err := os.MkdirAll(l.config.WorktreeDir, 0o755); err != nil {
		return fmt.Errorf("failed to create worktree root: %w", err)
	}
	if l.db != nil {
		reused, err := l.tryReuseCompletedWorktree(ctx, repoPath, worktreePath, sessionID)
		if err != nil {
			return err
		}
		if !reused {
			if err := l.createGitWorktree(repoPath, worktreePath, sessionID); err != nil {
				return err
			}
		}
	} else {
		if err := l.createGitWorktree(repoPath, worktreePath, sessionID); err != nil {
			return err
		}
	}
	if err := l.createTmuxBaseSession(sessionName, worktreePath); err != nil {
		return err
	}
	opencodeConfig := agentConfig.Opencode
	if err := l.ensureOpencodeServer(ctx, worktreePath, opencodeConfig); err != nil {
		return err
	}
	opencodeSessionID, err := l.createOpencodeSession(ctx, opencodeConfig)
	if err != nil {
		return err
	}
	agentConfig.Opencode = opencodeConfig
	if err := l.createTmuxOpencodeWindow(sessionName, worktreePath, agentConfig, opencodeSessionID); err != nil {
		return err
	}
	if agentConfig.Message != nil && len(agentConfig.Message.Parts) > 0 {
		if err := l.sendOpencodeMessage(ctx, opencodeConfig, opencodeSessionID, agentConfig.Model, agentConfig.Message); err != nil {
			return err
		}
	}
	if err := l.createTmuxTerminalWindow(sessionName, worktreePath); err != nil {
		return err
	}
	return nil
}

func (l LocalBackend) gitCommonDirFromRepo(repoPath string) (string, error) {
	cmd := execCommand("git", "-C", repoPath, "rev-parse", "--git-common-dir")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to determine git common dir: %s", strings.TrimSpace(string(output)))
	}
	commonDir := strings.TrimSpace(string(output))
	if commonDir == "" {
		return "", errors.New("failed to determine git common dir")
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(repoPath, commonDir)
	}
	return commonDir, nil
}

func (l LocalBackend) removeGitWorktreeFromRepo(repoPath string, worktreePath string) error {
	cmd := execCommand("git", "-C", repoPath, "worktree", "remove", "--force", worktreePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to remove worktree: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) DeleteSession(_ context.Context, worktreePath string, sessionID string) error {
	sessionName := tmuxSessionNameFromWorktreePath(worktreePath)
	commonGitDir, err := l.gitCommonDirFromWorktree(worktreePath)
	if err != nil {
		return err
	}
	if err := l.killTmuxSession(sessionName); err != nil {
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

func (l LocalBackend) CompleteSession(_ context.Context, worktreePath string, sessionID string) error {
	sessionName := ""
	if strings.TrimSpace(worktreePath) != "" {
		sessionName = tmuxSessionNameFromWorktreePath(worktreePath)
	}
	if strings.TrimSpace(sessionName) == "" {
		sessionName = strings.TrimSpace(sessionID)
	}
	if sessionName == "" {
		return nil
	}
	return l.killTmuxSession(sessionName)
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

func (l LocalBackend) ensureOpencodeServer(ctx context.Context, worktreePath string, config conf.OpenCodeConfig) error {
	if l.opencodeHealthy(ctx, config) {
		return nil
	}

	cmd := execCommand(
		"opencode",
		"serve",
		"--hostname",
		config.Hostname,
		"--port",
		fmt.Sprintf("%d", config.Port),
	)

	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		cmd.Dir = home
	} else {
		cmd.Dir = worktreePath
	}

	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.Env = os.Environ()

	// Detach from any controlling terminal/session (e.g. tmux) so the server
	// keeps running even if the tmux session is killed.
	detachCmd(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start opencode server: %w", err)
	}
	_ = cmd.Process.Release()

	return l.waitForOpencode(ctx, config, timeouts.SecondLong)
}

func (l LocalBackend) opencodeHealthy(ctx context.Context, config conf.OpenCodeConfig) bool {
	client := &http.Client{Timeout: timeouts.SecondShort}
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
		if err := ctx.Err(); err != nil {
			return err
		}
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
	NoReply bool                   `json:"noReply,omitempty"`
	Parts   []messages.MessagePart `json:"parts"`
	Model   *opencodeModel         `json:"model,omitempty"`
}

func (l LocalBackend) createOpencodeSession(ctx context.Context, config conf.OpenCodeConfig) (string, error) {
	url := fmt.Sprintf("http://%s:%d/session", config.Hostname, config.Port)
	client := &http.Client{Timeout: timeouts.SecondLong}
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
	client := &http.Client{Timeout: timeouts.SecondLong}
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
		if body := readHTTPResponseBody(resp.Body); body != "" {
			return fmt.Errorf("unexpected opencode message status: %s: %s", resp.Status, body)
		}
		return fmt.Errorf("unexpected opencode message status: %s", resp.Status)
	}
	return nil
}

func (l LocalBackend) sendOpencodeMessage(ctx context.Context, config conf.OpenCodeConfig, sessionID string, model string, message *messages.Message) error {
	if message == nil || len(message.Parts) == 0 {
		return nil
	}
	url := fmt.Sprintf("http://%s:%d/session/%s/message", config.Hostname, config.Port, sessionID)
	request := opencodeMessageRequest{Parts: message.Parts}
	if parsed := parseOpencodeModel(model); parsed != nil {
		request.Model = parsed
	}
	body, err := json.Marshal(request)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: timeouts.SecondLong}
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
		if body := readHTTPResponseBody(resp.Body); body != "" {
			return fmt.Errorf("unexpected opencode message status: %s: %s", resp.Status, body)
		}
		return fmt.Errorf("unexpected opencode message status: %s", resp.Status)
	}
	return nil
}

func readHTTPResponseBody(r io.Reader) string {
	if r == nil {
		return ""
	}
	const maxBytes = 8 * 1024
	b, err := io.ReadAll(io.LimitReader(r, maxBytes))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func parseOpencodeModel(raw string) *opencodeModel {
	parts := strings.SplitN(strings.TrimSpace(raw), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil
	}
	return &opencodeModel{ProviderID: parts[0], ModelID: parts[1]}
}

func (l LocalBackend) tryReuseCompletedWorktree(ctx context.Context, repoPath string, worktreePath string, sessionID string) (bool, error) {
	completed, err := l.db.ListSessionsByStatus(ctx, db.SessionStatusCompleted)
	if err != nil {
		return false, fmt.Errorf("failed to list completed sessions: %w", err)
	}
	if len(completed) == 0 {
		return false, nil
	}

	cleanRepoPath := filepath.Clean(repoPath)
	cleanTarget := filepath.Clean(worktreePath)
	cleanRoot := filepath.Clean(l.config.WorktreeDir)

	for _, session := range completed {
		if session.BackendID != conf.BackendLocal.String() {
			continue
		}
		if filepath.Clean(session.RepoPath) != cleanRepoPath {
			continue
		}
		oldWorktreePath := filepath.Clean(session.WorktreePath)
		if oldWorktreePath == "" || oldWorktreePath == cleanTarget {
			continue
		}
		rel, relErr := filepath.Rel(cleanRoot, oldWorktreePath)
		if relErr != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		info, statErr := os.Stat(oldWorktreePath)
		if statErr != nil || !info.IsDir() {
			continue
		}

		_ = l.killTmuxSession(session.SimpleID)
		if err := l.resetAndCleanWorktree(oldWorktreePath); err != nil {
			continue
		}
		if err := l.moveGitWorktree(cleanRepoPath, oldWorktreePath, cleanTarget); err != nil {
			continue
		}
		baseRef, err := l.resolveBaseRef(cleanRepoPath)
		if err != nil {
			return false, err
		}
		if err := l.checkoutNewBranch(cleanTarget, sessionID, baseRef); err != nil {
			return false, err
		}

		commonGitDir, err := l.gitCommonDirFromWorktree(cleanTarget)
		if err != nil {
			return false, err
		}
		if err := l.deleteGitBranch(commonGitDir, session.SimpleID); err != nil {
			return false, err
		}
		_, _ = l.db.UpdateSessionStatusBySimpleID(ctx, db.UpdateSessionStatusBySimpleIDParams{
			SimpleID: session.SimpleID,
			Status:   db.SessionStatusDeleted,
			Error:    sql.NullString{},
		})
		return true, nil
	}

	return false, nil
}

func (l LocalBackend) moveGitWorktree(repoPath string, fromPath string, toPath string) error {
	cmd := execCommand("git", "-C", repoPath, "worktree", "move", fromPath, toPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to move worktree: %s: %s", err.Error(), strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) resetAndCleanWorktree(worktreePath string) error {
	reset := execCommand("git", "-C", worktreePath, "reset", "--hard")
	if output, err := reset.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to reset worktree: %s: %s", err.Error(), strings.TrimSpace(string(output)))
	}
	clean := execCommand("git", "-C", worktreePath, "clean", "-ffd")
	if output, err := clean.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to clean worktree: %s: %s", err.Error(), strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) checkoutNewBranch(worktreePath string, branchName string, baseRef string) error {
	cmd := execCommand("git", "-C", worktreePath, "checkout", "-B", branchName, baseRef)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to checkout branch: %s: %s", err.Error(), strings.TrimSpace(string(output)))
	}
	return nil
}

func (l LocalBackend) resolveBaseRef(repoPath string) (string, error) {
	symbolic := execCommand("git", "-C", repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
	if output, err := symbolic.CombinedOutput(); err == nil {
		ref := strings.TrimSpace(string(output))
		if ref != "" {
			return ref, nil
		}
	}
	for _, ref := range []string{
		"refs/remotes/origin/main",
		"refs/remotes/origin/master",
		"refs/heads/main",
		"refs/heads/master",
	} {
		check := execCommand("git", "-C", repoPath, "show-ref", "--verify", "--quiet", ref)
		if err := check.Run(); err == nil {
			return ref, nil
		}
	}
	return "HEAD", nil
}
