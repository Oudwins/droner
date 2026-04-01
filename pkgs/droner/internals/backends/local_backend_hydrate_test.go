package backends

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/db"
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

func TestLocalBackendHydrateSessionReusesLatestSessionForDirectory(t *testing.T) {
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

	createCalled := false
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
		if r.Method != http.MethodGet {
			createCalled = true
			t.Fatalf("method = %s, want %s", r.Method, http.MethodGet)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"latest","directory":"` + worktreePath + `","projectID":"proj","time":{"created":1,"updated":10},"title":"latest","version":"1"},{"id":"older","directory":"` + worktreePath + `","projectID":"proj","time":{"created":1,"updated":5},"title":"older","version":"1"}]`))
	})
	mux.HandleFunc("/session/latest/message", func(w http.ResponseWriter, r *http.Request) {
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
	if createCalled {
		t.Fatalf("expected hydration not to create a new opencode session")
	}
	if messageCalled {
		t.Fatalf("expected hydration not to replay the original prompt when an existing session is found")
	}
	if !containsHydrationCall(calls, "tmux new-session") {
		t.Fatalf("expected tmux new-session call, got %v", calls)
	}
	if !containsHydrationCall(calls, "tmux new-window") {
		t.Fatalf("expected tmux new-window calls, got %v", calls)
	}
	opencodeURL := fmt.Sprintf("http://%s:%d", opencodeCfg.Hostname, opencodeCfg.Port)
	if !containsHydrationCallWithArgs(calls, []string{"opencode", "attach", opencodeURL, "--session", "latest", "--dir", worktreePath}) {
		t.Fatalf("expected opencode window to attach latest session, got %v", calls)
	}
}

func TestLocalBackendHydrateSessionCreatesAndAutorunsWhenDirectoryHasNoSessions(t *testing.T) {
	origTimeout := opencodeAutorunTimeout
	opencodeAutorunTimeout = 250 * time.Millisecond
	t.Cleanup(func() { opencodeAutorunTimeout = origTimeout })

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

	messageStarted := make(chan struct{})
	messageDone := make(chan struct{})
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
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`[]`))
		case http.MethodPost:
			_, _ = w.Write([]byte(`{"id":"created"}`))
		default:
			t.Fatalf("method = %s, want GET or POST", r.Method)
		}
	})
	mux.HandleFunc("/session/created/message", func(w http.ResponseWriter, r *http.Request) {
		if dir := r.URL.Query().Get("directory"); dir != worktreePath {
			t.Fatalf("directory = %q, want %q", dir, worktreePath)
		}
		select {
		case <-messageStarted:
		default:
			close(messageStarted)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"info":{"role":"assistant"},"parts":[{"type":"text","text":"ok"}]}`))
		close(messageDone)
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
		Model:     "openai/gpt-5-mini",
		AgentName: "plan",
		Message:   &messages.Message{Parts: []messages.MessagePart{messages.NewTextPart("hello")}},
		Opencode:  opencodeCfg,
	})
	if err != nil {
		t.Fatalf("HydrateSession: %v", err)
	}
	if result.Status != db.SessionStatusRunning {
		t.Fatalf("status = %s, want %s", result.Status, db.SessionStatusRunning)
	}
	select {
	case <-messageStarted:
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for hydration autorun request")
	}
	select {
	case <-messageDone:
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for hydration autorun request to finish")
	}
	opencodeURL := fmt.Sprintf("http://%s:%d", opencodeCfg.Hostname, opencodeCfg.Port)
	if !containsHydrationCallWithArgs(calls, []string{"opencode", "attach", opencodeURL, "--session", "created", "--dir", worktreePath}) {
		t.Fatalf("expected opencode window to attach created session, got %v", calls)
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

func containsHydrationCallWithArgs(calls []string, parts []string) bool {
	needle := strings.Join(parts, " ")
	for _, call := range calls {
		if strings.Contains(call, needle) {
			return true
		}
	}
	return false
}
