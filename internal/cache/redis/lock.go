// Package redis provides Redis caching layer for NSSAAF.
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// DistributedLock provides a distributed mutex using Redis SET NX EX.
type DistributedLock struct {
	client redis.Cmdable
	ttl    time.Duration
}

// NewDistributedLock creates a new distributed lock manager.
func NewDistributedLock(client redis.Cmdable, ttl time.Duration) *DistributedLock {
	if ttl == 0 {
		ttl = 30 * time.Second
	}
	return &DistributedLock{client: client, ttl: ttl}
}

// lockKey returns the Redis key for a lock.
func lockKey(resource string) string {
	return fmt.Sprintf("nssaa:lock:session:%s", resource)
}

// Lock attempts to acquire a lock on the given resource.
// Returns a token if successful, empty string if the lock is held.
func (l *DistributedLock) Lock(ctx context.Context, resource string) (string, error) {
	key := lockKey(resource)
	token := uuid.NewString()

	// SET NX EX: set only if not exists, with expiry.
	ok, err := l.client.SetNX(ctx, key, token, l.ttl).Result()
	if err != nil {
		return "", fmt.Errorf("lock acquire: %w", err)
	}
	if !ok {
		return "", nil
	}
	return token, nil
}

// TryLock attempts to acquire a lock with a timeout.
// It retries until the timeout is reached or the lock is acquired.
func (l *DistributedLock) TryLock(ctx context.Context, resource string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		token, err := l.Lock(ctx, resource)
		if err != nil {
			return "", err
		}
		if token != "" {
			return token, nil
		}

		if time.Now().After(deadline) {
			return "", nil
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}

// Unlock releases a lock if the token matches.
func (l *DistributedLock) Unlock(ctx context.Context, resource, token string) error {
	key := lockKey(resource)

	// Use Lua script for atomic check-and-delete.
	script := redis.NewScript(`
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`)

	result, err := script.Run(ctx, l.client, []string{key}, token).Int64()
	if err != nil {
		return fmt.Errorf("lock release: %w", err)
	}
	if result == 0 {
		// Token did not match — lock was not held or was expired.
		return nil
	}
	return nil
}

// Extend extends the TTL of a lock if the token matches.
func (l *DistributedLock) Extend(ctx context.Context, resource, token string) error {
	key := lockKey(resource)

	script := redis.NewScript(`
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("PEXPIRE", KEYS[1], ARGV[2])
		else
			return 0
		end
	`)

	result, err := script.Run(ctx, l.client, []string{key}, token, l.ttl.Milliseconds()).Int64()
	if err != nil {
		return fmt.Errorf("lock extend: %w", err)
	}
	if result == 0 {
		return fmt.Errorf("lock not held or token mismatch")
	}
	return nil
}

// IsLocked reports whether a resource is currently locked.
func (l *DistributedLock) IsLocked(ctx context.Context, resource string) (bool, error) {
	key := lockKey(resource)
	exists, err := l.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("lock exists check: %w", err)
	}
	return exists > 0, nil
}
