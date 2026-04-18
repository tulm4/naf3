// Package redis provides Redis caching layer for NSSAAF.
// Spec: Redis Cluster / Redis Sentinel
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Config holds Redis client configuration.
type Config struct {
	Addrs        []string
	Password     string
	DB           int
	PoolSize     int
	MinIdleConns int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// Pool wraps a Redis client (single or cluster).
type Pool struct {
	client redis.Cmdable
}

// NewPool creates a new single-node Redis client.
func NewPool(ctx context.Context, cfg Config) (*Pool, error) {
	opt := &redis.Options{
		Addr:         cfg.Addrs[0],
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	client := redis.NewClient(opt)

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redis: ping failed: %w", err)
	}

	return &Pool{client: client}, nil
}

// NewClusterPool creates a Redis Cluster client.
func NewClusterPool(ctx context.Context, cfg Config) (*Pool, error) {
	if len(cfg.Addrs) == 0 {
		return nil, fmt.Errorf("redis: at least one address required for cluster")
	}

	opt := &redis.ClusterOptions{
		Addrs:        cfg.Addrs,
		Password:     cfg.Password,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	client := redis.NewClusterClient(opt)

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redis: cluster ping failed: %w", err)
	}

	return &Pool{client: client}, nil
}

// Client returns the underlying Redis client.
func (p *Pool) Client() redis.Cmdable {
	return p.client
}

// Close shuts down the client.
func (p *Pool) Close() error {
	if c, ok := p.client.(*redis.Client); ok {
		return c.Close()
	}
	if c, ok := p.client.(*redis.ClusterClient); ok {
		return c.Close()
	}
	return nil
}
