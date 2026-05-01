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

// TestN58_HappyPath verifies complete NSSAA flow with UDM/NRF integration.
// Spec: TS 23.502 §4.2.9, TS 29.526 §7.2
func TestN58_HappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	h := fullchain.NewHarness(t)
	defer h.Close()
	h.ResetState()

	body := map[string]interface{}{
		"gpsi":     "520804600000001",
		"snssai":   map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapIdRsp": "dGVzdA==",
	}

	payloadBytes, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		h.HTTPGWURL()+"/nnssaaf-nssaa/v1/slice-authentications",
		bytes.NewReader(payloadBytes))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "n58-happy-"+t.Name())

	client := h.TLSClient()
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode, "NSSAA happy path should return 201")

	var authResp map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&authResp)
	require.NoError(t, err)
	authCtxID, ok := authResp["authCtxId"].(string)
	require.True(t, ok, "authCtxId must be present in response body")
	assert.NotEmpty(t, authCtxID)
}

// TestN58_InvalidGPSI verifies 400 for invalid GPSI format.
// Spec: TS 29.571 §5.2.2, TS 29.526 §7.2.2
func TestN58_InvalidGPSI(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	h := fullchain.NewHarness(t)
	defer h.Close()
	h.ResetState()

	body := map[string]interface{}{
		"gpsi":     "invalid",
		"snssai":   map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapIdRsp": "dGVzdA==",
	}

	payloadBytes, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		h.HTTPGWURL()+"/nnssaaf-nssaa/v1/slice-authentications",
		bytes.NewReader(payloadBytes))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	client := h.TLSClient()
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var problem map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&problem)
	require.NoError(t, err)
	assert.Equal(t, "INVALID_GPSI_FORMAT", problem["cause"])
}

// TestN58_InvalidSnssai verifies 400 for invalid Snssai.
// Spec: TS 29.571, TS 29.526 §7.2.2
func TestN58_InvalidSnssai(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	h := fullchain.NewHarness(t)
	defer h.Close()
	h.ResetState()

	body := map[string]interface{}{
		"gpsi":     "520804600000001",
		"snssai":   map[string]interface{}{"sst": 999, "sd": "000001"},
		"eapIdRsp": "dGVzdA==",
	}

	payloadBytes, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		h.HTTPGWURL()+"/nnssaaf-nssaa/v1/slice-authentications",
		bytes.NewReader(payloadBytes))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	client := h.TLSClient()
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
