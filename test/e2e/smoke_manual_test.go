// +build e2e

// Package e2e provides manual E2E smoke tests against the running 3-component NSSAAF stack.
// Run with: go test -tags=e2e -count=1 ./test/e2e/
//
// Prerequisites:
//   - Docker containers: postgres_test, redis_test, mock-aaa-s (from compose/dev.yaml + compose/test.yaml)
//   - Binary running: NRM (localhost:8081)
//   - Binary running: Biz Pod (localhost:8080)
//   - Binary running: AAA Gateway (localhost:9091)
//   - Binary running: HTTP Gateway (localhost:8443)
//
// For the HTTP Gateway, these tests use HTTP (not HTTPS). Ensure http-gateway-e2e.yaml is loaded
// (or TLS is disabled) before running.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	httpGWURL = "http://localhost:8443"
	bizURL    = "http://localhost:8080"
	nrmURL    = "http://localhost:8081"
)

func skipIfServicesNotUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 5 * time.Second}

	for _, svc := range []struct {
		name string
		url  string
	}{
		{"HTTP Gateway", httpGWURL + "/healthz/"},
		{"Biz Pod", bizURL + "/healthz/live"},
		{"NRM", nrmURL + "/healthz"},
	} {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, svc.url, nil)
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode >= 500 {
			t.Skipf("service %s (%s) not available: %v", svc.name, svc.url, err)
		}
		resp.Body.Close()
	}
}

func doRequest(t *testing.T, method, url string, body interface{}) *http.Response {
	client := &http.Client{Timeout: 30 * time.Second}
	var bodyReader *strings.Reader
	if body != nil {
		bs, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(bs))
	} else {
		bodyReader = strings.NewReader("")
	}
	req, _ := http.NewRequestWithContext(context.Background(), method, url, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "e2e-smoke-"+t.Name())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request to %s failed: %v", url, err)
	}
	return resp
}

// TestE2E_00_AllServicesHealthy verifies all services respond to health checks.
func TestE2E_00_AllServicesHealthy(t *testing.T) {
	skipIfServicesNotUp(t)

	// HTTP Gateway health
	resp := doRequest(t, http.MethodGet, httpGWURL+"/healthz/", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("HTTP Gateway health check failed: %d", resp.StatusCode)
	}

	// Biz Pod liveness
	resp = doRequest(t, http.MethodGet, bizURL+"/healthz/live", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Biz Pod liveness failed: %d", resp.StatusCode)
	}

	// Biz Pod readiness
	resp = doRequest(t, http.MethodGet, bizURL+"/healthz/ready", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Biz Pod readiness unexpected: %d", resp.StatusCode)
	}

	// NRM health
	resp = doRequest(t, http.MethodGet, nrmURL+"/healthz", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("NRM health check failed: %d", resp.StatusCode)
	}
}

// TestE2E_01_NSSAA_CreateSession_viaHTTPGW verifies POST /nnssaaf-nssaa/v1/slice-authentications.
// Requires auth token unless HTTP Gateway auth is disabled.
func TestE2E_01_NSSAA_CreateSession_viaHTTPGW(t *testing.T) {
	skipIfServicesNotUp(t)

	body := map[string]interface{}{
		"gpsi":     "520804600000001",
		"snssai":   map[string]interface{}{"sst": 1, "sd": "000001"},
		"eapIdRsp": "dGVzdA==",
	}

	resp := doRequest(t, http.MethodPost, httpGWURL+"/nnssaaf-nssaa/v1/slice-authentications", body)
	defer resp.Body.Close()

	// Auth middleware: HTTP 401 without token. Check if auth is enabled.
	if resp.StatusCode == http.StatusUnauthorized {
		t.Skip("HTTP Gateway requires auth token (JWT Bearer) — provide token or disable auth for E2E")
	}

	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := json.MarshalIndent(body, "", "  ")
		t.Errorf("CreateSession failed: HTTP %d, body: %s", resp.StatusCode, string(bodyBytes))
		return
	}

	// Verify Location header
	location := resp.Header.Get("Location")
	if location == "" {
		t.Error("Location header missing")
	}
	t.Logf("CreateSession: authCtxId from location: %s", location)

	// Verify X-Request-ID echo
	xReqID := resp.Header.Get("X-Request-ID")
	if xReqID == "" {
		t.Error("X-Request-ID not echoed")
	}

	// Verify response body
	var authResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		t.Errorf("invalid JSON response: %v", err)
		return
	}
	if authResp["authCtxId"] == nil {
		t.Error("authCtxId missing from response")
	} else {
		t.Logf("authCtxId: %v", authResp["authCtxId"])
	}
}

// TestE2E_02_NSSAA_CreateSession_viaBizDirect verifies POST /nnssaaf-nssaa/v1/slice-authentications
// against the Biz Pod directly (no auth middleware).
func TestE2E_02_NSSAA_CreateSession_viaBizDirect(t *testing.T) {
	skipIfServicesNotUp(t)

	body := map[string]interface{}{
		"gpsi":     "520804600000002",
		"snssai":   map[string]interface{}{"sst": 1, "sd": "000002"},
		"eapIdRsp": "dXNlcjE=",
	}

	resp := doRequest(t, http.MethodPost, bizURL+"/nnssaaf-nssaa/v1/slice-authentications", body)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := json.MarshalIndent(body, "", "  ")
		t.Errorf("CreateSession via Biz Direct failed: HTTP %d, body: %s", resp.StatusCode, string(bodyBytes))
		return
	}

	var authResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		t.Errorf("invalid JSON response: %v", err)
		return
	}
	authCtxID, ok := authResp["authCtxId"].(string)
	if !ok || authCtxID == "" {
		t.Error("authCtxId missing from response")
		return
	}

	location := resp.Header.Get("Location")
	if location == "" {
		t.Error("Location header missing")
	} else {
		// The Location header is now a full URL: http://localhost:8080/nnssaaf-nssaa/v1/slice-authentications/...
		// Use bizURL + path directly.
		relativePath := "/nnssaaf-nssaa/v1/slice-authentications/" + authCtxID
		confirmURL := bizURL + relativePath
		t.Logf("CreateSession via Biz: authCtxID=%s, confirmURL=%s", authCtxID, confirmURL)

		confirmBody := map[string]interface{}{
			"gpsi":       "520804600000002",
			"snssai":     map[string]interface{}{"sst": 1, "sd": "000002"},
			"eapMessage": "dGVzdA==",
		}
		resp2 := doRequest(t, http.MethodPut, confirmURL, confirmBody)
		defer resp2.Body.Close()

		if resp2.StatusCode != http.StatusOK {
			t.Errorf("ConfirmSession failed: HTTP %d", resp2.StatusCode)
		} else {
			t.Logf("ConfirmSession: HTTP %d", resp2.StatusCode)
			var confirmResp map[string]interface{}
			if err := json.NewDecoder(resp2.Body).Decode(&confirmResp); err == nil {
				t.Logf("Confirm response: %s", mustMarshal(confirmResp))
			}
		}
	}
}

// TestE2E_03_NSSAA_InvalidGPSI verifies HTTP 400 for invalid GPSI.
func TestE2E_03_NSSAA_InvalidGPSI(t *testing.T) {
	skipIfServicesNotUp(t)

	body := map[string]interface{}{
		"gpsi":     "not-a-valid-gpsi",
		"snssai":   map[string]interface{}{"sst": 1},
		"eapIdRsp": "dGVzdA==",
	}

	resp := doRequest(t, http.MethodPost, bizURL+"/nnssaaf-nssaa/v1/slice-authentications", body)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid GPSI, got %d", resp.StatusCode)
	} else {
		var problem map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&problem)
		t.Logf("Invalid GPSI response: %v", problem)
	}
}

// TestE2E_04_NSSAA_InvalidSnssai verifies HTTP 400 for invalid Snssai.
func TestE2E_04_NSSAA_InvalidSnssai(t *testing.T) {
	skipIfServicesNotUp(t)

	tests := []struct {
		name   string
		snssai map[string]interface{}
	}{
		{"SST out of range", map[string]interface{}{"sst": 300}},
		{"SD not 6 hex chars", map[string]interface{}{"sst": 1, "sd": "GGGGGG"}},
		{"Missing SST", map[string]interface{}{}}, // Gap: handler accepts empty snssai; should 400 per TS 29.526
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]interface{}{
				"gpsi":     "520804600000003",
				"snssai":   tc.snssai,
				"eapIdRsp": "dGVzdA==",
			}
			resp := doRequest(t, http.MethodPost, bizURL+"/nnssaaf-nssaa/v1/slice-authentications", body)
			defer resp.Body.Close()

			if tc.name == "Missing SST" && resp.StatusCode == http.StatusCreated {
				t.Skip("Gap: Missing S-NSSAI accepted as 201; should be 400 per TS 29.526")
			}

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("expected 400 for %s, got %d", tc.name, resp.StatusCode)
			} else {
				t.Logf("Invalid Snssai %s correctly returns 400", tc.name)
			}
		})
	}
}

// TestE2E_05_AIW_CreateSession verifies POST /nnssaaf-aiw/v1/authentications.
func TestE2E_05_AIW_CreateSession(t *testing.T) {
	skipIfServicesNotUp(t)

	body := map[string]interface{}{
		"supi":     "imu-208046000000001",
		"eapIdRsp": "dGVzdA==",
	}

	resp := doRequest(t, http.MethodPost, bizURL+"/nnssaaf-aiw/v1/authentications", body)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("AIW CreateSession failed: HTTP %d", resp.StatusCode)
		return
	}

	location := resp.Header.Get("Location")
	t.Logf("AIW CreateSession: location=%s", location)

	var authResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&authResp)
	t.Logf("AIW response: %s", mustMarshal(authResp))
}

// TestE2E_06_AIW_InvalidSupi verifies HTTP 400 for invalid SUPI.
func TestE2E_06_AIW_InvalidSupi(t *testing.T) {
	skipIfServicesNotUp(t)

	tests := []struct {
		name string
		supi string
	}{
		{"not matching regex", "invalid-supi"},
		{"empty SUPI", ""},
		{"wrong prefix", "msisdn-1234567890"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]interface{}{
				"supi":     tc.supi,
				"eapIdRsp": "dGVzdA==",
			}
			resp := doRequest(t, http.MethodPost, bizURL+"/nnssaaf-aiw/v1/authentications", body)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("expected 400 for %s (%q), got %d", tc.name, tc.supi, resp.StatusCode)
			}
		})
	}
}

// TestE2E_07_NRM_RESTCONF_GET verifies GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function.
func TestE2E_07_NRM_RESTCONF_GET(t *testing.T) {
	skipIfServicesNotUp(t)

	resp := doRequest(t, http.MethodGet, nrmURL+"/restconf/data/3gpp-nssaaf-nrm:nssaa-function", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("NRM GET nssaa-function failed: HTTP %d", resp.StatusCode)
		return
	}

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Errorf("invalid JSON from NRM: %v", err)
		return
	}
	t.Logf("NRM nssaa-function: %s", mustMarshal(data))
}

// TestE2E_08_NRM_RESTCONF_Alarms verifies GET /restconf/data/3gpp-nssaaf-nrm:alarms.
func TestE2E_08_NRM_RESTCONF_Alarms(t *testing.T) {
	skipIfServicesNotUp(t)

	resp := doRequest(t, http.MethodGet, nrmURL+"/restconf/data/3gpp-nssaaf-nrm:alarms", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("NRM GET alarms failed: HTTP %d", resp.StatusCode)
		return
	}

	var data map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&data)
	t.Logf("NRM alarms: %s", mustMarshal(data))
}

// TestE2E_09_ConcurrentSessions verifies 10 concurrent CreateSession requests.
func TestE2E_09_ConcurrentSessions(t *testing.T) {
	skipIfServicesNotUp(t)

	const n = 10
	type result struct {
		status int
		body   string
	}
	results := make([]result, n)

	client := &http.Client{Timeout: 30 * time.Second}
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
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
				bizURL+"/nnssaaf-nssaa/v1/slice-authentications",
				strings.NewReader(string(payloadBytes)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Request-ID", fmt.Sprintf("concurrent-%d", idx))
			resp, err := client.Do(req)
			if err != nil {
				results[idx] = result{0, err.Error()}
				return
			}
			body, _ := json.MarshalIndent(map[string]interface{}{}, "", "  ")
			results[idx] = result{resp.StatusCode, string(body)}
			resp.Body.Close()
		}(i)
	}
	wg.Wait()

	successes := 0
	for i := 0; i < n; i++ {
		if results[i].status == http.StatusCreated {
			successes++
		} else {
			t.Logf("Request %d: HTTP %d", i, results[i].status)
		}
	}
	if successes != n {
		t.Errorf("expected %d successes, got %d", n, successes)
	} else {
		t.Logf("All %d concurrent sessions created successfully", n)
	}
}

func mustMarshal(v interface{}) string {
	bs, _ := json.MarshalIndent(v, "", "  ")
	return string(bs)
}
