package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/operator/nssAAF/internal/api/aiw"
	"github.com/operator/nssAAF/internal/api/nssaa"
)

// NSSAAStoreAdapter wraps a session.Store to implement nssaa.AuthCtxStore.
// It converts between nssaa.AuthCtx and session.NSSAASession.
type NSSAAStoreAdapter struct {
	store SessionStore
}

// NewNSSAAStoreAdapter creates an adapter that implements nssaa.AuthCtxStore.
func NewNSSAAStoreAdapter(store SessionStore) *NSSAAStoreAdapter {
	return &NSSAAStoreAdapter{store: store}
}

// Load implements nssaa.AuthCtxStore.
func (a *NSSAAStoreAdapter) Load(id string) (*nssaa.AuthCtx, error) {
	s, err := a.store.Load(context.Background(), id)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, nssaa.ErrNotFound
		}
		return nil, fmt.Errorf("nssaa store load: %w", err)
	}
	if s == nil {
		return nil, nssaa.ErrNotFound
	}
	return NSSAASessionToAuthCtx(s), nil
}

// Save implements nssaa.AuthCtxStore.
func (a *NSSAAStoreAdapter) Save(ctx *nssaa.AuthCtx) error {
	s := AuthCtxToNSSASession(ctx)
	err := a.store.Save(context.Background(), s)
	if err != nil {
		return fmt.Errorf("nssaa store save: %w", err)
	}
	return nil
}

// Delete implements nssaa.AuthCtxStore.
func (a *NSSAAStoreAdapter) Delete(id string) error {
	return a.store.Delete(context.Background(), id)
}

// Close implements nssaa.AuthCtxStore.
func (a *NSSAAStoreAdapter) Close() error {
	return a.store.Close()
}

// NSSAAStoreAdapterAIW wraps a session.Store to implement aiw.AuthCtxStore.
type NSSAAStoreAdapterAIW struct {
	store SessionStore
}

// NewNSSAAStoreAdapterAIW creates an adapter that implements aiw.AuthCtxStore.
func NewNSSAAStoreAdapterAIW(store SessionStore) *NSSAAStoreAdapterAIW {
	return &NSSAAStoreAdapterAIW{store: store}
}

// Load implements aiw.AuthCtxStore.
func (a *NSSAAStoreAdapterAIW) Load(id string) (*aiw.AuthContext, error) {
	s, err := a.store.Load(context.Background(), id)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, aiw.ErrNotFound
		}
		return nil, fmt.Errorf("aiw store load: %w", err)
	}
	if s == nil {
		return nil, aiw.ErrNotFound
	}
	return AIWSessionToAuthCtx(s), nil
}

// Save implements aiw.AuthCtxStore.
func (a *NSSAAStoreAdapterAIW) Save(ctx *aiw.AuthContext) error {
	s := AuthCtxToAIWSession(ctx)
	err := a.store.Save(context.Background(), s)
	if err != nil {
		return fmt.Errorf("aiw store save: %w", err)
	}
	return nil
}

// Delete implements aiw.AuthCtxStore.
func (a *NSSAAStoreAdapterAIW) Delete(id string) error {
	return a.store.Delete(context.Background(), id)
}

// Close implements aiw.AuthCtxStore.
func (a *NSSAAStoreAdapterAIW) Close() error {
	return a.store.Close()
}

// ---------------------------------------------------------------------------
// Conversion functions
// ---------------------------------------------------------------------------

// NSSAASessionToAuthCtx converts session.NSSAASession → nssaa.AuthCtx.
func NSSAASessionToAuthCtx(s *Session) *nssaa.AuthCtx {
	if s == nil {
		return nil
	}
	ns, ok := s.Ext.(*NSSAASession)
	if !ok {
		return nil
	}
	return &nssaa.AuthCtx{
		AuthCtxID:   s.AuthCtxID,
		GPSI:        ns.GPSI,
		SnssaiSST:   ns.SnssaiSST,
		SnssaiSD:    ns.SnssaiSD,
		AmfInstance: ns.AmfInstance,
		ReauthURI:   ns.ReauthURI,
		RevocURI:    ns.RevocURI,
		EapPayload:  s.EapPayload,
	}
}

// AuthCtxToNSSASession converts nssaa.AuthCtx → session.NSSAASession.
func AuthCtxToNSSASession(a *nssaa.AuthCtx) *Session {
	now := time.Now()
	return &Session{
		AuthCtxID:  a.AuthCtxID,
		EapPayload: a.EapPayload,
		CreatedAt:  now,
		UpdatedAt:  now,
		Ext:        &NSSAASession{GPSI: a.GPSI, SnssaiSST: a.SnssaiSST, SnssaiSD: a.SnssaiSD, AmfInstance: a.AmfInstance, ReauthURI: a.ReauthURI, RevocURI: a.RevocURI},
	}
}

// AIWSessionToAuthCtx converts session.AIWSession → aiw.AuthContext.
func AIWSessionToAuthCtx(s *Session) *aiw.AuthContext {
	if s == nil {
		return nil
	}
	ai, ok := s.Ext.(*AIWSession)
	if !ok {
		return nil
	}
	ctx := &aiw.AuthContext{
		AuthCtxID:         s.AuthCtxID,
		Supi:              ai.Supi,
		EapPayload:        s.EapPayload,
		MSK:               ai.MSK,
		PvsInfo:           ai.PvsInfo,
		AusfID:            ai.AusfID,
		SupportedFeatures: ai.SupportedFeatures,
		Status:            ai.Status,
		AuthResult:        ai.AuthResult,
		CreatedAt:         s.CreatedAt,
		UpdatedAt:         s.UpdatedAt,
		ExpiresAt:         s.ExpiresAt,
	}
	if s.CompletedAt != nil {
		ctx.CompletedAt = s.CompletedAt
	}
	return ctx
}

// AuthCtxToAIWSession converts aiw.AuthContext → session.AIWSession.
func AuthCtxToAIWSession(a *aiw.AuthContext) *Session {
	now := time.Now()
	expiresAt := a.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = now.Add(24 * time.Hour)
	}
	var completedAt *time.Time
	if a.CompletedAt != nil {
		completedAt = a.CompletedAt
	}
	return &Session{
		AuthCtxID:   a.AuthCtxID,
		EapPayload:  a.EapPayload,
		CreatedAt:   now,
		UpdatedAt:   now,
		ExpiresAt:   expiresAt,
		CompletedAt: completedAt,
		Ext: &AIWSession{
			Supi:              a.Supi,
			MSK:               a.MSK,
			PvsInfo:           a.PvsInfo,
			AusfID:            a.AusfID,
			SupportedFeatures: a.SupportedFeatures,
			Status:            a.Status,
			AuthResult:        a.AuthResult,
		},
	}
}
