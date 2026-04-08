package cliutil

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
)

type SessionTarget struct {
	RepoPath string
	Branch   string
}

func ResolveSessionTarget(raw string) (SessionTarget, error) {
	repoToken, branch, err := parseRepoLocator(raw)
	if err != nil {
		return SessionTarget{}, err
	}

	if repoToken == "" {
		repoPath, err := RepoRootFromCwd()
		if err != nil {
			return SessionTarget{}, err
		}
		return SessionTarget{RepoPath: repoPath, Branch: branch}, nil
	}

	repoPath, err := resolveProjectRepo(repoToken)
	if err != nil {
		return SessionTarget{}, err
	}
	return SessionTarget{RepoPath: repoPath, Branch: branch}, nil
}

func parseRepoLocator(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", nil
	}

	idx := strings.LastIndex(raw, "@")
	if idx < 0 {
		return raw, "", nil
	}

	repoToken := strings.TrimSpace(raw[:idx])
	branch := strings.TrimSpace(raw[idx+1:])
	if repoToken == "" && branch == "" {
		return "", "", fmt.Errorf("invalid repo locator %q", raw)
	}
	return repoToken, branch, nil
}

func resolveProjectRepo(repoToken string) (string, error) {
	repoToken = strings.TrimSpace(repoToken)
	if repoToken == "" {
		return "", fmt.Errorf("repo name is required")
	}
	if repoToken != filepath.Base(repoToken) {
		return "", fmt.Errorf("repo name %q must not contain path separators", repoToken)
	}

	parentPaths := conf.GetConfig().Projects.ParentPaths
	if len(parentPaths) == 0 {
		return "", fmt.Errorf("config.projects.parentPaths must contain at least one directory")
	}

	matches := make([]string, 0, 1)
	for _, parentPath := range parentPaths {
		entries, err := os.ReadDir(parentPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", fmt.Errorf("read parent path %q: %w", parentPath, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() || entry.Name() != repoToken {
				continue
			}
			repoPath, err := RepoRootFromPath(filepath.Join(parentPath, entry.Name()))
			if err != nil {
				continue
			}
			matches = append(matches, repoPath)
		}
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("project %q not found under config.projects.parentPaths", repoToken)
	}

	sort.Strings(matches)
	matches = compactStrings(matches)
	if len(matches) > 1 {
		return "", fmt.Errorf("project %q is ambiguous: %s", repoToken, strings.Join(matches, ", "))
	}

	return matches[0], nil
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value == result[len(result)-1] {
			continue
		}
		result = append(result, value)
	}
	return result
}
