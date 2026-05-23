// Package nssaa provides HTTP handlers for the Nnssaaf_NSSAA service (N58 interface).
// Spec: TS 29.526 §7.2, TS 23.502 §4.2.9
package nssaa

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	AuthCtxKeyPrefix = "nssaa:auth:ctx:"
	AuthCtxTTL       = 24 * time.Hour
)

// RedisAuthCtxStore implements AuthCtxStore backed by Redis.
type RedisAuthCtxStore struct {
	client redis.Cmdable
}

// NewRedisAuthCtxStore creates a new Redis-backed auth context store.
func NewRedisAuthCtxStore(client redis.Cmdable) *RedisAuthCtxStore {
	return &RedisAuthCtxStore{client: client}
}

// Load retrieves an auth context by ID.
// Returns ErrNotFound if the key does not exist.
func (s *RedisAuthCtxStore) Load(ctx context.Context, id string) (*AuthCtx, error) {
	key := AuthCtxKeyPrefix + id
	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("redis get: %w", err)
	}
	var authCtx AuthCtx
	if err := json.Unmarshal(data, &authCtx); err != nil {
		return nil, fmt.Errorf("unmarshal auth context: %w", err)
	}
	return &authCtx, nil
}

// Save stores an auth context with a 24-hour TTL.
func (s *RedisAuthCtxStore) Save(ctx context.Context, authCtx *AuthCtx) error {
	key := AuthCtxKeyPrefix + authCtx.AuthCtxID
	data, err := json.Marshal(authCtx)
	if err != nil {
		return fmt.Errorf("marshal auth context: %w", err)
	}
	if err := s.client.Set(ctx, key, data, AuthCtxTTL).Err(); err != nil {
		return fmt.Errorf("redis set: %w", err)
	}
	return nil
}

// Delete removes an auth context by ID.
func (s *RedisAuthCtxStore) Delete(ctx context.Context, id string) error {
	key := AuthCtxKeyPrefix + id
	return s.client.Del(ctx, key).Err()
}

// Close implements AuthCtxStore. No-op for Redis client.
func (s *RedisAuthCtxStore) Close() error {
	return nil
}

// Compile-time interface check.
var _ AuthCtxStore = (*RedisAuthCtxStore)(nil)
