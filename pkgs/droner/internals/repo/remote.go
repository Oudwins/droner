package repo

import (
	"fmt"
	"strings"
)

// GetRemoteURL returns the git remote URL for `origin`.
//
// If the repo has no `origin` remote configured, it returns "" and a nil error.
func GetRemoteURL(repoPath string) (string, error) {
	cmd := execCommand("git", "-C", repoPath, "remote", "get-url", "origin")
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if strings.Contains(msg, "No such remote") || strings.Contains(msg, "No remote") {
			return "", nil
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git remote get-url origin failed: %s", msg)
	}

	return strings.TrimSpace(string(output)), nil
}
