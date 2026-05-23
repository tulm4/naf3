// Package postgres provides PostgreSQL data persistence layer for NSSAAF.
// Spec: TS 28.541 §5.3, TS 29.571 §7
package postgres

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconnectingPool(t *testing.T) {
	t.Run("ExecuteWithRetry succeeds on first attempt", func(t *testing.T) {
		rp := &ReconnectingPool{pool: &mockPool{}}

		called := 0
		err := rp.ExecuteWithRetry(context.Background(), 3, func(ctx context.Context, p *Pool) error {
			called++
			return nil
		})

		require.NoError(t, err)
		assert.Equal(t, 1, called)
	})

	t.Run("ExecuteWithRetry retries on connection error", func(t *testing.T) {
		rp := &ReconnectingPool{pool: &mockPool{failConn: true}, cfg: Config{}}

		called := 0
		err := rp.ExecuteWithRetry(context.Background(), 3, func(ctx context.Context, p *Pool) error {
			called++
			if called < 3 {
				// Use actual net.OpError that IsConnectionError can detect
				return &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset")}
			}
			return nil
		})

		require.NoError(t, err)
		assert.Equal(t, 3, called)
	})

	t.Run("ExecuteWithRetry returns non-connection errors immediately", func(t *testing.T) {
		rp := &ReconnectingPool{pool: &mockPool{}}

		err := rp.ExecuteWithRetry(context.Background(), 3, func(ctx context.Context, p *Pool) error {
			return errors.New("business error")
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "business error")
	})
}

// mockPool implements PoolClient interface for testing
type mockPool struct {
	failConn bool
}

func (m *mockPool) Ping(ctx context.Context) error {
	if m.failConn {
		return &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	}
	return nil
}

func (m *mockPool) Close() {}

// Verify mockPool implements PoolClient
var _ PoolClient = (*mockPool)(nil)


