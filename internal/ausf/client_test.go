package ausf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/nfclient"
	"github.com/operator/nssAAF/internal/resilience"
	"github.com/stretchr/testify/assert"
)

func TestForwardMSK_Success(t *testing.T) {
	var callCount atomic.Int32
	var receivedBody struct {
		AuthCtxID string `json:"authCtxId"`
		MSK       []byte `json:"msk"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)

		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/nnssaaaf-aiw/v1/msk", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		err := json.NewDecoder(r.Body).Decode(&receivedBody)
		assert.NoError(t, err)
		assert.Equal(t, "auth-123", receivedBody.AuthCtxID)
		assert.Equal(t, []byte("test-msk-data"), receivedBody.MSK)

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	factory := nfclient.NewFactory(nil)
	client := NewClient(config.AUSFConfig{BaseURL: server.URL}, factory)
	err := client.ForwardMSK(context.Background(), "auth-123", []byte("test-msk-data"))

	assert.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load())
}

func TestForwardMSK_Error_Non2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	factory := nfclient.NewFactory(nil)
	client := NewClient(config.AUSFConfig{BaseURL: server.URL}, factory)
	err := client.ForwardMSK(context.Background(), "auth-123", []byte("test-msk"))

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestForwardMSK_Error_NotConfigured(t *testing.T) {
	factory := nfclient.NewFactory(nil)
	client := NewClient(config.AUSFConfig{}, factory)
	err := client.ForwardMSK(context.Background(), "auth-123", []byte("test-msk"))

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "baseURL not configured")
}

func TestForwardMSK_Error_ConnectionRefused(t *testing.T) {
	factory := nfclient.NewFactory(nil)
	client := NewClient(config.AUSFConfig{BaseURL: "http://localhost:19999"}, factory)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := client.ForwardMSK(ctx, "auth-123", []byte("test-msk"))

	assert.Error(t, err)
}

func TestForwardMSK_ServerReturns404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	factory := nfclient.NewFactory(nil)
	client := NewClient(config.AUSFConfig{BaseURL: server.URL}, factory)
	err := client.ForwardMSK(context.Background(), "auth-nonexistent", []byte("test-msk"))

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestForwardMSK_MultipleCalls(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	factory := nfclient.NewFactory(nil)
	client := NewClient(config.AUSFConfig{BaseURL: server.URL}, factory)

	for i := 0; i < 5; i++ {
		err := client.ForwardMSK(context.Background(), fmt.Sprintf("auth-%d", i), []byte("msk-data"))
		assert.NoError(t, err)
	}

	assert.Equal(t, int32(5), callCount.Load())
}

func TestAUSFClient_OpensBreakerOnRepeatedFailures(t *testing.T) {
	// Server that always returns 503 Service Unavailable.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := config.AUSFConfig{BaseURL: server.URL}
	// Use a real registry with low thresholds so we can trip the breaker quickly.
	cbRegistry := resilience.NewRegistry(3, 10*time.Second, 2)
	factory := nfclient.NewFactory(cbRegistry)
	client := NewClient(cfg, factory)

	ctx := context.Background()

	// First three calls should all fail and trip the breaker.
	for i := 0; i < 3; i++ {
		err := client.ForwardMSK(ctx, "auth-123", []byte("test-msk"))
		assert.Error(t, err, "call %d should fail", i+1)
	}

	// The breaker should now be OPEN — verify its state.
	cb := cbRegistry.Get(server.URL)
	assert.Equal(t, resilience.StateOpen, cb.State(), "breaker should be OPEN after 3 failures")

	// Subsequent calls should fast-fail without hitting the server.
	fastFailCount := 0
	for i := 0; i < 5; i++ {
		err := client.ForwardMSK(ctx, "auth-123", []byte("test-msk"))
		if err != nil {
			fastFailCount++
			assert.Contains(t, err.Error(), "circuit breaker open",
				"call %d should fail fast due to open breaker", i+1)
		}
	}
	assert.Greater(t, fastFailCount, 0, "at least one call should have fast-failed")

	// Verify the breaker is still open.
	assert.Equal(t, resilience.StateOpen, cb.State(), "breaker should still be OPEN")

	// Verify via the factory testability method too.
	assert.Equal(t, resilience.StateOpen, factory.BreakerState(server.URL),
		"factory.BreakerState should reflect OPEN breaker")
}

func TestAUSFClient_BreakerHalfOpenAfterRecoveryTimeout(t *testing.T) {
	// Server that always returns 503 Service Unavailable.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := config.AUSFConfig{BaseURL: server.URL}
	// Very short recovery timeout so we can test the transition in unit test time.
	cbRegistry := resilience.NewRegistry(2, 50*time.Millisecond, 1)
	factory := nfclient.NewFactory(cbRegistry)
	client := NewClient(cfg, factory)

	ctx := context.Background()

	// Trip the breaker: 2 failures open it.
	for i := 0; i < 2; i++ {
		_ = client.ForwardMSK(ctx, "auth-123", []byte("test-msk"))
	}

	cb := cbRegistry.Get(server.URL)
	assert.Equal(t, resilience.StateOpen, cb.State(), "breaker should be OPEN")

	// Wait for recovery timeout to expire — breaker transitions to HALF_OPEN on next Allow().
	time.Sleep(70 * time.Millisecond)

	// The next ForwardMSK call should trigger Allow() which transitions to HALF_OPEN.
	// The call will still fail (server is still down), which should reopen the breaker.
	_ = client.ForwardMSK(ctx, "auth-123", []byte("test-msk"))

	// The breaker should have gone OPEN → HALF_OPEN → OPEN again.
	assert.Equal(t, resilience.StateOpen, cb.State(), "breaker should be OPEN after failed half-open probe")
}
