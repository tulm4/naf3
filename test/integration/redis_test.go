// Package integration provides integration tests for NSSAAF against real infrastructure.
package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	cacheredis "github.com/operator/nssAAF/internal/cache/redis"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func skipIfNoRedis_PG(t *testing.T) {
	if _, present := os.LookupEnv("TEST_REDIS_URL"); !present {
		t.Skip("TEST_REDIS_URL not set — skipping Redis integration test")
	}
}

func openTestRedisClient(t *testing.T) *goredis.Client {
	skipIfNoRedis_PG(t)
	client := goredis.NewClient(&goredis.Options{
		Addr: testRedisURL(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := client.Ping(ctx).Err()
	require.NoError(t, err, "failed to connect to test Redis")
	return client
}

// ─── Test: CacheSession with TTL ─────────────────────────────────────────────

func TestIntegration_Redis_CacheSession(t *testing.T) {
	client := openTestRedisClient(t)
	defer client.Close()

	cache := cacheredis.NewSessionCache(client, 10*time.Second)
	ctx := context.Background()

	entry := &cacheredis.SessionCacheEntry{
		SnssaiSST:   1,
		SnssaiSD:    "000001",
		NssaaStatus: "PENDING",
		EAPRounds:   1,
		Method:      "EAP-TLS",
	}

	err := cache.Set(ctx, "session-001", entry)
	require.NoError(t, err, "cache set should succeed")

	loaded, err := cache.Get(ctx, "session-001")
	require.NoError(t, err, "cache get should succeed")
	require.NotNil(t, loaded)
	assert.Equal(t, uint8(1), loaded.SnssaiSST)
	assert.Equal(t, "000001", loaded.SnssaiSD)
	assert.Equal(t, "PENDING", loaded.NssaaStatus)
}

// ─── Test: GetCachedSession ──────────────────────────────────────────────────

func TestIntegration_Redis_GetCachedSession(t *testing.T) {
	client := openTestRedisClient(t)
	defer client.Close()

	cache := cacheredis.NewSessionCache(client, 10*time.Second)
	ctx := context.Background()

	entry := &cacheredis.SessionCacheEntry{
		SnssaiSST:   128,
		SnssaiSD:    "ABCDEF",
		NssaaStatus: "EAP_SUCCESS",
		EAPRounds:   3,
		Method:      "EAP-AKA",
	}

	err := cache.Set(ctx, "session-002", entry)
	require.NoError(t, err)

	loaded, err := cache.Get(ctx, "session-002")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, uint8(128), loaded.SnssaiSST)
	assert.Equal(t, "ABCDEF", loaded.SnssaiSD)
	assert.Equal(t, "EAP_SUCCESS", loaded.NssaaStatus)
	assert.Equal(t, 3, loaded.EAPRounds)
}

// ─── Test: CacheExpiry ────────────────────────────────────────────────────────

func TestIntegration_Redis_CacheExpiry(t *testing.T) {
	client := openTestRedisClient(t)
	defer client.Close()

	cache := cacheredis.NewSessionCache(client, 500*time.Millisecond)
	ctx := context.Background()

	entry := &cacheredis.SessionCacheEntry{
		SnssaiSST:   1,
		SnssaiSD:    "000001",
		NssaaStatus: "PENDING",
		EAPRounds:   0,
		Method:      "EAP-TLS",
	}

	err := cache.Set(ctx, "session-003", entry)
	require.NoError(t, err)

	// Should be present immediately.
	exists, err := cache.Exists(ctx, "session-003")
	require.NoError(t, err)
	assert.True(t, exists, "session should exist immediately after set")

	// Wait for TTL to expire.
	time.Sleep(600 * time.Millisecond)

	// Should be gone after TTL.
	exists, err = cache.Exists(ctx, "session-003")
	require.NoError(t, err)
	assert.False(t, exists, "session should be expired after TTL")
}

// ─── Test: CacheEviction ─────────────────────────────────────────────────────
// Note: Testing LRU eviction under memory pressure requires configuring Redis
// with maxmemory. This test verifies the cache operations work correctly
// and that entries can be deleted individually.

func TestIntegration_Redis_CacheEviction(t *testing.T) {
	client := openTestRedisClient(t)
	defer client.Close()

	cache := cacheredis.NewSessionCache(client, 10*time.Second)
	ctx := context.Background()

	// Insert multiple entries.
	for i := 0; i < 5; i++ {
		entry := &cacheredis.SessionCacheEntry{
			SnssaiSST:   1,
			SnssaiSD:    "000001",
			NssaaStatus: "PENDING",
			EAPRounds:   0,
			Method:      "EAP-TLS",
		}
		err := cache.Set(ctx, fmt.Sprintf("session-evict-%d", i), entry)
		require.NoError(t, err)
	}

	// Delete a specific entry.
	err := cache.Delete(ctx, "session-evict-2")
	require.NoError(t, err, "delete should succeed")

	// Verify the entry is gone.
	exists, err := cache.Exists(ctx, "session-evict-2")
	require.NoError(t, err)
	assert.False(t, exists, "deleted session should not exist")

	// Verify other entries still exist.
	for i := 0; i < 5; i++ {
		if i == 2 {
			continue
		}
		exists, err := cache.Exists(ctx, fmt.Sprintf("session-evict-%d", i))
		require.NoError(t, err)
		assert.True(t, exists, "non-deleted session %d should still exist", i)
	}
}

// ─── Test: DLQ_Publish ────────────────────────────────────────────────────────

func TestIntegration_Redis_DLQ_Publish(t *testing.T) {
	client := openTestRedisClient(t)
	defer client.Close()

	ctx := context.Background()
	dlqKey := "nssAAF:dlq:amf-notifications"

	// Publish a message to the DLQ.
	payload := `{"authCtxId":"test-001","reason":"timeout","timestamp":"2026-04-29T00:00:00Z"}`
	err := client.LPush(ctx, dlqKey, payload).Err()
	require.NoError(t, err, "DLQ publish should succeed")

	// Verify message is in the queue.
	length, err := client.LLen(ctx, dlqKey).Result()
	require.NoError(t, err)
	assert.Greater(t, length, int64(0), "DLQ should contain published message")

	// Clean up.
	_ = client.Del(ctx, dlqKey)
}

// ─── Test: DLQ_Consume ───────────────────────────────────────────────────────

func TestIntegration_Redis_DLQ_Consume(t *testing.T) {
	client := openTestRedisClient(t)
	defer client.Close()

	ctx := context.Background()
	dlqKey := "nssAAF:dlq:amf-notifications"

	// Pre-populate the DLQ.
	payloads := []string{
		`{"authCtxId":"test-002","reason":"timeout"}`,
		`{"authCtxId":"test-003","reason":"aaa_unreachable"}`,
	}
	for _, p := range payloads {
		err := client.RPush(ctx, dlqKey, p).Err()
		require.NoError(t, err)
	}

	// Consume messages (RPOP in FIFO order).
	first, err := client.RPop(ctx, dlqKey).Result()
	require.NoError(t, err)
	assert.Contains(t, first, "test-002", "should consume in FIFO order")

	second, err := client.RPop(ctx, dlqKey).Result()
	require.NoError(t, err)
	assert.Contains(t, second, "test-003", "should consume in FIFO order")

	// Clean up.
	_ = client.Del(ctx, dlqKey)
}

// ─── Test: DLQ_RetryOrder ───────────────────────────────────────────────────

func TestIntegration_Redis_DLQ_RetryOrder(t *testing.T) {
	client := openTestRedisClient(t)
	defer client.Close()

	ctx := context.Background()
	dlqKey := "nssAAF:dlq:amf-notifications-retry"

	// Push in order: 1, 2, 3 using RPush (FIFO queue).
	for i := 1; i <= 3; i++ {
		payload := fmt.Sprintf(`{"attempt":%d}`, i)
		err := client.RPush(ctx, dlqKey, payload).Err()
		require.NoError(t, err)
	}

	// Repush first item for retry (LPush puts it at front).
	first, err := client.RPop(ctx, dlqKey).Result()
	require.NoError(t, err)
	err = client.LPush(ctx, dlqKey, first).Err()
	require.NoError(t, err)

	// Next pop should be the retried item (LPUSH puts it at head, RPOP gets head in FIFO).
	next, err := client.RPop(ctx, dlqKey).Result()
	require.NoError(t, err)
	assert.Contains(t, next, `"attempt":1`, "retry item should come first")

	// Clean up.
	_ = client.Del(ctx, dlqKey)
}

// ─── Test: CircuitBreakerCache ──────────────────────────────────────────────
// Verifies circuit breaker state can be cached in Redis.

func TestIntegration_Redis_CircuitBreakerCache(t *testing.T) {
	client := openTestRedisClient(t)
	defer client.Close()

	ctx := context.Background()
	cbKey := "nssaa:circuit-breaker:test-server:8080"

	// Store OPEN state.
	err := client.HSet(ctx, cbKey, map[string]interface{}{
		"state":       "OPEN",
		"failures":    5,
		"lastFailure": time.Now().Format(time.RFC3339),
	}).Err()
	require.NoError(t, err)

	// Retrieve and verify.
	state, err := client.HGet(ctx, cbKey, "state").Result()
	require.NoError(t, err)
	assert.Equal(t, "OPEN", state)

	failures, err := client.HGet(ctx, cbKey, "failures").Result()
	require.NoError(t, err)
	assert.Equal(t, "5", failures)

	// Update to HALF_OPEN.
	err = client.HSet(ctx, cbKey, "state", "HALF_OPEN").Err()
	require.NoError(t, err)
	state, err = client.HGet(ctx, cbKey, "state").Result()
	require.NoError(t, err)
	assert.Equal(t, "HALF_OPEN", state)

	// Clean up.
	_ = client.Del(ctx, cbKey)
}
