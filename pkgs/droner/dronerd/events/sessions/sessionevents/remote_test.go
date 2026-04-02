package sessionevents

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	coredb "github.com/Oudwins/droner/pkgs/droner/dronerd/db"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/sessions/sessionslog"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/backends"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/remote"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

type remoteTestBackend struct {
	worktreeRoot string

	createHydrateStatus coredb.SessionStatus
	mu                  sync.Mutex
	createCalls         int
	hydrateCalls        int
	completeCalls       int
	deleteCalls         int
}

func (b *remoteTestBackend) ID() conf.BackendID {
	return conf.BackendLocal
}

func (b *remoteTestBackend) WorktreePath(repoPath string, sessionID string) (string, error) {
	return filepath.Join(b.worktreeRoot, filepath.Base(repoPath)+".."+sessionID), nil
}

func (b *remoteTestBackend) ValidateSessionID(repoPath string, sessionID string) error {
	return nil
}

func (b *remoteTestBackend) CreateSession(ctx context.Context, repoPath string, worktreePath string, sessionID string, agentConfig backends.AgentConfig, opts ...backends.CreateSessionOptions) error {
	b.mu.Lock()
	b.createCalls++
	b.mu.Unlock()
	if len(opts) > 0 && opts[0].NextReusableWorktree != nil {
		candidate, err := opts[0].NextReusableWorktree(ctx)
		if err != nil {
			return err
		}
		if candidate != nil && opts[0].MarkReusableWorktreeDeletion != nil {
			opts[0].MarkReusableWorktreeDeletion(*candidate)
		}
	}
	return nil
}

func (b *remoteTestBackend) HydrateSession(ctx context.Context, session coredb.Session, agentConfig backends.AgentConfig) (backends.HydrationResult, error) {
	b.mu.Lock()
	b.hydrateCalls++
	status := b.createHydrateStatus
	b.mu.Unlock()
	if status == "" {
		status = coredb.SessionStatusRunning
	}
	return backends.HydrationResult{Status: status}, nil
}

func (b *remoteTestBackend) CompleteSession(ctx context.Context, worktreePath string, sessionID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.completeCalls++
	return nil
}

func (b *remoteTestBackend) DeleteSession(ctx context.Context, worktreePath string, sessionID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.deleteCalls++
	return nil
}

func (b *remoteTestBackend) CompleteCalls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.completeCalls
}

func (b *remoteTestBackend) CreateCalls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.createCalls
}

func (b *remoteTestBackend) HydrateCalls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.hydrateCalls
}

type remoteSubscriptionStub struct {
	mu               sync.Mutex
	handlers         map[string]remote.BranchEventHandler
	subscribeCalls   int
	unsubscribeCalls int
}

func newRemoteSubscriptionStub() *remoteSubscriptionStub {
	return &remoteSubscriptionStub{handlers: map[string]remote.BranchEventHandler{}}
}

func (s *remoteSubscriptionStub) subscribe(ctx context.Context, remoteURL string, branch string, handler remote.BranchEventHandler) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscribeCalls++
	s.handlers[remoteSubscriptionKey(remoteURL, branch)] = handler
	return nil
}

func (s *remoteSubscriptionStub) unsubscribe(ctx context.Context, remoteURL string, branch string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unsubscribeCalls++
	delete(s.handlers, remoteSubscriptionKey(remoteURL, branch))
	return nil
}

func (s *remoteSubscriptionStub) emit(event remote.BranchEvent) bool {
	s.mu.Lock()
	handler := s.handlers[remoteSubscriptionKey(event.RemoteURL, event.Branch)]
	s.mu.Unlock()
	if handler == nil {
		return false
	}
	handler(event)
	return true
}

func (s *remoteSubscriptionStub) hasSubscription(remoteURL string, branch string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.handlers[remoteSubscriptionKey(remoteURL, branch)]
	return ok
}

func (s *remoteSubscriptionStub) waitForSubscription(t *testing.T, remoteURL string, branch string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.hasSubscription(remoteURL, branch) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("subscription not registered for %s %s", remoteURL, branch)
}

func newRemoteTestSystem(t *testing.T) (*System, *remoteTestBackend, string, context.CancelFunc) {
	t.Helper()

	dataDir := t.TempDir()
	worktreeDir := filepath.Join(dataDir, "worktrees")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll worktrees: %v", err)
	}

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
		},
	}

	store := backends.NewStore(config)
	backend := &remoteTestBackend{worktreeRoot: worktreeDir}
	store.Register(backend)

	system, err := Open(dataDir, slog.New(slog.NewJSONHandler(io.Discard, nil)), config, store)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	system.Start(ctx)
	t.Cleanup(func() {
		cancel()
		_ = system.Close()
	})

	return system, backend, dataDir, cancel
}

func waitForPublicState(t *testing.T, system *System, branch string, want string) SessionRef {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ref, err := system.LookupSessionByBranch(context.Background(), branch)
		if err == nil && ref.PublicState == want {
			return ref
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("session %s did not reach state %s", branch, want)
	return SessionRef{}
}

func loadEventTypes(t *testing.T, dataDir string, streamID string) []eventlog.EventType {
	t.Helper()
	log, err := sessionslog.Open(dataDir)
	if err != nil {
		t.Fatalf("sessionslog.Open: %v", err)
	}
	defer func() { _ = log.Close() }()

	events, err := log.LoadStream(context.Background(), eventlog.StreamID(streamID), eventlog.LoadStreamOptions{})
	if err != nil {
		t.Fatalf("LoadStream: %v", err)
	}
	types := make([]eventlog.EventType, 0, len(events))
	for _, evt := range events {
		types = append(types, evt.Type)
	}
	return types
}

func assertEventOrder(t *testing.T, got []eventlog.EventType, want ...eventlog.EventType) {
	t.Helper()
	pos := 0
	for _, eventType := range got {
		if pos < len(want) && eventType == want[pos] {
			pos++
		}
	}
	if pos != len(want) {
		t.Fatalf("expected ordered subsequence %v in %v", want, got)
	}
}

func TestRemoteMergedObservationCompletesSession(t *testing.T) {
	stub := newRemoteSubscriptionStub()
	originalSubscribe := subscribeRemoteBranchEvents
	originalUnsubscribe := unsubscribeRemoteBranchEvents
	subscribeRemoteBranchEvents = stub.subscribe
	unsubscribeRemoteBranchEvents = stub.unsubscribe
	t.Cleanup(func() {
		subscribeRemoteBranchEvents = originalSubscribe
		unsubscribeRemoteBranchEvents = originalUnsubscribe
	})

	system, backend, dataDir, _ := newRemoteTestSystem(t)

	const (
		streamID  = "stream-remote-1"
		branch    = "watch-branch"
		repoPath  = "/tmp/repo"
		remoteURL = "git@github.com:org/repo.git"
	)

	if _, err := system.CreateSession(context.Background(), CreateSessionInput{
		StreamID:     streamID,
		Harness:      conf.HarnessOpenCode,
		Branch:       branch,
		BackendID:    conf.BackendLocal,
		RepoPath:     repoPath,
		WorktreePath: filepath.Join(dataDir, "worktrees", "repo..watch-branch"),
		RemoteURL:    remoteURL,
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	waitForPublicState(t, system, branch, "running")
	stub.waitForSubscription(t, remoteURL, branch)

	if !stub.emit(remote.BranchEvent{
		Type:      remote.PRMerged,
		RemoteURL: remoteURL,
		Branch:    branch,
		Timestamp: time.Now().UTC(),
	}) {
		t.Fatal("expected remote event handler to be registered")
	}

	waitForPublicState(t, system, branch, "completed")
	if backend.CompleteCalls() != 1 {
		t.Fatalf("expected 1 completion call, got %d", backend.CompleteCalls())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !stub.hasSubscription(remoteURL, branch) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if stub.hasSubscription(remoteURL, branch) {
		t.Fatal("expected remote subscription to be removed after completion")
	}

	eventTypes := loadEventTypes(t, dataDir, streamID)
	assertEventOrder(t, eventTypes,
		eventTypeSessionQueued,
		eventTypeSessionReady,
		eventTypeRemotePRMerged,
		eventTypeSessionCompletionRequested,
		eventTypeSessionCompletionStarted,
		eventTypeSessionCompletionSuccess,
	)
}

func TestHydrateRequestsRestartProvisioningForReadySession(t *testing.T) {
	system, backend, dataDir, _ := newRemoteTestSystem(t)

	const (
		streamID = "stream-hydrate-1"
		branch   = "hydrate-branch"
	)

	if _, err := system.CreateSession(context.Background(), CreateSessionInput{
		StreamID:     streamID,
		Harness:      conf.HarnessOpenCode,
		Branch:       branch,
		BackendID:    conf.BackendLocal,
		RepoPath:     "/tmp/repo",
		WorktreePath: filepath.Join(dataDir, "worktrees", "repo..hydrate-branch"),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	waitForPublicState(t, system, branch, "running")
	beforeCreateCalls := backend.CreateCalls()
	if beforeCreateCalls == 0 {
		t.Fatal("expected initial create provisioning to run")
	}

	if err := system.Hydrate(context.Background()); err != nil {
		t.Fatalf("Hydrate: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if backend.HydrateCalls() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if backend.HydrateCalls() == 0 {
		t.Fatal("expected hydrate session to be called for ready session")
	}
	if backend.CreateCalls() != beforeCreateCalls {
		t.Fatalf("expected no additional create provisioning for ready session, create calls before=%d after=%d", beforeCreateCalls, backend.CreateCalls())
	}

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		eventTypes := loadEventTypes(t, dataDir, streamID)
		pos := 0
		want := []eventlog.EventType{
			eventTypeSessionQueued,
			eventTypeSessionReady,
			eventTypeSessionHydrationRequested,
			eventTypeSessionEnvironmentProvisioningStarted,
			eventTypeSessionEnvironmentProvisioningSuccess,
			eventTypeSessionReady,
		}
		for _, eventType := range eventTypes {
			if pos < len(want) && eventType == want[pos] {
				pos++
			}
		}
		if pos == len(want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected hydration restart event sequence for %s", streamID)
}

func TestCreateSessionRequestsDeletionForReusedCompletedCandidate(t *testing.T) {
	system, backend, _, _ := newRemoteTestSystem(t)

	oldCreatedAt := time.Now().UTC().Add(-time.Hour)
	if err := system.queries.UpsertSessionProjection(context.Background(), coredb.UpsertSessionProjectionParams{
		StreamID:       "old-stream",
		Branch:         "old-branch",
		BackendID:      conf.BackendLocal.String(),
		RepoPath:       "/tmp/repo",
		WorktreePath:   filepath.Join(backend.worktreeRoot, "repo..old-branch"),
		RemoteUrl:      "",
		AgentConfig:    "",
		LifecycleState: string(eventTypeSessionCompletionSuccess),
		PublicState:    "completed",
		LastError:      "",
		CreatedAt:      oldCreatedAt,
		UpdatedAt:      oldCreatedAt,
	}); err != nil {
		t.Fatalf("UpsertSessionProjection old: %v", err)
	}

	if _, err := system.CreateSession(context.Background(), CreateSessionInput{
		StreamID:     "new-stream",
		Harness:      conf.HarnessOpenCode,
		Branch:       "new-branch",
		BackendID:    conf.BackendLocal,
		RepoPath:     "/tmp/repo",
		WorktreePath: filepath.Join(backend.worktreeRoot, "repo..new-branch"),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	waitForPublicState(t, system, "new-branch", "running")
	waitForPublicState(t, system, "old-branch", "deleted")
	if backend.deleteCalls != 1 {
		t.Fatalf("expected old reused session delete to run once, got %d", backend.deleteCalls)
	}

	eventTypes := loadEventTypes(t, system.config.Server.DataDir, "old-stream")
	assertEventOrder(t, eventTypes,
		eventTypeSessionDeletionRequested,
		eventTypeSessionDeletionStarted,
		eventTypeSessionDeletionSuccess,
	)
}
