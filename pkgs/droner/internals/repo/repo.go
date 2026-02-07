package repo

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type commandFunc func(name string, args ...string) *exec.Cmd

var execCommand commandFunc = exec.Command

func CheckRepo(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory")
	}
	cmd := execCommand("git", "-C", path, "rev-parse", "--is-inside-work-tree")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git check failed: %s", strings.TrimSpace(string(output)))
	}
	if strings.TrimSpace(string(output)) != "true" {
		return fmt.Errorf("not a git worktree")
	}
	return nil
}
