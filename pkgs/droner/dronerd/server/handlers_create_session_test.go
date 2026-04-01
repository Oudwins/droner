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
	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/backends"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/sessionevents"
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
			Agent: conf.AgentConfig{
				DefaultModel:    "default-model",
				DefaultProvider: conf.AgentProviderOpenCode,
				Providers: conf.AgentProvidersConfig{
					OpenCode: conf.OpenCodeConfig{Hostname: "127.0.0.1", Port: 4096},
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
		SessionID: schemas.NewSSessionID("cancelled-session"),
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
	_, err = projectionQueries.GetSessionProjectionBySimpleID(context.Background(), "cancelled-session")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected no projection row, got err=%v", err)
	}
}

func TestHandlerCreateSessionPersistsStructuredFilePrompt(t *testing.T) {
	server, projectionQueries, repoDir, _ := newEventSourcedCreateSessionTestServer(t)

	payload, err := json.Marshal(schemas.SessionCreateRequest{
		Path:      repoDir,
		SessionID: schemas.NewSSessionID("file-prompt-session"),
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
		SessionID: schemas.NewSSessionID("inline-image-session"),
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
		SessionID: schemas.NewSSessionID("evented-session"),
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
	projection := waitForProjection(t, projectionQueries, "evented-session")
	if projection.SimpleID != "evented-session" {
		t.Fatalf("projection simple_id = %q, want evented-session", projection.SimpleID)
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

	payload, err := json.Marshal(schemas.SessionCompleteRequest{SessionID: schemas.NewSSessionID("complete-me")})
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
	if response.Result == nil || response.Result.SessionID != "complete-me" || response.Result.WorktreePath != createResponse.WorktreePath {
		t.Fatalf("unexpected task result: %#v", response.Result)
	}

	waitForSessionState(t, server, "complete-me", "completed")
}

func TestHandlerDeleteSessionEventSourcedPathDeletesSession(t *testing.T) {
	server, _, repoDir, _ := newEventSourcedCreateSessionTestServer(t)

	createResponse := createEventSourcedSession(t, server, repoDir, "delete-me")
	waitForSessionState(t, server, "delete-me", "running")

	payload, err := json.Marshal(schemas.SessionDeleteRequest{SessionID: schemas.NewSSessionID("delete-me")})
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
	if response.Result == nil || response.Result.SessionID != "delete-me" || createResponse.WorktreePath == "" {
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

	listReq := httptest.NewRequest(http.MethodGet, "/sessions", nil)
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
		t.Fatalf("expected no active sessions after nuke, got %#v", listResponse.Sessions)
	}
}

func createEventSourcedSession(t *testing.T, server *Server, repoDir, sessionID string) schemas.SessionCreateResponse {
	t.Helper()

	payload, err := json.Marshal(schemas.SessionCreateRequest{
		Path:      repoDir,
		SessionID: schemas.NewSSessionID(sessionID),
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

func waitForSessionState(t *testing.T, server *Server, simpleID, wantState string) sessionevents.SessionRef {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ref, err := server.events.LookupSessionBySimpleID(context.Background(), simpleID)
		if err == nil && ref.PublicState == wantState {
			return ref
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("LookupSessionBySimpleID(%q): %v", simpleID, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for session %q state %q", simpleID, wantState)
	return sessionevents.SessionRef{}
}

func waitForProjection(t *testing.T, queries *db.Queries, simpleID string) db.SessionProjection {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		projection, err := queries.GetSessionProjectionBySimpleID(context.Background(), simpleID)
		if err == nil {
			return projection
		}
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("GetSessionProjectionBySimpleID(%q): %v", simpleID, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for projection %q", simpleID)
	return db.SessionProjection{}
}

func bytesReader(payload []byte) *io.SectionReader {
	return io.NewSectionReader(bytes.NewReader(payload), 0, int64(len(payload)))
}
