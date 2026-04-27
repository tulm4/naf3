package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddleware(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := GetClaimsFromContext(r.Context())
		if claims == nil {
			t.Error("expected claims in context")
		}
		w.WriteHeader(http.StatusOK)
	})

	// Test: missing Authorization header
	t.Run("missing_auth_header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/nnssaaf-nssaa/v1/", nil)
		rr := httptest.NewRecorder()
		mw := AuthMiddleware("nnssaaf-nssaa")(nextHandler)
		mw.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
	})

	// Test: invalid Authorization scheme
	t.Run("invalid_scheme", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/nnssaaf-nssaa/v1/", nil)
		req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
		rr := httptest.NewRecorder()
		mw := AuthMiddleware("nnssaaf-nssaa")(nextHandler)
		mw.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
	})

	// Test: empty token
	t.Run("empty_token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/nnssaaf-nssaa/v1/", nil)
		req.Header.Set("Authorization", "Bearer ")
		rr := httptest.NewRecorder()
		mw := AuthMiddleware("nnssaaf-nssaa")(nextHandler)
		mw.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
	})
}

func TestTokenHash(t *testing.T) {
	h := TokenHash("test-token")
	if len(h) != 16 {
		t.Errorf("expected 16 hex chars, got %d", len(h))
	}
	// Same input → same output
	h2 := TokenHash("test-token")
	if h != h2 {
		t.Error("TokenHash not deterministic")
	}
	// Different input → different output
	h3 := TokenHash("other-token")
	if h == h3 {
		t.Error("TokenHash produced same hash for different input")
	}
}

func TestGetClaimsFromContext(t *testing.T) {
	// No claims in empty context → nil
	ctx := context.Background()
	if claims := GetClaimsFromContext(ctx); claims != nil {
		t.Error("expected nil for empty context")
	}

	// Claims injected → returns TokenClaims
	claims := &TokenClaims{Scope: "nnssaaf-nssaa"}
	ctxWithClaims := context.WithValue(ctx, claimsContextKey, claims)
	got := GetClaimsFromContext(ctxWithClaims)
	if got == nil {
		t.Fatal("expected claims, got nil")
	}
	if got.Scope != "nnssaaf-nssaa" {
		t.Errorf("expected scope 'nnssaaf-nssaa', got %q", got.Scope)
	}
}

func TestAuthMiddlewareWithOptions_SkipPaths(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := AuthMiddlewareWithOptions("nnssaaf-nssaa", WithSkipPaths("/healthz/"))

	// Skip path → no auth
	t.Run("skip_path", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz/", nil)
		rr := httptest.NewRecorder()
		mw(nextHandler).ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 (skip auth), got %d", rr.Code)
		}
	})

	// Protected path → auth required (will 401 without valid token)
	t.Run("protected_path_no_token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/nnssaaf-nssaa/v1/", nil)
		rr := httptest.NewRecorder()
		mw(nextHandler).ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
	})
}
