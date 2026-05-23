// Package eap provides EAP (Extensible Authentication Protocol) engine implementation.
// Spec: TS 33.501 §5.13, RFC 3748
package eap

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisSessionManager(t *testing.T) {
	sr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(func() { sr.Close() })

	client := redis.NewClient(&redis.Options{Addr: sr.Addr()})
	t.Cleanup(func() { client.Close() })

	mgr := NewRedisSessionManager(client, 5*time.Minute)

	t.Run("Put and Get", func(t *testing.T) {
		session := NewSession("eap-123", "gpsi-1")
		session.State = SessionStateEapExchange
		session.Rounds = 2
		session.MaxRounds = 20
		session.ExpectedID = 3

		err := mgr.Put(context.Background(), session)
		require.NoError(t, err)

		loaded, err := mgr.Get(context.Background(), "eap-123")
		require.NoError(t, err)
		assert.Equal(t, session.AuthCtxID, loaded.AuthCtxID)
		assert.Equal(t, session.State, loaded.State)
		assert.Equal(t, session.Rounds, loaded.Rounds)
	})

	t.Run("Get not found", func(t *testing.T) {
		_, err := mgr.Get(context.Background(), "nonexistent")
		assert.ErrorIs(t, err, ErrSessionNotFound)
	})

	t.Run("Delete", func(t *testing.T) {
		session := NewSession("delete-test", "gpsi")
		err := mgr.Put(context.Background(), session)
		require.NoError(t, err)

		err = mgr.Delete(context.Background(), "delete-test")
		require.NoError(t, err)

		_, err = mgr.Get(context.Background(), "delete-test")
		assert.ErrorIs(t, err, ErrSessionNotFound)
	})

	t.Run("TTL is 5 minutes", func(t *testing.T) {
		session := NewSession("ttl-test", "gpsi")
		err := mgr.Put(context.Background(), session)
		require.NoError(t, err)

		ttl := sr.TTL("nssaa:eap:session:ttl-test")
		assert.True(t, ttl > 4*time.Minute && ttl <= 5*time.Minute)
	})

	t.Run("Preserves byte slices", func(t *testing.T) {
		session := NewSession("bytes-test", "gpsi")
		session.LastNonce = []byte{0x01, 0x02, 0x03}
		session.CachedResponse = []byte{0x04, 0x05, 0x06}

		err := mgr.Put(context.Background(), session)
		require.NoError(t, err)

		loaded, err := mgr.Get(context.Background(), "bytes-test")
		require.NoError(t, err)
		assert.Equal(t, []byte{0x01, 0x02, 0x03}, loaded.LastNonce)
		assert.Equal(t, []byte{0x04, 0x05, 0x06}, loaded.CachedResponse)
	})

	t.Run("Preserves session metadata", func(t *testing.T) {
		session := NewSession("meta-test", "gpsi@example.com")
		session.SnssaiKey = "1-ffffff"
		session.Method = MethodTLS
		session.State = SessionStateEapExchange
		session.Rounds = 5
		session.MaxRounds = 20
		session.ExpectedID = 6

		err := mgr.Put(context.Background(), session)
		require.NoError(t, err)

		loaded, err := mgr.Get(context.Background(), "meta-test")
		require.NoError(t, err)
		assert.Equal(t, "meta-test", loaded.AuthCtxID)
		assert.Equal(t, "gpsi@example.com", loaded.Gpsi)
		assert.Equal(t, "1-ffffff", loaded.SnssaiKey)
		assert.Equal(t, MethodTLS, loaded.Method)
		assert.Equal(t, SessionStateEapExchange, loaded.State)
		assert.Equal(t, 5, loaded.Rounds)
		assert.Equal(t, 20, loaded.MaxRounds)
		assert.Equal(t, uint8(6), loaded.ExpectedID)
	})
}
