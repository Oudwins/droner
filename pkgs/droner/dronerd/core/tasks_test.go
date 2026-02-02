package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
	"github.com/google/uuid"

	_ "modernc.org/sqlite"
)

type fakeWorkspace struct {
	createdWorktreeRepo   string
	createdWorktreePath   string
	createdWorktreeBranch string
	createdTmuxName       string
	createdTmuxPath       string
	createdTmuxModel      string
	createdTmuxPrompt     string
	removedWorktreePath   string
	killedTmuxName        string
	deletedBranchName     string
	commonGitDirPath      string
}

func (f *fakeWorkspace) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

func (f *fakeWorkspace) ReadDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}

func (f *fakeWorkspace) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (f *fakeWorkspace) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (f *fakeWorkspace) GitIsInsideWorkTree(repoPath string) error {
	return nil
}

func (f *fakeWorkspace) CreateGitWorktree(repoPath string, worktreePath string, branchName string) error {
	f.createdWorktreeRepo = repoPath
	f.createdWorktreePath = worktreePath
	f.createdWorktreeBranch = branchName
	return nil
}

func (f *fakeWorkspace) RemoveGitWorktree(worktreePath string) error {
	f.removedWorktreePath = worktreePath
	return nil
}

func (f *fakeWorkspace) GitCommonDirFromWorktree(worktreePath string) (string, error) {
	f.commonGitDirPath = filepath.Join(worktreePath, "common.git")
	return f.commonGitDirPath, nil
}

func (f *fakeWorkspace) DeleteGitBranch(commonGitDir string, sessionID string) error {
	f.deletedBranchName = sessionID
	return nil
}

func (f *fakeWorkspace) GetRemoteURL(repoPath string) (string, error) {
	return "", nil
}

func (f *fakeWorkspace) GetRemoteURLFromWorktree(worktreePath string) (string, error) {
	return "", nil
}

func (f *fakeWorkspace) RunWorktreeSetup(repoPath string, worktreePath string) error {
	return nil
}

func (f *fakeWorkspace) CreateTmuxSession(sessionName string, worktreePath string, model string, prompt string) error {
	f.createdTmuxName = sessionName
	f.createdTmuxPath = worktreePath
	f.createdTmuxModel = model
	f.createdTmuxPrompt = prompt
	return nil
}

func (f *fakeWorkspace) KillTmuxSession(sessionName string) error {
	f.killedTmuxName = sessionName
	return nil
}

func setupTestBase(t *testing.T) (*BaseServer, *fakeWorkspace, string) {
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
	if _, err := conn.Exec(readSchema(t)); err != nil {
		_ = conn.Close()
		t.Fatalf("failed to apply schema: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	ws := &fakeWorkspace{}
	config := &conf.Config{
		Server:    conf.ServerConfig{DataDir: tempDir},
		Worktrees: conf.WorktreesConfig{Dir: worktreesDir},
	}
	base := &BaseServer{
		Config:    config,
		Logger:    logger,
		Workspace: ws,
		DB:        db.New(conn),
	}

	queue, err := NewQueue(base)
	if err != nil {
		t.Fatalf("failed to create queue: %v", err)
	}
	base.TaskQueue = queue
	base.Subscriptions = newSubscriptionManager(base)

	return base, ws, repoDir
}

func readSchema(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve test file path")
	}
	coreDir := filepath.Dir(file)
	schemaPath := filepath.Join(coreDir, "db", "schemas", "sessions.sql")
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("failed to read schema: %v", err)
	}
	return string(data)
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
	base, ws, repoDir := setupTestBase(t)

	payload := schemas.SessionCreateRequest{
		Path:      repoDir,
		SessionID: "session-1",
		Agent: &schemas.SessionAgentConfig{
			Model:  "test-model",
			Prompt: "test-prompt",
		},
	}

	newID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("failed to create uuid: %v", err)
	}

	repoName := filepath.Base(payload.Path)
	worktreePath := path.Join(base.Config.Worktrees.Dir, repoName+delimiter+sessionIdToPathIdentifier(payload.SessionID))

	_, err = base.DB.CreateSession(context.Background(), db.CreateSessionParams{
		ID:           newID.String(),
		SimpleID:     payload.SessionID,
		Status:       db.SessionStatusQueued,
		RepoPath:     payload.Path,
		WorktreePath: worktreePath,
		Payload:      sql.NullString{},
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
		row, err := base.DB.GetSessionBySimpleID(context.Background(), payload.SessionID)
		return err == nil && row.Status == db.SessionStatusRunning
	})

	session := waitForSessionStatusBySimpleID(t, base.DB, payload.SessionID, db.SessionStatusRunning)
	if session.RepoPath != payload.Path {
		t.Fatalf("expected repo path %s, got %s", payload.Path, session.RepoPath)
	}
	if session.WorktreePath != worktreePath {
		t.Fatalf("expected worktree path %s, got %s", worktreePath, session.WorktreePath)
	}
	if ws.createdWorktreeRepo != payload.Path {
		t.Fatalf("expected git worktree repo %s, got %s", payload.Path, ws.createdWorktreeRepo)
	}
	if ws.createdWorktreePath != worktreePath {
		t.Fatalf("expected git worktree path %s, got %s", worktreePath, ws.createdWorktreePath)
	}
	if ws.createdTmuxName != payload.SessionID {
		t.Fatalf("expected tmux session name %s, got %s", payload.SessionID, ws.createdTmuxName)
	}
}

func TestDeleteSessionTaskMarksDeleted(t *testing.T) {
	base, ws, repoDir := setupTestBase(t)

	newID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("failed to create uuid: %v", err)
	}

	simpleID := "session-delete"
	repoName := filepath.Base(repoDir)
	worktreePath := path.Join(base.Config.Worktrees.Dir, repoName+delimiter+sessionIdToPathIdentifier(simpleID))

	created, err := base.DB.CreateSession(context.Background(), db.CreateSessionParams{
		ID:           newID.String(),
		SimpleID:     simpleID,
		Status:       db.SessionStatusRunning,
		RepoPath:     repoDir,
		WorktreePath: worktreePath,
		Payload:      sql.NullString{},
		Error:        sql.NullString{},
	})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	deletePayload := schemas.SessionDeleteRequest{SessionID: simpleID}
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
	if ws.killedTmuxName != simpleID {
		t.Fatalf("expected tmux session kill %s, got %s", simpleID, ws.killedTmuxName)
	}
	if ws.removedWorktreePath != worktreePath {
		t.Fatalf("expected worktree removal %s, got %s", worktreePath, ws.removedWorktreePath)
	}
	if ws.deletedBranchName != simpleID {
		t.Fatalf("expected branch deletion %s, got %s", simpleID, ws.deletedBranchName)
	}
}
