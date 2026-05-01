// Package tests provides end-to-end integration tests for the NSSAAF system.
// These are manual smoke tests that can run against a running NSSAAF stack.
package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/operator/nssAAF/test/e2e/suite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_00_AllServicesHealthy verifies all services respond to health checks.
func TestE2E_00_AllServicesHealthy(t *testing.T) {
	h := suite.NewHarnessForTest(t)
	defer h.Close()

	// Test HTTP Gateway health
	resp, err := h.TLSClient().Get(h.HTTPGWURL() + "/healthz/")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "HTTP Gateway health check should return 200")

	// Test Biz Pod liveness
	bizResp, err := http.Get(h.BizURL() + "/healthz/live")
	require.NoError(t, err)
	defer bizResp.Body.Close()
	assert.Equal(t, http.StatusOK, bizResp.StatusCode, "Biz Pod liveness should return 200")

	// Test NRM health
	nrmResp, err := http.Get(h.NRMURL() + "/healthz")
	require.NoError(t, err)
	defer nrmResp.Body.Close()
	assert.Equal(t, http.StatusOK, nrmResp.StatusCode, "NRM health check should return 200")
}

// TestE2E_01_NSSAA_CreateSession verifies POST /nnssaaf-nssaa/v1/slice-authentications.
func TestE2E_01_NSSAA_CreateSession(t *testing.T) {
	h := suite.NewHarnessForTest(t)
	defer h.Close()

	body := map[string]interface{}{
		"gpsi":     "520804600000001",
		"snssai":   map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapIdRsp": "dGVzdA==",
	}

	payloadBytes, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, h.BizURL()+"/nnssaaf-nssaa/v1/slice-authentications", strings.NewReader(string(payloadBytes)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "e2e-smoke-01")

	client := h.TLSClient()
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should return 201 Created.
	assert.Equal(t, http.StatusCreated, resp.StatusCode, "NSSAA CreateSession should return 201")

	// Parse response body.
	var authResp map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&authResp)
	require.NoError(t, err, "response should be valid JSON")
	authCtxID, ok := authResp["authCtxId"].(string)
	require.True(t, ok, "authCtxId must be present")
	t.Logf("CreateSession: authCtxId=%s", authCtxID)
}

// TestE2E_02_NSSAA_InvalidGPSI verifies HTTP 400 for invalid GPSI.
func TestE2E_02_NSSAA_InvalidGPSI(t *testing.T) {
	h := suite.NewHarnessForTest(t)
	defer h.Close()

	body := map[string]interface{}{
		"gpsi":     "not-a-valid-gpsi",
		"snssai":   map[string]interface{}{"sst": 1},
		"eapIdRsp": "dGVzdA==",
	}

	payloadBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, h.BizURL()+"/nnssaaf-nssaa/v1/slice-authentications", strings.NewReader(string(payloadBytes)))
	req.Header.Set("Content-Type", "application/json")

	client := h.TLSClient()
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "Invalid GPSI should return 400")
}

// TestE2E_03_AIW_CreateSession verifies POST /nnssaaf-aiw/v1/authentications.
func TestE2E_03_AIW_CreateSession(t *testing.T) {
	h := suite.NewHarnessForTest(t)
	defer h.Close()

	body := map[string]interface{}{
		"supi":     "imsi-208046000000001",
		"eapIdRsp": "dGVzdA==",
	}

	payloadBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, h.BizURL()+"/nnssaaf-aiw/v1/authentications", strings.NewReader(string(payloadBytes)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "e2e-smoke-aiw-03")

	client := h.TLSClient()
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode, "AIW CreateSession should return 201")

	var authResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&authResp)
	t.Logf("AIW response: %v", authResp)
}

// TestE2E_04_AIW_InvalidSupi verifies HTTP 400 for invalid SUPI.
func TestE2E_04_AIW_InvalidSupi(t *testing.T) {
	h := suite.NewHarnessForTest(t)
	defer h.Close()

	testCases := []struct {
		name string
		supi string
	}{
		{"not matching regex", "invalid-supi"},
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
			req, _ := http.NewRequest(http.MethodPost, h.BizURL()+"/nnssaaf-aiw/v1/authentications", strings.NewReader(string(payloadBytes)))
			req.Header.Set("Content-Type", "application/json")

			client := h.TLSClient()
			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "Invalid SUPI should return 400")
		})
	}
}

// TestE2E_05_ConcurrentSessions verifies 10 concurrent CreateSession requests.
func TestE2E_05_ConcurrentSessions(t *testing.T) {
	h := suite.NewHarnessForTest(t)
	defer h.Close()

	const n = 10
	type result struct {
		status int
		err    string
	}
	results := make([]result, n)

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			gpsi := fmt.Sprintf("5208046%06d", idx)
			payload := map[string]interface{}{
				"gpsi":     gpsi,
				"snssai":   map[string]interface{}{"sst": 1, "sd": "000001"},
				"eapIdRsp": "dGVzdA==",
			}
			payloadBytes, _ := json.Marshal(payload)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
				h.BizURL()+"/nnssaaf-nssaa/v1/slice-authentications",
				strings.NewReader(string(payloadBytes)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Request-ID", fmt.Sprintf("concurrent-%d", idx))

			client := h.TLSClient()
			resp, err := client.Do(req)
			if err != nil {
				results[idx] = result{0, err.Error()}
				return
			}
			defer resp.Body.Close()
			results[idx] = result{resp.StatusCode, ""}
		}(i)
	}
	wg.Wait()

	successes := 0
	for i := 0; i < n; i++ {
		if results[i].status == http.StatusCreated {
			successes++
		} else {
			t.Logf("Request %d: HTTP %d, err=%s", i, results[i].status, results[i].err)
		}
	}
	assert.Equal(t, n, successes, "All %d concurrent sessions should succeed", n)
}
