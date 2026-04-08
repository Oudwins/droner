package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/eventdebug"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/env"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
)

func captureOutput(t *testing.T, fn func() error) (string, error) {
	stdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = writer

	result := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, reader)
		close(result)
	}()

	err = fn()
	_ = writer.Close()
	<-result
	os.Stdout = stdout

	return buf.String(), err
}

func executeCLI(args []string) error {
	cmd := newRootCmd()
	cmd.SetArgs(args)
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	return cmd.Execute()
}

func setupCLIEnv(t *testing.T, baseURL string) {
	config := conf.GetConfig()
	origVersion := config.Version
	config.Version = "test-version"

	currentEnv := env.Get()
	origBase := currentEnv.BASE_URL
	currentEnv.BASE_URL = strings.TrimRight(baseURL, "/")

	t.Cleanup(func() {
		config.Version = origVersion
		currentEnv.BASE_URL = origBase
	})
}

func TestCLINewDeleteAndComplete(t *testing.T) {
	var createRequest schemas.SessionCreateRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte("test-version"))
		case "/sessions":
			if r.Method == http.MethodPost {
				if err := json.NewDecoder(r.Body).Decode(&createRequest); err != nil {
					t.Fatalf("decode create request: %v", err)
				}
				branch := schemas.NewSBranch("simple-new")
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(&schemas.SessionCreateResponse{ID: "stream-new", Harness: conf.HarnessOpenCode, Branch: &branch, TaskID: "task-new"})
				return
			}
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(&schemas.TaskResponse{TaskID: "task-del", Status: schemas.TaskStatusPending, Result: &schemas.TaskResult{Branch: "abc"}})
				return
			}
		case "/sessions/complete":
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(&schemas.TaskResponse{TaskID: "task-complete", Status: schemas.TaskStatusPending, Result: &schemas.TaskResult{Branch: "abc", WorktreePath: "/tmp/worktree"}})
				return
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	setupCLIEnv(t, server.URL)

	output, err := captureOutput(t, func() error {
		return executeCLI([]string{"new", "--path", "/repo", "--agent", "plan", "--prompt", "hello"})
	})
	if err != nil {
		t.Fatalf("run new: %v", err)
	}
	if !strings.Contains(output, "harness: opencode") || !strings.Contains(output, "branch: simple-new") || !strings.Contains(output, "id: stream-new") {
		t.Fatalf("unexpected new output: %s", output)
	}
	if createRequest.AgentConfig == nil {
		t.Fatalf("expected agentConfig in create request")
	}
	if createRequest.AgentConfig.AgentName != "plan" {
		t.Fatalf("agentName = %q, want %q", createRequest.AgentConfig.AgentName, "plan")
	}
	if createRequest.AgentConfig.Message == nil || len(createRequest.AgentConfig.Message.Parts) != 1 || createRequest.AgentConfig.Message.Parts[0].Text != "hello" {
		t.Fatalf("unexpected prompt payload: %#v", createRequest.AgentConfig.Message)
	}

	output, err = captureOutput(t, func() error {
		return executeCLI([]string{"del", "abc"})
	})
	if err != nil {
		t.Fatalf("run del: %v", err)
	}
	if !strings.Contains(output, "status: pending") || !strings.Contains(output, "branch: abc") {
		t.Fatalf("unexpected del output: %s", output)
	}

	output, err = captureOutput(t, func() error {
		return executeCLI([]string{"complete", "abc"})
	})
	if err != nil {
		t.Fatalf("run complete: %v", err)
	}
	if !strings.Contains(output, "status: pending") || !strings.Contains(output, "worktree: /tmp/worktree") {
		t.Fatalf("unexpected complete output: %s", output)
	}
}

func TestCLIVersionFlag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte("server-version"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	setupCLIEnv(t, server.URL)

	output, err := captureOutput(t, func() error {
		return executeCLI([]string{"--version"})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(output, "cli:") {
		t.Fatalf("expected cli version, got %q", output)
	}
	if !strings.Contains(output, "server: server-version") {
		t.Fatalf("expected server version, got %q", output)
	}
}

func TestCLIVersionFlagServerNotRunning(t *testing.T) {
	setupCLIEnv(t, "http://127.0.0.1:1")

	output, err := captureOutput(t, func() error {
		return executeCLI([]string{"--version"})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(output, "cli:") {
		t.Fatalf("expected cli version, got %q", output)
	}
	if !strings.Contains(output, "server: (not running)") {
		t.Fatalf("expected not running, got %q", output)
	}
}

func TestCLIDebuggerCommand(t *testing.T) {
	origRun := runEventDebugServer
	t.Cleanup(func() {
		runEventDebugServer = origRun
	})

	var gotCfg eventdebug.Config
	runEventDebugServer = func(_ context.Context, cfg eventdebug.Config) error {
		gotCfg = cfg
		return nil
	}

	if err := executeCLI([]string{"debugger"}); err != nil {
		t.Fatalf("run debugger: %v", err)
	}

	defaults := eventdebug.DefaultConfig()
	if gotCfg != defaults {
		t.Fatalf("config = %#v, want %#v", gotCfg, defaults)
	}
	if _, _, err := newRootCmd().Find([]string{"debugger"}); err != nil {
		t.Fatalf("expected debugger command to be registered: %v", err)
	}
	if _, _, err := newRootCmd().Find([]string{"eventdebug"}); err != nil {
		t.Fatalf("expected eventdebug alias to be registered: %v", err)
	}
	debuggerCmd, _, err := newRootCmd().Find([]string{"debugger"})
	if err != nil {
		t.Fatalf("find debugger command: %v", err)
	}
	for _, name := range []string{"addr", "db", "table", "title"} {
		if flag := debuggerCmd.Flags().Lookup(name); flag != nil {
			t.Fatalf("unexpected debugger flag registered: %s", name)
		}
	}
}
