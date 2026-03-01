package backends

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
)

func TestLocalBackend_CreateSession_StartsOpencodeInWorktreeDir(t *testing.T) {
	origExec := execCommand
	t.Cleanup(func() { execCommand = origExec })

	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "repo")
	worktreePath := filepath.Join(tmp, "worktree")

	var tmuxOpencodeArgs []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		if name == "tmux" && len(args) > 0 && args[0] == "new-window" {
			for i := 0; i+1 < len(args); i++ {
				if args[i] == "-n" && args[i+1] == "opencode" {
					tmuxOpencodeArgs = append([]string{name}, args...)
					break
				}
			}
		}
		return exec.Command("sh", "-c", "exit 0")
	}

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

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	opencodeCfg := opencodeConfigFromServer(t, srv)

	backend := LocalBackend{config: &conf.LocalBackendConfig{WorktreeDir: tmp}}
	agentCfg := AgentConfig{
		Opencode: conf.OpenCodeConfig{
			Hostname: opencodeCfg.Hostname,
			Port:     opencodeCfg.Port,
		},
	}

	if err := backend.CreateSession(context.Background(), repoPath, worktreePath, "sid", agentCfg); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if len(tmuxOpencodeArgs) == 0 {
		t.Fatalf("expected tmux new-window call for opencode")
	}
	args := tmuxOpencodeArgs[1:] // drop binary name
	opencodeURL := fmt.Sprintf("http://%s:%d", opencodeCfg.Hostname, opencodeCfg.Port)

	if !containsSubsequence(args, []string{"-c", worktreePath}) {
		t.Fatalf("expected tmux to set cwd to worktreePath, got: %v", args)
	}
	if !containsSubsequence(args, []string{"opencode", "attach", opencodeURL, "--session", "abc", "--dir", worktreePath}) {
		t.Fatalf("expected opencode attach url/session/dir args, got: %v", args)
	}
	if !containsString(args, opencodeURL) {
		t.Fatalf("expected opencode URL %q in args, got: %v", opencodeURL, args)
	}
	if !containsSubsequence(args, []string{"--session", "abc"}) {
		t.Fatalf("expected opencode attach to include --session abc, got: %v", args)
	}
	if !containsSubsequence(args, []string{"--dir", worktreePath}) {
		t.Fatalf("expected opencode attach to include --dir worktreePath, got: %v", args)
	}
}

func containsString(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

func containsSubsequence(haystack []string, seq []string) bool {
	if len(seq) == 0 {
		return true
	}
	if len(haystack) < len(seq) {
		return false
	}
	for i := 0; i+len(seq) <= len(haystack); i++ {
		ok := true
		for j := range seq {
			if haystack[i+j] != seq[j] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
