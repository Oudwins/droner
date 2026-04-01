package backends

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const cursorWorktreeConfigPath = ".cursor/worktrees.json"

type cursorWorktreeConfig struct {
	SetupWorktree []string `json:"setup-worktree"`
}

func loadCursorWorktreeConfig(repoPath string) (cursorWorktreeConfig, error) {
	configPath := filepath.Join(repoPath, cursorWorktreeConfigPath)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cursorWorktreeConfig{}, nil
		}
		return cursorWorktreeConfig{}, fmt.Errorf("failed to read cursor worktree config: %w", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return cursorWorktreeConfig{}, nil
	}

	var config cursorWorktreeConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return cursorWorktreeConfig{}, fmt.Errorf("failed to parse cursor worktree config: %w", err)
	}
	return config, nil
}

func (l LocalBackend) runCursorWorktreeSetup(repoPath string, worktreePath string, sessionID string) error {
	config, err := loadCursorWorktreeConfig(repoPath)
	if err != nil {
		return err
	}

	env := append(os.Environ(),
		"ROOT_WORKTREE_PATH="+filepath.Clean(repoPath),
		"WORKTREE_PATH="+filepath.Clean(worktreePath),
		"SESSION_ID="+sessionID,
	)
	for _, rawCommand := range config.SetupWorktree {
		command := strings.TrimSpace(rawCommand)
		if command == "" {
			continue
		}

		cmd := execCommand("sh", "-lc", command)
		cmd.Dir = worktreePath
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		if err != nil {
			trimmedOutput := strings.TrimSpace(string(output))
			if trimmedOutput == "" {
				return fmt.Errorf("cursor setup-worktree command failed: %q: %w", command, err)
			}
			return fmt.Errorf("cursor setup-worktree command failed: %q: %w: %s", command, err, trimmedOutput)
		}
	}

	return nil
}
