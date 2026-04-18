// Package redis provides Redis caching layer for NSSAAF.
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter implements a sliding-window rate limiter using Redis.
type RateLimiter struct {
	client redis.Cmdable
	window time.Duration
	limit  int
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(client redis.Cmdable, window time.Duration, limit int) *RateLimiter {
	return &RateLimiter{
		client: client,
		window: window,
		limit:  limit,
	}
}

// gpsiKey returns the rate limit key for a GPSI.
func gpsiKey(gpsiHash string) string {
	return fmt.Sprintf("nssaa:ratelimit:gpsi:%s", gpsiHash)
}

// amfKey returns the rate limit key for an AMF.
func amfKey(amfID string) string {
	return fmt.Sprintf("nssaa:ratelimit:amf:%s", amfID)
}

// Allow checks whether a request from the given GPSI hash is within the rate limit.
// Returns true if allowed, false if rate limited.
func (r *RateLimiter) Allow(ctx context.Context, key string) (bool, error) {
	return r.allow(ctx, key, r.window)
}

// AllowGPSI checks rate limit for a GPSI hash.
func (r *RateLimiter) AllowGPSI(ctx context.Context, gpsiHash string) (bool, error) {
	return r.Allow(ctx, gpsiKey(gpsiHash))
}

// AllowAMF checks rate limit for an AMF ID.
func (r *RateLimiter) AllowAMF(ctx context.Context, amfID string) (bool, error) {
	return r.Allow(ctx, amfKey(amfID))
}

func (r *RateLimiter) allow(ctx context.Context, key string, window time.Duration) (bool, error) {
	now := time.Now().UnixMilli()
	windowMs := window.Milliseconds()
	windowStart := now - windowMs

	pipe := r.client.Pipeline()

	// Remove old entries outside the window.
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart))

	// Count current entries in window (before adding this request).
	countCmd := pipe.ZCard(ctx, key)

	// Add current request.
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: now})

	// Set expiry on the key.
	pipe.Expire(ctx, key, window+time.Second)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("rate limiter: %w", err)
	}

	count := countCmd.Val()
	if count >= int64(r.limit) {
		return false, nil
	}
	return true, nil
}

// GetCount returns the current count for a key within the window.
func (r *RateLimiter) GetCount(ctx context.Context, key string) (int64, error) {
	now := time.Now().UnixMilli()
	windowMs := r.window.Milliseconds()
	windowStart := now - windowMs

	count, err := r.client.ZCount(ctx, key, fmt.Sprintf("%d", windowStart), "+inf").Result()
	if err != nil {
		return 0, fmt.Errorf("rate limiter count: %w", err)
	}
	return count, nil
}

// Reset clears the rate limit for a key.
func (r *RateLimiter) Reset(ctx context.Context, key string) error {
	if err := r.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("rate limiter reset: %w", err)
	}
	return nil
}
