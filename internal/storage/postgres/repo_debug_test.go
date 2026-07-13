// Package postgres provides PostgreSQL data persistence layer for NSSAAF.
// Spec: TS 28.541 §5.3, TS 29.571 §7
package postgres

import (
	"bytes"
	"testing"

	"github.com/operator/nssAAF/internal/debug"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewConfigRepository_AcceptsDebugArg proves the AAA config repo constructor
// has been extended with a *debug.Debug parameter (Task 10).
func TestNewConfigRepository_AcceptsDebugArg(t *testing.T) {
	var pool *Pool
	dbg := &debug.Debug{}
	repo := NewConfigRepository(pool, dbg)
	assert.NotNil(t, repo)
	assert.Same(t, dbg, repo.debug)
}

// TestNewAuditRepository_AcceptsDebugArg proves the audit repo constructor
// has been extended with a *debug.Debug parameter (Task 10).
func TestNewAuditRepository_AcceptsDebugArg(t *testing.T) {
	var pool *Pool
	dbg := &debug.Debug{}
	repo := NewAuditRepository(pool, dbg)
	assert.NotNil(t, repo)
	assert.Same(t, dbg, repo.debug)
}

// TestNewAiwRepository_AcceptsDebugArg proves the AIW repo constructor
// has been extended with a *debug.Debug parameter (Task 10).
func TestNewAiwRepository_AcceptsDebugArg(t *testing.T) {
	key := bytes.Repeat([]byte{0xAB}, 32)
	enc, err := NewEncryptor(key)
	require.NoError(t, err)

	var pool *Pool
	dbg := &debug.Debug{}
	repo := NewAiwRepository(pool, enc, dbg)
	assert.NotNil(t, repo)
	assert.Same(t, dbg, repo.debug)
}
