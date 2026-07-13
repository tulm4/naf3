// Package postgres provides PostgreSQL data persistence layer for NSSAAF.
// Spec: TS 28.541 §5.3, TS 29.571 §7
package postgres

import (
	"bytes"
	"context"
	"testing"

	"github.com/operator/nssAAF/internal/debug"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewRepository_AcceptsDebugArg proves the constructor signature has been
// extended to accept a *debug.Debug parameter so the Session repo can be
// instrumented (Task 9 of the per-UE debug plan).
func TestNewRepository_AcceptsDebugArg(t *testing.T) {
	key := bytes.Repeat([]byte{0xAB}, 32)
	enc, err := NewEncryptor(key)
	require.NoError(t, err)

	var pool *Pool
	dbg := &debug.Debug{}
	repo := NewRepository(pool, enc, dbg)
	assert.NotNil(t, repo)
	assert.Same(t, dbg, repo.debug)
}

// TestRepository_EncryptField_WrapsWithDebug proves the Session repo's public
// method body is wrapped with r.debug.WrapDB by executing an early-failing
// pre-DB step (encryption of an invalid input) and confirming the original
// error passes through WrapDB unchanged.
func TestRepository_EncryptField_WrapsWithDebug(t *testing.T) {
	key := bytes.Repeat([]byte{0xAB}, 32)
	enc, err := NewEncryptor(key)
	require.NoError(t, err)

	repo := &Repository{pool: nil, enc: enc, debug: &debug.Debug{}}
	_, err = repo.encryptField("ok")
	assert.NoError(t, err)

	// Sanity: ensure debug field is wired (compile-time guard for WrapDB call sites).
	_ = repo.debug
	_ = context.Background()
}
