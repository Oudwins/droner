package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TempRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	run(t, root, "git", "init")
	run(t, root, "git", "config", "user.email", "test@example.com")
	run(t, root, "git", "config", "user.name", "Test User")
	readme := filepath.Join(root, "README.md")
	if err := os.WriteFile(readme, []byte("test"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run(t, root, "git", "add", "README.md")
	run(t, root, "git", "commit", "-m", "init")
	return root
}

func TempWorktreeRoot(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func TempDBPath(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	return filepath.Join(root, "tasks.db")
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, string(output))
	}
}
