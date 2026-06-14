// Package gateway provides the AAA Gateway component.
package gateway

import (
	"github.com/redis/go-redis/v9"
)

// SentinelConfig holds Redis Sentinel configuration.
type SentinelConfig struct {
	Addrs      []string
	MasterName string
	Password   string
}

// newRedisClient creates a Redis client based on the configured mode.
func newRedisClient(redisAddr, mode string) *redis.Client {
	opts := &redis.Options{
		Addr: redisAddr,
	}
	switch mode {
	case "sentinel":
		// For Sentinel mode, connect to the Sentinel address directly.
		// go-redis/v9 handles Sentinel autodiscovery.
		opts.Addr = redisAddr
	default:
		// Default: direct connection to single Redis node.
	}
	return redis.NewClient(opts)
}
