package auth

import (
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNewReturnsStore(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if store == nil {
		t.Fatalf("expected store")
	}
}

func TestDefaultReturnsStore(t *testing.T) {
	store, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	if store == nil {
		t.Fatalf("expected store")
	}
}

func TestGitHubUsesEnvTokenFirst(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-token")

	originalLookPath := lookPath
	originalExecCommand := execCommand
	lookPath = func(file string) (string, error) {
		t.Fatalf("unexpected lookPath call for %q", file)
		return "", nil
	}
	execCommand = func(name string, args ...string) *exec.Cmd {
		t.Fatalf("unexpected execCommand call for %q", name)
		return nil
	}
	t.Cleanup(func() {
		lookPath = originalLookPath
		execCommand = originalExecCommand
	})

	store, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}

	githubAuth, ok := store.GitHub()
	if !ok || githubAuth == nil {
		t.Fatalf("expected github auth")
	}
	if githubAuth.AccessToken != "env-token" {
		t.Fatalf("AccessToken = %q, want %q", githubAuth.AccessToken, "env-token")
	}
}

func TestGitHubFallsBackToGhAuthToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	originalLookPath := lookPath
	originalExecCommand := execCommand
	lookPath = func(file string) (string, error) {
		if file != "gh" {
			t.Fatalf("lookPath file = %q, want %q", file, "gh")
		}
		return "/usr/bin/gh", nil
	}
	execCommand = func(name string, args ...string) *exec.Cmd {
		if name != "gh" {
			t.Fatalf("command name = %q, want %q", name, "gh")
		}
		if len(args) != 2 || args[0] != "auth" || args[1] != "token" {
			t.Fatalf("command args = %q, want [auth token]", args)
		}
		return exec.Command("sh", "-c", "printf 'gh-token\n'")
	}
	t.Cleanup(func() {
		lookPath = originalLookPath
		execCommand = originalExecCommand
	})

	store, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}

	githubAuth, ok := store.GitHub()
	if !ok || githubAuth == nil {
		t.Fatalf("expected github auth")
	}
	if githubAuth.AccessToken != "gh-token" {
		t.Fatalf("AccessToken = %q, want %q", githubAuth.AccessToken, "gh-token")
	}
}

func TestGitHubReturnsEmptyWhenGhMissing(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	originalLookPath := lookPath
	originalExecCommand := execCommand
	lookPath = func(file string) (string, error) {
		return "", errors.New("missing")
	}
	execCommand = func(name string, args ...string) *exec.Cmd {
		t.Fatalf("unexpected execCommand call for %q", name)
		return nil
	}
	t.Cleanup(func() {
		lookPath = originalLookPath
		execCommand = originalExecCommand
	})

	store, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}

	githubAuth, ok := store.GitHub()
	if ok || githubAuth != nil {
		t.Fatalf("expected no github auth, got ok=%v auth=%+v", ok, githubAuth)
	}
}

func TestGitHubReturnsEmptyWhenGhAuthTokenFails(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	originalLookPath := lookPath
	originalExecCommand := execCommand
	lookPath = func(file string) (string, error) {
		return "/usr/bin/gh", nil
	}
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("sh", "-c", "exit 1")
	}
	t.Cleanup(func() {
		lookPath = originalLookPath
		execCommand = originalExecCommand
	})

	store, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}

	githubAuth, ok := store.GitHub()
	if ok || githubAuth != nil {
		t.Fatalf("expected no github auth, got ok=%v auth=%+v", ok, githubAuth)
	}
}
