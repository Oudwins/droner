package backends

import (
	"context"
	"errors"
	"sync"
)

type BackendID string

func (b BackendID) String() string {
	return string(b)
}

const (
	BackendLocal BackendID = "local"
)

type Backend interface {
	ID() BackendID
	WorktreePath(repoPath string, sessionID string) (string, error)
	ValidateSessionID(repoPath string, sessionID string) error
	CreateSession(ctx context.Context, repoPath string, worktreePath string, sessionID string, agentConfig AgentConfig) error
	DeleteSession(ctx context.Context, worktreePath string, sessionID string) error
}

type SessionsConfig struct {
	DefaultBackend BackendID      `json:"default_backend"`
	Agent          AgentDefaults  `json:"agent"`
	Backends       BackendsConfig `json:"backends"`
}

type BackendsConfig struct {
	Local LocalBackendConfig `json:"local"`
}

type LocalBackendConfig struct {
	WorktreeDir string `json:"worktree_dir"`
}

type AgentDefaults struct {
	DefaultModel string `json:"default_model"`
}

var ErrUnknownBackend = errors.New("unknown backend")

type Store struct {
	mu       sync.RWMutex
	backends map[BackendID]Backend
}

type AgentConfig struct {
	Model  string
	Prompt string
}

func NewStore(config SessionsConfig) *Store {
	store := &Store{backends: map[BackendID]Backend{}}
	RegisterLocal(store, config.Backends.Local.WorktreeDir)
	return store
}

func (s *Store) Register(backend Backend) {
	if s == nil || backend == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backends[backend.ID()] = backend
}

func (s *Store) Get(backendID BackendID) (Backend, error) {
	if s == nil {
		return nil, ErrUnknownBackend
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	backend, ok := s.backends[backendID]
	if !ok {
		return nil, ErrUnknownBackend
	}
	return backend, nil
}

var AllBackendIDs = []BackendID{BackendLocal}
