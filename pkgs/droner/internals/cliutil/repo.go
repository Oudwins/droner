package cliutil

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func RepoRootFromCwd() (string, error) {
	return RepoRootFromPath(".")
}

func RepoRootFromPath(path string) (string, error) {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to determine repo root: %s", strings.TrimSpace(string(output)))
	}
	root := strings.TrimSpace(string(output))
	if root == "" {
		return "", errors.New("failed to determine repo root")
	}
	return root, nil
}
