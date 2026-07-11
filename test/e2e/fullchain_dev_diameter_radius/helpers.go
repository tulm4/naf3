//go:build e2e
// +build e2e

// Package fullchain_dev_diameter_radius exercises the static-IP fullchain
// compose stack end-to-end. The tests cover:
//   - Diameter CER/CEA handshake and DWR/DWA watchdog (TCP and SCTP)
//   - Diameter DER/DEA EAP exchange
//   - RADIUS Access-Request/Access-Accept with valid and invalid shared secrets
//
// Tests self-manage docker compose up/down because the existing test/e2e/
// harness assumes Makefile-managed compose with a single variant. We need two
// distinct stacks (TCP and SCTP) and the SCTP stack requires cap_add/INSTALL_SCTP
// at image build time, which the Makefile-managed target does not support.
package fullchain_dev_diameter_radius

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Static-IP plan from docs/superpowers/specs/2026-07-11-static-ip-compose-diameter-radius-e2e-design.md §4.1.
// composeFile is the TCP variant; referenced by RADIUS tests before diameter_tcp_test.go exists.
const tcpComposeFile = "compose/fullchain-dev-tcp.yaml"

const (
	aaaSimDiameterAddr = "172.0.3.14:3868"
	aaaSimRadiusAddr   = "172.0.3.14:1812"
	aaaGatewayHTTPAddr = "172.0.3.15:9090"

	diameterNetworkTCP  = "nssaa_fullchain_tcp"
	diameterNetworkSCTP = "nssaa_fullchain_sctp"

	composeUpTimeout   = 120 * time.Second
	healthCheckTimeout = 60 * time.Second
	healthCheckPoll    = 2 * time.Second
)

// bringUp starts the requested compose file, removes any pre-existing static-IP
// network of the same name to avoid IP collisions, and waits until aaa-gateway
// reports healthy via /health. Blocks until ready or context timeout.
//
// composeFile is relative to the repo root (e.g. "compose/fullchain-dev-tcp.yaml").
// extraEnv may contain overrides like {"DIAMETER_TRANSPORT": "sctp"}; it is also
// passed to docker compose via --env-file when non-empty.
func bringUp(t *testing.T, composeFile string, networkName string, extraEnv map[string]string) {
	t.Helper()

	repoRoot, err := repoRootFromThisFile()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}

	// Pre-clean: drop any stale network with this name so a fresh subnet
	// allocation succeeds even after a previous interrupted run.
	_ = runShell(t, repoRoot, "docker", "network", "rm", networkName)

	// Build args.
	args := []string{"compose", "-f", composeFile, "up", "-d", "--quiet-pull", "--wait"}
	cmd := exec.Command("docker", args...)
	cmd.Dir = repoRoot
	cmd.Env = os.Environ()
	for k, v := range extraEnv {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("docker compose up failed for %s: %v\n%s", composeFile, err, string(out))
	}

	// Wait for aaa-gateway /health.
	if err := waitHTTPHealthy(repoRoot, aaaGatewayHTTPAddr+"/health", healthCheckTimeout); err != nil {
		tearDown(t, composeFile) // best-effort
		t.Fatalf("aaa-gateway did not become healthy: %v", err)
	}

	// Extra 2s grace period to ensure aaa-sim has finished both radius+diameter
	// server initialization after its own healthcheck (spec §7.1 row 4).
	time.Sleep(2 * time.Second)
}

// tearDown runs `docker compose down -v` for the compose file.
func tearDown(t *testing.T, composeFile string) {
	t.Helper()
	repoRoot, err := repoRootFromThisFile()
	if err != nil {
		t.Logf("tearDown: locate repo root: %v", err)
		return
	}
	cmd := exec.Command("docker", "compose", "-f", composeFile, "down", "-v", "--remove-orphans")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("docker compose down failed (continuing): %v\n%s", err, string(out))
	}
}

// aaaSimAddr returns the static IP:port pair for a given AAA-S service.
// service is "diameter" or "radius".
func aaaSimAddr(service string) string {
	switch service {
	case "diameter":
		return aaaSimDiameterAddr
	case "radius":
		return aaaSimRadiusAddr
	default:
		panic(fmt.Sprintf("unknown service %q", service))
	}
}

// sctpKernelAvailable reports whether the host supports SCTP at runtime.
// Returns false on non-Linux hosts or when /proc/net/protocols lacks SCTP.
// Used by the SCTP tests to skip cleanly.
func sctpKernelAvailable() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	f, err := os.Open("/proc/net/protocols")
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// /proc/net/protocols lines look like:
		//   "SCTP     2      0  ...   132"
		if strings.HasPrefix(scanner.Text(), "SCTP") {
			return true
		}
	}
	return false
}

// requireSCTP skips the test if the host lacks SCTP support.
func requireSCTP(t *testing.T) {
	t.Helper()
	if !sctpKernelAvailable() {
		t.Skipf("SCTP kernel module unavailable on host %s", runtime.GOOS)
	}
}

// waitHTTPHealthy polls a HTTP endpoint until it returns 200 or the timeout elapses.
func waitHTTPHealthy(repoRoot, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", strings.TrimPrefix(url, "http://"), healthCheckPoll)
		if err == nil {
			_ = conn.Close()
			// Now do the actual GET.
			cmd := exec.Command("curl", "-sf", "-o", "/dev/null", url)
			cmd.Dir = repoRoot
			if curlErr := cmd.Run(); curlErr == nil {
				return nil
			}
		}
		time.Sleep(healthCheckPoll)
	}
	return fmt.Errorf("timeout after %v waiting for %s", timeout, url)
}

// runShell runs an external command with stdout/stderr captured for test output.
func runShell(t *testing.T, dir string, name string, args ...string) error {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// repoRootFromThisFile finds the repository root by walking up from this file's
// directory until it finds go.mod. Mirrors ofThisFile() in test/e2e/harness.go,
// but kept independent to avoid coupling this package to the Makefile-managed
// harness package.
func repoRootFromThisFile() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := cwd; dir != "/"; dir = filepath.Dir(dir) {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
	}
	return "", fmt.Errorf("go.mod not found above %s", cwd)
}

// ctxWithTimeout is a convenience for tests that need a bounded context.
func ctxWithTimeout(t *testing.T, d time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), d)
}

// containerLogs fetches the logs of a named container from the compose stack.
func containerLogs(t *testing.T, composeFile, service string) string {
	t.Helper()
	repoRoot, err := repoRootFromThisFile()
	if err != nil {
		t.Logf("containerLogs: locate repo root: %v", err)
		return ""
	}
	cmd := exec.Command("docker", "compose", "-f", composeFile, "logs", service)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		t.Logf("docker compose logs %s: %v", service, err)
		return ""
	}
	return string(out)
}

// contains reports whether substr appears anywhere in s.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
