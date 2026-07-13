// Package redis provides Redis caching layer for NSSAAF.
// Spec: TS 29.571 §7
package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/operator/nssAAF/internal/debug"
)

// SessionCache provides hot-cache for EAP sessions.
type SessionCache struct {
	client redis.Cmdable
	ttl    time.Duration
	debug  *debug.Debug
}

// NewSessionCache creates a new session cache.
// The *debug.Debug parameter is nil-safe: WrapRedis short-circuits when
// debug is nil or disabled (see internal/debug/hooks.go).
func NewSessionCache(client redis.Cmdable, ttl time.Duration, d *debug.Debug) *SessionCache {
	return &SessionCache{client: client, ttl: ttl, debug: d}
}

// SessionCacheEntry is the serialized session cache value.
type SessionCacheEntry struct {
	SnssaiSST   uint8  `json:"snssai_sst"`
	SnssaiSD    string `json:"snssai_sd"`
	NssaaStatus string `json:"nssaa_status"`
	EAPRounds   int    `json:"eap_rounds"`
	Method      string `json:"method"`
}

// sessionKey returns the Redis key for a session.
func sessionKey(authCtxID string) string {
	return fmt.Sprintf("nssaa:session:%s", authCtxID)
}

// Get retrieves a cached session entry.
// Returns (nil, nil) on cache miss (redis.Nil is converted inside the
// WrapRedis closure so it does not surface as a debug error event).
func (c *SessionCache) Get(ctx context.Context, authCtxID string) (*SessionCacheEntry, error) {
	key := sessionKey(authCtxID)
	var entry *SessionCacheEntry
	var jsonErr error
	var getErr error
	if wrapErr := c.debug.WrapRedis(ctx, "redis.session_cache.get", key, func() error {
		val, err := c.client.Get(ctx, key).Bytes()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				return nil
			}
			getErr = fmt.Errorf("session cache get: %w", err)
			return getErr
		}
		entry = &SessionCacheEntry{}
		if uerr := json.Unmarshal(val, entry); uerr != nil {
			jsonErr = fmt.Errorf("session cache unmarshal: %w", uerr)
			return jsonErr
		}
		return nil
	}); wrapErr != nil {
		return nil, wrapErr
	}
	if getErr != nil {
		return nil, getErr
	}
	if jsonErr != nil {
		return nil, jsonErr
	}
	return entry, nil
}

// Set stores a session entry with TTL.
func (c *SessionCache) Set(ctx context.Context, authCtxID string, entry *SessionCacheEntry) error {
	key := sessionKey(authCtxID)
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("session cache marshal: %w", err)
	}

	return c.debug.WrapRedis(ctx, "redis.session.set", key, func() error {
		if err := c.client.Set(ctx, key, data, c.ttl).Err(); err != nil {
			return fmt.Errorf("session cache set: %w", err)
		}
		return nil
	})
}

// Delete removes a session entry.
func (c *SessionCache) Delete(ctx context.Context, authCtxID string) error {
	key := sessionKey(authCtxID)
	return c.debug.WrapRedis(ctx, "redis.session_cache.delete", key, func() error {
		if err := c.client.Del(ctx, key).Err(); err != nil {
			return fmt.Errorf("session cache delete: %w", err)
		}
		return nil
	})
}

// Refresh extends the TTL of an existing entry.
func (c *SessionCache) Refresh(ctx context.Context, authCtxID string) error {
	key := sessionKey(authCtxID)
	return c.debug.WrapRedis(ctx, "redis.session_cache.refresh", key, func() error {
		if err := c.client.Expire(ctx, key, c.ttl).Err(); err != nil {
			return fmt.Errorf("session cache refresh: %w", err)
		}
		return nil
	})
}

// Exists reports whether a session entry exists.
func (c *SessionCache) Exists(ctx context.Context, authCtxID string) (bool, error) {
	key := sessionKey(authCtxID)
	var exists bool
	var existsErr error
	if wrapErr := c.debug.WrapRedis(ctx, "redis.session_cache.exists", key, func() error {
		n, err := c.client.Exists(ctx, key).Result()
		if err != nil {
			existsErr = fmt.Errorf("session cache exists: %w", err)
			return existsErr
		}
		exists = n > 0
		return nil
	}); wrapErr != nil {
		return false, wrapErr
	}
	if existsErr != nil {
		return false, existsErr
	}
	return exists, nil
}

// HashGPSI hashes a GPSI for storage in the audit log.
// Uses SHA-256, takes first 16 bytes, hex-encodes.
func HashGPSI(gpsi string) string {
	h := sha256.Sum256([]byte(gpsi))
	return hex.EncodeToString(h[:16])
}
