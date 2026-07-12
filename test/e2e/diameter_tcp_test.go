//go:build e2e
// +build e2e

// Diameter CER/CEA + DWR/DWA + DER/DEA verification via logs-only.
// Stack lifecycle is owned by Makefile (`make test-diameter-radius`).
// This file only OBSERVES the running containers.
//
// Spec: RFC 6733 §5.5 (CER/CEA), §5.6 (DWR/DWA), §5.7 (session termination),
//
//	RFC 4072 (Diameter EAP), TS 29.561 Ch.17.
package e2e

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// skipIfNotE2E skips when the test is not running under the Makefile target.
func skipIfNotE2E(t *testing.T) {
	t.Helper()
	if os.Getenv("E2E_DOCKER_MANAGED") != "1" {
		t.Skip("E2E_DOCKER_MANAGED not set; run via `make test-diameter-radius`")
	}
}

// aaaGWBaseURL returns the aaa-gateway HTTP base URL exposed on host.
// The compose file maps container :9090 → host :9090 (no TLS, no auth on
// aaa-gateway's /aaa/forward), so this is the most direct path to drive
// a DER without depending on http-gateway routing, JWT middleware, or
// the biz pod.
func aaaGWBaseURL() string {
	if v := os.Getenv("FULLCHAIN_AAA_GW_URL"); v != "" {
		return v
	}
	return "http://localhost:9090"
}

// TestDiameter_TCP_HelloWatchdog verifies:
// 1. CER/CEA handshake succeeds (Diameter capabilities exchange).
// 2. DWR/DWA watchdog succeeds (Diameter watchdog exchange, every ~30s).
func TestDiameter_TCP_HelloWatchdog(t *testing.T) {
	skipIfNotE2E(t)
	drv := NewContainerDriver()
	if drv == nil {
		t.Fatal("ContainerDriver is nil; FULLCHAIN_NRF_URL env not set?")
	}

	// Give the persistent forwarder time to dial aaa-sim and complete CER/CEA.
	time.Sleep(3 * time.Second)

	logs, err := drv.Logs("aaa-gateway", 200)
	if err != nil {
		t.Fatalf("Logs(aaa-gateway): %v", err)
	}
	// diameter_forward_connected (Info) is emitted AFTER CER/CEA handshake
	// succeeds; until then we expect only connect_failed (Error) lines.
	if !containsAny(logs, "diameter_forward_connected") {
		t.Errorf("aaa-gateway logs do not show successful CER/CEA handshake; expected log key `diameter_forward_connected` (Info). Logs:\n%s", logs)
	}
	if containsAny(logs, "diameter_forward_connect_failed", "diameter_forward_reconnect_failed") {
		t.Errorf("aaa-gateway logs show Diameter connect failure; logs:\n%s", logs)
	}

	// Wait for at least one DWR/DWA cycle (~30s cadence). go-diameter's sm
	// handles DWR/DWA internally without logging; we verify by checking that
	// the connection remains alive (no peer_lost / reconnect_failed events).
	time.Sleep(35 * time.Second)
	logs, err = drv.Logs("aaa-gateway", 200)
	if err != nil {
		t.Fatalf("Logs(aaa-gateway): %v", err)
	}
	if containsAny(logs, "diameter_forward_peer_lost", "diameter_forward_reconnect_failed") {
		t.Errorf("aaa-gateway dropped Diameter peer during watchdog window; logs:\n%s", logs)
	}
}

// TestDiameter_TCP_DER_DEA_EAP verifies:
// 1. CER/CEA handshake has succeeded (gated by the previous test).
// 2. Drive a NSSAA round-trip by POSTing an AaaForwardRequest with
//    transportType=DIAMETER directly to aaa-gateway's /aaa/forward.
//    This path bypasses http-gateway, the JWT middleware, and the biz pod
//    — the request goes: aaa-gateway HTTP listener → diamForwarder.Forward
//    → DESSend→ AAA-S — which is exactly the path we want to observe.
// 3. Observe the resulting log entries:
//    - diameter_forward_connected (Info)         — CER/CEA already succeeded
//    - diameter_forward_der_sent (Info)          — DER was actually written
//    - ForwardEAP failed (Error)                  — round-trip error path
//    - diameter_forward_connect_failed (Error)    — AAA-S unreachable
//    - diameter_forward_unexpected_responser (Warn) — orphan DEA
func TestDiameter_TCP_DER_DEA_EAP(t *testing.T) {
	skipIfNotE2E(t)
	drv := NewContainerDriver()
	if drv == nil {
		t.Fatal("ContainerDriver is nil; FULLCHAIN_NRF_URL env not set?")
	}

	// Wait for CER/CEA to complete first.
	time.Sleep(5 * time.Second)

	// Send the forwarded EAP payload directly to aaa-gateway. We use a
	// generous timeout (8s) so that if AAA-S doesn't respond, aaa-gateway
	// returns its ForwardEAP error after its internal context deadline —
	// which our log check below then verifies as a positive signal.
	body, err := json.Marshal(map[string]any{
		"v":         "1.0",
		"sessionId": "e2e-diameter-tcp-der-test",
		"authCtxId": "e2e-auth-ctx-diameter-tcp",
		// Note: aaa-gateway reads "transportType"; using a non-DIAMETER value
		// here would make the test silently skip the Forward() path.
		"transportType": "DIAMETER",
		"sst":           1,
		// "FFFFFF" is the sentinel SD value used across the codebase
		// when no slice-differentiator is configured.
		"sd":        "FFFFFF",
		"direction": "CLIENT_INITIATED",
		// EAP-Response/Identity "jmich" (RFC 3748 §4.1).
		"payload": base64.StdEncoding.EncodeToString([]byte{0x01, 0x00, 0x00, 0x05, 0x01, 0x6a, 0x6d, 0x69, 0x63}),
	})
	if err != nil {
		t.Fatalf("marshal AaaForwardRequest: %v", err)
	}

	req, _ := http.NewRequest("POST", aaaGWBaseURL()+"/aaa/forward", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 8 * time.Second}
	// We deliberately ignore the response body — what matters is the
	// DER/DEA evidence in aaa-gateway's structured logs.
	_, _ = client.Do(req)

	// Give aaa-gateway time to (a) send the DER, (b) receive a DEA or
	// time out, (c) write its structured log line.
	time.Sleep(5 * time.Second)
	logs, err := drv.Logs("aaa-gateway", 200)
	if err != nil {
		t.Fatalf("Logs(aaa-gateway): %v", err)
	}

	// Positive signal: CER/CEA succeeded earlier (set by CER/CEA handshake).
	if !containsAny(logs, "diameter_forward_connected") {
		t.Errorf("aaa-gateway logs missing `diameter_forward_connected` (CER/CEA never succeeded); logs:\n%s", logs)
	}

	// Strong positive signal: the DER was actually written to the socket.
	// (Logged at Info level by internal/aaa/gateway/diameter_forward.go
	// inside Forward() right after m.WriteTo(conn) returns nil.)
	if !containsAny(logs, "diameter_forward_der_sent") {
		t.Errorf("aaa-gateway logs missing `diameter_forward_der_sent` — DER was not emitted to AAA-S; logs:\n%s", logs)
	}

	// Negative signals: distinguish WHY a DER may not have been seen.
	if containsAny(logs, "diameter_forward_connect_failed", "diameter_forward_reconnect_failed") {
		t.Errorf("aaa-gateway could not reach AAA-S (TCP/SCTP dial failure); logs:\n%s", logs)
	}
	if containsAny(logs, "diameter_forward_peer_lost") {
		t.Errorf("aaa-gateway lost the Diameter peer before DER could be sent; logs:\n%s", logs)
	}
	if containsAny(logs, "ForwardEAP failed") {
		t.Errorf("aaa-gateway returned ForwardEAP error (e.g. context deadline); logs:\n%s", logs)
	}
	if containsAny(logs, "diameter_forward_unexpected_responser") {
		t.Errorf("aaa-gateway received a DEA with no matching pending request; logs:\n%s", logs)
	}
}

// containsAny reports whether s contains any of the given substrings (case-sensitive).
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
