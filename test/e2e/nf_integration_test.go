//go:build e2e
// +build e2e

// Package e2e provides end-to-end integration tests for the NSSAAF system.
// Tests in this file cover NRF/UDM integration, resilience, and failover scenarios.
package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"

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
// CircuitBreaker is wrapped around the AAA client in NSSAA Confirm (Phase 2),
// and around Redis in the AIW handler. In Phase 1, the AAA client is not
// called during session creation — the handler returns a Phase-1 stub response.
// Consequently, repeated requests do not trip the circuit breaker.
//
// Skipped until Phase 2 implements:
// - Real AAA client invocation in NSSAA Confirm → circuit trips on repeated failures
// - Circuit breaker check in AIW handler → returns 503 on open circuit
//
// Covered by integration tests in internal/resilience/ when Phase 2 lands.
func TestE2E_Resilience_CircuitBreaker(t *testing.T) {
	t.Skip("Circuit breaker requires Phase 2 AAA integration — not implemented in Phase 1 stub")
}

// TestE2E_Resilience_RedisDown verifies system handles Redis unavailability.
// Spec: internal resilience design
//
// ResetState() flushes Redis via FLUSHDB, simulating Redis being down.
// However, in Phase 1 the AIW handler uses an in-memory store backed by
// PostgreSQL — it does not call Redis at all. Therefore, Redis unavailability
// does not cause a 503 response in Phase 1.
//
// Skipped until Phase 2 implements:
// - Redis-backed cache for AIW sessions (e.g., NssaaStatus, authResult lookup)
// - Redis health check in AIW handler → returns 503 when Redis is unavailable
//
// Covered by integration tests in internal/cache/ when Phase 2 lands.
func TestE2E_Resilience_RedisDown(t *testing.T) {
	t.Skip("Redis availability check requires Phase 2 cache integration — not implemented in Phase 1 stub")
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
