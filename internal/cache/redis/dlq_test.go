package redis

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDLQ(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	pool, err := NewPool(context.Background(), Config{
		Addrs:        []string{mr.Addr()},
		PoolSize:     10,
		MinIdleConns: 1,
		DialTimeout:  100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer pool.Close()

	dlq := NewDLQ(pool)
	assert.NotNil(t, dlq)
}

func TestDLQ_Enqueue(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	pool, err := NewPool(context.Background(), Config{
		Addrs:        []string{mr.Addr()},
		PoolSize:     10,
		MinIdleConns: 1,
		DialTimeout:  100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer pool.Close()

	dlq := NewDLQ(pool)

	item := &AMFDLQItem{
		ID:        "test-123",
		Type:      "reauth",
		URI:       "http://amf:8080/reauth",
		AuthCtxID: "auth-456",
		Attempt:   1,
		CreatedAt: time.Now(),
		LastError: "connection refused",
	}

	err = dlq.Enqueue(context.Background(), item)
	assert.NoError(t, err)
}

func TestDLQ_Len(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	pool, err := NewPool(context.Background(), Config{
		Addrs:        []string{mr.Addr()},
		PoolSize:     10,
		MinIdleConns: 1,
		DialTimeout:  100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer pool.Close()

	dlq := NewDLQ(pool)

	// Empty queue
	length, err := dlq.Len(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(0), length)

	// Add items
	for i := 0; i < 3; i++ {
		item := &AMFDLQItem{ID: "item-" + string(rune('a'+i))}
		err := dlq.Enqueue(context.Background(), item)
		require.NoError(t, err)
	}

	length, err = dlq.Len(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(3), length)
}
