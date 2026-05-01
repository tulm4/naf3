// Package postgres provides PostgreSQL data persistence layer for NSSAAF.
// REQ-09: PostgreSQL session store replaces in-memory store via NewSessionStore/NewAIWSessionStore.
// D-06: NewSessionStore(*Pool) and NewAIWSessionStore(*Pool) implement the AuthCtxStore interfaces.
package postgres

import (
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/api/aiw"
	"github.com/operator/nssAAF/internal/api/nssaa"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// Compile-time interface checks
// ---------------------------------------------------------------------------

// Ensure Store implements nssaa.AuthCtxStore at compile time.
var _ nssaa.AuthCtxStore = (*Store)(nil)

// Ensure AIWStore implements aiw.AuthCtxStore at compile time.
var _ aiw.AuthCtxStore = (*AIWStore)(nil)

// ---------------------------------------------------------------------------
// Factory functions
// ---------------------------------------------------------------------------

func TestNewSessionStore_ReturnsNonNil(t *testing.T) {
	// Factory should return a non-nil store even with nil arguments.
	// This is useful for dependency injection patterns.
	store := NewSessionStore(nil, nil)
	assert.NotNil(t, store)
	assert.NotNil(t, store.repo)
}

func TestNewAIWSessionStore_ReturnsNonNil(t *testing.T) {
	store := NewAIWSessionStore(nil, nil)
	assert.NotNil(t, store)
	assert.NotNil(t, store.repo)
}

// ---------------------------------------------------------------------------
// Store interface compliance (no-op methods)
// ---------------------------------------------------------------------------

func TestStore_Close_IsNoOp(t *testing.T) {
	store := NewSessionStore(nil, nil)
	err := store.Close()
	assert.NoError(t, err)
}

func TestAIWStore_Close_IsNoOp(t *testing.T) {
	store := NewAIWSessionStore(nil, nil)
	err := store.Close()
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Session conversion helpers
// ---------------------------------------------------------------------------

func TestSessionToAuthCtx(t *testing.T) {
	session := &Session{
		AuthCtxID:       "auth-123",
		GPSI:            "52080460000001",
		SnssaiSST:       1,
		SnssaiSD:        "ABCDEF",
		AMFInstanceID:   "amf-instance-1",
		ReauthNotifURI:  "http://example.com/reauth",
		RevocNotifURI:   "http://example.com/revoc",
		EAPSessionState: []byte("eap-session-data"),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	authCtx := sessionToAuthCtx(session)

	assert.Equal(t, "auth-123", authCtx.AuthCtxID)
	assert.Equal(t, "52080460000001", authCtx.GPSI)
	assert.Equal(t, uint8(1), authCtx.SnssaiSST)
	assert.Equal(t, "ABCDEF", authCtx.SnssaiSD)
	assert.Equal(t, "amf-instance-1", authCtx.AmfInstance)
	assert.Equal(t, "http://example.com/reauth", authCtx.ReauthURI)
	assert.Equal(t, "http://example.com/revoc", authCtx.RevocURI)
	assert.Equal(t, []byte("eap-session-data"), authCtx.EapPayload)
}

func TestAuthCtxToSession(t *testing.T) {
	authCtx := &nssaa.AuthCtx{
		AuthCtxID:   "auth-456",
		GPSI:        "52080460000002",
		SnssaiSST:   2,
		SnssaiSD:    "123456",
		AmfInstance: "amf-instance-2",
		ReauthURI:   "http://example.com/reauth2",
		RevocURI:    "http://example.com/revoc2",
		EapPayload:  []byte("eap-payload-2"),
	}

	session := authCtxToSession(authCtx)

	assert.Equal(t, "auth-456", session.AuthCtxID)
	assert.Equal(t, "52080460000002", session.GPSI)
	assert.Equal(t, uint8(2), session.SnssaiSST)
	assert.Equal(t, "123456", session.SnssaiSD)
	assert.Equal(t, "amf-instance-2", session.AMFInstanceID)
	assert.Equal(t, "http://example.com/reauth2", session.ReauthNotifURI)
	assert.Equal(t, "http://example.com/revoc2", session.RevocNotifURI)
	assert.Equal(t, []byte("eap-payload-2"), session.EAPSessionState)
	assert.False(t, session.CreatedAt.IsZero())
	assert.False(t, session.UpdatedAt.IsZero())
}

func TestAIWSessionToAuthCtx(t *testing.T) {
	session := &AIWSession{
		AuthCtxID:       "aiw-auth-123",
		Supi:            "imu-123456789012345",
		EAPSessionState: []byte("aiw-eap-state"),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	authCtx := aiwsessionToAuthCtx(session)

	assert.Equal(t, "aiw-auth-123", authCtx.AuthCtxID)
	assert.Equal(t, "imu-123456789012345", authCtx.Supi)
	assert.Equal(t, []byte("aiw-eap-state"), authCtx.EapPayload)
}

func TestAuthCtxToAIWSession(t *testing.T) {
	authCtx := &aiw.AuthContext{
		AuthCtxID:  "aiw-auth-456",
		Supi:       "imu-987654321098765",
		EapPayload: []byte("aiw-eap-payload"),
	}

	session := authCtxToAIWSession(authCtx)

	assert.Equal(t, "aiw-auth-456", session.AuthCtxID)
	assert.Equal(t, "imu-987654321098765", session.Supi)
	assert.Equal(t, []byte("aiw-eap-payload"), session.EAPSessionState)
}

// ---------------------------------------------------------------------------
// Round-trip conversion
// ---------------------------------------------------------------------------

func TestSessionToAuthCtx_RoundTrip(t *testing.T) {
	original := &nssaa.AuthCtx{
		AuthCtxID:   "auth-roundtrip",
		GPSI:        "52012345678901",
		SnssaiSST:   128,
		SnssaiSD:    "FFFFFF",
		AmfInstance: "amf-roundtrip",
		ReauthURI:   "http://example.com/rereauth",
		RevocURI:    "http://example.com/rerevoc",
		EapPayload:  []byte("roundtrip-payload"),
	}

	session := authCtxToSession(original)
	restored := sessionToAuthCtx(session)

	assert.Equal(t, original.AuthCtxID, restored.AuthCtxID)
	assert.Equal(t, original.GPSI, restored.GPSI)
	assert.Equal(t, original.SnssaiSST, restored.SnssaiSST)
	assert.Equal(t, original.SnssaiSD, restored.SnssaiSD)
	assert.Equal(t, original.AmfInstance, restored.AmfInstance)
	assert.Equal(t, original.ReauthURI, restored.ReauthURI)
	assert.Equal(t, original.RevocURI, restored.RevocURI)
	assert.Equal(t, original.EapPayload, restored.EapPayload)
}

func TestAIWAuthCtxRoundTrip(t *testing.T) {
	original := &aiw.AuthContext{
		AuthCtxID:  "aiw-roundtrip",
		Supi:       "imu-555555555555555",
		EapPayload: []byte("aiw-roundtrip-payload"),
	}

	session := authCtxToAIWSession(original)
	restored := aiwsessionToAuthCtx(session)

	assert.Equal(t, original.AuthCtxID, restored.AuthCtxID)
	assert.Equal(t, original.Supi, restored.Supi)
	assert.Equal(t, original.EapPayload, restored.EapPayload)
}
