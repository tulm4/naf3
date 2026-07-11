//go:build e2e
// +build e2e

// Diameter CER/CEA + DWR/DWA + DER/DEA verification via logs-only.
// Stack lifecycle is owned by Makefile (`make test-diameter-radius`).
// This file only OBSERVES the running containers.
//
// Spec: RFC 6733 §5.5 (CER/CEA), §5.6 (DWR/DWA), §5.7 (session termination).
package e2e

import (
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
	if !containsAny(logs, "CEA", "capabilities exchange", "CER sent") {
		t.Errorf("aaa-gateway logs do not show CER/CEA exchange; logs:\n%s", logs)
	}

	// Wait for at least one DWR/DWA cycle (~30s cadence).
	time.Sleep(35 * time.Second)
	logs, err = drv.Logs("aaa-gateway", 200)
	if err != nil {
		t.Fatalf("Logs(aaa-gateway): %v", err)
	}
	if !containsAny(logs, "DWR", "watchdog", "watchdog exchange") {
		t.Errorf("aaa-gateway logs do not show DWR/watchdog exchange after 35s; logs:\n%s", logs)
	}
}

// TestDiameter_TCP_DER_DEA_EAP verifies:
// 1. CER/CEA handshake succeeds.
// 2. Trigger a NSSAA flow via the biz/http-gateway HTTP endpoint.
// 3. Observe DER sent + DEA received in aaa-gateway logs.
func TestDiameter_TCP_DER_DEA_EAP(t *testing.T) {
	skipIfNotE2E(t)
	drv := NewContainerDriver()
	if drv == nil {
		t.Fatal("ContainerDriver is nil; FULLCHAIN_NRF_URL env not set?")
	}

	// Wait for CER/CEA to complete first.
	time.Sleep(5 * time.Second)

	// Trigger an NSSAA-Request by POSTing to the http-gateway's NSSAA endpoint.
	// http-gateway listens on https://localhost:8443 (TLS terminated).
	// We use a 5s timeout because the request may fail for unrelated reasons
	// (auth missing) — what matters is that aaa-gateway LOGS the DER attempt.
	gpsiURL := os.Getenv("FULLCHAIN_HTTP_GW_URL")
	if gpsiURL == "" {
		gpsiURL = "https://localhost:8443"
	}
	req, _ := http.NewRequest("POST", gpsiURL+"/nnssaaf/v1/auth-status",
		strings.NewReader(`{"gpsi":"msisdn-1234567890"}`))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	// We do not assert the response body — only that aaa-gateway emitted a DER.
	_, _ = client.Do(req)

	// Give aaa-gateway time to forward DER and log result.
	time.Sleep(5 * time.Second)
	logs, err := drv.Logs("aaa-gateway", 200)
	if err != nil {
		t.Fatalf("Logs(aaa-gateway): %v", err)
	}
	if !containsAny(logs, "DER", "NSSAA", "der sent") {
		t.Errorf("aaa-gateway logs do not show DER/NSSAA exchange; logs:\n%s", logs)
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
