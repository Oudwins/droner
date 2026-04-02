package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/db"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/sessions/sessionevents"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/backends"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
)

type createSessionBackend struct {
	worktreeRoot string
}

func (b *createSessionBackend) ID() conf.BackendID {
	return conf.BackendLocal
}

func (b *createSessionBackend) WorktreePath(repoPath string, sessionID string) (string, error) {
	return filepath.Join(b.worktreeRoot, filepath.Base(repoPath)+".."+sessionID), nil
}

func (b *createSessionBackend) ValidateSessionID(repoPath string, sessionID string) error {
	return nil
}

func (b *createSessionBackend) CreateSession(ctx context.Context, repoPath string, worktreePath string, sessionID string, agentConfig backends.AgentConfig, opts ...backends.CreateSessionOptions) error {
	return nil
}

func (b *createSessionBackend) HydrateSession(ctx context.Context, session db.Session, agentConfig backends.AgentConfig) (backends.HydrationResult, error) {
	return backends.HydrationResult{Status: db.SessionStatusRunning}, nil
}

func (b *createSessionBackend) CompleteSession(ctx context.Context, worktreePath string, sessionID string) error {
	return nil
}

func (b *createSessionBackend) DeleteSession(ctx context.Context, worktreePath string, sessionID string) error {
	return nil
}

func newEventSourcedCreateSessionTestServer(t *testing.T) (*Server, *db.Queries, string, string) {
	t.Helper()

	dataDir := t.TempDir()
	repoDir := filepath.Join(dataDir, "repo")
	worktreeDir := filepath.Join(dataDir, "worktrees")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll worktrees: %v", err)
	}
	initGitRepo(t, repoDir)

	config := &conf.Config{
		Server: conf.ServerConfig{DataDir: dataDir},
		Sessions: conf.SessionsConfig{
			Harness: conf.SessionHarnessConfig{
				Defaults: conf.SessionHarnessDefaultsConfig{Selected: conf.HarnessOpenCode},
				Providers: conf.SessionHarnessProvidersConfig{
					OpenCode: conf.OpenCodeConfig{DefaultModel: "default-model", Hostname: "127.0.0.1", Port: 4096},
				},
			},
			Backends: conf.BackendsConfig{
				Default: conf.BackendLocal,
				Local:   conf.LocalBackendConfig{WorktreeDir: worktreeDir},
			},
			Naming: conf.SessionNamingConfig{Strategy: conf.SessionNamingStrategyRandom},
		},
	}

	projectionConn, err := db.OpenSQLiteDB(db.DBPath(dataDir))
	if err != nil {
		t.Fatalf("OpenSQLiteDB projection db: %v", err)
	}
	projectionQueries := db.New(projectionConn)

	store := backends.NewStore(config)
	store.Register(&createSessionBackend{worktreeRoot: worktreeDir})

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	server := &Server{Base: &core.BaseServer{
		Config:       config,
		Logger:       logger,
		BackendStore: store,
	}}

	events, err := sessionevents.Open(server.Base.Config.Server.DataDir, server.Base.Logger, server.Base.Config, server.Base.BackendStore)
	if err != nil {
		t.Fatalf("sessionevents.Open: %v", err)
	}
	server.events = events

	ctx, cancel := context.WithCancel(context.Background())
	server.events.Start(ctx)
	t.Cleanup(func() {
		cancel()
		_ = server.events.Close()
		_ = projectionConn.Close()
	})

	return server, projectionQueries, repoDir, dataDir
}

func initGitRepo(t *testing.T, repoDir string) {
	t.Helper()
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll repo: %v", err)
	}
	cmd := exec.Command("git", "init", "-b", "main", repoDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, output)
	}
}

func TestHandlerCreateSessionHonorsCanceledRequestContext(t *testing.T) {
	server, projectionQueries, repoDir, _ := newEventSourcedCreateSessionTestServer(t)

	payload, err := json.Marshal(schemas.SessionCreateRequest{
		Path:      repoDir,
		Branch:    schemas.NewSBranch("cancelled-session"),
		BackendID: conf.BackendLocal,
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/sessions", bytesReader(payload)).WithContext(ctx)
	rec := httptest.NewRecorder()

	server.HandlerCreateSession(server.Base.Logger, rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	_, err = projectionQueries.GetSessionProjectionByBranch(context.Background(), "cancelled-session")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected no projection row, got err=%v", err)
	}
}

func TestHandlerCreateSessionPersistsStructuredFilePrompt(t *testing.T) {
	server, projectionQueries, repoDir, _ := newEventSourcedCreateSessionTestServer(t)

	payload, err := json.Marshal(schemas.SessionCreateRequest{
		Path:      repoDir,
		Branch:    schemas.NewSBranch("file-prompt-session"),
		BackendID: conf.BackendLocal,
		AgentConfig: &schemas.SessionAgentConfig{
			Message: &messages.Message{
				Role: messages.MessageRoleUser,
				Parts: []messages.MessagePart{
					messages.NewTextPart("inspect "),
					messages.NewFilePart("pkgs/droner/tui/tui.go"),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/sessions", bytesReader(payload))
	rec := httptest.NewRecorder()
	server.HandlerCreateSession(server.Base.Logger, rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	projection := waitForProjection(t, projectionQueries, "file-prompt-session")
	if projection.AgentConfig == "" {
		t.Fatal("expected agent config to be stored")
	}
	var stored schemas.SessionAgentConfig
	if err := json.Unmarshal([]byte(projection.AgentConfig), &stored); err != nil {
		t.Fatalf("json.Unmarshal stored agent config: %v", err)
	}
	if stored.Message == nil || len(stored.Message.Parts) != 2 {
		t.Fatalf("stored message parts = %#v", stored.Message)
	}
	if stored.Message.Parts[1].Type != messages.PartTypeFile || stored.Message.Parts[1].File == nil || stored.Message.Parts[1].File.Source == nil || stored.Message.Parts[1].File.Source.Path != "pkgs/droner/tui/tui.go" {
		t.Fatalf("stored file part = %#v", stored.Message.Parts[1])
	}
	if stored.Message.Parts[1].File.URL != nil {
		t.Fatalf("expected stored file url to be nil, got %#v", stored.Message.Parts[1].File.URL)
	}
}

func TestHandlerCreateSessionPersistsInlineImagePrompt(t *testing.T) {
	server, projectionQueries, repoDir, _ := newEventSourcedCreateSessionTestServer(t)

	payload, err := json.Marshal(schemas.SessionCreateRequest{
		Path:      repoDir,
		Branch:    schemas.NewSBranch("inline-image-session"),
		BackendID: conf.BackendLocal,
		AgentConfig: &schemas.SessionAgentConfig{
			Message: &messages.Message{
				Role: messages.MessageRoleUser,
				Parts: []messages.MessagePart{
					messages.NewTextPart("inspect this"),
					messages.NewDataURLFilePart("image/png", "pasted-image-1.png", "data:image/png;base64,ZmFrZQ=="),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/sessions", bytesReader(payload))
	rec := httptest.NewRecorder()
	server.HandlerCreateSession(server.Base.Logger, rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	projection := waitForProjection(t, projectionQueries, "inline-image-session")
	if projection.AgentConfig == "" {
		t.Fatal("expected agent config to be stored")
	}
	var stored schemas.SessionAgentConfig
	if err := json.Unmarshal([]byte(projection.AgentConfig), &stored); err != nil {
		t.Fatalf("json.Unmarshal stored agent config: %v", err)
	}
	if stored.Message == nil || len(stored.Message.Parts) != 2 {
		t.Fatalf("stored message parts = %#v", stored.Message)
	}
	imagePart := stored.Message.Parts[1]
	if imagePart.Type != messages.PartTypeFile || imagePart.File == nil || imagePart.File.URL == nil {
		t.Fatalf("stored image part = %#v", imagePart)
	}
	if *imagePart.File.URL != "data:image/png;base64,ZmFrZQ==" {
		t.Fatalf("stored image url = %q, want inline data url", *imagePart.File.URL)
	}
	if imagePart.File.Mime != "image/png" || imagePart.File.Filename != "pasted-image-1.png" {
		t.Fatalf("stored image metadata = %#v", imagePart.File)
	}
	if imagePart.File.Source != nil {
		t.Fatalf("expected stored inline image source to be nil, got %#v", imagePart.File.Source)
	}
}

func TestHandlerCreateSessionUsesUnifiedDronerDB(t *testing.T) {
	server, projectionQueries, repoDir, dataDir := newEventSourcedCreateSessionTestServer(t)

	payload, err := json.Marshal(schemas.SessionCreateRequest{
		Path:      repoDir,
		Branch:    schemas.NewSBranch("evented-session"),
		BackendID: conf.BackendLocal,
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/sessions", bytesReader(payload))
	rec := httptest.NewRecorder()
	server.HandlerCreateSession(server.Base.Logger, rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	var response schemas.SessionCreateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal response: %v", err)
	}
	if response.TaskID == "" {
		t.Fatal("expected event task id")
	}
	if response.Harness != conf.HarnessOpenCode {
		t.Fatalf("response harness = %q, want %q", response.Harness, conf.HarnessOpenCode)
	}
	projection := waitForProjection(t, projectionQueries, "evented-session")
	if projection.Harness != conf.HarnessOpenCode.String() {
		t.Fatalf("projection harness = %q, want %q", projection.Harness, conf.HarnessOpenCode)
	}
	if projection.Branch != "evented-session" {
		t.Fatalf("projection branch = %q, want evented-session", projection.Branch)
	}

	projectionConn, err := sql.Open("sqlite", db.DBPath(dataDir))
	if err != nil {
		t.Fatalf("sql.Open droner.db: %v", err)
	}
	defer projectionConn.Close()

	var sessionsTableCount int
	err = projectionConn.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'sessions'`).Scan(&sessionsTableCount)
	if err != nil {
		t.Fatalf("count sessions table: %v", err)
	}
	if sessionsTableCount != 0 {
		t.Fatalf("sessions table count = %d, want 0", sessionsTableCount)
	}
	waitForSessionState(t, server, "evented-session", "running")
}

func TestHandlerCompleteSessionEventSourcedPathCompletesSession(t *testing.T) {
	server, _, repoDir, _ := newEventSourcedCreateSessionTestServer(t)

	createResponse := createEventSourcedSession(t, server, repoDir, "complete-me")
	waitForSessionState(t, server, "complete-me", "running")

	payload, err := json.Marshal(schemas.SessionCompleteRequest{Branch: schemas.NewSBranch("complete-me")})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/sessions/complete", bytesReader(payload))
	rec := httptest.NewRecorder()
	server.HandlerCompleteSession(server.Base.Logger, rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	var response schemas.TaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal task response: %v", err)
	}
	if response.TaskID == "" || !strings.HasPrefix(response.TaskID, "session-complete:") {
		t.Fatalf("task id = %q, want session-complete prefix", response.TaskID)
	}
	if response.Type != "session_complete" {
		t.Fatalf("task type = %q, want session_complete", response.Type)
	}
	if response.Status != schemas.TaskStatusPending {
		t.Fatalf("task status = %q, want %q", response.Status, schemas.TaskStatusPending)
	}
	if response.Result == nil || response.Result.Branch != "complete-me" || response.Result.WorktreePath != createResponse.WorktreePath {
		t.Fatalf("unexpected task result: %#v", response.Result)
	}

	waitForSessionState(t, server, "complete-me", "completed")
}

func TestHandlerListSessionsWithoutStatusFilterIncludesCompleted(t *testing.T) {
	server, _, repoDir, _ := newEventSourcedCreateSessionTestServer(t)

	createEventSourcedSession(t, server, repoDir, "completed-visible")
	waitForSessionState(t, server, "completed-visible", "running")

	payload, err := json.Marshal(schemas.SessionCompleteRequest{Branch: schemas.NewSBranch("completed-visible")})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/sessions/complete", bytesReader(payload))
	rec := httptest.NewRecorder()
	server.HandlerCompleteSession(server.Base.Logger, rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("complete status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	completed := waitForSessionState(t, server, "completed-visible", "completed")

	response := listSessions(t, server, "/sessions?limit=10")
	if len(response.Sessions) != 1 {
		t.Fatalf("listed %d sessions, want 1", len(response.Sessions))
	}
	if got := response.Sessions[0].ID; got != completed.StreamID {
		t.Fatalf("listed id = %q, want %q", got, completed.StreamID)
	}
	if got := response.Sessions[0].State; got != "completed" {
		t.Fatalf("listed state = %q, want completed", got)
	}
}

func TestHandlerDeleteSessionEventSourcedPathDeletesSession(t *testing.T) {
	server, _, repoDir, _ := newEventSourcedCreateSessionTestServer(t)

	createResponse := createEventSourcedSession(t, server, repoDir, "delete-me")
	waitForSessionState(t, server, "delete-me", "running")

	payload, err := json.Marshal(schemas.SessionDeleteRequest{Branch: schemas.NewSBranch("delete-me")})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/sessions", bytesReader(payload))
	rec := httptest.NewRecorder()
	server.HandlerDeleteSession(server.Base.Logger, rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	var response schemas.TaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal task response: %v", err)
	}
	if response.TaskID == "" || !strings.HasPrefix(response.TaskID, "session-delete:") {
		t.Fatalf("task id = %q, want session-delete prefix", response.TaskID)
	}
	if response.Type != "session_delete" {
		t.Fatalf("task type = %q, want session_delete", response.Type)
	}
	if response.Status != schemas.TaskStatusPending {
		t.Fatalf("task status = %q, want %q", response.Status, schemas.TaskStatusPending)
	}
	if response.Result == nil || response.Result.Branch != "delete-me" || createResponse.WorktreePath == "" {
		t.Fatalf("unexpected delete task result: %#v", response.Result)
	}

	waitForSessionState(t, server, "delete-me", "deleted")
}

func TestHandlerNukeSessionsEventSourcedPathDeletesActiveSessions(t *testing.T) {
	server, _, repoDir, _ := newEventSourcedCreateSessionTestServer(t)

	createEventSourcedSession(t, server, repoDir, "nuke-a")
	createEventSourcedSession(t, server, repoDir, "nuke-b")
	waitForSessionState(t, server, "nuke-a", "running")
	waitForSessionState(t, server, "nuke-b", "running")

	req := httptest.NewRequest(http.MethodPost, "/sessions/nuke", bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()
	server.HandlerNukeSessions(server.Base.Logger, rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	var response schemas.TaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal nuke response: %v", err)
	}
	if response.Type != "session_nuke" {
		t.Fatalf("task type = %q, want session_nuke", response.Type)
	}
	if response.Status != schemas.TaskStatusSucceeded {
		t.Fatalf("task status = %q, want %q", response.Status, schemas.TaskStatusSucceeded)
	}

	waitForSessionState(t, server, "nuke-a", "deleted")
	waitForSessionState(t, server, "nuke-b", "deleted")

	listReq := httptest.NewRequest(http.MethodGet, "/sessions?status=queued&status=running", nil)
	listRec := httptest.NewRecorder()
	server.HandlerListSessions(server.Base.Logger, listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body=%s", listRec.Code, listRec.Body.String())
	}
	var listResponse schemas.SessionListResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResponse); err != nil {
		t.Fatalf("json.Unmarshal list response: %v", err)
	}
	if len(listResponse.Sessions) != 0 {
		t.Fatalf("expected no queued or running sessions after nuke, got %#v", listResponse.Sessions)
	}
}

func TestHandlerListSessionsSupportsCursorDirections(t *testing.T) {
	server, _, repoDir, _ := newEventSourcedCreateSessionTestServer(t)

	branches := []string{"cursor-a", "cursor-b", "cursor-c", "cursor-d"}
	for _, branch := range branches {
		createEventSourcedSession(t, server, repoDir, branch)
		waitForSessionState(t, server, branch, "running")
		time.Sleep(2 * time.Millisecond)
	}

	fullResponse := listSessions(t, server, "/sessions?limit=10")
	if len(fullResponse.Sessions) != len(branches) {
		t.Fatalf("listed %d sessions, want %d", len(fullResponse.Sessions), len(branches))
	}

	anchorBefore := fullResponse.Sessions[2]
	beforeResponse := listSessions(t, server, "/sessions?limit=1&cursor="+anchorBefore.ID+"&direction=before")
	if len(beforeResponse.Sessions) != 1 {
		t.Fatalf("before listed %d sessions, want 1", len(beforeResponse.Sessions))
	}
	if got, want := beforeResponse.Sessions[0].ID, fullResponse.Sessions[1].ID; got != want {
		t.Fatalf("before id = %q, want %q", got, want)
	}

	anchorAfter := fullResponse.Sessions[1]
	afterResponse := listSessions(t, server, "/sessions?limit=1&cursor="+anchorAfter.ID+"&direction=after")
	if len(afterResponse.Sessions) != 1 {
		t.Fatalf("after listed %d sessions, want 1", len(afterResponse.Sessions))
	}
	if got, want := afterResponse.Sessions[0].ID, fullResponse.Sessions[2].ID; got != want {
		t.Fatalf("after id = %q, want %q", got, want)
	}

	defaultResponse := listSessions(t, server, "/sessions?limit=1&cursor="+anchorAfter.ID)
	if len(defaultResponse.Sessions) != 1 {
		t.Fatalf("default listed %d sessions, want 1", len(defaultResponse.Sessions))
	}
	if got, want := defaultResponse.Sessions[0].ID, fullResponse.Sessions[2].ID; got != want {
		t.Fatalf("default direction id = %q, want %q", got, want)
	}
}

func TestHandlerSessionNavigationWithoutParamsReturnsFirstRunningSession(t *testing.T) {
	server, _, repoDir, _ := newEventSourcedCreateSessionTestServer(t)

	branches := []string{"nav-a", "nav-b", "nav-c"}
	for _, branch := range branches {
		createEventSourcedSession(t, server, repoDir, branch)
		waitForSessionState(t, server, branch, "running")
		time.Sleep(2 * time.Millisecond)
	}

	expected := listSessions(t, server, "/sessions?status=running&limit=1")
	if len(expected.Sessions) != 1 {
		t.Fatalf("expected list returned %d sessions, want 1", len(expected.Sessions))
	}

	nextResponse := navigateSession(t, server, "/_session/next")
	if got, want := len(nextResponse.Sessions), 1; got != want {
		t.Fatalf("next listed %d sessions, want %d", got, want)
	}
	if got, want := nextResponse.Sessions[0].ID, expected.Sessions[0].ID; got != want {
		t.Fatalf("next id = %q, want %q", got, want)
	}

	prevResponse := navigateSession(t, server, "/_session/prev")
	if got, want := len(prevResponse.Sessions), 1; got != want {
		t.Fatalf("prev listed %d sessions, want %d", got, want)
	}
	if got, want := prevResponse.Sessions[0].ID, expected.Sessions[0].ID; got != want {
		t.Fatalf("prev id = %q, want %q", got, want)
	}
}

func TestHandlerSessionNavigationByIDMatchesSessionListing(t *testing.T) {
	server, _, repoDir, _ := newEventSourcedCreateSessionTestServer(t)

	branches := []string{"nav-id-a", "nav-id-b", "nav-id-c", "nav-id-d"}
	for _, branch := range branches {
		createEventSourcedSession(t, server, repoDir, branch)
		waitForSessionState(t, server, branch, "running")
		time.Sleep(2 * time.Millisecond)
	}

	fullResponse := listSessions(t, server, "/sessions?status=running&limit=10")
	anchorID := fullResponse.Sessions[1].ID

	nextResponse := navigateSession(t, server, "/_session/next?id="+anchorID)
	expectedNext := listSessions(t, server, "/sessions?status=running&limit=1&cursor="+anchorID+"&direction=after")
	if got, want := nextResponse, expectedNext; len(got.Sessions) != len(want.Sessions) || got.Sessions[0].ID != want.Sessions[0].ID {
		t.Fatalf("next response = %#v, want %#v", got, want)
	}

	prevResponse := navigateSession(t, server, "/_session/prev?id="+anchorID)
	expectedPrev := listSessions(t, server, "/sessions?status=running&limit=1&cursor="+anchorID+"&direction=before")
	if got, want := prevResponse, expectedPrev; len(got.Sessions) != len(want.Sessions) || got.Sessions[0].ID != want.Sessions[0].ID {
		t.Fatalf("prev response = %#v, want %#v", got, want)
	}
}

func TestHandlerSessionNavigationIDTakesPrecedenceOverBranch(t *testing.T) {
	server, _, repoDir, _ := newEventSourcedCreateSessionTestServer(t)

	branches := []string{"nav-priority-a", "nav-priority-b", "nav-priority-c"}
	for _, branch := range branches {
		createEventSourcedSession(t, server, repoDir, branch)
		waitForSessionState(t, server, branch, "running")
		time.Sleep(2 * time.Millisecond)
	}

	fullResponse := listSessions(t, server, "/sessions?status=running&limit=10")
	anchorID := fullResponse.Sessions[0].ID

	response := navigateSession(t, server, "/_session/next?id="+anchorID+"&branch=nav-priority-c")
	expected := listSessions(t, server, "/sessions?status=running&limit=1&cursor="+anchorID+"&direction=after")
	if got, want := response, expected; len(got.Sessions) != len(want.Sessions) || got.Sessions[0].ID != want.Sessions[0].ID {
		t.Fatalf("response = %#v, want %#v", got, want)
	}
}

func TestHandlerSessionNavigationByBranchResolvesCompletedSessionID(t *testing.T) {
	server, _, repoDir, _ := newEventSourcedCreateSessionTestServer(t)

	createEventSourcedSession(t, server, repoDir, "branch-nav-a")
	createEventSourcedSession(t, server, repoDir, "branch-nav-b")
	createEventSourcedSession(t, server, repoDir, "branch-nav-c")
	waitForSessionState(t, server, "branch-nav-a", "running")
	completedRef := waitForSessionState(t, server, "branch-nav-b", "running")
	waitForSessionState(t, server, "branch-nav-c", "running")
	time.Sleep(2 * time.Millisecond)

	completeSession(t, server, "branch-nav-b")
	completedRef = waitForSessionState(t, server, "branch-nav-b", "completed")

	response := navigateSession(t, server, "/_session/next?branch=branch-nav-b")
	expected := listSessions(t, server, "/sessions?status=running&limit=1&cursor="+completedRef.StreamID+"&direction=after")
	if len(expected.Sessions) == 0 {
		t.Fatalf("expected navigation target for completed branch")
	}
	if got, want := response.Sessions[0].ID, expected.Sessions[0].ID; got != want {
		t.Fatalf("response id = %q, want %q", got, want)
	}
}

func TestHandlerSessionNavigationReturnsEmptyWhenNoMatches(t *testing.T) {
	server, _, repoDir, _ := newEventSourcedCreateSessionTestServer(t)

	createEventSourcedSession(t, server, repoDir, "nav-empty")
	ref := waitForSessionState(t, server, "nav-empty", "running")

	response := navigateSession(t, server, "/_session/next?id="+ref.StreamID)
	if len(response.Sessions) != 0 {
		t.Fatalf("next listed %d sessions, want 0", len(response.Sessions))
	}

	response = navigateSession(t, server, "/_session/next?branch=does-not-exist")
	if len(response.Sessions) != 0 {
		t.Fatalf("branch listed %d sessions, want 0", len(response.Sessions))
	}
}

func TestHandlerListSessionsRejectsInvalidDirection(t *testing.T) {
	server, _, _, _ := newEventSourcedCreateSessionTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/sessions?direction=sideways", nil)
	rec := httptest.NewRecorder()

	server.HandlerListSessions(server.Base.Logger, rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func createEventSourcedSession(t *testing.T, server *Server, repoDir, branch string) schemas.SessionCreateResponse {
	t.Helper()

	payload, err := json.Marshal(schemas.SessionCreateRequest{
		Path:      repoDir,
		Branch:    schemas.NewSBranch(branch),
		BackendID: conf.BackendLocal,
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/sessions", bytesReader(payload))
	rec := httptest.NewRecorder()
	server.HandlerCreateSession(server.Base.Logger, rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	var response schemas.SessionCreateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal create response: %v", err)
	}
	return response
}

func listSessions(t *testing.T, server *Server, target string) schemas.SessionListResponse {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	server.HandlerListSessions(server.Base.Logger, rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var response schemas.SessionListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal list response: %v", err)
	}
	return response
}

func navigateSession(t *testing.T, server *Server, target string) schemas.SessionListResponse {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("navigate status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var response schemas.SessionListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal navigate response: %v", err)
	}
	return response
}

func completeSession(t *testing.T, server *Server, branch string) {
	t.Helper()

	payload, err := json.Marshal(schemas.SessionCompleteRequest{Branch: schemas.NewSBranch(branch)})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/sessions/complete", bytesReader(payload))
	rec := httptest.NewRecorder()
	server.HandlerCompleteSession(server.Base.Logger, rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("complete status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
}

func waitForSessionState(t *testing.T, server *Server, branch, wantState string) sessionevents.SessionRef {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ref, err := server.events.LookupSessionByBranch(context.Background(), branch)
		if err == nil && ref.PublicState == wantState {
			return ref
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("LookupSessionByBranch(%q): %v", branch, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for session %q state %q", branch, wantState)
	return sessionevents.SessionRef{}
}

func waitForProjection(t *testing.T, queries *db.Queries, branch string) db.SessionProjection {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		projection, err := queries.GetSessionProjectionByBranch(context.Background(), branch)
		if err == nil {
			return projection
		}
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("GetSessionProjectionByBranch(%q): %v", branch, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for projection %q", branch)
	return db.SessionProjection{}
}

func bytesReader(payload []byte) *io.SectionReader {
	return io.NewSectionReader(bytes.NewReader(payload), 0, int64(len(payload)))
}
