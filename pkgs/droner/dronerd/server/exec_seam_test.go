package server

import (
	"os/exec"
	"reflect"
	"testing"
)

func TestExecCommandSeam(t *testing.T) {
	t.Cleanup(func() {
		execCommand = exec.Command
	})

	var gotName string
	var gotArgs []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return exec.Command("sh", "-c", "printf true")
	}

	repoPath := "/tmp/repo"
	if err := gitIsInsideWorkTree(repoPath); err != nil {
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
