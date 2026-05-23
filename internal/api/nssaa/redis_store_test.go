// Package nssaa provides HTTP handlers for the Nnssaaf_NSSAA service (N58 interface).
// Spec: TS 29.526 §7.2, TS 23.502 §4.2.9
package nssaa

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisAuthCtxStore(t *testing.T) {
	sr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(func() { sr.Close() })

	client := redis.NewClient(&redis.Options{Addr: sr.Addr()})
	t.Cleanup(func() { client.Close() })

	store := NewRedisAuthCtxStore(client)

	t.Run("Save and Load", func(t *testing.T) {
		authCtx := &AuthCtx{
			AuthCtxID:   "test-123",
			GPSI:        "msisdn-1234567890",
			SnssaiSST:   1,
			SnssaiSD:    "000001",
			AmfInstance: "amf-1",
		}

		err := store.Save(context.Background(), authCtx)
		require.NoError(t, err)

		loaded, err := store.Load(context.Background(), "test-123")
		require.NoError(t, err)
		assert.Equal(t, authCtx.AuthCtxID, loaded.AuthCtxID)
		assert.Equal(t, authCtx.GPSI, loaded.GPSI)
	})

	t.Run("Load not found", func(t *testing.T) {
		_, err := store.Load(context.Background(), "nonexistent")
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("Delete", func(t *testing.T) {
		authCtx := &AuthCtx{AuthCtxID: "delete-123"}
		err := store.Save(context.Background(), authCtx)
		require.NoError(t, err)

		err = store.Delete(context.Background(), "delete-123")
		require.NoError(t, err)

		_, err = store.Load(context.Background(), "delete-123")
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("TTL is 24 hours", func(t *testing.T) {
		authCtx := &AuthCtx{AuthCtxID: "ttl-test"}
		err := store.Save(context.Background(), authCtx)
		require.NoError(t, err)

		ttl := sr.TTL("nssaa:auth:ctx:ttl-test")
		assert.True(t, ttl > 23*time.Hour && ttl <= 24*time.Hour)
	})
}
