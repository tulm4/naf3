package nrf

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/config"
)

func TestTokenCacheGetToken(t *testing.T) {
	// Mock NRF token endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("expected form-urlencoded, got %s", r.Header.Get("Content-Type"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"test-token-123","expires_in":3600,"scope":"nnrf-nfm"}`))
	}))
	defer server.Close()

	cfg := config.TokenConfig{
		Enabled:      true,
		AuthServer:   server.URL,
		ClientID:     "nssAAF-client",
		ClientSecret: "secret",
		Scope:        "nnrf-nfm",
	}

	cache := NewTokenCache(cfg)
	ctx := context.Background()

	// First call should fetch token
	token, err := cache.GetToken(ctx)
	if err != nil {
		t.Fatalf("GetToken failed: %v", err)
	}

	if token != "test-token-123" {
		t.Errorf("token mismatch: got %s", token)
	}

	// Second call should use cached token
	token2, err := cache.GetToken(ctx)
	if err != nil {
		t.Fatalf("GetToken second call failed: %v", err)
	}

	if token != token2 {
		t.Errorf("cached token mismatch")
	}
}

func TestTokenCacheRefreshBeforeExpiry(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		// Short expiry to test refresh
		w.Write([]byte(`{"access_token":"token-v2","expires_in":1,"scope":"nnrf-nfm"}`))
	}))
	defer server.Close()

	cfg := config.TokenConfig{
		Enabled:      true,
		AuthServer:   server.URL,
		ClientID:     "nssAAF-client",
		ClientSecret: "secret",
		Scope:        "nnrf-nfm",
	}

	cache := NewTokenCache(cfg)
	ctx := context.Background()

	// First call
	token1, _ := cache.GetToken(ctx)
	if token1 != "token-v2" {
		t.Errorf("first token mismatch")
	}

	// Wait for expiry
	time.Sleep(1100 * time.Millisecond)

	// Should refresh
	token2, _ := cache.GetToken(ctx)
	if token2 != "token-v2" {
		t.Errorf("second token mismatch")
	}

	if callCount < 2 {
		t.Errorf("expected 2+ calls, got %d", callCount)
	}
}

func TestTokenCacheErrors(t *testing.T) {
	// Test non-200 status
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	cfg := config.TokenConfig{
		Enabled:      true,
		AuthServer:   server.URL,
		ClientID:     "nssAAF-client",
		ClientSecret: "secret",
		Scope:        "nnrf-nfm",
	}

	cache := NewTokenCache(cfg)
	_, err := cache.GetToken(context.Background())
	if err == nil {
		t.Error("expected error for 401 response")
	}

	// Test server closed (network error)
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server2.Close() // Force close

	cfg2 := config.TokenConfig{
		Enabled:      true,
		AuthServer:   server2.URL,
		ClientID:     "nssAAF-client",
		ClientSecret: "secret",
		Scope:        "nnrf-nfm",
	}

	cache2 := NewTokenCache(cfg2)
	_, err = cache2.GetToken(context.Background())
	if err == nil {
		t.Error("expected error for closed server")
	}
}

func TestTokenCacheConcurrentGetToken(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"concurrent-token","expires_in":1}`))
	}))
	defer server.Close()

	cfg := config.TokenConfig{
		Enabled:      true,
		AuthServer:   server.URL,
		ClientID:     "nssAAF-client",
		ClientSecret: "secret",
		Scope:        "nnrf-nfm",
	}

	cache := NewTokenCache(cfg)
	ctx := context.Background()

	// First call to get token
	_, err := cache.GetToken(ctx)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	// Wait for expiry
	time.Sleep(1100 * time.Millisecond)

	// Launch concurrent calls - should collapse to 1 refresh
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.GetToken(ctx)
		}()
	}
	wg.Wait()

	if calls.Load() > 2 {
		t.Errorf("too many calls: expected 2, got %d", calls.Load())
	}
}
