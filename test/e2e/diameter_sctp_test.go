//go:build e2e
// +build e2e

// Diameter over SCTP verification via logs-only.
// Same shape as the TCP tests, but uses compose/fullchain-dev-sctp.yaml
// (selected by Makefile via $E2E_COMPOSE_FILE).
// Skips cleanly when the host kernel does not support SCTP.
//
// Spec: TS 29.561 §17.3 (Diameter EAP application over SCTP),
//
//	RFC 6733 §5.5 (CER/CEA), §5.6 (DWR/DWA), RFC 4072 (Diameter EAP).
package e2e

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// sctpKernelAvailable reports whether the host supports SCTP at runtime.
// Returns false on non-Linux hosts or when /proc/net/protocols lacks SCTP.
func sctpKernelAvailable() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	data, err := os.ReadFile("/proc/net/protocols")
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "SCTP")
}

// requireSCTP skips the current test if SCTP is unavailable on the host.
func requireSCTP(t *testing.T) {
	t.Helper()
	if !sctpKernelAvailable() {
		t.Skip("SCTP kernel module not available on this host (Linux required)")
	}
}

// TestDiameter_SCTP_HelloWatchdog verifies CER/CEA + DWR/DWA over SCTP.
func TestDiameter_SCTP_HelloWatchdog(t *testing.T) {
	skipIfNotE2E(t)
	requireSCTP(t)
	drv := NewContainerDriver()
	if drv == nil {
		t.Fatal("ContainerDriver is nil; FULLCHAIN_NRF_URL env not set?")
	}

	time.Sleep(3 * time.Second)
	logs, err := drv.Logs("aaa-gateway", 200)
	if err != nil {
		t.Fatalf("Logs(aaa-gateway): %v", err)
	}
	if !containsAny(logs, "diameter_forward_connected") {
		t.Errorf("aaa-gateway (SCTP) logs do not show successful CER/CEA handshake; expected log key `diameter_forward_connected` (Info). Logs:\n%s", logs)
	}
	if containsAny(logs, "diameter_forward_connect_failed", "diameter_forward_reconnect_failed") {
		t.Errorf("aaa-gateway (SCTP) logs show connect failure; logs:\n%s", logs)
	}

	time.Sleep(35 * time.Second)
	logs, err = drv.Logs("aaa-gateway", 200)
	if err != nil {
		t.Fatalf("Logs(aaa-gateway): %v", err)
	}
	if containsAny(logs, "diameter_forward_peer_lost", "diameter_forward_reconnect_failed") {
		t.Errorf("aaa-gateway (SCTP) dropped peer during watchdog window; logs:\n%s", logs)
	}
}

// TestDiameter_SCTP_DER_DEA_EAP verifies CER/CEA + DER/DEA over SCTP.
// See TestDiameter_TCP_DER_DEA_EAP for the rationale of using
// aaa-gateway's /aaa/forward directly (bypasses http-gateway, JWT, biz pod).
func TestDiameter_SCTP_DER_DEA_EAP(t *testing.T) {
	skipIfNotE2E(t)
	requireSCTP(t)
	drv := NewContainerDriver()
	if drv == nil {
		t.Fatal("ContainerDriver is nil; FULLCHAIN_NRF_URL env not set?")
	}

	time.Sleep(5 * time.Second)

	body, err := json.Marshal(map[string]any{
		"v":             "1.0",
		"sessionId":     "e2e-diameter-sctp-der-test",
		"authCtxId":     "e2e-auth-ctx-diameter-sctp",
		"transportType": "DIAMETER",
		"sst":           1,
		"sd":            "FFFFFF",
		"direction":     "CLIENT_INITIATED",
		// EAP-Response/Identity "jmich" (RFC 3748 §4.1).
		"payload": base64.StdEncoding.EncodeToString([]byte{0x01, 0x00, 0x00, 0x05, 0x01, 0x6a, 0x6d, 0x69, 0x63}),
	})
	if err != nil {
		t.Fatalf("marshal AaaForwardRequest: %v", err)
	}

	req, _ := http.NewRequest("POST", aaaGWBaseURL()+"/aaa/forward", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 8 * time.Second}
	_, _ = client.Do(req)

	time.Sleep(5 * time.Second)
	logs, err := drv.Logs("aaa-gateway", 200)
	if err != nil {
		t.Fatalf("Logs(aaa-gateway): %v", err)
	}

	if !containsAny(logs, "diameter_forward_connected") {
		t.Errorf("aaa-gateway (SCTP) logs missing `diameter_forward_connected`; logs:\n%s", logs)
	}
	if !containsAny(logs, "diameter_forward_der_sent") {
		t.Errorf("aaa-gateway (SCTP) logs missing `diameter_forward_der_sent` — DER was not emitted; logs:\n%s", logs)
	}
	if containsAny(logs, "diameter_forward_connect_failed", "diameter_forward_reconnect_failed") {
		t.Errorf("aaa-gateway (SCTP) could not reach AAA-S; logs:\n%s", logs)
	}
	if containsAny(logs, "diameter_forward_peer_lost") {
		t.Errorf("aaa-gateway (SCTP) lost the Diameter peer before DER could be sent; logs:\n%s", logs)
	}
	if containsAny(logs, "ForwardEAP failed") {
		t.Errorf("aaa-gateway (SCTP) returned ForwardEAP error; logs:\n%s", logs)
	}
	if containsAny(logs, "diameter_forward_unexpected_responser") {
		t.Errorf("aaa-gateway (SCTP) received a DEA with no matching pending request; logs:\n%s", logs)
	}
}
