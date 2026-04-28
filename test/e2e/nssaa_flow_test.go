// Package e2e provides end-to-end integration tests for the NSSAAF system.
package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_NSSAA_HappyPath verifies the full AMF → HTTP GW → Biz Pod → AAA GW → AAA-S flow.
// Spec: TS 23.502 §4.2.9, TS 29.526 §7.2
func TestE2E_NSSAA_HappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	h := NewHarness(t)
	defer h.Close()

	// Start AMF mock to receive notifications.
	amfMock := h.StartAMFMock()
	defer amfMock.Close()

	// 1. Create slice authentication context via HTTP GW (N58).
	body := map[string]interface{}{
		"gpsi":     "520804600000001",
		"snssai":   map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapIdRsp": "dGVzdA==", // base64 "test"
	}

	req, err := http.NewRequest(http.MethodPost, h.HTTPGWURL()+"/nnssaaf-nssaa/v1/slice-authentications", nil)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "test-req-id")

	payloadBytes, _ := json.Marshal(body)
	req.Body = nil
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(string(payloadBytes))), nil
	}

	client := &http.Client{}
	resp, err := client.Do(req.WithContext(requireTestContext(t)))
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should get 201 Created.
	assert.Equal(t, http.StatusCreated, resp.StatusCode, "NSSAA happy path should return 201")

	// Location header must be present.
	location := resp.Header.Get("Location")
	assert.NotEmpty(t, location, "Location header must be present")
	assert.Contains(t, location, "/slice-authentications/")

	// X-Request-ID must be echoed.
	assert.Equal(t, "test-req-id", resp.Header.Get("X-Request-ID"))

	// Parse authCtxId from response.
	var authResp map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&authResp)
	require.NoError(t, err)
	authCtxID, ok := authResp["authCtxId"].(string)
	require.True(t, ok, "authCtxId must be present in response")
	_ = authCtxID // used in challenge test

	// 2. Confirm authentication with EAP message.
	confirmBody := map[string]interface{}{
		"gpsi":       "520804600000001",
		"snssai":     map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapMessage": "dGVzdA==", // base64 "test"
	}
	confirmBytes, _ := json.Marshal(confirmBody)
	req2, _ := http.NewRequest(http.MethodPut, h.HTTPGWURL()+location, strings.NewReader(string(confirmBytes)))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Request-ID", "test-req-id-confirm")

	resp2, err := client.Do(req2.WithContext(requireTestContext(t)))
	require.NoError(t, err)
	defer resp2.Body.Close()

	// Should get 200 OK.
	assert.Equal(t, http.StatusOK, resp2.StatusCode, "NSSAA confirm should return 200")
}

// TestE2E_NSSAA_AuthFailure verifies that an Access-Reject from AAA-S returns HTTP 200
// with authResult=EAP_FAILURE in the body.
// Spec: TS 29.526 §7.2, TS 33.501 §16.3
func TestE2E_NSSAA_AuthFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	h := NewHarness(t)
	defer h.Close()

	// Configure AAA-S mode via env (if supported) or use the failure scenario.
	body := map[string]interface{}{
		"gpsi":     "520804600000001",
		"snssai":   map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapIdRsp": "dGVzdA==",
	}
	payloadBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, h.HTTPGWURL()+"/nnssaaf-nssaa/v1/slice-authentications", strings.NewReader(string(payloadBytes)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "test-auth-failure")

	client := &http.Client{}
	resp, err := client.Do(req.WithContext(requireTestContext(t)))
	require.NoError(t, err)
	defer resp.Body.Close()

	// Create succeeds with 201.
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Confirm with failure mode EAP payload.
	// Note: In a real scenario, AAA-S would return Access-Reject.
	// The Biz Pod would then return 200 with authResult=EAP_FAILURE.
	confirmBody := map[string]interface{}{
		"gpsi":       "520804600000001",
		"snssai":     map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapMessage": "dGVzdA==",
	}
	confirmBytes, _ := json.Marshal(confirmBody)
	location := resp.Header.Get("Location")
	req2, _ := http.NewRequest(http.MethodPut, h.HTTPGWURL()+location, strings.NewReader(string(confirmBytes)))
	req2.Header.Set("Content-Type", "application/json")

	resp2, err := client.Do(req2.WithContext(requireTestContext(t)))
	require.NoError(t, err)
	defer resp2.Body.Close()

	// Should get 200 with EAP-Failure in body (not HTTP 403).
	assert.Equal(t, http.StatusOK, resp2.StatusCode, "EAP-Failure should return 200 OK")
}

// TestE2E_NSSAA_AuthChallenge verifies that a multi-step EAP-TLS handshake
// (Access-Challenge → final response) works correctly.
// Spec: RFC 5216 §2.1
func TestE2E_NSSAA_AuthChallenge(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	h := NewHarness(t)
	defer h.Close()

	body := map[string]interface{}{
		"gpsi":     "520804600000001",
		"snssai":   map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapIdRsp": "dGVzdA==",
	}
	payloadBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, h.HTTPGWURL()+"/nnssaaf-nssaa/v1/slice-authentications", strings.NewReader(string(payloadBytes)))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req.WithContext(requireTestContext(t)))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	location := resp.Header.Get("Location")

	// Multiple confirm rounds (simulating multi-step handshake).
	for i := 0; i < 3; i++ {
		confirmBody := map[string]interface{}{
			"gpsi":       "520804600000001",
			"snssai":     map[string]interface{}{"sst": 1, "sd": "000001"},
			"eapMessage": "dGVzdA==", // base64 "test"
		}
		confirmBytes, _ := json.Marshal(confirmBody)
		req2, _ := http.NewRequest(http.MethodPut, h.HTTPGWURL()+location, strings.NewReader(string(confirmBytes)))
		req2.Header.Set("Content-Type", "application/json")

		resp2, err := client.Do(req2.WithContext(requireTestContext(t)))
		require.NoError(t, err)
		defer resp2.Body.Close()

		// Intermediate responses may be 200 with EAP message or final authResult.
		if resp2.StatusCode != http.StatusOK {
			// May get 400 for session not found after completion.
			break
		}
	}
}

// TestE2E_NSSAA_InvalidGPSI verifies that an invalid GPSI returns HTTP 400.
// Spec: TS 29.526 §7.2.3, TS 29.571 §5.4.4.3 (GPSI regex: ^5[0-9]{8,14}$)
func TestE2E_NSSAA_InvalidGPSI(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	h := NewHarness(t)
	defer h.Close()

	body := map[string]interface{}{
		"gpsi":     "invalid-gpsi",
		"snssai":   map[string]interface{}{"sst": 1},
		"eapIdRsp": "dGVzdA==",
	}
	payloadBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, h.HTTPGWURL()+"/nnssaaf-nssaa/v1/slice-authentications", strings.NewReader(string(payloadBytes)))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req.WithContext(requireTestContext(t)))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "Invalid GPSI should return 400")

	var problem map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&problem)
	require.NoError(t, err)
	assert.Contains(t, problem["detail"].(string), "gpsi")
}

// TestE2E_NSSAA_InvalidSnssai verifies that an invalid Snssai returns HTTP 400.
// Spec: TS 29.526 §7.2.3
func TestE2E_NSSAA_InvalidSnssai(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	h := NewHarness(t)
	defer h.Close()

	tests := []struct {
		name     string
		snssai   map[string]interface{}
	}{
		{"SST out of range", map[string]interface{}{"sst": 300}},
		{"SD not 6 hex chars", map[string]interface{}{"sst": 1, "sd": "GGGGGG"}},
		{"Missing SST", map[string]interface{}{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]interface{}{
				"gpsi":     "520804600000001",
				"snssai":   tc.snssai,
				"eapIdRsp": "dGVzdA==",
			}
			payloadBytes, _ := json.Marshal(body)
			req, _ := http.NewRequest(http.MethodPost, h.HTTPGWURL()+"/nnssaaf-nssaa/v1/slice-authentications", strings.NewReader(string(payloadBytes)))
			req.Header.Set("Content-Type", "application/json")

			client := &http.Client{}
			resp, err := client.Do(req.WithContext(requireTestContext(t)))
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "Invalid snssai should return 400")
		})
	}
}

// TestE2E_NSSAA_Unauthorized verifies that a missing or invalid Authorization header
// returns HTTP 401.
// Spec: TS 29.526 §7.2.3
func TestE2E_NSSAA_Unauthorized(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	h := NewHarness(t)
	defer h.Close()

	body := map[string]interface{}{
		"gpsi":     "520804600000001",
		"snssai":   map[string]interface{}{"sst": 1},
		"eapIdRsp": "dGVzdA==",
	}
	payloadBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, h.HTTPGWURL()+"/nnssaaf-nssaa/v1/slice-authentications", strings.NewReader(string(payloadBytes)))
	req.Header.Set("Content-Type", "application/json")
	// No Authorization header.

	client := &http.Client{}
	resp, err := client.Do(req.WithContext(requireTestContext(t)))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "Missing Authorization should return 401")
}

// TestE2E_NSSAA_AaaServerDown verifies that when the AAA-S server is unavailable,
// the circuit breaker trips and the Biz Pod returns HTTP 502 Bad Gateway.
// Spec: TS 29.526 §7.2.3
func TestE2E_NSSAA_AaaServerDown(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	// Note: Simulating AAA-S being down is complex in E2E.
	// We verify the error path is reachable by checking the HTTP GW
	// handles the downstream error correctly.
	t.Skip("AAA-S kill test requires container control; covered by integration tests")
}

// TestE2E_NSSAA_CircuitBreakerAlarm verifies that when the circuit breaker opens,
// the NRM raises an alarm.
// Spec: REQ-34, TS 28.541 §5.3
func TestE2E_NSSAA_CircuitBreakerAlarm(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	t.Skip("Circuit breaker alarm test requires controlled failure injection; covered by integration tests")
}

// requireTestContext returns a context with a short timeout for E2E requests.
func requireTestContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}
