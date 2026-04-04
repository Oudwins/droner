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
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
)

func TestLocalBackend_CreateSession_StartsOpencodeInWorktreeDir(t *testing.T) {
	origExec := execCommand
	t.Cleanup(func() { execCommand = origExec })

	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "repo")
	worktreePath := filepath.Join(tmp, "worktree")

	var tmuxOpencodeArgs []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		if name == "tmux" && len(args) > 0 && args[0] == "new-session" {
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
		Message: &messages.Message{Parts: []messages.MessagePart{messages.NewTextPart("hello")}},
		Opencode: conf.OpenCodeConfig{
			Hostname: opencodeCfg.Hostname,
			Port:     opencodeCfg.Port,
		},
	}

	if err := backend.CreateSession(context.Background(), repoPath, worktreePath, "sid", agentCfg); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if len(tmuxOpencodeArgs) == 0 {
		t.Fatalf("expected tmux new-session call for opencode")
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

func TestLocalBackend_CreateSession_OpensOpencodeWithoutSessionWhenPromptMissing(t *testing.T) {
	origExec := execCommand
	t.Cleanup(func() { execCommand = origExec })

	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "repo")
	worktreePath := filepath.Join(tmp, "worktree")

	var tmuxOpencodeArgs []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		if name == "tmux" && len(args) > 0 && args[0] == "new-session" {
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
		w.WriteHeader(http.StatusInternalServerError)
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
		t.Fatalf("expected tmux new-session call for opencode")
	}
	args := tmuxOpencodeArgs[1:]
	opencodeURL := fmt.Sprintf("http://%s:%d", opencodeCfg.Hostname, opencodeCfg.Port)

	if !containsSubsequence(args, []string{"opencode", "attach", opencodeURL, "--dir", worktreePath}) {
		t.Fatalf("expected opencode attach url/dir args, got: %v", args)
	}
	if containsString(args, "--session") {
		t.Fatalf("expected opencode attach to omit --session, got: %v", args)
	}
}

func TestLocalBackend_CreateSession_OpensSplitTerminalWindow(t *testing.T) {
	origExec := execCommand
	t.Cleanup(func() { execCommand = origExec })

	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "repo")
	worktreePath := filepath.Join(tmp, "worktree")

	var calls [][]string
	execCommand = func(name string, args ...string) *exec.Cmd {
		if name == "tmux" {
			calls = append(calls, append([]string{name}, args...))
		}
		return exec.Command("sh", "-c", "exit 0")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/global/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
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

	if !containsTmuxCall(calls, []string{"new-window", "-t", "repo#sid", "-n", "terminal", "-c", worktreePath}) {
		t.Fatalf("expected terminal window creation, got: %v", calls)
	}
	if !containsTmuxCall(calls, []string{"new-window", "-t", "repo#sid", "-n", "terminal-split", "-c", worktreePath}) {
		t.Fatalf("expected split terminal window creation, got: %v", calls)
	}
	if !containsTmuxCall(calls, []string{"split-window", "-h", "-t", "repo#sid:terminal-split", "-c", worktreePath}) {
		t.Fatalf("expected terminal split window to be split side-by-side, got: %v", calls)
	}
	if !tmuxCallOrder(calls, []string{"new-session", "-d", "-s", "repo#sid", "-n", "opencode"}, []string{"new-window", "-t", "repo#sid", "-n", "terminal"}, []string{"new-window", "-t", "repo#sid", "-n", "terminal-split"}, []string{"split-window", "-h", "-t", "repo#sid:terminal-split"}) {
		t.Fatalf("expected tmux window order opencode -> terminal -> terminal-split -> split-window, got: %v", calls)
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

func containsTmuxCall(calls [][]string, seq []string) bool {
	for _, call := range calls {
		if containsSubsequence(call, seq) {
			return true
		}
	}
	return false
}

func tmuxCallOrder(calls [][]string, seqs ...[]string) bool {
	callIndex := 0
	for _, seq := range seqs {
		matched := false
		for callIndex < len(calls) {
			if containsSubsequence(calls[callIndex], seq) {
				matched = true
				callIndex++
				break
			}
			callIndex++
		}
		if !matched {
			return false
		}
	}
	return true
}
