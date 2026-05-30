// Package storage provides data persistence layer for NSSAAF.
// Spec: TS 28.541 §5.3, TS 29.571 §7
package storage

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/operator/nssAAF/internal/eap"
	"github.com/operator/nssAAF/internal/types"
)

// ErrSessionNotFound is returned when a session is not found.
var ErrSessionNotFound = errors.New("session not found")

// AuthSession is the Postgres-persisted version of an EAP session.
// It carries AMF metadata, notification URIs, and failure details that are not
// needed for EAP engine runtime state but are required for audit and compliance.
type AuthSession struct {
	AuthCtxID        string
	GPSI             string
	Supi             string
	SnssaiSST        uint8
	SnssaiSD         string
	AMFInstanceID    string
	AMFIP            *string
	AMFRegion        string
	AAAConfigID      *string
	EAPSessionState  []byte
	NssaaStatus      types.NssaaStatus
	EAPRounds        int
	MaxEAPRounds     int
	FailureReason    string
	FailureCause     string
	ReauthNotifURI   string
	RevocNotifURI    string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ExpiresAt        time.Time
	CompletedAt      *time.Time
	TerminatedAt     *time.Time
}

// SessionPersistence is the interface for long-term session audit.
// Postgres satisfies this. Tests use a nop adapter.
type SessionPersistence interface {
	SaveAuthSession(ctx context.Context, s *AuthSession) error
	GetAuthSession(ctx context.Context, authCtxID string) (*AuthSession, error)
}

// nopPersistence is a no-op SessionPersistence for tests.
type nopPersistence struct{}

func (n *nopPersistence) SaveAuthSession(_ context.Context, _ *AuthSession) error { return nil }
func (n *nopPersistence) GetAuthSession(_ context.Context, _ string) (*AuthSession, error) {
	return nil, ErrSessionNotFound
}

// WriteThroughStore composes a SessionStore (TTL cache) with a SessionPersistence
// (long-term audit). Reads come from the cache; writes go to both.
// The persistence write is fire-and-forget — failures are logged but do not fail
// the cache write. This matches the production requirement: a Redis failure must
// not block the EAP exchange, and a Postgres failure must not block the cache.
type WriteThroughStore struct {
	cache   eap.SessionStore
	persist SessionPersistence
	logger  *slog.Logger
}

// NewWriteThroughStore creates a write-through store.
func NewWriteThroughStore(cache eap.SessionStore, persist SessionPersistence, logger *slog.Logger) *WriteThroughStore {
	return &WriteThroughStore{
		cache:   cache,
		persist: persist,
		logger:  logger,
	}
}

// Get implements eap.SessionStore. Reads always from the cache.
func (w *WriteThroughStore) Get(ctx context.Context, authCtxID string) (*eap.Session, error) {
	return w.cache.Get(ctx, authCtxID)
}

// Put implements eap.SessionStore. Writes to both cache (blocking) and audit (fire-and-forget).
func (w *WriteThroughStore) Put(ctx context.Context, session *eap.Session) error {
	if err := w.cache.Put(ctx, session); err != nil {
		return err
	}
	authSess := toAuthSession(session)
	if err := w.persist.SaveAuthSession(ctx, authSess); err != nil {
		w.logger.Warn("audit_write_failed",
			"auth_ctx_id", session.AuthCtxID,
			"error", err,
		)
	}
	return nil
}

// Delete implements eap.SessionStore. Cache-only delete.
func (w *WriteThroughStore) Delete(ctx context.Context, authCtxID string) error {
	return w.cache.Delete(ctx, authCtxID)
}

// Size implements eap.SessionStore.
func (w *WriteThroughStore) Size() int {
	return w.cache.Size()
}

// toAuthSession converts an EAP session to a Postgres-persisted AuthSession.
// S-NSSAI is decoded from the Session.SnssaiKey composite key.
func toAuthSession(s *eap.Session) *AuthSession {
	sst, sd := decodeSnssaiKey(s.SnssaiKey)
	return &AuthSession{
		AuthCtxID:       s.AuthCtxID,
		GPSI:            s.Gpsi,
		Supi:            s.Supi,
		SnssaiSST:       sst,
		SnssaiSD:        sd,
		EAPSessionState: nil, // engine state not persisted to Postgres
		EAPRounds:       s.Rounds,
		MaxEAPRounds:    s.MaxRounds,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.LastActivity,
	}
}

// decodeSnssaiKey parses "sst" or "sst-sd" format.
func decodeSnssaiKey(key string) (sst uint8, sd string) {
	if key == "" {
		return 0, ""
	}
	parts := strings.SplitN(key, "-", 2)
	sstVal, err := strconv.ParseUint(parts[0], 10, 8)
	if err != nil {
		return 0, ""
	}
	if len(parts) == 1 {
		return uint8(sstVal), ""
	}
	return uint8(sstVal), parts[1]
}
