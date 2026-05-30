// Package eap provides EAP (Extensible Authentication Protocol) engine implementation.
// Spec: TS 33.501 §5.13, RFC 3748
package eap

import (
	"context"
	"crypto/sha256"
	"sync"
	"time"
)

// sha256Hash computes the SHA-256 hash of data.
func sha256Hash(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

// bytesEqual performs constant-time comparison of two byte slices.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// sessionManager manages in-memory EAP sessions with TTL expiry.
// Thread-safe. Implements SessionStore interface.
type sessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
}

// newSessionManager creates a new session manager with the given TTL.
func newSessionManager(ttl time.Duration) *sessionManager {
	return &sessionManager{
		sessions: make(map[string]*Session),
		ttl:      ttl,
	}
}

// get returns a session by authCtxID (unexported, used internally).
func (m *sessionManager) get(ctx context.Context, authCtxID string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[authCtxID]
	if !ok {
		return nil, ErrSessionNotFound
	}

	// Check TTL.
	if session.IsExpired(m.ttl) {
		return nil, ErrSessionNotFound
	}

	return session, nil
}

// put stores or updates a session (unexported, used internally).
func (m *sessionManager) put(ctx context.Context, session *Session) error {
	m.mu.Lock()
	m.sessions[session.AuthCtxID] = session
	m.mu.Unlock()
	return nil
}

// delete removes a session (unexported, used internally).
func (m *sessionManager) delete(ctx context.Context, authCtxID string) error {
	m.mu.Lock()
	delete(m.sessions, authCtxID)
	m.mu.Unlock()
	return nil
}

// Size returns the number of active sessions.
func (m *sessionManager) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// Get implements SessionStore interface.
func (m *sessionManager) Get(ctx context.Context, authCtxID string) (*Session, error) {
	return m.get(ctx, authCtxID)
}

// Put implements SessionStore interface.
func (m *sessionManager) Put(ctx context.Context, session *Session) error {
	return m.put(ctx, session)
}

// Delete implements SessionStore interface.
func (m *sessionManager) Delete(ctx context.Context, authCtxID string) error {
	return m.delete(ctx, authCtxID)
}

// cleanup removes expired sessions.
// Returns the number of sessions removed.
func (m *sessionManager) cleanup() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-m.ttl)
	count := 0
	for id, session := range m.sessions {
		if session.CreatedAt.Before(cutoff) {
			delete(m.sessions, id)
			count++
		}
	}
	return count
}

// Stats returns session manager statistics.
func (m *sessionManager) stats() SessionManagerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return SessionManagerStats{
		ActiveSessions: len(m.sessions),
		TTL:            m.ttl,
	}
}

// SessionManagerStats holds statistics for the session manager.
type SessionManagerStats struct {
	ActiveSessions int
	TTL            time.Duration
}

// --- Test helpers (exported for package-level tests) ---

// NewTestSessionManager creates a session manager for testing.
func NewTestSessionManager(ttl time.Duration) *sessionManager {
	return newSessionManager(ttl)
}

// TestPut stores a session in the manager (for testing).
func (m *sessionManager) TestPut(ctx context.Context, session *Session) error {
	return m.put(ctx, session)
}

// TestGet retrieves a session by authCtxID (for testing).
func (m *sessionManager) TestGet(ctx context.Context, authCtxID string) (*Session, error) {
	return m.get(ctx, authCtxID)
}

// TestSize returns the number of sessions (for testing).
func (m *sessionManager) TestSize() int {
	return m.Size()
}

// NewTestSession creates a session for testing.
func NewTestSession(authCtxID, gpsi string) *Session {
	return NewSession(authCtxID, gpsi)
}
