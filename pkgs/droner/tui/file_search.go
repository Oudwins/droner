package tui

import (
	"bufio"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func loadRepoFileCandidates(repoRoot string) ([]string, error) {
	paths, err := gitTrackedAndUntrackedFiles(repoRoot)
	if err == nil {
		return paths, nil
	}
	return walkRepoFiles(repoRoot)
}

func gitTrackedAndUntrackedFiles(repoRoot string) ([]string, error) {
	cmd := exec.Command("git", "-C", repoRoot, "ls-files", "--cached", "--others", "--exclude-standard")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	paths := make([]string, 0)
	seen := make(map[string]struct{})
	for scanner.Scan() {
		path := strings.TrimSpace(scanner.Text())
		if path == "" {
			continue
		}
		path = filepath.ToSlash(filepath.Clean(path))
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func walkRepoFiles(repoRoot string) ([]string, error) {
	paths := make([]string, 0)
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func fileRefToken(path string) string {
	return "@" + filepath.ToSlash(filepath.Clean(path))
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
