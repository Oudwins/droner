package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/baseserver"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/env"
	"github.com/Oudwins/droner/pkgs/droner/internals/logbuf"
	"github.com/Oudwins/droner/pkgs/droner/internals/remote"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/internals/testutil"
)

type testHost struct {
	failGitCheck bool
}

func (h testHost) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

func (h testHost) ReadDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}

func (h testHost) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (h testHost) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (h testHost) GitIsInsideWorkTree(repoPath string) error {
	if h.failGitCheck {
		return errors.New("not a git worktree")
	}
	return nil
}

func (h testHost) CreateGitWorktree(sessionID string, repoPath string, worktreePath string) error {
	return nil
}

func (h testHost) RemoveGitWorktree(worktreePath string) error {
	return nil
}

func (h testHost) GitCommonDirFromWorktree(worktreePath string) (string, error) {
	return filepath.Join(worktreePath, ".git"), nil
}

func (h testHost) DeleteGitBranch(commonGitDir string, sessionID string) error {
	return nil
}

func (h testHost) GetRemoteURL(repoPath string) (string, error) {
	return "git@github.com:org/repo.git", nil
}

func (h testHost) GetRemoteURLFromWorktree(worktreePath string) (string, error) {
	return "git@github.com:org/repo.git", nil
}

func (h testHost) RunWorktreeSetup(repoPath string, worktreePath string) error {
	return nil
}

func (h testHost) CreateTmuxSession(sessionName string, worktreePath string, model string, prompt string) error {
	return nil
}

func (h testHost) KillTmuxSession(sessionName string) error {
	return nil
}

func newTestServer(t *testing.T) (*Server, *taskStore) {
	config := conf.GetConfig()
	origWorktrees := config.Worktrees.Dir
	origDataDir := config.Server.DataDir
	origVersion := config.Version

	worktreeRoot := t.TempDir()
	config.Worktrees.Dir = worktreeRoot
	config.Server.DataDir = t.TempDir()
	config.Version = "test-version"

	dataEnv := env.Get()
	origBase := dataEnv.BASE_URL
	origListen := dataEnv.LISTEN_ADDR
	origPort := dataEnv.PORT
	dataEnv.BASE_URL = "http://localhost"
	dataEnv.LISTEN_ADDR = "localhost:0"
	dataEnv.PORT = 0

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	store, err := newTaskStore(testutil.TempDBPath(t))
	if err != nil {
		t.Fatalf("newTaskStore: %v", err)
	}
	manager := newTaskManager(store, logger)

	server := &Server{
		Base: &baseserver.BaseServer{
			Config: config,
			Env:    dataEnv,
			Logger: logger,
		},
		Logbuf:    logbuf.New(),
		subs:      newSubscriptionManager(),
		oauth:     newOAuthStateStore(),
		tasks:     manager,
		Workspace: testHost{},
	}

	t.Cleanup(func() {
		config.Worktrees.Dir = origWorktrees
		config.Server.DataDir = origDataDir
		config.Version = origVersion
		dataEnv.BASE_URL = origBase
		dataEnv.LISTEN_ADDR = origListen
		dataEnv.PORT = origPort
	})

	originalSubscribe := subscribeRemote
	originalUnsubscribe := unsubscribeRemote
	subscribeRemote = func(ctx context.Context, remoteURL string, branch string, handler remote.BranchEventHandler) error {
		return nil
	}
	unsubscribeRemote = func(ctx context.Context, remoteURL string, branch string) error {
		return nil
	}
	t.Cleanup(func() {
		subscribeRemote = originalSubscribe
		unsubscribeRemote = originalUnsubscribe
	})

	return server, store
}

func TestHTTPVersion(t *testing.T) {
	server, _ := newTestServer(t)

	client := httptest.NewServer(server.Router())
	defer client.Close()

	resp, err := http.Get(client.URL + "/version")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if contentType := resp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/plain") {
		t.Fatalf("expected text/plain content type, got %q", contentType)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if strings.TrimSpace(string(body)) != "test-version" {
		t.Fatalf("unexpected version body: %q", string(body))
	}
}

func TestHTTPCreateSessionInvalidJSON(t *testing.T) {
	server, _ := newTestServer(t)

	client := httptest.NewServer(server.Router())
	defer client.Close()

	resp, err := http.Post(client.URL+"/sessions", "application/json", bytes.NewBufferString("{"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestHTTPCreateSessionValidation(t *testing.T) {
	server, _ := newTestServer(t)

	client := httptest.NewServer(server.Router())
	defer client.Close()

	resp, err := http.Post(client.URL+"/sessions", "application/json", bytes.NewBufferString(`{"path":""}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestHTTPCreateSessionPathNotFound(t *testing.T) {
	server, _ := newTestServer(t)

	client := httptest.NewServer(server.Router())
	defer client.Close()

	body := `{"path":"/missing/path"}`
	resp, err := http.Post(client.URL+"/sessions", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestHTTPCreateSessionNotGitRepo(t *testing.T) {
	server, _ := newTestServer(t)
	server.Workspace = testHost{failGitCheck: true}

	client := httptest.NewServer(server.Router())
	defer client.Close()

	repoPath := t.TempDir()
	body := `{"path":"` + repoPath + `"}`
	resp, err := http.Post(client.URL+"/sessions", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestHTTPCreateSessionSuccess(t *testing.T) {
	server, store := newTestServer(t)

	client := httptest.NewServer(server.Router())
	defer client.Close()

	repoPath := t.TempDir()
	body := `{"path":"` + repoPath + `"}`
	resp, err := http.Post(client.URL+"/sessions", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", resp.StatusCode)
	}

	var payload schemas.TaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.TaskID == "" {
		t.Fatalf("expected task id")
	}
	if payload.Result == nil || payload.Result.SessionID == "" || payload.Result.WorktreePath == "" {
		t.Fatalf("expected result fields")
	}

	if err := waitForStatus(store, payload.TaskID, schemas.TaskStatusSucceeded); err != nil {
		t.Fatalf("wait for task: %v", err)
	}
}

func TestHTTPDeleteSession(t *testing.T) {
	server, store := newTestServer(t)

	client := httptest.NewServer(server.Router())
	defer client.Close()

	resp, err := http.NewRequest(http.MethodDelete, client.URL+"/sessions", bytes.NewBufferString("{"))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	res, err := http.DefaultClient.Do(resp)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", res.StatusCode)
	}

	missingBody := `{"session_id":"missing"}`
	resp, err = http.NewRequest(http.MethodDelete, client.URL+"/sessions", bytes.NewBufferString(missingBody))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	res, err = http.DefaultClient.Do(resp)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", res.StatusCode)
	}

	worktreeRoot := server.Base.Config.Worktrees.Dir
	worktreePath := filepath.Join(worktreeRoot, "repo#abc")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	validBody := `{"session_id":"abc"}`
	resp, err = http.NewRequest(http.MethodDelete, client.URL+"/sessions", bytes.NewBufferString(validBody))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	res, err = http.DefaultClient.Do(resp)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", res.StatusCode)
	}

	var payload schemas.TaskResponse
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.TaskID == "" {
		t.Fatalf("expected task id")
	}

	if err := waitForStatus(store, payload.TaskID, schemas.TaskStatusSucceeded); err != nil {
		t.Fatalf("wait for task: %v", err)
	}
}

func TestHTTPTaskStatus(t *testing.T) {
	server, _ := newTestServer(t)

	client := httptest.NewServer(server.Router())
	defer client.Close()

	resp, err := http.Get(client.URL + "/tasks/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}

	resp, err = http.Get(client.URL + "/tasks/unknown")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}

	created, err := server.tasks.Enqueue("task", nil, nil)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	resp, err = http.Get(client.URL + "/tasks/" + created.TaskID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var payload schemas.TaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.TaskID != created.TaskID {
		t.Fatalf("expected task id %s, got %s", created.TaskID, payload.TaskID)
	}
}
