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
)

// Store implements nssaa.AuthCtxStore for PostgreSQL.
// Wraps Repository from session.go.
type Store struct {
	repo *Repository
}

// NewSessionStore creates a new PostgreSQL-backed session store for NSSAA.
// D-06: This is the factory function required by the implementation plan.
func NewSessionStore(pool *Pool, encryptor *Encryptor) *Store {
	return &Store{repo: NewRepository(pool, encryptor)}
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

// AIWStore implements aiw.AuthCtxStore for PostgreSQL.
type AIWStore struct {
	repo *Repository
}

// NewAIWSessionStore creates a new PostgreSQL-backed session store for AIW.
// D-06: This is the factory function required by the implementation plan.
func NewAIWSessionStore(pool *Pool, encryptor *Encryptor) *AIWStore {
	return &AIWStore{repo: NewRepository(pool, encryptor)}
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
	return sessionToAIWAuthCtx(session), nil
}

// Save stores or updates an AIW authentication context.
// If the session does not exist (Update returns ErrSessionNotFound),
// Create is called to insert it first.
func (s *AIWStore) Save(ctx *aiw.AuthContext) error {
	session := aiwAuthCtxToSession(ctx)
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
	return &Session{
		AuthCtxID:       a.AuthCtxID,
		GPSI:            a.GPSI,
		SnssaiSST:       a.SnssaiSST,
		SnssaiSD:        a.SnssaiSD,
		AMFInstanceID:   a.AmfInstance,
		ReauthNotifURI:  a.ReauthURI,
		RevocNotifURI:   a.RevocURI,
		EAPSessionState: a.EapPayload,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
}

// sessionToAIWAuthCtx converts Session (DB) → aiw.AuthContext.
func sessionToAIWAuthCtx(s *Session) *aiw.AuthContext {
	return &aiw.AuthContext{
		AuthCtxID:  s.AuthCtxID,
		Supi:       s.Supi,
		EapPayload: s.EAPSessionState,
	}
}

// aiwAuthCtxToSession converts aiw.AuthContext → Session (DB).
func aiwAuthCtxToSession(a *aiw.AuthContext) *Session {
	return &Session{
		AuthCtxID:       a.AuthCtxID,
		Supi:            a.Supi,
		EAPSessionState: a.EapPayload,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
}
