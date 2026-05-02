package session

import (
	"context"
	"sync"
)

// MemoryStore is a simple in-memory implementation of SessionStore.
// It is thread-safe and intended for development and testing.
// Phase 3 replaces this with Redis-backed storage per the roadmap.
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]*Session
}

// NewMemoryStore creates a new in-memory session store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[string]*Session)}
}

// Load implements SessionStore.
func (s *MemoryStore) Load(_ context.Context, id string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if ctx, ok := s.data[id]; ok {
		return ctx, nil
	}
	return nil, ErrSessionNotFound
}

// Save implements SessionStore.
func (s *MemoryStore) Save(_ context.Context, ctx *Session) error {
	s.mu.Lock()
	s.data[ctx.AuthCtxID] = ctx
	s.mu.Unlock()
	return nil
}

// Delete implements SessionStore.
func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	delete(s.data, id)
	s.mu.Unlock()
	return nil
}

// Close implements SessionStore. No-op for in-memory store.
func (s *MemoryStore) Close() error {
	return nil
}

// Size returns the number of sessions in the store (for testing/debugging).
func (s *MemoryStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data)
}
