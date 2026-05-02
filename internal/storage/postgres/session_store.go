// Package postgres provides PostgreSQL data persistence layer for NSSAAF.
// REQ-09: PostgreSQL session store replaces in-memory store via NewSessionStore/NewAIWSessionStore.
// D-06: NewSessionStore(*Pool) and NewAIWSessionStore(*Pool) implement the AuthCtxStore interfaces.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/operator/nssAAF/internal/api/aiw"
	"github.com/operator/nssAAF/internal/api/nssaa"
	"github.com/operator/nssAAF/internal/types"
)

// Store implements nssaa.AuthCtxStore for PostgreSQL.
// Wraps Repository from session.go.
type Store struct {
	repo *Repository
}

// NewSessionStore creates a new PostgreSQL-backed session store for NSSAA.
func NewSessionStore(pool *Pool, enc *encryptor) *Store {
	return &Store{repo: NewRepository(pool, enc)}
}

// Load retrieves a slice authentication context by authCtxID.
func (s *Store) Load(id string) (*nssaa.AuthCtx, error) {
	session, err := s.repo.GetByAuthCtxID(context.Background(), id)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, nssaa.ErrNotFound
		}
		return nil, fmt.Errorf("session store load: %w", err)
	}
	return sessionToAuthCtx(session), nil
}

// Save stores or updates a slice authentication context.
// If the session does not exist (Update returns ErrSessionNotFound),
// Create is called to insert it first.
func (s *Store) Save(ctx *nssaa.AuthCtx) error {
	session := authCtxToSession(ctx)
	err := s.repo.Update(context.Background(), session)
	if errors.Is(err, ErrSessionNotFound) {
		return s.repo.Create(context.Background(), session)
	}
	return err
}

// Delete removes a slice authentication context by authCtxID.
func (s *Store) Delete(id string) error {
	return s.repo.Delete(context.Background(), id)
}

// Close is a no-op. Pool lifecycle managed by main.go.
func (s *Store) Close() error {
	return nil
}

// sessionToAuthCtx converts Session (DB) → nssaa.AuthCtx.
func sessionToAuthCtx(s *Session) *nssaa.AuthCtx {
	return &nssaa.AuthCtx{
		AuthCtxID:   s.AuthCtxID,
		GPSI:        s.GPSI,
		SnssaiSST:   s.SnssaiSST,
		SnssaiSD:    s.SnssaiSD,
		AmfInstance: s.AMFInstanceID,
		ReauthURI:   s.ReauthNotifURI,
		RevocURI:    s.RevocNotifURI,
		EapPayload:  s.EAPSessionState,
	}
}

// authCtxToSession converts nssaa.AuthCtx → Session (DB).
func authCtxToSession(a *nssaa.AuthCtx) *Session {
	now := time.Now()
	return &Session{
		AuthCtxID:       a.AuthCtxID,
		GPSI:            a.GPSI,
		SnssaiSST:       a.SnssaiSST,
		SnssaiSD:        a.SnssaiSD,
		AMFInstanceID:   a.AmfInstance,
		ReauthNotifURI:  a.ReauthURI,
		RevocNotifURI:   a.RevocURI,
		EAPSessionState: a.EapPayload,
		NssaaStatus:     types.NssaaStatusNotExecuted,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

// AIWStore implements aiw.AuthCtxStore for PostgreSQL.
// Uses aiw_auth_sessions table per design doc §3.6.
type AIWStore struct {
	repo *AIWRepository
}

// NewAIWSessionStore creates a new PostgreSQL-backed session store for AIW.
func NewAIWSessionStore(pool *Pool, enc *encryptor) *AIWStore {
	return &AIWStore{repo: NewAIWRepository(pool, enc)}
}

// Load retrieves an AIW authentication context by authCtxID.
func (s *AIWStore) Load(id string) (*aiw.AuthContext, error) {
	session, err := s.repo.GetByAuthCtxID(context.Background(), id)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, aiw.ErrNotFound
		}
		return nil, fmt.Errorf("aiw session store load: %w", err)
	}
	return aiwsessionToAuthCtx(session), nil
}

// Save stores or updates an AIW authentication context.
func (s *AIWStore) Save(ctx *aiw.AuthContext) error {
	session := authCtxToAIWSession(ctx)
	err := s.repo.Update(context.Background(), session)
	if errors.Is(err, ErrSessionNotFound) {
		return s.repo.Create(context.Background(), session)
	}
	return err
}

// Delete removes an AIW authentication context by authCtxID.
func (s *AIWStore) Delete(id string) error {
	return s.repo.Delete(context.Background(), id)
}

// Close is a no-op.
func (s *AIWStore) Close() error {
	return nil
}

// aiwsessionToAuthCtx converts AIWSession (DB) → aiw.AuthContext.
func aiwsessionToAuthCtx(s *AIWSession) *aiw.AuthContext {
	ctx := &aiw.AuthContext{
		AuthCtxID:         s.AuthCtxID,
		Supi:              s.Supi,
		EapPayload:        s.EAPSessionState,
		TtlsInner:         s.TtlsInner,
		MSK:               s.MSK,
		PvsInfo:           s.PvsInfo,
		AusfID:            s.AusfID,
		SupportedFeatures: s.SupportedFeatures,
		Status:            s.NssaaStatus,
		AuthResult:        s.AuthResult,
		CreatedAt:         s.CreatedAt,
		UpdatedAt:         s.UpdatedAt,
		ExpiresAt:         s.ExpiresAt,
	}
	if s.CompletedAt != nil {
		ctx.CompletedAt = s.CompletedAt
	}
	return ctx
}

// authCtxToAIWSession converts aiw.AuthContext → AIWSession (DB).
func authCtxToAIWSession(a *aiw.AuthContext) *AIWSession {
	now := time.Now()
	expiresAt := a.ExpiresAt
	if expiresAt.IsZero() {
		// Default: 24-hour session lifetime per TS 29.526 §7.3
		expiresAt = now.Add(24 * time.Hour)
	}
	return &AIWSession{
		AuthCtxID:         a.AuthCtxID,
		Supi:              a.Supi,
		AusfID:            a.AusfID,
		EAPSessionState:   a.EapPayload,
		TtlsInner:         a.TtlsInner,
		MSK:               a.MSK,
		PvsInfo:           a.PvsInfo,
		SupportedFeatures: a.SupportedFeatures,
		NssaaStatus:       a.Status,
		AuthResult:        a.AuthResult,
		CreatedAt:         now,
		UpdatedAt:         now,
		ExpiresAt:         expiresAt,
	}
}
