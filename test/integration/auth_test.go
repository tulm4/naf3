// Package integration provides integration tests for NSSAAF against real infrastructure.
package integration

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/operator/nssAAF/internal/auth"
	"github.com/stretchr/testify/require"
)

// TestAuthBypass_E2EMode verifies that requests succeed without a JWT
// Authorization header when auth is disabled via NAF3_AUTH_DISABLED=1.
func TestAuthBypass_E2EMode(t *testing.T) {
	if os.Getenv("NAF3_AUTH_DISABLED") != "1" {
		t.Skip("NAF3_AUTH_DISABLED != 1 — set to run this test")
	}

	// Downstream Biz handler — returns 200 if request reaches it.
	bizCalled := false
	bizHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bizCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Auth middleware with Disabled=true (env var NAF3_AUTH_DISABLED=1 already set).
	mw := auth.NewAuthMiddleware(auth.Config{Disabled: true})
	handler := mw(bizHandler)

	// Request without Authorization header.
	req := httptest.NewRequest(http.MethodPost, "/nnssaaf-nssaa/v1/slice-authentications", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code,
		"HTTP Gateway should return 200 when auth is disabled, got %d: %s",
		rec.Code, rec.Body.String())
	require.True(t, bizCalled, "downstream Biz handler should be called when auth is bypassed")
}

// TestAuthEnforced_WhenEnabled verifies that requests without a valid JWT
// are rejected when auth is enabled (NAF3_AUTH_DISABLED is not set).
// This test runs in unit-test mode — it directly exercises the middleware.
func TestAuthEnforced_WhenEnabled(t *testing.T) {
	if os.Getenv("NAF3_AUTH_DISABLED") == "1" {
		t.Skip("NAF3_AUTH_DISABLED=1 — skipping enforcement test (auth is bypassed)")
	}

	// Auth middleware with auth enabled (Disabled=false).
	mw := auth.NewAuthMiddleware(auth.Config{Disabled: false})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream handler called — auth was not enforced")
	}))

	req := httptest.NewRequest(http.MethodGet, "/nnssaaf-nssaa/v1/slice-authentications/some-id", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should return 401 when auth is enabled and no Authorization header is present.
	require.Equal(t, http.StatusUnauthorized, rec.Code,
		"HTTP Gateway should return 401 when auth is enabled and no token is provided, got %d",
		rec.Code)
}
