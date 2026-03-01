package backends

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
)

func TestLocalBackend_CreateSession_AutorunsPromptViaMessageEndpoint(t *testing.T) {
	// Stub execCommand so we don't require tmux/git/nvim.
	origExec := execCommand
	execCommand = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command("sh", "-c", "exit 0")
	}
	t.Cleanup(func() { execCommand = origExec })

	gotMessage := false

	mux := http.NewServeMux()
	mux.HandleFunc("/global/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	})
	mux.HandleFunc("/session/abc/prompt", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected call to /session/abc/prompt")
	})
	mux.HandleFunc("/session/abc/message", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if v, ok := body["noReply"]; ok && v != false {
			t.Fatalf("noReply = %v, want omitted or false", v)
		}
		parts, ok := body["parts"].([]any)
		if !ok || len(parts) == 0 {
			t.Fatalf("parts missing or empty")
		}
		gotMessage = true
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	opencodeCfg := opencodeConfigFromServer(t, srv)

	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "repo")
	worktreePath := filepath.Join(tmp, "worktree")

	backend := LocalBackend{config: &conf.LocalBackendConfig{WorktreeDir: tmp}}
	agentCfg := AgentConfig{
		Model:   "openai/gpt-5-mini",
		Message: &messages.Message{Parts: []messages.MessagePart{messages.NewTextPart("hello")}},
		Opencode: conf.OpenCodeConfig{
			Hostname: opencodeCfg.Hostname,
			Port:     opencodeCfg.Port,
		},
	}

	if err := backend.CreateSession(context.Background(), repoPath, worktreePath, "sid", agentCfg); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if !gotMessage {
		t.Fatalf("expected a POST to /session/abc/message")
	}
}
