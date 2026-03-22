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
	"testing"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/core/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/backends"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
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

func (b *createSessionBackend) CreateSession(ctx context.Context, repoPath string, worktreePath string, sessionID string, agentConfig backends.AgentConfig) error {
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

type enqueueRecorderBackend struct {
	enqueueCalls  int
	lastTask      *tasky.Task[core.Jobs]
	enqueueCtxErr error
	hasDeadline   bool
	timeoutWindow time.Duration
	enqueueErr    error
}

func (b *enqueueRecorderBackend) Enqueue(ctx context.Context, task *tasky.Task[core.Jobs], job *tasky.Job[core.Jobs]) error {
	b.enqueueCalls++
	b.enqueueCtxErr = ctx.Err()
	if deadline, ok := ctx.Deadline(); ok {
		b.hasDeadline = true
		b.timeoutWindow = time.Until(deadline)
	}
	copyTask := *task
	b.lastTask = &copyTask
	return b.enqueueErr
}

func (b *enqueueRecorderBackend) Dequeue(ctx context.Context) (core.Jobs, tasky.TaskID, []byte, error) {
	return "", "", nil, context.Canceled
}

func (b *enqueueRecorderBackend) Ack(ctx context.Context, taskID tasky.TaskID) error {
	return nil
}

func (b *enqueueRecorderBackend) Nack(ctx context.Context, taskID tasky.TaskID) error {
	return nil
}

func (b *enqueueRecorderBackend) ForceFlush(ctx context.Context) error {
	return nil
}

func newCreateSessionTestServer(t *testing.T) (*Server, *db.Queries, *enqueueRecorderBackend, string) {
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
			Naming: conf.SessionNamingConfig{
				Strategy: conf.SessionNamingStrategyRandom,
			},
		},
	}

	queries, err := core.InitDB(config)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	store := backends.NewStore(config.Sessions, queries)
	store.Register(&createSessionBackend{worktreeRoot: worktreeDir})

	queueBackend := &enqueueRecorderBackend{}
	queue, err := tasky.NewQueue(tasky.QueueConfig[core.Jobs]{
		Jobs: []tasky.Job[core.Jobs]{
			tasky.NewJob(core.JobCreateSession, tasky.JobConfig[core.Jobs]{
				Run: func(ctx context.Context, task *tasky.Task[core.Jobs]) error { return nil },
			}),
		},
		Backend: queueBackend,
	})
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	server := &Server{Base: &core.BaseServer{
		Config:       config,
		Logger:       logger,
		DB:           queries,
		BackendStore: store,
		TaskQueue:    queue,
	}}

	return server, queries, queueBackend, repoDir
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

func TestHandlerCreateSessionUsesRequestContextForDBCreate(t *testing.T) {
	server, queries, queueBackend, repoDir := newCreateSessionTestServer(t)

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

	if queueBackend.enqueueCalls != 0 {
		t.Fatalf("expected no enqueue calls, got %d", queueBackend.enqueueCalls)
	}
	_, err = queries.GetSessionBySimpleIDAnyStatus(context.Background(), "cancelled-session")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected no session row, got err=%v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestHandlerCreateSessionUsesBoundedBackgroundContextForEnqueue(t *testing.T) {
	server, queries, queueBackend, repoDir := newCreateSessionTestServer(t)

	payload, err := json.Marshal(schemas.SessionCreateRequest{
		Path:      repoDir,
		SessionID: schemas.NewSSessionID("queued-session"),
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
	if queueBackend.enqueueCalls != 1 {
		t.Fatalf("enqueue calls = %d, want 1", queueBackend.enqueueCalls)
	}
	if queueBackend.enqueueCtxErr != nil {
		t.Fatalf("enqueue ctx err = %v, want nil", queueBackend.enqueueCtxErr)
	}
	if !queueBackend.hasDeadline {
		t.Fatal("expected enqueue context to have a deadline")
	}
	if queueBackend.timeoutWindow <= 0 || queueBackend.timeoutWindow > 2*time.Second {
		t.Fatalf("enqueue timeout window = %v, want within 0-2s", queueBackend.timeoutWindow)
	}
	if queueBackend.lastTask == nil || queueBackend.lastTask.JobID != core.JobCreateSession {
		t.Fatalf("expected create-session task to be enqueued, got %#v", queueBackend.lastTask)
	}
	session, err := queries.GetSessionBySimpleIDAnyStatus(context.Background(), "queued-session")
	if err != nil {
		t.Fatalf("GetSessionBySimpleIDAnyStatus: %v", err)
	}
	if session.Status != db.SessionStatusQueued {
		t.Fatalf("status = %s, want %s", session.Status, db.SessionStatusQueued)
	}
}

func TestHandlerCreateSessionPersistsStructuredFilePrompt(t *testing.T) {
	server, queries, _, repoDir := newCreateSessionTestServer(t)

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
	session, err := queries.GetSessionBySimpleIDAnyStatus(context.Background(), "file-prompt-session")
	if err != nil {
		t.Fatalf("GetSessionBySimpleIDAnyStatus: %v", err)
	}
	if !session.AgentConfig.Valid {
		t.Fatal("expected agent config to be stored")
	}
	var stored schemas.SessionAgentConfig
	if err := json.Unmarshal([]byte(session.AgentConfig.String), &stored); err != nil {
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

func bytesReader(payload []byte) *io.SectionReader {
	return io.NewSectionReader(bytes.NewReader(payload), 0, int64(len(payload)))
}
