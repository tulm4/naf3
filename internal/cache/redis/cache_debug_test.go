// Package redis provides Redis caching layer for NSSAAF.
// Spec: TS 29.571 §7
package redis

import (
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/debug"
	"github.com/stretchr/testify/assert"
	miniredis "github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

// TestNewSessionCache_AcceptsDebugArg proves the SessionCache constructor
// has been extended with a *debug.Debug parameter (Task 11).
func TestNewSessionCache_AcceptsDebugArg(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	dbg := &debug.Debug{}
	cache := NewSessionCache(rdb, time.Minute, dbg)
	assert.NotNil(t, cache)
	assert.Same(t, dbg, cache.debug)
}

// TestNewRateLimiter_AcceptsDebugArg proves the RateLimiter constructor
// has been extended with a *debug.Debug parameter (Task 11).
func TestNewRateLimiter_AcceptsDebugArg(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	dbg := &debug.Debug{}
	rl := NewRateLimiter(rdb, time.Second, 100, dbg)
	assert.NotNil(t, rl)
	assert.Same(t, dbg, rl.debug)
}
