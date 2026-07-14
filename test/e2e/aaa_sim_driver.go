// aaa_sim_driver shells out to aaa-sim running inside the compose network
// to trigger server-initiated RADIUS flows (RAR / ASR) from E2E tests.
//
// aaa-sim exposes two one-shot subcommands — `trigger-rar` and `trigger-asr` —
// that bind a UDP socket, build a CoA-Request / Disconnect-Request (RFC 5176)
// with a random Request Authenticator and a valid Message-Authenticator, and
// send it to a target (typically the aaa-gateway RADIUS listener). This
// driver wraps those subcommands in `docker exec` so integration tests can
// exercise the AAA-S → AAA-Client reverse flow without coupling to the
// aaa-sim source.
package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// AaaSimDriver wraps shell-out to the aaa-sim container.
type AaaSimDriver struct {
	// Container is the docker compose service / container name for aaa-sim.
	// Defaults to "aaa-sim".
	Container string
}

// NewAaaSimDriver returns a driver; container defaults to "aaa-sim" when empty.
func NewAaaSimDriver(container string) *AaaSimDriver {
	if container == "" {
		container = "aaa-sim"
	}
	return &AaaSimDriver{Container: container}
}

// TriggerRAR asks aaa-sim to send a RADIUS Re-Auth-Request (code 43) to
// targetAddr for the given sessionID.
//
// targetAddr is the aaa-gateway RADIUS address from the aaa-sim container's
// perspective (default compose network: 172.0.3.15:1812).
func (d *AaaSimDriver) TriggerRAR(t *testing.T, sessionID, targetAddr string) {
	t.Helper()
	d.trigger(t, "trigger-rar", sessionID, targetAddr)
}

// TriggerASR asks aaa-sim to send a RADIUS Abort-Session-Request (code 44)
// to targetAddr for the given sessionID.
func (d *AaaSimDriver) TriggerASR(t *testing.T, sessionID, targetAddr string) {
	t.Helper()
	d.trigger(t, "trigger-asr", sessionID, targetAddr)
}

func (d *AaaSimDriver) trigger(t *testing.T, cmd, sessionID, targetAddr string) {
	t.Helper()
	out, err := exec.Command(
		"docker", "exec", d.Container, "aaa-sim", cmd,
		"--target", targetAddr,
		"--session-id", sessionID,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("aaa-sim %s failed: %v\noutput: %s", cmd, err, out)
	}
	t.Logf("aaa-sim %s ok (target=%s session-id=%s): %s",
		cmd, targetAddr, sessionID, out)
}

// ComposeRunning reports whether the named compose service is running.
//
// It runs `docker compose ps` from the test binary's working directory so it
// works regardless of the compose project prefix (e.g. "compose-" in CI).
func ComposeRunning(service string) error {
	if service == "" {
		service = "aaa-sim"
	}
	// Use os.Executable() to find the test binary's directory, then walk up
	// to find the compose file. This is more robust than parsing E2E_COMPOSE_FILE
	// because the working directory of `go test -C <dir>` is <dir>.
	execPath, err := os.Executable()
	var projDir string
	if err == nil {
		// Try to find a compose file by walking up from the test binary.
		// The binary is in <worktree>/bin/ or similar; the compose file
		// should be in <worktree>/compose/.
		for dir := filepath.Dir(execPath); dir != "." && dir != "/"; dir = filepath.Dir(dir) {
			candidate := filepath.Join(dir, "compose", "fullchain-dev-tcp.yaml")
			if _, err := os.Stat(candidate); err == nil {
				projDir = filepath.Join(dir, "compose")
				break
			}
		}
	}
	// Fallback to E2E_COMPOSE_FILE env var if directory detection failed.
	composeFile := os.Getenv("E2E_COMPOSE_FILE")
	if composeFile == "" {
		composeFile = "compose/fullchain-dev-tcp.yaml"
	}
	if projDir == "" && composeFile != "" {
		projDir = filepath.Dir(composeFile)
	}
	args := []string{"compose", "ps", "--format", "json"}
	cmd := exec.Command("docker", args...)
	cmd.Dir = projDir
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("docker compose ps: %w\nstderr: %s", err, string(out))
	}
	var services []struct {
		Service string `json:"Service"`
		State   string `json:"State"`
	}
	if err := json.Unmarshal(out, &services); err != nil {
		if len(out) > 0 {
			return fmt.Errorf("parse docker compose ps json: %w output=%s", err, string(out))
		}
	}
	for _, s := range services {
		if s.Service == service && (s.State == "running" || s.State == "Up") {
			return nil
		}
	}
	return fmt.Errorf("service %q not running (state=%s)", service, string(out))
}
