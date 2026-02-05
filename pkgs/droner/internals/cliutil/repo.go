package cliutil

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func RepoRootFromCwd() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
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
