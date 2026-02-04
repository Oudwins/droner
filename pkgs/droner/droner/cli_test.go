package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/desktop"
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

func TestCLITaskFlow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte("test-version"))
		case "/tasks/abc":
			_ = json.NewEncoder(w).Encode(&schemas.TaskResponse{TaskID: "abc", Status: schemas.TaskStatusSucceeded})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	setupCLIEnv(t, server.URL)

	output, err := captureOutput(t, func() error {
		return executeCLI([]string{"task", "abc"})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(output, "task: abc") || !strings.Contains(output, "status: succeeded") {
		t.Fatalf("unexpected output: %s", output)
	}
}

func TestCLINewAndDeleteWait(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte("test-version"))
		case "/sessions":
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(&schemas.SessionCreateResponse{SessionID: schemas.NewSSessionID("simple-new"), SimpleID: "simple-new", TaskID: "task-new"})
				return
			}
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(&schemas.TaskResponse{TaskID: "task-del", Status: schemas.TaskStatusPending})
				return
			}
		case "/tasks/task-new":
			_ = json.NewEncoder(w).Encode(&schemas.TaskResponse{TaskID: "task-new", Status: schemas.TaskStatusSucceeded})
		case "/tasks/task-del":
			_ = json.NewEncoder(w).Encode(&schemas.TaskResponse{TaskID: "task-del", Status: schemas.TaskStatusSucceeded})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	setupCLIEnv(t, server.URL)

	output, err := captureOutput(t, func() error {
		return executeCLI([]string{"new", "--path", "/repo", "--wait"})
	})
	if err != nil {
		t.Fatalf("run new: %v", err)
	}
	if !strings.Contains(output, "session: simple-new") || !strings.Contains(output, "status: succeeded") {
		t.Fatalf("unexpected new output: %s", output)
	}

	output, err = captureOutput(t, func() error {
		return executeCLI([]string{"del", "abc", "--wait"})
	})
	if err != nil {
		t.Fatalf("run del: %v", err)
	}
	if !strings.Contains(output, "status: pending") || !strings.Contains(output, "status: succeeded") {
		t.Fatalf("unexpected del output: %s", output)
	}
}

func TestCLIAuthFlow(t *testing.T) {
	originalExec := desktop.ExecCommand
	originalGOOS := desktop.RuntimeGOOS
	desktop.ExecCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("sh", "-c", "true")
	}
	desktop.RuntimeGOOS = "linux"
	t.Cleanup(func() {
		desktop.ExecCommand = originalExec
		desktop.RuntimeGOOS = originalGOOS
	})

	completeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte("test-version"))
		case "/oauth/github/start":
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "state1", "verification_uri": "https://example.com", "expires_in": 30, "interval": 1})
		case "/oauth/github/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "complete"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer completeServer.Close()
	setupCLIEnv(t, completeServer.URL)

	if _, err := captureOutput(t, func() error {
		return executeCLI([]string{"auth", "github"})
	}); err != nil {
		t.Fatalf("expected auth to succeed: %v", err)
	}

	failedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte("test-version"))
		case "/oauth/github/start":
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "state2", "verification_uri": "https://example.com", "expires_in": 30, "interval": 1})
		case "/oauth/github/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "failed", "error": "denied"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer failedServer.Close()
	setupCLIEnv(t, failedServer.URL)

	if _, err := captureOutput(t, func() error {
		return executeCLI([]string{"auth", "github"})
	}); err == nil {
		t.Fatalf("expected auth to fail")
	}

	timeoutServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte("test-version"))
		case "/oauth/github/start":
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "state3", "verification_uri": "https://example.com", "expires_in": 1, "interval": 1})
		case "/oauth/github/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "pending"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer timeoutServer.Close()
	setupCLIEnv(t, timeoutServer.URL)

	start := time.Now()
	_, err := captureOutput(t, func() error {
		return executeCLI([]string{"auth", "github"})
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("timeout test took too long")
	}
}
