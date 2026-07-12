package common

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/operator/nssAAF/internal/debug"
)

// TestDebugMiddleware_NilDebugIsPassThrough verifies that DebugMiddleware
// passes the request through unchanged when called with a nil Debug.
// Spec: docs/superpowers/specs/2026-07-12-nssAAF-per-ue-debug-tracing-design.md §4.2
func TestDebugMiddleware_NilDebugIsPassThrough(t *testing.T) {
	h := DebugMiddleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("expected 418, got %d", rec.Code)
	}
}

// TestDebugMiddleware_DisabledDebugIsPassThrough verifies that DebugMiddleware
// passes the request through unchanged when Debug.Enabled() == false.
// Spec: docs/superpowers/specs/2026-07-12-nssAAF-per-ue-debug-tracing-design.md §4.2
func TestDebugMiddleware_DisabledDebugIsPassThrough(t *testing.T) {
	d := &debug.Debug{} // Enabled() == false by zero value
	h := DebugMiddleware(d)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}