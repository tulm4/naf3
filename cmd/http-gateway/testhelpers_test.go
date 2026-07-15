// Package main — test helpers for cmd/http-gateway/main_test.go.
//
// These helpers exist only so the wiring tests can exercise the handler
// chain constructed in main.go without spinning up the full process (TLS,
// signal handling, etc.). They are not used by production code.
package main

import (
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/operator/nssAAF/internal/auth"
	"github.com/operator/nssAAF/internal/debug"
)

// newEnabledDebugForTest returns a debug.Debug backed by an in-process
// miniredis so the http.request Emit call round-trips through XAdd without
// needing a real Redis. Caller must NOT call Close — the cleanup hook in
// the test does it.
func newEnabledDebugForTest(t *testing.T) *debug.Debug {
	t.Helper()

	mr := miniredis.RunT(t)

	podID, _ := os.Hostname()
	d, err := debug.New(t.Context(), debug.Config{
		Enabled:   true,
		RedisAddr: mr.Addr(),
		Service:   "http-gw-test",
		PodID:     podID,
		TTL:       time.Hour,
		MaxLen:    1000,
	})
	if err != nil {
		t.Fatalf("debug.New: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// noAuth returns an Auth.Config that bypasses authentication. Used in unit
// tests where JWT forging is out of scope.
func noAuth() auth.Config { return auth.Config{Disabled: true} }

// authConfigDisabled returns AuthConfig-shaped value for tests that need to
// plug into the buildHandler deps. Aliased to noAuth for clarity at call
// sites.
func authConfigDisabled() auth.Config { return noAuth() }
