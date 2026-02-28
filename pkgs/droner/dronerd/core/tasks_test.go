package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/backends"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
	"github.com/google/uuid"

	_ "modernc.org/sqlite"
)

type fakeBackend struct {
	worktreeRoot          string
	createdWorktreeRepo   string
	createdWorktreePath   string
	createdWorktreeBranch string
	createdTmuxName       string
	createdTmuxPath       string
	createdTmuxModel      string
	createdMessageParts   []messages.MessagePart
	createdOpencodeConfig conf.OpenCodeConfig
	removedWorktreePath   string
	killedTmuxName        string
	completedTmuxName     string
}

func (f *fakeBackend) ID() conf.BackendID {
	return conf.BackendLocal
}

func (f *fakeBackend) WorktreePath(repoPath string, sessionID string) (string, error) {
	repoName := filepath.Base(repoPath)
	worktreeName := repoName + ".." + sessionID
	return filepath.Join(f.worktreeRoot, worktreeName), nil
}

func (f *fakeBackend) ValidateSessionID(repoPath string, sessionID string) error {
	return nil
}

func (f *fakeBackend) CreateSession(_ context.Context, repoPath string, worktreePath string, sessionID string, agentConfig backends.AgentConfig) error {
	f.createdWorktreeRepo = repoPath
	f.createdWorktreePath = worktreePath
	f.createdWorktreeBranch = sessionID
	f.createdTmuxName = sessionID
	f.createdTmuxPath = worktreePath
	f.createdTmuxModel = agentConfig.Model
	if agentConfig.Message != nil {
		f.createdMessageParts = agentConfig.Message.Parts
	}
	f.createdOpencodeConfig = agentConfig.Opencode
	return nil
}

func (f *fakeBackend) DeleteSession(_ context.Context, worktreePath string, sessionID string) error {
	f.removedWorktreePath = worktreePath
	f.killedTmuxName = sessionID
	return nil
}

func (f *fakeBackend) CompleteSession(_ context.Context, _ string, sessionID string) error {
	f.completedTmuxName = sessionID
	return nil
}

func setupTestBase(t *testing.T) (*BaseServer, *fakeBackend, string) {
	t.Helper()

	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	worktreesDir := filepath.Join(tempDir, "worktrees")
	queueDir := filepath.Join(tempDir, "queue")
	dbDir := filepath.Join(tempDir, "db")

	for _, dir := range []string{repoDir, worktreesDir, queueDir, dbDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("failed to create dir %s: %v", dir, err)
		}
	}

	dbPath := filepath.Join(dbDir, "droner.db")
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	schemas, err := loadSchemas()
	if err != nil {
		_ = conn.Close()
		t.Fatalf("failed to load schemas: %v", err)
	}
	for _, schema := range schemas {
		if _, err := conn.Exec(schema); err != nil {
			_ = conn.Close()
			t.Fatalf("failed to apply schema: %v", err)
		}
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	config := &conf.Config{
		Server: conf.ServerConfig{DataDir: tempDir},
		Sessions: conf.SessionsConfig{
			Agent: conf.AgentConfig{
				DefaultModel:    "default-model",
				DefaultProvider: conf.AgentProviderOpenCode,
				Providers: conf.AgentProvidersConfig{
					OpenCode: conf.OpenCodeConfig{Hostname: "127.0.0.1", Port: 4096},
				},
			},
			Backends: conf.BackendsConfig{
				Local: conf.LocalBackendConfig{WorktreeDir: worktreesDir},
			},
		},
	}

	backend := &fakeBackend{worktreeRoot: worktreesDir}
	backendStore := backends.NewStore(config.Sessions, nil)
	backendStore.Register(backend)
	base := &BaseServer{
		Config:       config,
		Logger:       logger,
		BackendStore: backendStore,
		DB:           db.New(conn),
	}

	queue, err := NewQueue(base)
	if err != nil {
		t.Fatalf("failed to create queue: %v", err)
	}
	base.TaskQueue = queue
	base.Subscriptions = newSubscriptionManager(base)

	return base, backend, repoDir
}

func runQueueUntil(t *testing.T, queue *tasky.Queue[Jobs], waitFn func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	consumer := tasky.NewConsumer(queue, tasky.ConsumerOptions{Workers: 1})
	consumer.Start(ctx)

	deadline := time.Now().Add(2 * time.Second)
	completed := false
	for time.Now().Before(deadline) {
		if waitFn() {
			cancel()
			completed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer shutdownCancel()
	if err := consumer.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("consumer shutdown failed: %v", err)
	}

	err := <-consumer.Err()
	if err != nil && ctx.Err() == nil {
		t.Fatalf("consumer error: %v", err)
	}
	if !completed {
		t.Fatalf("timed out waiting for task to complete")
	}
}

func waitForSessionStatusBySimpleID(t *testing.T, queries *db.Queries, simpleID string, status db.SessionStatus) db.Session {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		row, err := queries.GetSessionBySimpleID(context.Background(), simpleID)
		if err == nil && row.Status == status {
			return row
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for session %s to reach status %s", simpleID, status)
	return db.Session{}
}

func waitForSessionStatusByID(t *testing.T, queries *db.Queries, id string, status db.SessionStatus) db.Session {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		row, err := queries.GetSessionByID(context.Background(), id)
		if err == nil && row.Status == status {
			return row
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for session %s to reach status %s", id, status)
	return db.Session{}
}

func TestCreateSessionTaskCreatesRecordAndMarksRunning(t *testing.T) {
	base, backend, repoDir := setupTestBase(t)

	payload := schemas.SessionCreateRequest{
		Path:      repoDir,
		SessionID: schemas.NewSSessionID("session-1"),
		BackendID: conf.BackendLocal,
		AgentConfig: &schemas.SessionAgentConfig{
			Model: "test-model",
			Message: &messages.Message{
				Parts: []messages.MessagePart{{Type: "text", Text: "test-prompt"}},
			},
		},
	}

	newID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("failed to create uuid: %v", err)
	}

	worktreePath, err := backend.WorktreePath(payload.Path, payload.SessionID.String())
	if err != nil {
		t.Fatalf("failed to resolve worktree path: %v", err)
	}

	_, err = base.DB.CreateSession(context.Background(), db.CreateSessionParams{
		ID:           newID.String(),
		SimpleID:     payload.SessionID.String(),
		Status:       db.SessionStatusQueued,
		BackendID:    "local",
		RepoPath:     payload.Path,
		WorktreePath: worktreePath,
		AgentConfig:  sql.NullString{},
		Error:        sql.NullString{},
	})
	if err != nil {
		t.Fatalf("failed to create session record: %v", err)
	}

	bytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	_, err = base.TaskQueue.Enqueue(context.Background(), tasky.NewTask(JobCreateSession, bytes))
	if err != nil {
		t.Fatalf("failed to enqueue task: %v", err)
	}

	runQueueUntil(t, base.TaskQueue, func() bool {
		row, err := base.DB.GetSessionBySimpleID(context.Background(), payload.SessionID.String())
		return err == nil && row.Status == db.SessionStatusRunning
	})

	session := waitForSessionStatusBySimpleID(t, base.DB, payload.SessionID.String(), db.SessionStatusRunning)
	if session.RepoPath != payload.Path {
		t.Fatalf("expected repo path %s, got %s", payload.Path, session.RepoPath)
	}
	if session.WorktreePath != worktreePath {
		t.Fatalf("expected worktree path %s, got %s", worktreePath, session.WorktreePath)
	}
	if backend.createdWorktreeRepo != payload.Path {
		t.Fatalf("expected git worktree repo %s, got %s", payload.Path, backend.createdWorktreeRepo)
	}
	if backend.createdWorktreePath != worktreePath {
		t.Fatalf("expected git worktree path %s, got %s", worktreePath, backend.createdWorktreePath)
	}
	if backend.createdTmuxName != payload.SessionID.String() {
		t.Fatalf("expected tmux session name %s, got %s", payload.SessionID.String(), backend.createdTmuxName)
	}
}

func TestDeleteSessionTaskMarksDeleted(t *testing.T) {
	base, backend, repoDir := setupTestBase(t)

	newID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("failed to create uuid: %v", err)
	}

	simpleID := "session-delete"
	worktreePath, err := backend.WorktreePath(repoDir, simpleID)
	if err != nil {
		t.Fatalf("failed to resolve worktree path: %v", err)
	}

	created, err := base.DB.CreateSession(context.Background(), db.CreateSessionParams{
		ID:           newID.String(),
		SimpleID:     simpleID,
		Status:       db.SessionStatusRunning,
		BackendID:    "local",
		RepoPath:     repoDir,
		WorktreePath: worktreePath,
		AgentConfig:  sql.NullString{},
		Error:        sql.NullString{},
	})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	deletePayload := schemas.SessionDeleteRequest{SessionID: schemas.NewSSessionID(simpleID)}
	bytes, err := json.Marshal(deletePayload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	_, err = base.TaskQueue.Enqueue(context.Background(), tasky.NewTask(JobDeleteSession, bytes))
	if err != nil {
		t.Fatalf("failed to enqueue delete task: %v", err)
	}

	runQueueUntil(t, base.TaskQueue, func() bool {
		row, err := base.DB.GetSessionByID(context.Background(), created.ID)
		return err == nil && row.Status == db.SessionStatusDeleted
	})

	session := waitForSessionStatusByID(t, base.DB, created.ID, db.SessionStatusDeleted)
	if session.Status != db.SessionStatusDeleted {
		t.Fatalf("expected status deleted, got %s", session.Status)
	}
	if backend.killedTmuxName != simpleID {
		t.Fatalf("expected tmux session kill %s, got %s", simpleID, backend.killedTmuxName)
	}
	if backend.removedWorktreePath != worktreePath {
		t.Fatalf("expected worktree removal %s, got %s", worktreePath, backend.removedWorktreePath)
	}
}

func TestCompleteSessionTaskMarksCompletedAndKeepsWorktree(t *testing.T) {
	base, backend, repoDir := setupTestBase(t)

	newID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("failed to create uuid: %v", err)
	}

	simpleID := "session-complete"
	worktreePath, err := backend.WorktreePath(repoDir, simpleID)
	if err != nil {
		t.Fatalf("failed to resolve worktree path: %v", err)
	}

	created, err := base.DB.CreateSession(context.Background(), db.CreateSessionParams{
		ID:           newID.String(),
		SimpleID:     simpleID,
		Status:       db.SessionStatusRunning,
		BackendID:    "local",
		RepoPath:     repoDir,
		WorktreePath: worktreePath,
		AgentConfig:  sql.NullString{},
		Error:        sql.NullString{},
	})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	completePayload := schemas.SessionCompleteRequest{SessionID: schemas.NewSSessionID(simpleID)}
	bytes, err := json.Marshal(completePayload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	_, err = base.TaskQueue.Enqueue(context.Background(), tasky.NewTask(JobCompleteSession, bytes))
	if err != nil {
		t.Fatalf("failed to enqueue complete task: %v", err)
	}

	runQueueUntil(t, base.TaskQueue, func() bool {
		row, err := base.DB.GetSessionByID(context.Background(), created.ID)
		return err == nil && row.Status == db.SessionStatusCompleted
	})

	session := waitForSessionStatusByID(t, base.DB, created.ID, db.SessionStatusCompleted)
	if session.Status != db.SessionStatusCompleted {
		t.Fatalf("expected status completed, got %s", session.Status)
	}
	if backend.completedTmuxName != simpleID {
		t.Fatalf("expected tmux session complete %s, got %s", simpleID, backend.completedTmuxName)
	}
	if backend.removedWorktreePath != "" {
		t.Fatalf("expected worktree to be kept, got removal %s", backend.removedWorktreePath)
	}
}
