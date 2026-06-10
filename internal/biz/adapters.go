package biz

import (
	"context"
	"fmt"
	"time"

	"github.com/operator/nssAAF/internal/storage"
)

// AiwRepositoryAdapter is the subset of AIW store behavior needed for reverse-path linking.
type AiwRepositoryAdapter interface {
	Load(ctx context.Context, id string) (*storage.AiwSession, error)
	Save(ctx context.Context, session *storage.AiwSession) error
}

// NssaaSessionResolver adapts the durable NSSAA store to the reverse-path lookup interfaces.
type NssaaSessionResolver struct {
	store storage.NssaaStore
}

// NewNssaaSessionResolver creates a persistent reverse-path context lookup over the NSSAA store.
func NewNssaaSessionResolver(store storage.NssaaStore) *NssaaSessionResolver {
	return &NssaaSessionResolver{store: store}
}

// Resolve satisfies SessionContextResolver using the durable NSSAA store.
func (r *NssaaSessionResolver) Resolve(ctx context.Context, sessionID string, authCtxID string) (*NssaaSessionContext, error) {
	return r.load(ctx, sessionID, authCtxID)
}

// LoadAuthContext satisfies PersistentContextLookup.
func (r *NssaaSessionResolver) LoadAuthContext(ctx context.Context, authCtxID string) (*NssaaSessionContext, error) {
	return r.load(ctx, "", authCtxID)
}

func (r *NssaaSessionResolver) load(ctx context.Context, sessionID string, authCtxID string) (*NssaaSessionContext, error) {
	session, err := r.store.Load(ctx, authCtxID)
	if err != nil {
		return nil, fmt.Errorf("load nssaa session %s: %w", authCtxID, err)
	}
	return &NssaaSessionContext{
		AuthCtxID:      session.AuthCtxID,
		SessionID:      sessionID,
		GPSI:           session.GPSI,
		ReauthNotifURI: session.ReauthURI,
		RevocNotifURI:  session.RevocURI,
		CallbackOwner:  session.CallbackOwner,
		HasAIWContext:  session.HasAIWContext,
	}, nil
}

// ReverseFlowStateWriter persists reverse-path updates via the NSSAA store.
type ReverseFlowStateWriter struct {
	store storage.NssaaStore
}

// NewReverseFlowStateWriter creates a reverse-path state writer over the NSSAA store.
func NewReverseFlowStateWriter(store storage.NssaaStore) *ReverseFlowStateWriter {
	return &ReverseFlowStateWriter{store: store}
}

// MarkReauthPending records that AAA requested reauthentication.
func (w *ReverseFlowStateWriter) MarkReauthPending(ctx context.Context, authCtxID string) error {
	session, err := w.store.Load(ctx, authCtxID)
	if err != nil {
		return fmt.Errorf("load session for reauth %s: %w", authCtxID, err)
	}
	session.Status = "PENDING"
	session.UpdatedAt = time.Now().UTC()
	if err := w.store.Save(ctx, session); err != nil {
		return fmt.Errorf("save reauth session %s: %w", authCtxID, err)
	}
	return nil
}

// MarkRevoked records that AAA revoked the NSSAA authorization state.
func (w *ReverseFlowStateWriter) MarkRevoked(ctx context.Context, authCtxID string) error {
	session, err := w.store.Load(ctx, authCtxID)
	if err != nil {
		return fmt.Errorf("load session for revocation %s: %w", authCtxID, err)
	}
	session.Status = "REVOKED"
	session.UpdatedAt = time.Now().UTC()
	if err := w.store.Save(ctx, session); err != nil {
		return fmt.Errorf("save revoked session %s: %w", authCtxID, err)
	}
	return nil
}

// ApplyCoA persists updated reverse-flow state for Change of Authorization.
func (w *ReverseFlowStateWriter) ApplyCoA(ctx context.Context, authCtxID string, payload []byte) error {
	session, err := w.store.Load(ctx, authCtxID)
	if err != nil {
		return fmt.Errorf("load session for coa %s: %w", authCtxID, err)
	}
	session.EapPayload = append([]byte(nil), payload...)
	session.Status = "AUTHORIZED"
	session.UpdatedAt = time.Now().UTC()
	if err := w.store.Save(ctx, session); err != nil {
		return fmt.Errorf("save coa session %s: %w", authCtxID, err)
	}
	return nil
}

// AIWCompletionLinker marks AIW-owned auth contexts as reverse-flow linked.
type AIWCompletionLinker struct {
	store AiwRepositoryAdapter
}

// NewAIWCompletionLinker creates an AIW reverse-flow linker.
func NewAIWCompletionLinker(store AiwRepositoryAdapter) *AIWCompletionLinker {
	return &AIWCompletionLinker{store: store}
}

// MarkAIWLinked updates the AIW auth context after reverse-path completion.
func (l *AIWCompletionLinker) MarkAIWLinked(ctx context.Context, authCtxID string) error {
	session, err := l.store.Load(ctx, authCtxID)
	if err != nil {
		return fmt.Errorf("load aiw session %s: %w", authCtxID, err)
	}
	now := time.Now().UTC()
	session.CompletedAt = &now
	session.UpdatedAt = now
	if session.Status == "" {
		session.Status = "PENDING"
	}
	if err := l.store.Save(ctx, session); err != nil {
		return fmt.Errorf("save aiw session %s: %w", authCtxID, err)
	}
	return nil
}
