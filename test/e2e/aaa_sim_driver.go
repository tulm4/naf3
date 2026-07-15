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
	"strings"
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
	// Find the actual container name using docker compose ps.
	containerName := d.findContainer(t)
	args := []string{"exec", containerName, "/app/aaa-sim", cmd,
		"--target", targetAddr,
		"--session-id", sessionID,
	}
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("aaa-sim %s failed: %v\noutput: %s", cmd, err, out)
	}
	t.Logf("aaa-sim %s ok (target=%s session-id=%s): %s",
		cmd, targetAddr, sessionID, out)
}

// findContainer looks up the actual Docker container name for the service.
func (d *AaaSimDriver) findContainer(t *testing.T) string {
	t.Helper()
	// Find the compose file and project directory (same logic as ComposeRunning).
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	var projDir, composeFile string
	for dir := cwd; dir != "." && dir != "/"; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "compose", "fullchain-dev-tcp.yaml")
		if _, err := os.Stat(candidate); err == nil {
			composeFile = candidate
			projDir = dir
			break
		}
	}
	if composeFile == "" {
		cf := os.Getenv("E2E_COMPOSE_FILE")
		if cf == "" {
			cf = "compose/fullchain-dev-tcp.yaml"
		}
		composeFile, _ = filepath.Abs(cf)
		projDir = filepath.Dir(composeFile)
	}

	// Use docker compose ps to find the container name.
	cmd := exec.Command("docker", "compose", "-f", composeFile, "ps", "--format", "{{.Name}}", d.Container)
	cmd.Dir = projDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("docker compose ps %s: %v", d.Container, err)
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		t.Fatalf("no container found for service %s", d.Container)
	}
	return name
}

// ComposeRunning reports whether the named compose service is running.
//
// It runs `docker compose ps` from a directory containing the compose file so
// it works regardless of the compose project prefix (e.g. "compose-" in CI).
func ComposeRunning(service string) error {
	if service == "" {
		service = "aaa-sim"
	}
	// Walk up from CWD looking for a compose file. This works because
	// `go test -C <worktree>` sets the working directory to <worktree>.
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	var projDir, composeFile string
	for dir := cwd; dir != "." && dir != "/"; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "compose", "fullchain-dev-tcp.yaml")
		if _, err := os.Stat(candidate); err == nil {
			composeFile = candidate
			projDir = dir
			break
		}
	}
	if composeFile == "" {
		// Last resort: try E2E_COMPOSE_FILE relative to CWD.
		cf := os.Getenv("E2E_COMPOSE_FILE")
		if cf == "" {
			cf = "compose/fullchain-dev-tcp.yaml"
		}
		composeFile, _ = filepath.Abs(cf)
		projDir = filepath.Dir(composeFile)
	}
	projDir, _ = filepath.Abs(projDir)
	args := []string{"compose", "-f", composeFile, "ps", "--format", "json"}
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
	// docker compose ps --format json outputs one JSON object per line, not an array.
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "{}" {
			continue
		}
		var s struct {
			Service string `json:"Service"`
			State   string `json:"State"`
		}
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			continue
		}
		services = append(services, s)
	}
	if len(services) == 0 && len(strings.TrimSpace(string(out))) > 0 {
		return fmt.Errorf("no services found parsing compose output: %s", strings.TrimSpace(string(out))[:min(200, len(out))])
	}
	for _, s := range services {
		if s.Service == service && (s.State == "running" || s.State == "Up") {
			return nil
		}
	}
	return fmt.Errorf("service %q not running (state=%s)", service, string(out))
}
