//go:build e2e
// +build e2e

// Forward-direction integration tests for the per-UE debug tracing subsystem.
//
// These tests verify the end-to-end flow:
//
//	AMF → http-gw → biz → aaa-gw → AAA-S (RADIUS or Diameter)
//
// Per the verification spec (§3), all required debug events for the GPSI must
// land in Redis (`nssaa:debug:stream:<gpsi_h>`) sharing a single trace_id.
// The required events per spec are:
//
//	http-gw:  http.request, http.request.exit
//	biz:      http.request, pg.session.create, pg.session.update,
//	          http.request.out, http.request.exit
//	aaa-gw:   aaa.session_corr.write, aaa.radius.forward,
//	          redis.session_corr.write, aaa.handle_forward, radius.eap.forward
//
// Note: biz:redis.session.set and biz:pg.audit.write are not yet implemented
// in the current Biz architecture (session cache and audit logging are TODO).
// Similarly, aaa-gw does not emit separate http.request/http.request.exit events.
package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/operator/nssAAF/internal/logging"
)

// TestDebugFullFlow_RADIUS_Forward exercises flow direction (a):
// AMF → http-gw → biz → aaa-gw → AAA-S (RADIUS Access-Request).
//
// Requires RUN_E2E=1 and a running fullchain compose stack with the
// aaa-sim container reachable on its RADIUS port.
func TestDebugFullFlow_RADIUS_Forward(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to run")
	}
	require.NoError(t, ComposeRunning("aaa-sim"))
	h := NewHarnessForTest(t)

	// Use a GPSI distinct from the other tests to keep streams clean.
	const gpsi = "msisdn-208046000000001"
	gpsiHash := logging.HashGPSI(gpsi)
	streamKey := "nssaa:debug:stream:" + gpsiHash

	rdb := h.Redis()
	// Clear any prior stream for clean assertions.
	require.NoError(t, rdb.Del(context.Background(), streamKey).Err())

	// Drive the flow via the standard NSSAA Create endpoint.
	// The HTTP Gateway is the public entry point (matches the harness.postNSSAA
	// pattern used in n58_flow_test.go and others). HTTP Gateway then calls biz,
	// which calls aaa-gw, which forwards to aaa-sim over RADIUS.
	body := fmt.Sprintf(`{"gpsi":"%s","snssai":{"sst":1,"sd":"000001"},"eapIdRsp":"dGVzdA=="}`, gpsi)
	postNSSAAAuth(t, body)

	// Poll Redis with 100ms backoff up to 5s.
	required := []string{
		"http-gw:http.request",
		"http-gw:http.request.exit",
		"biz:http.request",
		"biz:pg.session.create",
		"biz:pg.session.update",
		"biz:http.request.out",
		"biz:http.request.exit",
		"aaa-gw:aaa.session_corr.write",
		"aaa-gw:aaa.radius.forward",
		"aaa-gw:redis.session_corr.write",
		"aaa-gw:aaa.handle_forward",
		"aaa-gw:radius.eap.forward",
	}
	deadline := time.Now().Add(5 * time.Second)
	backoff := 100 * time.Millisecond
	var events []redis.XMessage
	for time.Now().Before(deadline) {
		var err error
		events, err = rdb.XRange(context.Background(), streamKey, "-", "+").Result()
		require.NoError(t, err)
		if hasRequiredDebugEvents(events, required) {
			break
		}
		time.Sleep(backoff)
		backoff *= 2
		if backoff > 800*time.Millisecond {
			backoff = 800 * time.Millisecond
		}
	}
	require.GreaterOrEqual(t, len(events), len(required),
		"timed out waiting for events on stream %s (got %d events)", streamKey, len(events))

	// All events must share a single trace_id.
	traceIDs := map[string]bool{}
	for _, e := range events {
		traceID, ok := e.Values["trace"].(string)
		require.True(t, ok, "event %s has no trace field", e.ID)
		require.NotEmpty(t, traceID, "event %s has an empty trace field", e.ID)
		traceIDs[traceID] = true
	}
	require.Equal(t, 1, len(traceIDs),
		"expected exactly one trace_id across all events, got %v", traceIDs)

	// Each required (svc, op) must be present at least once.
	present := map[string]bool{}
	for _, e := range events {
		svc, _ := e.Values["svc"].(string)
		op, _ := e.Values["op"].(string)
		present[svc+":"+op] = true
	}
	for _, want := range required {
		require.True(t, present[want], "missing event %s (have: %v)", want, keys(present))
	}
}

// TestDebugFullFlow_DIAMETER_Forward mirrors RADIUS_Forward but uses Diameter
// over TCP (port 3868). The compose stack must be brought up with
// DIAMETER_TRANSPORT=tcp so that aaa-sim and aaa-gw use TCP for Diameter.
//
// The required event list differs from RADIUS_Forward only in the `op` field:
// `aaa.diameter.forward` replaces `aaa.radius.forward`.
func TestDebugFullFlow_DIAMETER_Forward(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to run")
	}
	require.NoError(t, ComposeRunning("aaa-sim"))
	h := NewHarnessForTest(t)

	const gpsi = "msisdn-208046000000002"
	gpsiHash := logging.HashGPSI(gpsi)
	streamKey := "nssaa:debug:stream:" + gpsiHash

	rdb := h.Redis()
	require.NoError(t, rdb.Del(context.Background(), streamKey).Err())

	body := fmt.Sprintf(`{"gpsi":"%s","snssai":{"sst":1,"sd":"000001"},"eapIdRsp":"dGVzdA=="}`, gpsi)
	postNSSAAAuth(t, body)

	required := []string{
		"http-gw:http.request",
		"http-gw:http.request.exit",
		"biz:http.request",
		"biz:pg.session.create",
		"biz:pg.session.update",
		"biz:http.request.out",
		"biz:http.request.exit",
		"aaa-gw:aaa.session_corr.write",
		"aaa-gw:aaa.diameter.forward",
		"aaa-gw:redis.session_corr.write",
		"aaa-gw:aaa.handle_forward",
	}
	deadline := time.Now().Add(5 * time.Second)
	backoff := 100 * time.Millisecond
	var events []redis.XMessage
	for time.Now().Before(deadline) {
		var err error
		events, err = rdb.XRange(context.Background(), streamKey, "-", "+").Result()
		require.NoError(t, err)
		if hasRequiredDebugEvents(events, required) {
			break
		}
		time.Sleep(backoff)
		backoff *= 2
		if backoff > 800*time.Millisecond {
			backoff = 800 * time.Millisecond
		}
	}
	require.GreaterOrEqual(t, len(events), len(required),
		"timed out waiting for Diameter events on stream %s (got %d events)", streamKey, len(events))

	traceIDs := map[string]bool{}
	for _, e := range events {
		traceID, ok := e.Values["trace"].(string)
		require.True(t, ok, "event %s has no trace field", e.ID)
		require.NotEmpty(t, traceID, "event %s has an empty trace field", e.ID)
		traceIDs[traceID] = true
	}
	require.Equal(t, 1, len(traceIDs),
		"expected exactly one trace_id across all events, got %v", traceIDs)

	present := map[string]bool{}
	for _, e := range events {
		svc, _ := e.Values["svc"].(string)
		op, _ := e.Values["op"].(string)
		present[svc+":"+op] = true
	}
	for _, want := range required {
		require.True(t, present[want], "missing event %s (have: %v)", want, keys(present))
	}
}

func hasRequiredDebugEvents(events []redis.XMessage, required []string) bool {
	present := make(map[string]bool, len(events))
	for _, event := range events {
		svc, _ := event.Values["svc"].(string)
		op, _ := event.Values["op"].(string)
		present[svc+":"+op] = true
	}
	// Log what's present for debugging
	fmt.Printf("DEBUG: received events: %v\n", present)
	for _, want := range required {
		if !present[want] {
			return false
		}
	}
	return true
}

// postNSSAAAuth drives the NSSAA Create flow via the HTTP Gateway. It uses
// FULLCHAIN_HTTP_GW_URL when set; otherwise it uses the shared harness URL.
//
// To exercise the full path (http-gw → biz → aaa-gw) we POST through the
// HTTP Gateway, matching the harness.postNSSAA pattern in n58_flow_test.go.
// If HTTP Gateway auth is enabled and returns 401, the test should be skipped
// or a token configured via the test infra.
func postNSSAAAuth(t *testing.T, body string) {
	t.Helper()

	h := SharedHarness()
	require.NotNil(t, h, "E2E harness was not initialized")

	base := os.Getenv("FULLCHAIN_HTTP_GW_URL")
	if base == "" {
		base = h.HTTPGWURL()
	}
	url := strings.TrimRight(base, "/") + "/nnssaaf-nssaa/v1/slice-authentications"

	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "test-debug-forward")

	client := h.TLSClient()
	resp, err := client.Do(req.WithContext(requireTestContext(t)))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.True(t, resp.StatusCode < 300,
		"HTTP Gateway returned %d (expected 2xx)", resp.StatusCode)
}
