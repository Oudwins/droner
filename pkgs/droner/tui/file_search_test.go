package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadRepoFileCandidatesListsTrackedFilesOnly(t *testing.T) {
	repoRoot := t.TempDir()
	runGit(t, repoRoot, "init", "-b", "main")

	writeTestFile(t, filepath.Join(repoRoot, ".gitignore"), "ignored.log\n")
	writeTestFile(t, filepath.Join(repoRoot, "committed.txt"), "committed\n")
	writeTestFile(t, filepath.Join(repoRoot, "edited.txt"), "before\n")
	runGit(t, repoRoot, "add", ".gitignore", "committed.txt", "edited.txt")
	runGit(t, repoRoot, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "initial commit")

	writeTestFile(t, filepath.Join(repoRoot, "edited.txt"), "after\n")
	writeTestFile(t, filepath.Join(repoRoot, "untracked.txt"), "untracked\n")
	writeTestFile(t, filepath.Join(repoRoot, "ignored.log"), "ignored\n")

	paths, err := loadRepoFileCandidates(repoRoot)
	if err != nil {
		t.Fatalf("loadRepoFileCandidates: %v", err)
	}

	want := []string{".gitignore", "committed.txt", "edited.txt"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func TestLoadRepoFileCandidatesFallsBackToRepoWalk(t *testing.T) {
	repoRoot := t.TempDir()
	writeTestFile(t, filepath.Join(repoRoot, "visible.txt"), "visible\n")
	writeTestFile(t, filepath.Join(repoRoot, "nested", "child.txt"), "child\n")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll .git: %v", err)
	}
	writeTestFile(t, filepath.Join(repoRoot, ".git", "config"), "[core]\n")

	paths, err := loadRepoFileCandidates(repoRoot)
	if err != nil {
		t.Fatalf("loadRepoFileCandidates: %v", err)
	}

	want := []string{"nested/child.txt", "visible.txt"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %q: %v", path, err)
	}
}

func runGit(t *testing.T, repoRoot string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, output)
	}
}
