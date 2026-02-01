package workspace

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGitIsInsideWorkTreeCommand(t *testing.T) {
	original := execCommand
	t.Cleanup(func() {
		execCommand = original
	})

	var gotName string
	var gotArgs []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return exec.Command("sh", "-c", "printf true")
	}

	host := NewLocalHost()
	repoPath := "/tmp/repo"
	if err := host.GitIsInsideWorkTree(repoPath); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if gotName != "git" {
		t.Fatalf("expected exec name git, got %s", gotName)
	}
	expectedArgs := []string{"-C", repoPath, "rev-parse", "--is-inside-work-tree"}
	if !reflect.DeepEqual(gotArgs, expectedArgs) {
		t.Fatalf("expected args %v, got %v", expectedArgs, gotArgs)
	}
}

func TestGitCommonDirFromWorktreeRelative(t *testing.T) {
	original := execCommand
	t.Cleanup(func() {
		execCommand = original
	})

	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("sh", "-c", "printf .git")
	}

	host := NewLocalHost()
	worktreePath := "/tmp/worktree"
	got, err := host.GitCommonDirFromWorktree(worktreePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := filepath.Join(worktreePath, ".git")
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}
