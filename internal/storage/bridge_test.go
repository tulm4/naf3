// Package storage provides data persistence layer for NSSAAF.
package storage

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator/nssAAF/internal/eap"
)

type mockPersistence struct {
	mu    sync.Mutex
	saved *AuthSession
	getFn func(string) (*AuthSession, error)
	err   error
}

func (m *mockPersistence) SaveAuthSession(_ context.Context, s *AuthSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saved = s
	return m.err
}

func (m *mockPersistence) GetAuthSession(_ context.Context, authCtxID string) (*AuthSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getFn != nil {
		return m.getFn(authCtxID)
	}
	return nil, ErrSessionNotFound
}

func TestWriteThroughStore_Put_BothStoresUpdated(t *testing.T) {
	cache := eap.NewTestSessionManager(time.Minute)
	persist := &mockPersistence{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	store := NewWriteThroughStore(cache, persist, logger)

	session := eap.NewSession("auth-1", "alice@example.com")
	err := store.Put(context.Background(), session)
	require.NoError(t, err)

	persist.mu.Lock()
	saved := persist.saved
	persist.mu.Unlock()
	require.NotNil(t, saved)
	assert.Equal(t, "auth-1", saved.AuthCtxID)
	assert.Equal(t, "alice@example.com", saved.GPSI)
}

func TestWriteThroughStore_Put_CacheFailure_Propagates(t *testing.T) {
	cache := &failingSessionStore{putErr: errors.New("redis connection lost")}
	persist := &mockPersistence{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	store := NewWriteThroughStore(cache, persist, logger)

	err := store.Put(context.Background(), eap.NewSession("auth-1", "alice@example.com"))
	assert.Error(t, err)

	persist.mu.Lock()
	assert.Nil(t, persist.saved) // persistence should not be called if cache fails
	persist.mu.Unlock()
}

func TestWriteThroughStore_Put_PersistenceFailure_Logged_NotPropagated(t *testing.T) {
	cache := eap.NewTestSessionManager(time.Minute)
	persist := &mockPersistence{err: errors.New("postgres connection lost")}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	store := NewWriteThroughStore(cache, persist, logger)

	err := store.Put(context.Background(), eap.NewSession("auth-1", "alice@example.com"))
	assert.NoError(t, err) // cache succeeded, persistence failure is logged, not propagated
}

func TestWriteThroughStore_Get_ReadsFromCache(t *testing.T) {
	cache := eap.NewTestSessionManager(time.Minute)
	persist := &mockPersistence{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	store := NewWriteThroughStore(cache, persist, logger)

	session := eap.NewSession("auth-1", "alice@example.com")
	_ = cache.Put(context.Background(), session)

	got, err := store.Get(context.Background(), "auth-1")
	require.NoError(t, err)
	assert.Equal(t, "auth-1", got.AuthCtxID)
}

func TestWriteThroughStore_Delete_CacheOnly(t *testing.T) {
	cache := eap.NewTestSessionManager(time.Minute)
	persist := &mockPersistence{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	store := NewWriteThroughStore(cache, persist, logger)

	session := eap.NewSession("auth-1", "alice@example.com")
	_ = cache.Put(context.Background(), session)

	err := store.Delete(context.Background(), "auth-1")
	require.NoError(t, err)

	_, err = store.Get(context.Background(), "auth-1")
	assert.Error(t, err)
}

// failingSessionStore is a SessionStore that fails on Put.
type failingSessionStore struct {
	putErr error
}

func (f *failingSessionStore) Get(_ context.Context, _ string) (*eap.Session, error) {
	return nil, eap.ErrSessionNotFound
}
func (f *failingSessionStore) Put(_ context.Context, _ *eap.Session) error {
	return f.putErr
}
func (f *failingSessionStore) Delete(_ context.Context, _ string) error { return nil }
func (f *failingSessionStore) Size() int                            { return 0 }
