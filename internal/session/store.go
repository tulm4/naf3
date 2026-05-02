package session

import "context"

// SessionStore is the interface for session persistence.
// Both NSSAA and AIW handlers use this interface; the implementation
// (in-memory, Redis, or PostgreSQL) is chosen at wiring time.
type SessionStore interface {
	// Load retrieves a session by its authCtxID.
	// Returns ErrSessionNotFound if the session does not exist.
	Load(ctx context.Context, id string) (*Session, error)

	// Save stores or updates a session.
	// If the session does not exist, it is created; otherwise it is updated.
	Save(ctx context.Context, s *Session) error

	// Delete removes a session by its authCtxID.
	Delete(ctx context.Context, id string) error

	// Close releases resources held by the store.
	// For in-memory stores this is a no-op; for network stores it closes connections.
	Close() error
}
