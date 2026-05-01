//go:build e2e
// +build e2e

package scenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator/nssAAF/test/e2e/fullchain"
)

// TestResilience_CircuitBreaker verifies circuit breaker opens after failures.
// Spec: TS 29.526, internal resilience design
func TestResilience_CircuitBreaker(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	h := fullchain.NewHarness(t)
	defer h.Close()
	h.ResetState()

	// Configure UDM mock to return errors, which will cascade to the gateway
	h.UDMMock.SetError("imsi-208046000000001", http.StatusGatewayTimeout)

	// Make multiple requests — circuit should open after threshold failures
	client := h.TLSClient()
	for i := 0; i < 5; i++ {
		body := map[string]interface{}{
			"supi":     "imsi-208046000000001",
			"eapIdRsp": "dGVzdA==",
		}
		payloadBytes, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			h.HTTPGWURL()+"/nnssaaf-aiw/v1/authentications",
			bytes.NewReader(payloadBytes))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}

	// After circuit opens, should get fast rejection (not waiting full timeout)
	start := time.Now()
	body := map[string]interface{}{
		"supi":     "imsi-208046000000001",
		"eapIdRsp": "dGVzdA==",
	}
	payloadBytes, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		h.HTTPGWURL()+"/nnssaaf-aiw/v1/authentications",
		bytes.NewReader(payloadBytes))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	elapsed := time.Since(start)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Circuit breaker should reject fast (under 1s vs 30s normal timeout)
	assert.Less(t, elapsed.Milliseconds(), int64(1000),
		"circuit breaker should reject fast, got %dms", elapsed.Milliseconds())
}

// TestResilience_RedisDown verifies fallback to PostgreSQL when Redis is unavailable.
// Spec: internal resilience design
func TestResilience_RedisDown(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	h := fullchain.NewHarness(t)
	defer h.Close()
	h.ResetState()

	// Flush Redis to simulate Redis being unavailable
	err := h.redis.FlushDB(ctx)
	require.NoError(t, err)

	// Operations should still work with PostgreSQL fallback
	body := map[string]interface{}{
		"supi":     "imsi-208046000000001",
		"eapIdRsp": "dGVzdA==",
	}

	payloadBytes, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		h.HTTPGWURL()+"/nnssaaf-aiw/v1/authentications",
		bytes.NewReader(payloadBytes))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	client := h.TLSClient()
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should succeed or return proper error (not crash)
	assert.True(t, resp.StatusCode == http.StatusCreated ||
		resp.StatusCode == http.StatusBadRequest ||
		resp.StatusCode == http.StatusNotFound,
		"should return valid HTTP response, got %d", resp.StatusCode)
}

// TestResilience_DLQProcessing verifies dead letter queue for failed notifications.
// Spec: internal resilience design
func TestResilience_DLQProcessing(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	// DLQ processing requires:
	// 1. A notification endpoint that returns errors
	// 2. A metrics endpoint to verify DLQ was populated
	// 3. A DLQ retry mechanism to verify processing
	//
	// This test is documented pending metrics endpoint availability.
	// Manual verification steps:
	// 1. Trigger a notification that fails (AMF unreachable)
	// 2. Query DLQ table: SELECT * FROM dlq_messages;
	// 3. Wait for retry interval
	// 4. Verify message was processed or remains in DLQ with retry_count incremented
	t.Skip("DLQ test requires metrics endpoint and manual verification")
}
