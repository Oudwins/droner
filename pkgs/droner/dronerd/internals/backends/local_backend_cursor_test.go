package backends

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
)

func TestLocalBackendRunCursorWorktreeSetup_NoConfig(t *testing.T) {
	repoPath := t.TempDir()
	worktreePath := t.TempDir()

	backend := LocalBackend{}
	if err := backend.runCursorWorktreeSetup(repoPath, worktreePath, "sid"); err != nil {
		t.Fatalf("runCursorWorktreeSetup: %v", err)
	}
}

func TestLocalBackendRunCursorWorktreeSetup_RunsCommandsIndependently(t *testing.T) {
	repoPath := t.TempDir()
	worktreePath := t.TempDir()

	if err := os.MkdirAll(filepath.Join(repoPath, ".cursor"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile README: %v", err)
	}

	config := `{
		"setup-worktree": [
			"cp \"$ROOT_WORKTREE_PATH/README.md\" README_COPY.md",
			"printf '%s' \"$SESSION_ID\" > session_id.txt",
			"printf '%s' \"$WORKTREE_PATH\" > worktree_path.txt"
		]
	}`
	configPath := filepath.Join(repoPath, ".cursor", "worktrees.json")
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	backend := LocalBackend{}
	if err := backend.runCursorWorktreeSetup(repoPath, worktreePath, "sid-123"); err != nil {
		t.Fatalf("runCursorWorktreeSetup: %v", err)
	}

	readmeCopy, err := os.ReadFile(filepath.Join(worktreePath, "README_COPY.md"))
	if err != nil {
		t.Fatalf("ReadFile README_COPY: %v", err)
	}
	if got := string(readmeCopy); got != "hello\n" {
		t.Fatalf("README_COPY.md = %q, want %q", got, "hello\n")
	}

	sessionID, err := os.ReadFile(filepath.Join(worktreePath, "session_id.txt"))
	if err != nil {
		t.Fatalf("ReadFile session_id: %v", err)
	}
	if got := string(sessionID); got != "sid-123" {
		t.Fatalf("session_id.txt = %q, want %q", got, "sid-123")
	}

	worktreeValue, err := os.ReadFile(filepath.Join(worktreePath, "worktree_path.txt"))
	if err != nil {
		t.Fatalf("ReadFile worktree_path: %v", err)
	}
	if got := string(worktreeValue); got != filepath.Clean(worktreePath) {
		t.Fatalf("worktree_path.txt = %q, want %q", got, filepath.Clean(worktreePath))
	}
}

func TestLocalBackendRunCursorWorktreeSetup_FailsOnCommandError(t *testing.T) {
	repoPath := t.TempDir()
	worktreePath := t.TempDir()

	if err := os.MkdirAll(filepath.Join(repoPath, ".cursor"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	configPath := filepath.Join(repoPath, ".cursor", "worktrees.json")
	if err := os.WriteFile(configPath, []byte(`{"setup-worktree": ["exit 7"]}`), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	backend := LocalBackend{}
	err := backend.runCursorWorktreeSetup(repoPath, worktreePath, "sid")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), `cursor setup-worktree command failed: "exit 7"`) {
		t.Fatalf("error = %q", err)
	}
}

func TestLocalBackendCreateSession_RunsCursorWorktreeSetup(t *testing.T) {
	origExec := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		switch name {
		case "git":
			if len(args) >= 4 && args[2] == "worktree" && args[3] == "add" {
				worktreePath := args[len(args)-1]
				return exec.Command("sh", "-lc", "mkdir -p \"$1\"", "--", worktreePath)
			}
			return exec.Command("sh", "-lc", "exit 0")
		case "tmux":
			return exec.Command("sh", "-lc", "exit 0")
		default:
			return exec.Command(name, args...)
		}
	}
	t.Cleanup(func() { execCommand = origExec })

	repoPath := t.TempDir()
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	if err := os.MkdirAll(filepath.Join(repoPath, ".cursor"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	configPath := filepath.Join(repoPath, ".cursor", "worktrees.json")
	if err := os.WriteFile(configPath, []byte(`{"setup-worktree": ["touch setup.txt"]}`), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/global/health", func(w http.ResponseWriter, _ *http.Request) {
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
	srv := httptest.NewServer(mux)
	t.Cleanup(func() { srv.Close() })
	opencodeCfg := opencodeConfigFromServer(t, srv)

	backend := LocalBackend{config: &conf.LocalBackendConfig{WorktreeDir: filepath.Dir(worktreePath)}}
	agentCfg := AgentConfig{Opencode: conf.OpenCodeConfig{Hostname: opencodeCfg.Hostname, Port: opencodeCfg.Port}}

	if err := backend.CreateSession(context.Background(), repoPath, worktreePath, "sid", agentCfg); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if _, err := os.Stat(filepath.Join(worktreePath, "setup.txt")); err != nil {
		t.Fatalf("expected setup.txt to exist: %v", err)
	}
}
