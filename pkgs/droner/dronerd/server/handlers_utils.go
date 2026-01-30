package server

import (
	"fmt"
	"path/filepath"
	"strings"
)

// getRemoteURL gets the origin remote URL from a git repository
func getRemoteURL(repoPath string) (string, error) {
	cmd := execCommand("git", "-C", repoPath, "remote", "get-url", "origin")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get origin URL: %s", strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

// getRemoteURLFromWorktree gets the origin remote URL from a worktree path
// by climbing to the git common dir and getting the remote URL from there
func getRemoteURLFromWorktree(worktreePath string) (string, error) {
	// Get the common git directory for this worktree
	cmd := execCommand("git", "-C", worktreePath, "rev-parse", "--git-common-dir")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get git common dir: %s", strings.TrimSpace(string(output)))
	}

	commonDir := strings.TrimSpace(string(output))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(worktreePath, commonDir)
	}

	// Get the repo path (commonDir is inside the repo)
	repoPath := filepath.Dir(commonDir)
	return getRemoteURL(repoPath)
}
