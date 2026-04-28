// Package redis provides Redis caching layer for NSSAAF.
package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// IdempotencyCache prevents duplicate processing of the same request.
// Key: SHA-256 hash of (authCtxID + EAP message), stored for 1 hour.
type IdempotencyCache struct {
	client redis.Cmdable
	ttl    time.Duration
}

// NewIdempotencyCache creates a new idempotency cache.
func NewIdempotencyCache(client redis.Cmdable, ttl time.Duration) *IdempotencyCache {
	if ttl == 0 {
		ttl = time.Hour
	}
	return &IdempotencyCache{client: client, ttl: ttl}
}

// idempotencyKey returns the Redis key for an idempotency entry.
func idempotencyKey(ctxID, msgHash string) string {
	return fmt.Sprintf("nssaa:idempotency:%s:%s", ctxID, msgHash)
}

// hashPayload computes the idempotency key from an EAP payload.
func hashPayload(authCtxID string, payload []byte) string {
	h := sha256.Sum256(append([]byte(authCtxID), payload...))
	return hex.EncodeToString(h[:])
}

// Check checks whether a request has already been processed.
// Returns true if it was already processed (caller should use cached response).
// Returns false if it is new (caller should process and then call Record).
func (c *IdempotencyCache) Check(ctx context.Context, authCtxID string, payload []byte) (bool, error) {
	key := idempotencyKey(authCtxID, hashPayload(authCtxID, payload))

	exists, err := c.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("idempotency check: %w", err)
	}
	return exists > 0, nil
}

// Record marks a request as processed with the given response.
func (c *IdempotencyCache) Record(ctx context.Context, authCtxID string, payload []byte, response []byte) error {
	key := idempotencyKey(authCtxID, hashPayload(authCtxID, payload))
	if err := c.client.Set(ctx, key, response, c.ttl).Err(); err != nil {
		return fmt.Errorf("idempotency record: %w", err)
	}
	return nil
}

// GetResponse retrieves the cached response for a request.
func (c *IdempotencyCache) GetResponse(ctx context.Context, authCtxID string, payload []byte) ([]byte, error) {
	key := idempotencyKey(authCtxID, hashPayload(authCtxID, payload))
	val, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("idempotency get: %w", err)
	}
	return val, nil
}

// Invalidate removes an idempotency entry.
func (c *IdempotencyCache) Invalidate(ctx context.Context, authCtxID string, payload []byte) error {
	key := idempotencyKey(authCtxID, hashPayload(authCtxID, payload))
	if err := c.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("idempotency invalidate: %w", err)
	}
	return nil
}
