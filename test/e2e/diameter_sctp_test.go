//go:build e2e
// +build e2e

// Diameter over SCTP verification via logs-only.
// Same shape as the TCP tests, but uses compose/fullchain-dev-sctp.yaml
// (selected by Makefile via $E2E_COMPOSE_FILE).
// Skips cleanly when the host kernel does not support SCTP.
//
// Spec: TS 29.561 §17.3 (Diameter EAP application over SCTP).
package e2e

import (
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
	if !containsAny(logs, "diameter_forward_connected", "CEA") {
		t.Errorf("aaa-gateway (SCTP) logs do not show successful CER/CEA exchange; logs:\n%s", logs)
	}
	if containsAny(logs, "diameter_forward_connect_failed") {
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
func TestDiameter_SCTP_DER_DEA_EAP(t *testing.T) {
	skipIfNotE2E(t)
	requireSCTP(t)
	drv := NewContainerDriver()
	if drv == nil {
		t.Fatal("ContainerDriver is nil; FULLCHAIN_NRF_URL env not set?")
	}

	time.Sleep(5 * time.Second)

	gpsiURL := os.Getenv("FULLCHAIN_HTTP_GW_URL")
	if gpsiURL == "" {
		gpsiURL = "https://localhost:8443"
	}
	req, _ := http.NewRequest("POST", gpsiURL+"/nnssaaf/v1/auth-status",
		strings.NewReader(`{"gpsi":"msisdn-1234567890"}`))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	_, _ = client.Do(req)

	time.Sleep(5 * time.Second)
	logs, err := drv.Logs("aaa-gateway", 200)
	if err != nil {
		t.Fatalf("Logs(aaa-gateway): %v", err)
	}
	if !containsAny(logs, "diameter_forward_der_sent", "DER", "NSSAA") {
		t.Errorf("aaa-gateway (SCTP) logs do not show DER/NSSAA exchange; logs:\n%s", logs)
	}
}
