// Package postgres provides PostgreSQL data persistence layer for NSSAAF.
// Spec: TS 28.541 §5.3, TS 29.571 §7
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Config holds the PostgreSQL connection pool configuration.
type Config struct {
	DSN               string
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
}

// Pool wraps a PostgreSQL connection pool with health checks.
type Pool struct {
	pool *pgxpool.Pool
}

// NewPool creates a new PostgreSQL connection pool.
func NewPool(ctx context.Context, cfg Config) (*Pool, error) {
	config, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to parse DSN: %w", err)
	}

	if cfg.MaxConns > 0 {
		config.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		config.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		config.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.MaxConnIdleTime > 0 {
		config.MaxConnIdleTime = cfg.MaxConnIdleTime
	}
	if cfg.HealthCheckPeriod > 0 {
		config.HealthCheckPeriod = cfg.HealthCheckPeriod
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to create pool: %w", err)
	}

	// Verify connectivity.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: failed to ping: %w", err)
	}

	return &Pool{pool: pool}, nil
}

// Acquire returns a connection from the pool.
func (p *Pool) Acquire(ctx context.Context) (*pgxpool.Conn, error) {
	return p.pool.Acquire(ctx)
}

// Exec executes a query without returning rows.
func (p *Pool) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := p.pool.Exec(ctx, sql, args...)
	return err
}

// ExecResult executes a query and returns the command tag (e.g. for RowsAffected).
func (p *Pool) ExecResult(ctx context.Context, sql string, args ...any) (int64, error) {
	tag, err := p.pool.Exec(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Query executes a query and returns rows.
func (p *Pool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return p.pool.Query(ctx, sql, args...)
}

// QueryRow executes a query and returns a single row.
func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return p.pool.QueryRow(ctx, sql, args...)
}

// BeginTx starts a new transaction.
func (p *Pool) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return p.pool.Begin(ctx)
}

// Stats returns pool statistics.
func (p *Pool) Stats() *pgxpool.Stat {
	return p.pool.Stat()
}

// Close shuts down the pool.
func (p *Pool) Close() {
	p.pool.Close()
}

// Ping checks database connectivity.
func (p *Pool) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}
