package server

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/core/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/backends"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
	"github.com/google/uuid"
)

type hydrationBackend struct {
	result          backends.HydrationResult
	hydrateCalls    int
	hydratedSession db.Session
	hydratedConfig  backends.AgentConfig
}

func (h *hydrationBackend) ID() conf.BackendID {
	return conf.BackendLocal
}

func (h *hydrationBackend) WorktreePath(repoPath string, sessionID string) (string, error) {
	return repoPath + "/" + sessionID, nil
}

func (h *hydrationBackend) ValidateSessionID(repoPath string, sessionID string) error {
	return nil
}

func (h *hydrationBackend) CreateSession(ctx context.Context, repoPath string, worktreePath string, sessionID string, agentConfig backends.AgentConfig) error {
	return nil
}

func (h *hydrationBackend) HydrateSession(ctx context.Context, session db.Session, agentConfig backends.AgentConfig) (backends.HydrationResult, error) {
	h.hydrateCalls++
	h.hydratedSession = session
	h.hydratedConfig = agentConfig
	if h.result.Status == "" {
		return backends.HydrationResult{Status: db.SessionStatusRunning}, nil
	}
	return h.result, nil
}

func (h *hydrationBackend) CompleteSession(ctx context.Context, worktreePath string, sessionID string) error {
	return nil
}

func (h *hydrationBackend) DeleteSession(ctx context.Context, worktreePath string, sessionID string) error {
	return nil
}

func newHydrationTestServer(t *testing.T, backend *hydrationBackend) *Server {
	t.Helper()
	return newHydrationTestServerWithLogger(t, backend, slog.New(slog.NewJSONHandler(io.Discard, nil)))
}

func newHydrationTestServerWithLogger(t *testing.T, backend *hydrationBackend, logger *slog.Logger) *Server {
	t.Helper()

	config := &conf.Config{
		Server: conf.ServerConfig{DataDir: t.TempDir()},
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
				Local:   conf.LocalBackendConfig{WorktreeDir: t.TempDir()},
			},
		},
	}

	queries, err := core.InitDB(config)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	store := backends.NewStore(config.Sessions, queries)
	store.Register(backend)

	return &Server{Base: &core.BaseServer{
		Config:       config,
		Logger:       logger,
		DB:           queries,
		BackendStore: store,
	}}
}

func createHydrationTestSession(t *testing.T, queries *db.Queries, status db.SessionStatus, agentConfig sql.NullString) db.Session {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	simpleID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}

	session, err := queries.CreateSession(context.Background(), db.CreateSessionParams{
		ID:           id.String(),
		SimpleID:     simpleID.String(),
		Status:       status,
		BackendID:    conf.BackendLocal.String(),
		RepoPath:     "/repo",
		WorktreePath: "/repo/worktree",
		AgentConfig:  agentConfig,
		Error:        sql.NullString{},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return session
}

func TestHydrateRunningSessionsOnlyHydratesRunningSessions(t *testing.T) {
	backend := &hydrationBackend{}
	srv := newHydrationTestServer(t, backend)

	createHydrationTestSession(t, srv.Base.DB, db.SessionStatusRunning, sql.NullString{})
	createHydrationTestSession(t, srv.Base.DB, db.SessionStatusQueued, sql.NullString{})

	if err := srv.hydrateRunningSessions(context.Background()); err != nil {
		t.Fatalf("hydrateRunningSessions: %v", err)
	}
	if backend.hydrateCalls != 1 {
		t.Fatalf("hydrateCalls = %d, want 1", backend.hydrateCalls)
	}
}

func TestHydrateRunningSessionsAppliesBackendStatusUpdate(t *testing.T) {
	backend := &hydrationBackend{result: backends.HydrationResult{Status: db.SessionStatusDeleted}}
	srv := newHydrationTestServer(t, backend)
	created := createHydrationTestSession(t, srv.Base.DB, db.SessionStatusRunning, sql.NullString{})

	if err := srv.hydrateRunningSessions(context.Background()); err != nil {
		t.Fatalf("hydrateRunningSessions: %v", err)
	}

	updated, err := srv.Base.DB.GetSessionByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetSessionByID: %v", err)
	}
	if updated.Status != db.SessionStatusDeleted {
		t.Fatalf("status = %s, want %s", updated.Status, db.SessionStatusDeleted)
	}
}

func TestHydrateRunningSessionsBuildsAgentConfigFromPersistedSession(t *testing.T) {
	backend := &hydrationBackend{}
	srv := newHydrationTestServer(t, backend)

	agentConfig := sql.NullString{String: `{"model":"openai/gpt-5-mini","agentName":"plan","message":{"parts":[{"type":"text","text":"hello"}]}}`, Valid: true}
	createHydrationTestSession(t, srv.Base.DB, db.SessionStatusRunning, agentConfig)

	if err := srv.hydrateRunningSessions(context.Background()); err != nil {
		t.Fatalf("hydrateRunningSessions: %v", err)
	}

	if backend.hydratedConfig.Model != "openai/gpt-5-mini" {
		t.Fatalf("model = %q, want %q", backend.hydratedConfig.Model, "openai/gpt-5-mini")
	}
	if backend.hydratedConfig.AgentName != "plan" {
		t.Fatalf("agentName = %q, want %q", backend.hydratedConfig.AgentName, "plan")
	}
	if backend.hydratedConfig.Message == nil || len(backend.hydratedConfig.Message.Parts) != 1 {
		t.Fatalf("expected persisted message to be passed to backend hydration")
	}
	if backend.hydratedConfig.Message.Parts[0] != (messages.NewTextPart("hello")) {
		t.Fatalf("message part = %#v, want %#v", backend.hydratedConfig.Message.Parts[0], messages.NewTextPart("hello"))
	}
	if backend.hydratedConfig.Opencode.Hostname != "127.0.0.1" || backend.hydratedConfig.Opencode.Port != 4096 {
		t.Fatalf("unexpected opencode config: %#v", backend.hydratedConfig.Opencode)
	}
}

func TestHydrateRunningSessionsLogsSummary(t *testing.T) {
	backend := &hydrationBackend{result: backends.HydrationResult{Status: db.SessionStatusDeleted}}
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	srv := newHydrationTestServerWithLogger(t, backend, logger)

	createHydrationTestSession(t, srv.Base.DB, db.SessionStatusRunning, sql.NullString{})
	createHydrationTestSession(t, srv.Base.DB, db.SessionStatusRunning, sql.NullString{})
	backend.result = backends.HydrationResult{Status: db.SessionStatusRunning}
	if err := srv.hydrateRunningSessions(context.Background()); err != nil {
		t.Fatalf("hydrateRunningSessions: %v", err)
	}

	logs := logBuf.String()
	if !strings.Contains(logs, "startup session hydration complete") {
		t.Fatalf("expected hydration summary log, got %q", logs)
	}
	if !strings.Contains(logs, `"total":2`) {
		t.Fatalf("expected total count in logs, got %q", logs)
	}
	if !strings.Contains(logs, `"running":2`) {
		t.Fatalf("expected running count in logs, got %q", logs)
	}
	if !strings.Contains(logs, `"deleted":0`) {
		t.Fatalf("expected deleted count in logs, got %q", logs)
	}
}
