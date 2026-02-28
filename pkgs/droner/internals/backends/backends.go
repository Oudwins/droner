package backends

import (
	"context"
	"errors"
	"sync"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/core/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
)

type Backend interface {
	ID() conf.BackendID
	WorktreePath(repoPath string, sessionID string) (string, error)
	ValidateSessionID(repoPath string, sessionID string) error
	CreateSession(ctx context.Context, repoPath string, worktreePath string, sessionID string, agentConfig AgentConfig) error
	// CompleteSession stops the active session runtime (e.g. tmux/opencode) but keeps the worktree/branch for reuse.
	CompleteSession(ctx context.Context, worktreePath string, sessionID string) error
	DeleteSession(ctx context.Context, worktreePath string, sessionID string) error
}

var ErrUnknownBackend = errors.New("unknown backend")

type Store struct {
	mu       sync.RWMutex
	backends map[conf.BackendID]Backend
}

type AgentConfig struct {
	Model    string
	Message  *messages.Message
	Opencode conf.OpenCodeConfig
}

func NewStore(config conf.SessionsConfig, queries *db.Queries) *Store {
	store := &Store{backends: map[conf.BackendID]Backend{}}
	RegisterLocal(store, &config.Backends.Local, queries)
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

func (s *Store) Get(backendID conf.BackendID) (Backend, error) {
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
