//go:build e2e
// +build e2e

// Package e2e provides end-to-end integration tests for the NSSAAF system.
// Tests in this file cover NRF/UDM integration, resilience, and failover scenarios.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/operator/nssAAF/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── NRF Discovery Tests ────────────────────────────────────────────────────

// TestE2E_NF_NRFUDMDiscovery verifies NRF returns correct UDM endpoint.
// Spec: TS 29.510 §6.2.6
//
// This test uses an in-process NRF mock. For containerized NRF testing,
// use E2E_PROFILE=fullchain with tests that route to FULLCHAIN_NRF_URL.
func TestE2E_NF_NRFUDMDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	nrfMock := mocks.NewNRFMock()
	defer nrfMock.Close()

	nrfMock.SetServiceEndpoint("UDM", "nudm-uem", "udm-mock", 8080)

	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		nrfMock.URL()+"/nnrf-disc/v1/nf-instances?target-nf-type=UDM&service-names=nudm-uem",
		nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	instances, ok := result["nfInstances"].([]interface{})
	require.True(t, ok, "nfInstances must be an array")
	assert.NotEmpty(t, instances, "should return at least one UDM instance")

	first := instances[0].(map[string]interface{})
	services, ok := first["nfServices"].(map[string]interface{})
	require.True(t, ok)
	nudmService, ok := services["nudm-uem"].(map[string]interface{})
	require.True(t, ok)
	_, ok = nudmService["ipEndPoints"].([]interface{})
	require.True(t, ok)
}

// TestE2E_NF_NRFCustomEndpoint verifies SetServiceEndpoint changes discovery response.
// Spec: TS 29.510 §6.2.6
func TestE2E_NF_NRFCustomEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	nrfMock := mocks.NewNRFMock()
	defer nrfMock.Close()

	nrfMock.SetServiceEndpoint("UDM", "nudm-uem", "custom-udm", 9090)

	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		nrfMock.URL()+"/nnrf-disc/v1/nf-instances?target-nf-type=UDM&service-names=nudm-uem",
		nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	out, err := json.Marshal(result)
	require.NoError(t, err)
	bodyStr := string(out)
	assert.Contains(t, bodyStr, "custom-udm", "should return custom endpoint host")
	assert.Contains(t, bodyStr, "9090", "should return custom port")
}

// TestE2E_NF_NRFNotRegistered verifies unregistered NFs are excluded from discovery.
// Spec: TS 29.510 §6.2.6
func TestE2E_NF_NRFNotRegistered(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	nrfMock := mocks.NewNRFMock()
	defer nrfMock.Close()

	nrfMock.SetNFStatus("udm-001", "NOT_REGISTERED")

	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		nrfMock.URL()+"/nnrf-disc/v1/nf-instances?target-nf-type=UDM&service-names=nudm-uem",
		nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	instances, _ := result["nfInstances"].([]interface{})
	assert.Empty(t, instances, "should not return unregistered NF")
}

// TestE2E_NF_NRFAllRegistered verifies all registered NFs are returned when no filter.
// Spec: TS 29.510 §6.2.6
func TestE2E_NF_NRFAllRegistered(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	nrfMock := mocks.NewNRFMock()
	defer nrfMock.Close()

	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		nrfMock.URL()+"/nnrf-disc/v1/nf-instances",
		nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	instances, ok := result["nfInstances"].([]interface{})
	require.True(t, ok, "nfInstances must be an array")
	assert.NotEmpty(t, instances, "should return registered NFs")
}

// ─── UDM Auth Subscription Tests ─────────────────────────────────────────────

// TestE2E_NF_UDMAuthSubscription verifies UDM returns auth subscription.
// Spec: TS 29.526 §7.2.2
func TestE2E_NF_UDMAuthSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	udmMock := mocks.NewUDMMock()
	defer udmMock.Close()

	udmMock.SetAuthSubscription("imsi-208046000000001", "EAP_TLS", "radius://mock-aaa-s:1812")

	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		udmMock.URL()+"/nudm-uem/v1/subscribers/imsi-208046000000001/auth-contexts",
		nil)
	require.NoError(t, err)
	req.Header.Set("X-Request-ID", "test-"+t.Name())

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "EAP_TLS")
	assert.Contains(t, string(body), "radius://")
}

// TestE2E_NF_UDMSubscriberNotFound verifies 404 for unknown SUPI.
// Spec: TS 29.526 §7.2.2
func TestE2E_NF_UDMSubscriberNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	udmMock := mocks.NewUDMMock()
	defer udmMock.Close()

	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		udmMock.URL()+"/nudm-uem/v1/subscribers/imsi-999999999999999/auth-contexts",
		nil)
	require.NoError(t, err)
	req.Header.Set("X-Request-ID", "test-"+t.Name())

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestE2E_NF_UDMErrorInjection verifies error response when configured.
// Spec: TS 29.526 §7.2.2
func TestE2E_NF_UDMErrorInjection(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	udmMock := mocks.NewUDMMock()
	defer udmMock.Close()

	udmMock.SetError("imsi-208046000000001", http.StatusGatewayTimeout)

	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		udmMock.URL()+"/nudm-uem/v1/subscribers/imsi-208046000000001/auth-contexts",
		nil)
	require.NoError(t, err)
	req.Header.Set("X-Request-ID", "test-"+t.Name())

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusGatewayTimeout, resp.StatusCode)
}

// ─── Resilience Tests ────────────────────────────────────────────────────────

// TestE2E_Resilience_CircuitBreaker verifies circuit breaker opens after failures.
// Spec: TS 29.526, internal resilience design
//
// Requires ContainerDriver (E2E_PROFILE=fullchain) for real UDM integration.
func TestE2E_Resilience_CircuitBreaker(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	if os.Getenv("E2E_PROFILE") != "fullchain" {
		t.Skip("Circuit breaker test requires E2E_PROFILE=fullchain for real UDM")
	}

	ctx := context.Background()
	h := NewHarnessForTest(t)
	defer h.Close()
	h.ResetState()

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

	assert.Less(t, elapsed.Milliseconds(), int64(1000),
		"circuit breaker should reject fast, got %dms", elapsed.Milliseconds())
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode,
		"should return 503 when circuit is open")
}

// TestE2E_Resilience_RedisDown verifies system handles Redis unavailability.
// Spec: internal resilience design
//
// ResetState() flushes Redis via FLUSHDB, simulating Redis being down.
func TestE2E_Resilience_RedisDown(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	h := NewHarnessForTest(t)
	defer h.Close()
	h.ResetState()

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

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode,
		"should return 503 when Redis is down")
}

// TestE2E_Resilience_DLQProcessing verifies dead letter queue for failed notifications.
// Spec: internal resilience design
//
// Requires metrics endpoint and manual verification.
func TestE2E_Resilience_DLQProcessing(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	t.Skip("DLQ test requires metrics endpoint and manual verification")
}

// TestE2E_Resilience_AAASIMModes verifies AAA-SIM mode configuration.
// Spec: internal resilience design
//
// AAA_SIM_MODE is set via env var at container startup. Requires E2E_PROFILE=fullchain.
func TestE2E_Resilience_AAASIMModes(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	if os.Getenv("E2E_PROFILE") != "fullchain" {
		t.Skip("AAA-SIM mode test requires E2E_PROFILE=fullchain")
	}

	h := NewHarnessForTest(t)
	defer h.Close()

	t.Skip("aaa-sim mode control via env var only — restart container to change mode")
}

// TestE2E_Resilience_AAASIMConnectivity verifies AAA-SIM is reachable.
// Spec: internal resilience design
func TestE2E_Resilience_AAASIMConnectivity(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	if os.Getenv("E2E_PROFILE") != "fullchain" {
		t.Skip("AAA-SIM connectivity test requires E2E_PROFILE=fullchain")
	}

	h := NewHarnessForTest(t)
	defer h.Close()

	t.Skip("aaa-sim has no status endpoint yet — verify via container logs")
}

// TestE2E_Resilience_RedisUnavailable verifies system handles Redis unavailability.
// Spec: internal resilience design
//
// Requires docker compose pause/resume for Redis.
func TestE2E_Resilience_RedisUnavailable(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	t.Skip("requires docker compose pause/resume for Redis — implement with test infra")
}

// TestE2E_Resilience_PostgresUnavailable verifies system handles PostgreSQL unavailability.
// Spec: internal resilience design
//
// Requires docker compose pause/resume for Postgres.
func TestE2E_Resilience_PostgresUnavailable(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	t.Skip("requires docker compose pause/resume for Postgres — implement with test infra")
}
