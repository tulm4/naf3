// Package fullchain_dev_diameter_radius provides E2E tests for the static-IP
// fullchain compose environment, verifying Diameter (TCP/SCTP) and RADIUS
// transport between aaa-gateway and aaa-sim.
//
// Tests manage their own Docker Compose lifecycle (bringUp / tearDown) so each
// test is independent and can use a different compose file (TCP or SCTP variant).
// This differs from the shared-harness approach used by the top-level e2e package.
//
// Spec: docs/superpowers/specs/2026-07-11-static-ip-compose-diameter-radius-e2e-design.md
package fullchain_dev_diameter_radius

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// bringUp starts the Docker Compose stack and waits for it to be healthy.
// composeFile is relative to the repo root (e.g., "compose/fullchain-dev-tcp.yaml").
// networkName is the Docker network name (e.g., "nssaa_fullchain_tcp").
// extraEnv adds or overrides environment variables for docker compose up.
func bringUp(t *testing.T, composeFile string, networkName string, extraEnv map[string]string) {
	t.Helper()
	repoRoot := repoRoot(t)
	dc := composeCmd(repoRoot, composeFile)

	// Merge extra env into the compose invocation's environment.
	env := os.Environ()
	for k, v := range extraEnv {
		env = append(env, k+"="+v)
	}

	// Build images first (required since the compose file uses build: directives).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := runCmd(ctx, t, append(dc, "build", "--pull", "--quiet")...); err != nil {
		t.Fatalf("docker compose build failed: %v", err)
	}

	// Bring up with --wait (blocks until all health checks pass).
	if err := runCmd(ctx, t, append(dc, "up", "-d", "--wait")...); err != nil {
		// On failure, dump container logs for diagnosis.
		_ = runCmd(context.Background(), t, append(dc, "logs", "--tail=50")...)
		t.Fatalf("docker compose up --wait failed: %v", err)
	}
}

// tearDown stops and removes the Docker Compose stack.
func tearDown(t *testing.T, composeFile string) {
	t.Helper()
	repoRoot := repoRoot(t)
	dc := composeCmd(repoRoot, composeFile)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := runCmd(ctx, t, append(dc, "down", "-v", "--remove-orphans")...); err != nil {
		t.Logf("docker compose down failed (non-fatal): %v", err)
	}
}

// aaaSimAddr returns the aaa-sim service's IPv4 address for a given compose network.
// Use this to construct Diameter/RADIUS client connections to aaa-sim from the host.
func aaaSimAddr(service string) string {
	// In the static-IP overlay, aaa-sim always has 172.0.3.14.
	// This is a constant for the test network plan (spec §4.1).
	return "172.0.3.14"
}

// sctpKernelAvailable checks whether the SCTP kernel module is loaded on the host.
// This determines whether SCTP-variant E2E tests can run.
// Returns true if /proc/net/protocols contains an SCTP line.
func sctpKernelAvailable() bool {
	data, err := os.ReadFile("/proc/net/protocols")
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "sctp")
}

// requireSCTP skips the current test if SCTP is unavailable on the host.
func requireSCTP(t *testing.T) {
	t.Helper()
	if !sctpKernelAvailable() {
		t.Skip("SCTP kernel module not available on this host (Linux required for SCTP E2E)")
	}
}

// --- Internal helpers ---

// repoRoot returns the repository root directory for tests.
func repoRoot(t *testing.T) string {
	t.Helper()
	// Walk up from this file's directory to find the repo root.
	const maxDepth = 10
	for entry := range walkUp(t, maxDepth) {
		if entry.depth > maxDepth {
			break
		}
		if _, err := os.Stat(filepath.Join(entry.dir, "go.mod")); err == nil {
			return entry.dir
		}
	}
	t.Fatal("could not find repo root (go.mod not found)")
	return ""
}

// walkUp yields directory paths starting from the test binary's current working
// directory and walking up toward the filesystem root.
func walkUp(t *testing.T, maxDepth int) <-chan walkEntry {
	t.Helper()
	ch := make(chan walkEntry, maxDepth)
	go func() {
		defer close(ch)
		wd, err := os.Getwd()
		if err != nil {
			return
		}
		dir := wd
		for i := 0; i <= maxDepth; i++ {
			ch <- walkEntry{dir: dir, depth: i}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}()
	return ch
}

type walkEntry struct {
	dir   string
	depth int
}

// composeCmd returns the docker compose command slice for a given compose file path,
// relative to the repo root.
func composeCmd(repoRoot string, composeFile string) []string {
	// Use "docker compose" (v2 plugin) if available, fall back to "docker-compose".
	base := "docker"
	pluginCheck := exec.Command("docker", "compose", "version")
	if pluginCheck.Run() == nil {
		base = "docker"
		return []string{base, "compose", "-f", composeFile, "-p", composeProjectName(composeFile)}
	}
	return []string{"docker-compose", "-f", composeFile, "-p", composeProjectName(composeFile)}
}

// composeProjectName returns a stable project name for the given compose file
// so multiple variants can coexist.
func composeProjectName(composeFile string) string {
	base := filepath.Base(composeFile)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.TrimPrefix(base, "fullchain-dev-")
	return "nssaa-" + base
}

// runCmd executes a command and returns an error if it fails.
// stdout and stderr are streamed to the test's log.
func runCmd(ctx context.Context, t *testing.T, args ...string) error {
	t.Helper()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdout = &testLogWriter{t: t, prefix: "  [stdout] "}
	cmd.Stderr = &testLogWriter{t: t, prefix: "  [stderr] "}
	return cmd.Run()
}

type testLogWriter struct {
	t      *testing.T
	prefix string
}

func (w *testLogWriter) Write(p []byte) (int, error) {
	w.t.Logf("%s%s", w.prefix, strings.TrimSpace(string(p)))
	return len(p), nil
}
