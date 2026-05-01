//go:build e2e
// +build e2e

package scenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/operator/nssAAF/test/e2e/fullchain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestN60_HappyPath verifies complete AIW flow with UDM/NRF integration.
// Spec: TS 29.526 §7.3, TS 23.502 §4.2.9
func TestN60_HappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	h := fullchain.NewHarness(t)
	defer h.Close()
	h.ResetState()

	// Set auth subscription for SUPI
	h.UDMMock.SetAuthSubscription("imsi-208046000000001", "EAP_TLS", "radius://mock-aaa-s:1812")

	// 1. Create authentication context via HTTP GW (N60 API).
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
	req.Header.Set("X-Request-ID", "n60-happy-"+t.Name())

	client := h.TLSClient()
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode, "AIW happy path should return 201")

	var authResp map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&authResp)
	require.NoError(t, err)
	authCtxID, ok := authResp["authCtxId"].(string)
	require.True(t, ok, "authCtxId must be present in response body")
	assert.NotEmpty(t, authCtxID)
}

// TestN60_InvalidSupi verifies 400 for invalid SUPI format.
// Spec: TS 29.571 §5.4.4.61, TS 29.526 §7.3
func TestN60_InvalidSupi(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	h := fullchain.NewHarness(t)
	defer h.Close()
	h.ResetState()

	body := map[string]interface{}{
		"supi":     "imsi-12345", // Only 5 digits, should be 5-15
		"eapIdRsp": "dGVzdA==",
	}

	payloadBytes, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		h.HTTPGWURL()+"/nnssaaf-aiw/v1/authentications",
		bytes.NewReader(payloadBytes))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "n60-invalid-supi-"+t.Name())

	client := h.TLSClient()
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var problem map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&problem)
	require.NoError(t, err)
	assert.Equal(t, "INVALID_SUPI_FORMAT", problem["cause"])
}

// TestN60_SUPINotFound verifies 404 when SUPI not in UDM.
// Spec: TS 29.526 §7.3
func TestN60_SUPINotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	h := fullchain.NewHarness(t)
	defer h.Close()
	h.ResetState()

	// Do NOT set auth subscription - UDM will return 404

	body := map[string]interface{}{
		"supi":     "imsi-999999999999999",
		"eapIdRsp": "dGVzdA==",
	}

	payloadBytes, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		h.HTTPGWURL()+"/nnssaaf-aiw/v1/authentications",
		bytes.NewReader(payloadBytes))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "n60-notfound-"+t.Name())

	client := h.TLSClient()
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
