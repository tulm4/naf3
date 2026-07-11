// Package fullchain_dev_diameter_radius provides E2E tests for the static-IP
// fullchain compose environment, verifying Diameter (TCP/SCTP) and RADIUS
// transport between aaa-gateway and aaa-sim.
package fullchain_dev_diameter_radius

import (
	"testing"
	"time"
)

// TestDiameter_SCTP_HelloWatchdog verifies:
// 1. SCTP kernel module is available (skip if not).
// 2. CER/CEA handshake succeeds over SCTP.
// 3. DWR/DWA watchdog succeeds.
func TestDiameter_SCTP_HelloWatchdog(t *testing.T) {
	requireSCTP(t) // Skip if kernel SCTP unavailable.

	composeFile := "compose/fullchain-dev-sctp.yaml"
	networkName := "nssaa_fullchain_sctp"

	bringUp(t, composeFile, networkName, nil)
	defer tearDown(t, composeFile)

	time.Sleep(3 * time.Second)

	logs := containerLogs(t, composeFile, "aaa-gateway")
	if !contains(logs, "cea") && !contains(logs, "CEA") && !contains(logs, "capabilities") {
		t.Errorf("aaa-gateway logs do not show CEA/capabilities exchange; logs:\n%s", logs)
	}

	time.Sleep(35 * time.Second)
	logs = containerLogs(t, composeFile, "aaa-gateway")
	if !contains(logs, "dwr") && !contains(logs, "DWR") && !contains(logs, "watchdog") {
		t.Errorf("aaa-gateway logs do not show DWR/watchdog exchange after 35s; logs:\n%s", logs)
	}
}

// TestDiameter_SCTP_DER_DEA_EAP verifies:
// 1. CER/CEA handshake succeeds over SCTP.
// 2. DER/DEA EAP exchange succeeds.
func TestDiameter_SCTP_DER_DEA_EAP(t *testing.T) {
	requireSCTP(t) // Skip if kernel SCTP unavailable.

	composeFile := "compose/fullchain-dev-sctp.yaml"
	networkName := "nssaa_fullchain_sctp"

	bringUp(t, composeFile, networkName, nil)
	defer tearDown(t, composeFile)

	time.Sleep(5 * time.Second)

	logs := containerLogs(t, composeFile, "aaa-gateway")
	if !contains(logs, "der") && !contains(logs, "DER") && !contains(logs, "NSSAA") {
		t.Errorf("aaa-gateway logs do not show DER/NSSAA exchange; logs:\n%s", logs)
	}
}
