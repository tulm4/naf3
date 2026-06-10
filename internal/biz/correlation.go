package biz

import (
	"context"
	"fmt"

	"github.com/operator/nssAAF/internal/cache/redis"
	goredis "github.com/redis/go-redis/v9"
)

// CorrelationResolver resolves reverse-path session context from Redis first,
// then enriches it with durable auth-context state.
type CorrelationResolver struct {
	store  *redis.SessionCorrelationStore
	lookup PersistentContextLookup
}

// NewCorrelationResolver creates a reverse-path resolver backed by Redis
// correlation entries and durable context lookup.
func NewCorrelationResolver(rdb goredis.Cmdable, lookup PersistentContextLookup) *CorrelationResolver {
	return &CorrelationResolver{
		store:  redis.NewSessionCorrelationStore(rdb, 0),
		lookup: lookup,
	}
}

// Resolve resolves session context using Redis correlation data when available,
// then loads the persisted auth context.
func (r *CorrelationResolver) Resolve(ctx context.Context, sessionID string, authCtxID string) (*NssaaSessionContext, error) {
	resolvedAuthCtxID := authCtxID
	if sessionID != "" {
		entry, err := r.store.Load(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		if entry != nil && resolvedAuthCtxID == "" {
			resolvedAuthCtxID = entry.AuthCtxID
		}
	}
	if resolvedAuthCtxID == "" {
		return nil, fmt.Errorf("auth context could not be resolved")
	}

	sessionCtx, err := r.lookup.LoadAuthContext(ctx, resolvedAuthCtxID)
	if err != nil {
		return nil, fmt.Errorf("load auth context %s: %w", resolvedAuthCtxID, err)
	}
	sessionCtx.AuthCtxID = resolvedAuthCtxID
	sessionCtx.SessionID = sessionID
	return sessionCtx, nil
}
