package backends

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
)

func TestLocalBackendHydrateSessionReturnsRunningWhenTmuxSessionAlreadyExists(t *testing.T) {
	origExec := execCommand
	t.Cleanup(func() { execCommand = origExec })

	var calls []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "tmux" && len(args) > 0 && args[0] == "has-session" {
			return exec.Command("sh", "-c", "exit 0")
		}
		return exec.Command("sh", "-c", "exit 0")
	}

	backend := LocalBackend{config: &conf.LocalBackendConfig{WorktreeDir: t.TempDir()}}
	result, err := backend.HydrateSession(context.Background(), db.Session{
		SimpleID:     "sid",
		RepoPath:     filepath.Join(t.TempDir(), "repo"),
		WorktreePath: filepath.Join(t.TempDir(), "worktree"),
	}, AgentConfig{})
	if err != nil {
		t.Fatalf("HydrateSession: %v", err)
	}
	if result.Status != db.SessionStatusRunning {
		t.Fatalf("status = %s, want %s", result.Status, db.SessionStatusRunning)
	}
	if len(calls) != 1 || !strings.Contains(calls[0], "tmux has-session") {
		t.Fatalf("expected only tmux has-session call, got %v", calls)
	}
}

func TestLocalBackendHydrateSessionReturnsDeletedWhenWorktreeMissing(t *testing.T) {
	origExec := execCommand
	t.Cleanup(func() { execCommand = origExec })

	execCommand = func(name string, args ...string) *exec.Cmd {
		if name == "tmux" && len(args) > 0 && args[0] == "has-session" {
			return exec.Command("sh", "-c", "exit 1")
		}
		return exec.Command("sh", "-c", "exit 0")
	}

	backend := LocalBackend{config: &conf.LocalBackendConfig{WorktreeDir: t.TempDir()}}
	result, err := backend.HydrateSession(context.Background(), db.Session{
		SimpleID:     "sid",
		RepoPath:     filepath.Join(t.TempDir(), "repo"),
		WorktreePath: filepath.Join(t.TempDir(), "missing"),
	}, AgentConfig{})
	if err != nil {
		t.Fatalf("HydrateSession: %v", err)
	}
	if result.Status != db.SessionStatusDeleted {
		t.Fatalf("status = %s, want %s", result.Status, db.SessionStatusDeleted)
	}
}

func TestLocalBackendHydrateSessionReturnsFailedWhenRuntimeRecreationFails(t *testing.T) {
	origExec := execCommand
	t.Cleanup(func() { execCommand = origExec })

	worktreePath := filepath.Join(t.TempDir(), "worktree")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}

	execCommand = func(name string, args ...string) *exec.Cmd {
		if name == "tmux" && len(args) > 0 {
			switch args[0] {
			case "has-session":
				return exec.Command("sh", "-c", "exit 1")
			case "new-session":
				return exec.Command("sh", "-c", "exit 1")
			default:
				return exec.Command("sh", "-c", "exit 0")
			}
		}
		return exec.Command("sh", "-c", "exit 0")
	}

	backend := LocalBackend{config: &conf.LocalBackendConfig{WorktreeDir: t.TempDir()}}
	result, err := backend.HydrateSession(context.Background(), db.Session{
		SimpleID:     "sid",
		RepoPath:     filepath.Join(t.TempDir(), "repo"),
		WorktreePath: worktreePath,
	}, AgentConfig{})
	if err != nil {
		t.Fatalf("HydrateSession: %v", err)
	}
	if result.Status != db.SessionStatusFailed {
		t.Fatalf("status = %s, want %s", result.Status, db.SessionStatusFailed)
	}
	if strings.TrimSpace(result.Error) == "" {
		t.Fatalf("expected failed hydration error message")
	}
}

func TestLocalBackendHydrateSessionRecreatesRuntimeWithoutReplayingPrompt(t *testing.T) {
	origExec := execCommand
	t.Cleanup(func() { execCommand = origExec })

	worktreePath := filepath.Join(t.TempDir(), "worktree")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}

	var calls []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name == "tmux" && len(args) > 0 && args[0] == "has-session" {
			return exec.Command("sh", "-c", "exit 1")
		}
		return exec.Command("sh", "-c", "exit 0")
	}

	messageCalled := false
	mux := http.NewServeMux()
	mux.HandleFunc("/global/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		if dir := r.URL.Query().Get("directory"); dir != worktreePath {
			t.Fatalf("directory = %q, want %q", dir, worktreePath)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	})
	mux.HandleFunc("/session/abc/message", func(w http.ResponseWriter, r *http.Request) {
		messageCalled = true
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	opencodeCfg := opencodeConfigFromServer(t, srv)

	backend := LocalBackend{config: &conf.LocalBackendConfig{WorktreeDir: t.TempDir()}}
	result, err := backend.HydrateSession(context.Background(), db.Session{
		SimpleID:     "sid",
		RepoPath:     filepath.Join(t.TempDir(), "repo"),
		WorktreePath: worktreePath,
		AgentConfig:  sql.NullString{String: `{"model":"openai/gpt-5-mini"}`, Valid: true},
	}, AgentConfig{
		Model:    "openai/gpt-5-mini",
		Message:  &messages.Message{Parts: []messages.MessagePart{messages.NewTextPart("hello")}},
		Opencode: opencodeCfg,
	})
	if err != nil {
		t.Fatalf("HydrateSession: %v", err)
	}
	if result.Status != db.SessionStatusRunning {
		t.Fatalf("status = %s, want %s", result.Status, db.SessionStatusRunning)
	}
	if messageCalled {
		t.Fatalf("expected hydration not to replay the original prompt")
	}
	if !containsHydrationCall(calls, "tmux new-session") {
		t.Fatalf("expected tmux new-session call, got %v", calls)
	}
	if !containsHydrationCall(calls, "tmux new-window") {
		t.Fatalf("expected tmux new-window calls, got %v", calls)
	}
}

func containsHydrationCall(calls []string, prefix string) bool {
	for _, call := range calls {
		if strings.HasPrefix(call, prefix) {
			return true
		}
	}
	return false
}
