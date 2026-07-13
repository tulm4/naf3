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
	"fmt"
	"os/exec"
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

// ComposeRunning reports whether the named container is running.
func ComposeRunning(container string) error {
	if container == "" {
		container = "aaa-sim"
	}
	out, err := exec.Command(
		"docker", "inspect", "--format", "{{.State.Running}}", container,
	).Output()
	if err != nil {
		return fmt.Errorf("docker inspect %s: %w", container, err)
	}
	if string(out) != "true\n" {
		return fmt.Errorf("container %s not running: %s", container, out)
	}
	return nil
}
