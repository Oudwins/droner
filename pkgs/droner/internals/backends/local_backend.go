package backends

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type commandFunc func(name string, args ...string) *exec.Cmd

var execCommand commandFunc = exec.Command

func (l LocalBackend) WorktreePath(repoPath string, sessionID string) (string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return "", errors.New("session id is required")
	}
	repoName := filepath.Base(repoPath)
	worktreeName := fmt.Sprintf("%s..%s", repoName, sessionID)
	return filepath.Join(l.worktreeRoot, worktreeName), nil
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

func (l LocalBackend) CreateSession(_ context.Context, repoPath string, worktreePath string, sessionID string, agentConfig AgentConfig) error {
	if err := os.MkdirAll(l.worktreeRoot, 0o755); err != nil {
		return fmt.Errorf("failed to create worktree root: %w", err)
	}
	if err := l.createGitWorktree(repoPath, worktreePath, sessionID); err != nil {
		return err
	}
	if err := l.createTmuxSession(sessionID, worktreePath, agentConfig.Model, agentConfig.Prompt); err != nil {
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

func (l LocalBackend) createTmuxSession(sessionName string, worktreePath string, model string, prompt string) error {
	newSession := execCommand("tmux", "new-session", "-d", "-s", sessionName, "-n", "nvim", "-c", worktreePath, "nvim")
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

	newOpencode := execCommand("tmux", opencodeArgs...)
	if output, err := newOpencode.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create tmux opencode window: %s", strings.TrimSpace(string(output)))
	}

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
