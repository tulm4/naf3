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
)

// SessionCache provides hot-cache for EAP sessions.
type SessionCache struct {
	client redis.Cmdable
	ttl    time.Duration
}

// NewSessionCache creates a new session cache.
func NewSessionCache(client redis.Cmdable, ttl time.Duration) *SessionCache {
	return &SessionCache{client: client, ttl: ttl}
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
func (c *SessionCache) Get(ctx context.Context, authCtxID string) (*SessionCacheEntry, error) {
	key := sessionKey(authCtxID)
	val, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("session cache get: %w", err)
	}

	var entry SessionCacheEntry
	if err := json.Unmarshal(val, &entry); err != nil {
		return nil, fmt.Errorf("session cache unmarshal: %w", err)
	}
	return &entry, nil
}

// Set stores a session entry with TTL.
func (c *SessionCache) Set(ctx context.Context, authCtxID string, entry *SessionCacheEntry) error {
	key := sessionKey(authCtxID)
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("session cache marshal: %w", err)
	}

	if err := c.client.Set(ctx, key, data, c.ttl).Err(); err != nil {
		return fmt.Errorf("session cache set: %w", err)
	}
	return nil
}

// Delete removes a session entry.
func (c *SessionCache) Delete(ctx context.Context, authCtxID string) error {
	key := sessionKey(authCtxID)
	if err := c.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("session cache delete: %w", err)
	}
	return nil
}

// Refresh extends the TTL of an existing entry.
func (c *SessionCache) Refresh(ctx context.Context, authCtxID string) error {
	key := sessionKey(authCtxID)
	if err := c.client.Expire(ctx, key, c.ttl).Err(); err != nil {
		return fmt.Errorf("session cache refresh: %w", err)
	}
	return nil
}

// Exists reports whether a session entry exists.
func (c *SessionCache) Exists(ctx context.Context, authCtxID string) (bool, error) {
	key := sessionKey(authCtxID)
	n, err := c.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("session cache exists: %w", err)
	}
	return n > 0, nil
}

// HashGPSI hashes a GPSI for storage in the audit log.
// Uses SHA-256, takes first 16 bytes, hex-encodes.
func HashGPSI(gpsi string) string {
	h := sha256.Sum256([]byte(gpsi))
	return hex.EncodeToString(h[:16])
}
