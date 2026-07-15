// Package redis provides Redis caching layer for NSSAAF.
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/operator/nssAAF/internal/debug"
)

// RateLimiter implements a sliding-window rate limiter using Redis.
type RateLimiter struct {
	client redis.Cmdable
	window time.Duration
	limit  int
	debug  *debug.Debug
}

// NewRateLimiter creates a new rate limiter.
// The *debug.Debug parameter is nil-safe: WrapRedis short-circuits when
// debug is nil or disabled (see internal/debug/hooks.go).
func NewRateLimiter(client redis.Cmdable, window time.Duration, limit int, d *debug.Debug) *RateLimiter {
	return &RateLimiter{
		client: client,
		window: window,
		limit:  limit,
		debug:  d,
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
	return r.allow(ctx, key, r.window, "redis.rate_limit.allow")
}

// AllowGPSI checks rate limit for a GPSI hash.
func (r *RateLimiter) AllowGPSI(ctx context.Context, gpsiHash string) (bool, error) {
	return r.Allow(ctx, gpsiKey(gpsiHash))
}

// AllowAMF checks rate limit for an AMF ID.
func (r *RateLimiter) AllowAMF(ctx context.Context, amfID string) (bool, error) {
	return r.Allow(ctx, amfKey(amfID))
}

func (r *RateLimiter) allow(ctx context.Context, key string, window time.Duration, op string) (bool, error) {
	now := time.Now().UnixMilli()
	windowMs := window.Milliseconds()
	windowStart := now - windowMs

	var count int64
	var allowed bool
	var rlErr error
	if wrapErr := r.debug.WrapRedis(ctx, op, key, func() error {
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
			rlErr = fmt.Errorf("rate limiter: %w", err)
			return rlErr
		}

		count = countCmd.Val()
		allowed = count < int64(r.limit)
		return nil
	}); wrapErr != nil {
		return false, wrapErr
	}
	if rlErr != nil {
		return false, rlErr
	}
	return allowed, nil
}

// GetCount returns the current count for a key within the window.
func (r *RateLimiter) GetCount(ctx context.Context, key string) (int64, error) {
	now := time.Now().UnixMilli()
	windowMs := r.window.Milliseconds()
	windowStart := now - windowMs

	var count int64
	var countErr error
	if wrapErr := r.debug.WrapRedis(ctx, "redis.rate_limit.get_count", key, func() error {
		c, err := r.client.ZCount(ctx, key, fmt.Sprintf("%d", windowStart), "+inf").Result()
		if err != nil {
			countErr = fmt.Errorf("rate limiter count: %w", err)
			return countErr
		}
		count = c
		return nil
	}); wrapErr != nil {
		return 0, wrapErr
	}
	if countErr != nil {
		return 0, countErr
	}
	return count, nil
}

// Reset clears the rate limit for a key.
func (r *RateLimiter) Reset(ctx context.Context, key string) error {
	return r.debug.WrapRedis(ctx, "redis.rate_limit.reset", key, func() error {
		if err := r.client.Del(ctx, key).Err(); err != nil {
			return fmt.Errorf("rate limiter reset: %w", err)
		}
		return nil
	})
}
