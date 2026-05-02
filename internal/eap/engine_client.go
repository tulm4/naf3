// Package eap provides EAP (Extensible Authentication Protocol) engine implementation.
// Spec: TS 33.501 §5.13, RFC 3748
package eap

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"time"
)

// ErrNoAAAClient is returned when no AAA client is configured.
var ErrNoAAAClient = errors.New("eap: aaa client not configured")

// AAARouter is the interface for forwarding EAP messages to AAA-S.
// Spec: TS 29.561 §16-17
//
// Both the NSSAA handler and the AIW handler use this interface.
// Protocol clients (RADIUS, Diameter) implement it by extracting routing
// context (GPSI, S-NSSAI) from the eap.Session.
type AAARouter interface {
	// SendEAP forwards an EAP message to AAA-S for the given session
	// and returns the AAA response (EAP-Request, Success, or Failure).
	//
	// The session carries all routing context (GPSI, S-NSSAI, etc.)
	// so the protocol client can determine the correct AAA server.
	SendEAP(ctx context.Context, session *Session, eapPayload []byte) ([]byte, error)
}

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
// Thread-safe.
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

// get returns a session by authCtxID.
func (m *sessionManager) get(authCtxID string) (*Session, error) {
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

// put stores or updates a session.
func (m *sessionManager) put(session *Session) {
	m.mu.Lock()
	m.sessions[session.AuthCtxID] = session
	m.mu.Unlock()
}

// delete removes a session.
func (m *sessionManager) delete(authCtxID string) {
	m.mu.Lock()
	delete(m.sessions, authCtxID)
	m.mu.Unlock()
}

// size returns the number of active sessions.
func (m *sessionManager) size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
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
func (m *sessionManager) TestPut(session *Session) {
	m.put(session)
}

// TestGet retrieves a session by authCtxID (for testing).
func (m *sessionManager) TestGet(authCtxID string) (*Session, error) {
	return m.get(authCtxID)
}

// TestSize returns the number of sessions (for testing).
func (m *sessionManager) TestSize() int {
	return m.size()
}

// NewTestSession creates a session for testing.
func NewTestSession(authCtxID, gpsi string) *Session {
	return NewSession(authCtxID, gpsi)
}
