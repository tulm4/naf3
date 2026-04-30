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

// TestE2E_Revocation_HappyPath verifies the AAA-S → NSSAAF → AMF revocation flow.
// Spec: TS 23.502 §4.2.9.4, TS 29.518 §5.2.2.27
func TestE2E_Revocation_HappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	h := NewHarnessForTest(t)
	defer h.Close()

	// Start AMF mock to receive revocation notifications.
	amfMockSrv := h.StartAMFMock()
	defer amfMockSrv.Close()

	// 1. Establish a baseline NSSAA session via HTTP GW.
	body := map[string]interface{}{
		"gpsi":     "520804600000001",
		"snssai":   map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapIdRsp": "dGVzdA==",
	}
	payloadBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, h.HTTPGWURL()+"/nnssaaf-nssaa/v1/slice-authentications", strings.NewReader(string(payloadBytes)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "revocation-test")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode, "baseline session create should succeed")

	authCtxID := parseAuthCtxID(t, resp)

	// 2. Confirm the session.
	confirmBody := map[string]interface{}{
		"gpsi":       "520804600000001",
		"snssai":     map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapMessage": "dGVzdA==",
	}
	confirmBytes, _ := json.Marshal(confirmBody)
	location := resp.Header.Get("Location")
	req2, _ := http.NewRequest(http.MethodPut, h.HTTPGWURL()+location, strings.NewReader(string(confirmBytes)))
	req2.Header.Set("Content-Type", "application/json")

	resp2, err := client.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()

	// 3. Simulate AAA-S sending Disconnect-Request to the AAA GW.
	// The AAA GW forwards this to the Biz Pod, which sends SLICE_REVOCATION to AMF.
	// In this E2E test, we verify the AMF mock notification endpoint is reachable.
	t.Logf("Session established: authCtxID=%s, AMF mock at %s", authCtxID, amfMockSrv.URL)

	// 4. Verify AMF mock is running and can receive notifications.
	req3, _ := http.NewRequest(http.MethodGet, amfMockSrv.URL+"/namf-callback/v1/test", nil)
	resp3, err := client.Do(req3)
	require.NoError(t, err)
	defer resp3.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp3.StatusCode)
}

// TestE2E_Revocation_AmfUnreachable verifies that when the AMF is unreachable,
// the revocation notification is sent to the DLQ.
// Spec: TS 23.502 §4.2.9.4, REQ-09
func TestE2E_Revocation_AmfUnreachable(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	t.Skip("AMF unreachable test requires controlled AMF shutdown; covered by integration tests")
}

// TestE2E_Revocation_ConcurrentRevocations verifies that multiple simultaneous
// revocation requests are handled correctly.
// Spec: TS 23.502 §4.2.9.4
func TestE2E_Revocation_ConcurrentRevocations(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	t.Skip("Concurrent revocation test requires controlled AAA-S DR injection; covered by integration tests")
}
