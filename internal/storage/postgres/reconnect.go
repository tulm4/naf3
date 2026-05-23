// Package postgres provides PostgreSQL data persistence layer for NSSAAF.
// Spec: TS 28.541 §5.3, TS 29.571 §7
package postgres

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrConnectionFailed is returned when all reconnection attempts fail.
var ErrConnectionFailed = errors.New("failed to reconnect to database")

// pgx connection error codes that indicate connection issues
var connectionErrorCodes = map[string]bool{
	"08000": true, // connection_exception
	"08003": true, // connection_does_not_exist
	"08006": true, // connection_failure
	"08001": true, // sqlclient_unable_to_establish_sqlconnection
	"08004": true, // sqlserver_rejected_establishment_of_sqlconnection
	"57P01": true, // admin_shutdown
	"57P02": true, // crash_shutdown
	"57P03": true, // cannot_connect_now
	"40001": true, // serialization_failure (after retry)
}

// IsConnectionError classifies connection-related errors.
func IsConnectionError(err error) bool {
	if err == nil {
		return false
	}

	var pgxErr *pgconn.PgError
	if errors.As(err, &pgxErr) {
		return connectionErrorCodes[pgxErr.Code]
	}

	// Network errors
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}

	return false
}

// PooClient defines the interface for pool operations needed by ReconnectingPool.
// This allows for easy testing with mock implementations.
type PoolClient interface {
	Close()
	Ping(ctx context.Context) error
}

// ReconnectingPool wraps Pool with automatic reconnection.
type ReconnectingPool struct {
	pool PoolClient
	cfg  Config
}

// NewReconnectingPool creates a reconnecting pool wrapper.
func NewReconnectingPool(ctx context.Context, cfg Config) (*ReconnectingPool, error) {
	pool, err := NewPool(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &ReconnectingPool{pool: pool, cfg: cfg}, nil
}

// ExecuteWithRetry executes a function with automatic reconnection on failure.
func (rp *ReconnectingPool) ExecuteWithRetry(ctx context.Context, maxRetries int, fn func(context.Context, *Pool) error) error {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := fn(ctx, nil); err != nil {
			lastErr = err

			if !IsConnectionError(err) {
				return err // Non-connection error, don't retry
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt+1) * time.Second):
				if err := rp.reconnect(ctx); err != nil {
					lastErr = err
					continue
				}
			}
			continue
		}
		return nil
	}

	return errors.Join(ErrConnectionFailed, lastErr)
}

// reconnect attempts to reconnect to the database.
func (rp *ReconnectingPool) reconnect(ctx context.Context) error {
	rp.pool.Close()

	newPool, err := NewPool(ctx, rp.cfg)
	if err != nil {
		return err
	}

	rp.pool = newPool
	return nil
}
