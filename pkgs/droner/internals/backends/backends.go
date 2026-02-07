package backends

import (
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
}

var ErrUnknownBackend = errors.New("unknown backend")

type Store struct {
	mu       sync.RWMutex
	backends map[BackendID]Backend
}

func NewStore() *Store {
	store := &Store{backends: map[BackendID]Backend{}}
	RegisterLocal(store)
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
