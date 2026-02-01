package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type LocalHost struct{}

func NewLocalHost() *LocalHost {
	return &LocalHost{}
}

var _ Host = (*LocalHost)(nil)

type commandFunc func(name string, args ...string) *exec.Cmd

var execCommand commandFunc = exec.Command

func (l *LocalHost) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

func (l *LocalHost) ReadDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}

func (l *LocalHost) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (l *LocalHost) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (l *LocalHost) GitIsInsideWorkTree(repoPath string) error {
	cmd := execCommand("git", "-C", repoPath, "rev-parse", "--is-inside-work-tree")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git check failed: %s", strings.TrimSpace(string(output)))
	}
	if strings.TrimSpace(string(output)) != "true" {
		return fmt.Errorf("not a git worktree")
	}
	return nil
}

func (l *LocalHost) CreateGitWorktree(repoPath string, worktreePath string, branchName string) error {
	cmd := execCommand("git", "-C", repoPath, "worktree", "add", "-b", branchName, worktreePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create worktree: %s: %s", err.Error(), strings.TrimSpace(string(output)))
	}
	return nil
}

func (l *LocalHost) RemoveGitWorktree(worktreePath string) error {
	cmd := execCommand("git", "-C", worktreePath, "worktree", "remove", "--force", worktreePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to remove worktree: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (l *LocalHost) GitCommonDirFromWorktree(worktreePath string) (string, error) {
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

func (l *LocalHost) DeleteGitBranch(commonGitDir string, sessionID string) error {
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

func (l *LocalHost) GetRemoteURL(repoPath string) (string, error) {
	cmd := execCommand("git", "-C", repoPath, "remote", "get-url", "origin")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get origin URL: %s", strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func (l *LocalHost) GetRemoteURLFromWorktree(worktreePath string) (string, error) {
	commonDir, err := l.GitCommonDirFromWorktree(worktreePath)
	if err != nil {
		return "", err
	}
	repoPath := filepath.Dir(commonDir)
	return l.GetRemoteURL(repoPath)
}

type worktreeConfig struct {
	SetupWorktree []string `json:"setup-worktree"`
}

func (l *LocalHost) RunWorktreeSetup(repoPath string, worktreePath string) error {
	configPath := filepath.Join(repoPath, ".cursor", "worktrees.json")
	if _, err := l.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read worktree config")
	}

	data, err := l.ReadFile(configPath)
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
		cmd := execCommand("sh", "-c", command)
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

func (l *LocalHost) CreateTmuxSession(sessionName string, worktreePath string, model string, prompt string) error {
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

func (l *LocalHost) KillTmuxSession(sessionName string) error {
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
