package udm

import (
	"context"
	"encoding/json"
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

func TestNewClient(t *testing.T) {
	cfg := config.UDMConfig{
		BaseURL: "http://udm.operator.com:8080",
	}
	factory := nfclient.NewFactory(nil)

	client := NewClient(cfg, factory, nil)

	assert.NotNil(t, client)
	assert.Equal(t, "http://udm.operator.com:8080", client.baseURL)
}

func TestGetAuthContext_Success(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		assert.Equal(t, "/nudm-uem/v1/subscribers/imsi-208001000000000/auth-contexts", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		resp := map[string]interface{}{
			"authContexts": []map[string]string{
				{"authType": "EAP_TLS", "aaaServer": "radius://aaa.operator.com:1812"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)

	cfg := config.UDMConfig{BaseURL: server.URL}
	factory := nfclient.NewFactory(nil)
	client := NewClient(cfg, factory, nil)

	ctx := context.Background()
	result, err := client.GetAuthContext(ctx, "imsi-208001000000000")

	assert.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load())
	sub, ok := result.(*AuthSubscription)
	assert.True(t, ok)
	assert.Equal(t, "EAP_TLS", sub.AuthType)
	assert.Equal(t, "radius://aaa.operator.com:1812", sub.AAAServer)
}

func TestGetAuthContext_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	cfg := config.UDMConfig{BaseURL: server.URL}
	factory := nfclient.NewFactory(nil)
	client := NewClient(cfg, factory, nil)

	ctx := context.Background()
	result, err := client.GetAuthContext(ctx, "imsi-999999999999999")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetAuthContext_UnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	cfg := config.UDMConfig{BaseURL: server.URL}
	factory := nfclient.NewFactory(nil)
	client := NewClient(cfg, factory, nil)

	ctx := context.Background()
	result, err := client.GetAuthContext(ctx, "imsi-208001000000000")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unexpected status 500")
}

func TestGetAuthContext_EmptyAuthContexts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"authContexts": []map[string]string{},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)

	cfg := config.UDMConfig{BaseURL: server.URL}
	factory := nfclient.NewFactory(nil)
	client := NewClient(cfg, factory, nil)

	ctx := context.Background()
	result, err := client.GetAuthContext(ctx, "imsi-208001000000000")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no auth contexts found")
}

func TestGetAuthContext_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{invalid json`))
	}))
	t.Cleanup(server.Close)

	cfg := config.UDMConfig{BaseURL: server.URL}
	factory := nfclient.NewFactory(nil)
	client := NewClient(cfg, factory, nil)

	ctx := context.Background()
	result, err := client.GetAuthContext(ctx, "imsi-208001000000000")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "decode response")
}

func TestGetAuthContext_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	cfg := config.UDMConfig{BaseURL: server.URL}
	factory := nfclient.NewFactory(nil)
	client := NewClient(cfg, factory, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := client.GetAuthContext(ctx, "imsi-208001000000000")

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestGetAuthContext_NoBaseURL(t *testing.T) {
	cfg := config.UDMConfig{BaseURL: ""}
	factory := nfclient.NewFactory(nil)
	client := NewClient(cfg, factory, nil)

	ctx := context.Background()
	result, err := client.GetAuthContext(ctx, "imsi-208001000000000")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no baseURL and no NRF client configured")
}

func TestUpdateAuthContext_Success(t *testing.T) {
	var callCount atomic.Int32
	var reqBody atomic.Value

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		assert.Equal(t, "/nudm-uem/v1/subscribers/imsi-208001000000000/auth-contexts/auth-123", r.URL.Path)
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			reqBody.Store(body)
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	cfg := config.UDMConfig{BaseURL: server.URL}
	factory := nfclient.NewFactory(nil)
	client := NewClient(cfg, factory, nil)

	ctx := context.Background()
	err := client.UpdateAuthContext(ctx, "imsi-208001000000000", "auth-123", "EAP_SUCCESS")

	assert.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load())

	body, ok := reqBody.Load().(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "EAP_SUCCESS", body["authResult"])
}

func TestUpdateAuthContext_UnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)

	cfg := config.UDMConfig{BaseURL: server.URL}
	factory := nfclient.NewFactory(nil)
	client := NewClient(cfg, factory, nil)

	ctx := context.Background()
	err := client.UpdateAuthContext(ctx, "imsi-208001000000000", "auth-456", "EAP_FAILURE")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "update status 400")
}

func TestUpdateAuthContext_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	cfg := config.UDMConfig{BaseURL: server.URL}
	factory := nfclient.NewFactory(nil)
	client := NewClient(cfg, factory, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := client.UpdateAuthContext(ctx, "imsi-208001000000000", "auth-789", "EAP_SUCCESS")

	assert.Error(t, err)
}

func TestUpdateAuthContext_NoBaseURL(t *testing.T) {
	cfg := config.UDMConfig{BaseURL: ""}
	factory := nfclient.NewFactory(nil)
	client := NewClient(cfg, factory, nil)

	ctx := context.Background()
	err := client.UpdateAuthContext(ctx, "imsi-208001000000000", "auth-xyz", "EAP_SUCCESS")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no baseURL and no NRF client configured")
}

func TestExtractPLMNFromSupi(t *testing.T) {
	tests := []struct {
		supi     string
		expected string
	}{
		{"imsi-208001000000000", "208001"},
		{"imsi-440010123456789", "440010"},
		{"imsi-310410999999999", "310410"},
		{"imsi-12345", "208001"},  // too short → default
		{"", "208001"},            // empty → default
		{"imsi-208", "208001"},    // just enough for "imsi-" + MCC = 7 chars → "208001"
		{"imsi-208001", "208001"}, // "imsi-"(4) + "208001"(6) = 10 → matches
	}

	for _, tt := range tests {
		t.Run(tt.supi, func(t *testing.T) {
			result := extractPLMNFromSupi(tt.supi)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAuthSubscription_JSON(t *testing.T) {
	sub := AuthSubscription{
		AuthType:  "EAP_AKA_PRIME",
		AAAServer: "radius://aaa.operator.com:1813",
	}

	data, err := json.Marshal(sub)
	assert.NoError(t, err)

	var decoded AuthSubscription
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, sub.AuthType, decoded.AuthType)
	assert.Equal(t, sub.AAAServer, decoded.AAAServer)
}

// Verify interface compatibility with nssaa.WithUDMClient.
// nssaa.WithUDMClient requires GetAuthContext(context.Context, string) (interface{}, error).
var _ interface {
	GetAuthContext(context.Context, string) (interface{}, error)
} = (*Client)(nil)

// Verify interface compatibility for UpdateAuthContext.
var _ interface {
	UpdateAuthContext(context.Context, string, string, string) error
} = (*Client)(nil)

func TestUDMClient_OpensBreakerOnRepeatedFailures(t *testing.T) {
	// Server that always returns 503 Service Unavailable.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := config.UDMConfig{BaseURL: server.URL}
	// Use a real registry with low thresholds so we can trip the breaker quickly.
	cbRegistry := resilience.NewRegistry(3, 10*time.Second, 2)
	factory := nfclient.NewFactory(cbRegistry)
	client := NewClient(cfg, factory, nil)

	ctx := context.Background()
	supi := "imsi-208001000000000"

	// First three calls should all fail and trip the breaker.
	for i := 0; i < 3; i++ {
		_, err := client.GetAuthContext(ctx, supi)
		assert.Error(t, err, "call %d should fail", i+1)
	}

	// The breaker should now be OPEN — verify its state.
	cb := cbRegistry.Get(server.URL)
	assert.Equal(t, resilience.StateOpen, cb.State(), "breaker should be OPEN after 3 failures")

	// Subsequent calls should fast-fail without hitting the server.
	fastFailCount := 0
	for i := 0; i < 5; i++ {
		_, err := client.GetAuthContext(ctx, supi)
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

func TestUDMClient_UpdateAuthContext_OpensBreakerOnRepeatedFailures(t *testing.T) {
	// Server that always returns 503 Service Unavailable.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := config.UDMConfig{BaseURL: server.URL}
	cbRegistry := resilience.NewRegistry(2, 10*time.Second, 1)
	factory := nfclient.NewFactory(cbRegistry)
	client := NewClient(cfg, factory, nil)

	ctx := context.Background()
	supi := "imsi-208001000000000"
	authCtxID := "auth-123"

	// Trip the breaker with UpdateAuthContext calls.
	for i := 0; i < 2; i++ {
		err := client.UpdateAuthContext(ctx, supi, authCtxID, "EAP_SUCCESS")
		assert.Error(t, err, "call %d should fail", i+1)
	}

	cb := cbRegistry.Get(server.URL)
	assert.Equal(t, resilience.StateOpen, cb.State(), "breaker should be OPEN after 2 failures")

	// Subsequent calls should fast-fail.
	err := client.UpdateAuthContext(ctx, supi, authCtxID, "EAP_SUCCESS")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker open",
		"subsequent call should fail fast due to open breaker")
}

func TestUDMClient_BreakerHalfOpenAfterRecoveryTimeout(t *testing.T) {
	// Server that always returns 503 Service Unavailable.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := config.UDMConfig{BaseURL: server.URL}
	// Very short recovery timeout so we can test the transition in unit test time.
	cbRegistry := resilience.NewRegistry(2, 50*time.Millisecond, 1)
	factory := nfclient.NewFactory(cbRegistry)
	client := NewClient(cfg, factory, nil)

	ctx := context.Background()
	supi := "imsi-208001000000000"

	// Trip the breaker: 2 failures open it.
	for i := 0; i < 2; i++ {
		_, _ = client.GetAuthContext(ctx, supi)
	}

	cb := cbRegistry.Get(server.URL)
	assert.Equal(t, resilience.StateOpen, cb.State(), "breaker should be OPEN")

	// Wait for recovery timeout to expire — breaker transitions to HALF_OPEN on next Allow().
	time.Sleep(70 * time.Millisecond)

	// The next GetAuthContext call should trigger Allow() which transitions to HALF_OPEN.
	// The call will still fail (server is still down), which should reopen the breaker.
	_, _ = client.GetAuthContext(ctx, supi)

	// The breaker should have gone OPEN → HALF_OPEN → OPEN again.
	assert.Equal(t, resilience.StateOpen, cb.State(), "breaker should be OPEN after failed half-open probe")
}
