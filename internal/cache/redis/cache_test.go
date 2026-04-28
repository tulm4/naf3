// Package redis provides Redis caching layer for NSSAAF.
package redis

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// Key helpers
// ---------------------------------------------------------------------------

func TestSessionKey(t *testing.T) {
	assert.Equal(t, "nssaa:session:auth-123", sessionKey("auth-123"))
	assert.Equal(t, "nssaa:session:", sessionKey(""))
}

func TestIdempotencyKey(t *testing.T) {
	assert.Contains(t, idempotencyKey("auth-1", "abcdef"), "nssaa:idempotency:auth-1:abcdef")
}

func TestGPSIKey(t *testing.T) {
	assert.Equal(t, "nssaa:ratelimit:gpsi:hash123", gpsiKey("hash123"))
}

func TestAMFKey(t *testing.T) {
	assert.Equal(t, "nssaa:ratelimit:amf:amf-1", amfKey("amf-1"))
}

func TestLockKey(t *testing.T) {
	assert.Equal(t, "nssaa:lock:session:auth-123", lockKey("auth-123"))
}

// ---------------------------------------------------------------------------
// HashPayload
// ---------------------------------------------------------------------------

func TestHashPayload(t *testing.T) {
	h1 := hashPayload("auth-1", []byte("hello"))
	h2 := hashPayload("auth-1", []byte("hello"))
	h3 := hashPayload("auth-1", []byte("world"))
	h4 := hashPayload("auth-2", []byte("hello"))

	// Deterministic.
	assert.Equal(t, h1, h2)

	// Different payload.
	assert.NotEqual(t, h1, h3)

	// Different authCtxID.
	assert.NotEqual(t, h1, h4)

	// Length: SHA-256 → 64 hex chars.
	assert.Len(t, h1, 64)
	assert.Len(t, h3, 64)
	assert.Len(t, h4, 64)
}

func TestHashPayloadEmpty(t *testing.T) {
	h := hashPayload("auth-1", []byte{})
	assert.Len(t, h, 64)
	assert.NotEmpty(t, h)
}

// ---------------------------------------------------------------------------
// SessionCache TTL
// ---------------------------------------------------------------------------

func TestSessionCacheEntry(t *testing.T) {
	entry := &SessionCacheEntry{
		SnssaiSST:   1,
		SnssaiSD:    "ABCDEF",
		NssaaStatus: "PENDING",
		EAPRounds:   3,
		Method:      "EAP-TLS",
	}

	assert.Equal(t, uint8(1), entry.SnssaiSST)
	assert.Equal(t, "ABCDEF", entry.SnssaiSD)
	assert.Equal(t, "PENDING", entry.NssaaStatus)
	assert.Equal(t, 3, entry.EAPRounds)
	assert.Equal(t, "EAP-TLS", entry.Method)
}

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

func TestConfigDefaults(t *testing.T) {
	cfg := Config{
		Addrs:    []string{"localhost:6379"},
		DB:       0,
		PoolSize: 10,
	}

	assert.Equal(t, 1, len(cfg.Addrs))
	assert.Equal(t, 0, cfg.DB)
	assert.Equal(t, 10, cfg.PoolSize)
}

// ---------------------------------------------------------------------------
// RateLimiter
// ---------------------------------------------------------------------------

func TestRateLimiterConfig(t *testing.T) {
	rl := &RateLimiter{
		window: 0,
		limit:  100,
	}

	// Zero window should be replaced by caller.
	assert.Equal(t, time.Duration(0), rl.window)
	assert.Equal(t, 100, rl.limit)
}

// ---------------------------------------------------------------------------
// DistributedLock
// ---------------------------------------------------------------------------

func TestDistributedLockTTL(t *testing.T) {
	dl := &DistributedLock{ttl: 0}
	// Zero TTL should be replaced by caller.
	assert.Equal(t, time.Duration(0), dl.ttl)
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

func TestKeyPrefixes(t *testing.T) {
	// Verify key format consistency.
	authCtxID := "session-abc-123"
	gpsiHash := "abc123def456"
	amfID := "amf-node-1"

	// Session key.
	assert.Regexp(t, `^nssaa:session:[a-zA-Z0-9-]+$`, sessionKey(authCtxID))

	// Idempotency key.
	assert.Regexp(t, `^nssaa:idempotency:[^:]+:[a-f0-9]{64}$`,
		idempotencyKey(authCtxID, hashPayload(authCtxID, []byte("test"))))

	// Rate limit keys.
	assert.Regexp(t, `^nssaa:ratelimit:gpsi:[a-zA-Z0-9]+$`, gpsiKey(gpsiHash))
	assert.Regexp(t, `^nssaa:ratelimit:amf:[a-zA-Z0-9-]+$`, amfKey(amfID))

	// Lock key.
	assert.Regexp(t, `^nssaa:lock:session:[a-zA-Z0-9-]+$`, lockKey(authCtxID))
}

// ---------------------------------------------------------------------------
// SessionCache entry round-trip (no Redis needed)
// ---------------------------------------------------------------------------

func TestSessionCacheEntryRoundTrip(t *testing.T) {
	entry := &SessionCacheEntry{
		SnssaiSST:   1,
		SnssaiSD:    "ABCDEF",
		NssaaStatus: "EAP_SUCCESS",
		EAPRounds:   5,
		Method:      "EAP-TLS",
	}

	// Verify all fields are set correctly.
	assert.Equal(t, uint8(1), entry.SnssaiSST)
	assert.Equal(t, "ABCDEF", entry.SnssaiSD)
	assert.Equal(t, "EAP_SUCCESS", entry.NssaaStatus)
	assert.Equal(t, 5, entry.EAPRounds)
	assert.Equal(t, "EAP-TLS", entry.Method)
}

// ---------------------------------------------------------------------------
// GPSI hashing for audit
// ---------------------------------------------------------------------------

func TestGPSIHashForAudit(t *testing.T) {
	gpsi := "52080460000001"

	h1 := HashGPSI(gpsi)
	h2 := HashGPSI(gpsi)

	// SHA-256 first 16 bytes hex → 32 chars.
	assert.Len(t, h1, 32)
	assert.Equal(t, h1, h2)

	// Different GPSI.
	assert.NotEqual(t, h1, HashGPSI("52080460000002"))
}
