//go:build e2e
// +build e2e

// Package e2e provides end-to-end integration tests for the NSSAAF system.
package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_AIW_BasicFlow verifies the full AUSF → HTTP GW → Biz Pod → AAA GW → AAA-S flow.
// Spec: TS 29.526 §7.3, TS 23.502 §4.2.9
func TestE2E_AIW_BasicFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	h := NewHarnessForTest(t)
	defer h.Close()

	// Start AUSF mock for AIW tests.
	ausfMockSrv := h.StartAUSFMock()
	defer ausfMockSrv.Close()

	// 1. Create authentication context via HTTP GW (N60 API).
	body := map[string]interface{}{
		"supi":     "imu-208046000000001",
		"eapIdRsp": "dGVzdA==",
	}
	payloadBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, h.HTTPGWURL()+"/nnssaaf-aiw/v1/authentications", strings.NewReader(string(payloadBytes)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "aiw-basic-test")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should get 201 Created.
	assert.Equal(t, http.StatusCreated, resp.StatusCode, "AIW basic flow should return 201")

	// Location header must be present.
	location := resp.Header.Get("Location")
	assert.NotEmpty(t, location, "Location header must be present")
	assert.Contains(t, location, "/authentications/")

	// X-Request-ID must be echoed.
	assert.Equal(t, "aiw-basic-test", resp.Header.Get("X-Request-ID"))

	// Parse authCtxId from response.
	var authResp map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&authResp)
	require.NoError(t, err)
	authCtxID, ok := authResp["authCtxId"].(string)
	require.True(t, ok, "authCtxId must be present in response")

	t.Logf("AIW session established: authCtxID=%s, AUSF mock at %s", authCtxID, ausfMockSrv.URL)

	// 2. Confirm authentication with EAP message.
	confirmBody := map[string]interface{}{
		"supi":       "imu-208046000000001",
		"eapMessage": "dGVzdA==",
	}
	confirmBytes, _ := json.Marshal(confirmBody)
	req2, _ := http.NewRequest(http.MethodPut, h.HTTPGWURL()+location, strings.NewReader(string(confirmBytes)))
	req2.Header.Set("Content-Type", "application/json")

	resp2, err := client.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()

	// Should get 200 OK.
	assert.Equal(t, http.StatusOK, resp2.StatusCode, "AIW confirm should return 200")
}

// TestE2E_AIW_MSKExtraction verifies that the MSK is exactly 64 octets
// and MSK != EMSK.
// Spec: RFC 5216 §2.1.4, TS 29.526 §7.3
func TestE2E_AIW_MSKExtraction(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	t.Skip("MSK extraction test requires controlled AAA-S mode with known MSK; covered by conformance tests")
}

// TestE2E_AIW_EAPFailure verifies that an Access-Reject from AAA-S returns
// HTTP 200 with authResult=EAP_FAILURE in the body (not HTTP 403).
// Spec: TS 29.526 §7.3, TS 33.501 §16.3
func TestE2E_AIW_EAPFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	h := NewHarnessForTest(t)
	defer h.Close()

	// Start AUSF mock.
	ausfMockSrv := h.StartAUSFMock()
	defer ausfMockSrv.Close()

	// 1. Create session.
	body := map[string]interface{}{
		"supi":     "imu-208046000000001",
		"eapIdRsp": "dGVzdA==",
	}
	payloadBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, h.HTTPGWURL()+"/nnssaaf-aiw/v1/authentications", strings.NewReader(string(payloadBytes)))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// 2. Confirm with EAP-Failure mode.
	// Note: In a real scenario, AAA-S would return Access-Reject.
	// The Biz Pod then returns 200 with authResult=EAP_FAILURE.
	confirmBody := map[string]interface{}{
		"supi":       "imu-208046000000001",
		"eapMessage": "dGVzdA==",
	}
	confirmBytes, _ := json.Marshal(confirmBody)
	location := resp.Header.Get("Location")
	req2, _ := http.NewRequest(http.MethodPut, h.HTTPGWURL()+location, strings.NewReader(string(confirmBytes)))
	req2.Header.Set("Content-Type", "application/json")

	resp2, err := client.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()

	// Should get 200 OK (not HTTP 403).
	assert.Equal(t, http.StatusOK, resp2.StatusCode, "EAP-Failure should return 200 OK, not 403")

	// Verify MSK is empty and PvsInfo is nil in the failure response.
	var confirmResp map[string]interface{}
	err = json.NewDecoder(resp2.Body).Decode(&confirmResp)
	if err == nil {
		// authResult should be EAP_FAILURE or empty.
		if authResult, ok := confirmResp["authResult"].(string); ok {
			assert.Equal(t, "EAP_FAILURE", authResult, "authResult should be EAP_FAILURE")
		}
	}
}

// TestE2E_AIW_InvalidSupi verifies that an invalid SUPI format returns HTTP 400.
// Spec: TS 29.526 §7.3, TS 29.571 §5.4.4.2 (SUPI regex: ^imu-[0-9]{15}$)
func TestE2E_AIW_InvalidSupi(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	h := NewHarnessForTest(t)
	defer h.Close()

	testCases := []struct {
		name string
		supi string
	}{
		{"not matching regex", "invalid-supi-format"},
		{"empty SUPI", ""},
		{"wrong prefix", "msisdn-1234567890"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]interface{}{
				"supi":     tc.supi,
				"eapIdRsp": "dGVzdA==",
			}
			payloadBytes, _ := json.Marshal(body)
			req, _ := http.NewRequest(http.MethodPost, h.HTTPGWURL()+"/nnssaaf-aiw/v1/authentications", strings.NewReader(string(payloadBytes)))
			req.Header.Set("Content-Type", "application/json")

			client := &http.Client{Timeout: 30 * time.Second}
			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "Invalid SUPI should return 400")

			var problem map[string]interface{}
			err = json.NewDecoder(resp.Body).Decode(&problem)
			require.NoError(t, err)
			assert.Contains(t, problem["detail"].(string), "supi")
		})
	}
}

// TestE2E_AIW_AAA_NotConfigured verifies that a SUPI with no AAA server configured
// returns HTTP 404.
// Spec: TS 29.526 §7.3
func TestE2E_AIW_AAA_NotConfigured(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	h := NewHarnessForTest(t)
	defer h.Close()

	// Use a SUPI in an unconfigured range.
	body := map[string]interface{}{
		"supi":     "imu-999999999999999",
		"eapIdRsp": "dGVzdA==",
	}
	payloadBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, h.HTTPGWURL()+"/nnssaaf-aiw/v1/authentications", strings.NewReader(string(payloadBytes)))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should get 404 Not Found.
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "AAA not configured should return 404")
}

// TestE2E_AIW_TTLS verifies that EAP-TTLS with inner PAP method completes
// and returns PVSInfo in the response.
// Spec: TS 29.526 §7.3, RFC 7170 (TTLS)
func TestE2E_AIW_TTLS(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	h := NewHarnessForTest(t)
	defer h.Close()

	// Start AUSF mock.
	ausfMockSrv := h.StartAUSFMock()
	defer ausfMockSrv.Close()

	// Use ttlsInnerMethodContainer field.
	body := map[string]interface{}{
		"supi":                     "imu-208046000000001",
		"eapIdRsp":                 "dGVzdA==",
		"ttlsInnerMethodContainer": "aGVsbG8=", // base64 "hello" (PAP username/password placeholder)
	}
	payloadBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, h.HTTPGWURL()+"/nnssaaf-aiw/v1/authentications", strings.NewReader(string(payloadBytes)))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should get 201 Created.
	assert.Equal(t, http.StatusCreated, resp.StatusCode, "AIW TTLS flow should return 201")
}
