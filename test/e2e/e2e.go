//go:build e2e
// +build e2e

// Package e2e provides end-to-end integration tests for the NSSAAF system.
package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// composeManaged is set to true once TestMain has brought up docker compose.
var composeManaged int32

// sharedHarness is the single Harness instance shared by all tests in this run.
var sharedHarness *Harness

// sharedDriver is the driver selected by TestMain based on E2E_PROFILE.
var sharedDriver Driver

// TestMain coordinates the shared lifecycle for the entire E2E test suite:
//
//  1. (if E2E_DOCKER_MANAGED not set) docker compose up — managed by Makefile
//  2. select driver (ContainerDriver with compose/fullchain-dev.yaml)
//  3. connect to docker compose services (sharedHarness via NewHarnessFromDriver)
//  4. each test: NewHarnessForTest() → run
//  5. Close() sharedHarness (close DB/Redis connections and mocks)
//  6. (if E2E_DOCKER_MANAGED not set) docker compose down — managed by Makefile
//
// This avoids the O(n) compose cycle cost where each test case would
// independently start and tear down the infrastructure.
//
// Supported environment variables:
//
//	E2E_DOCKER_MANAGED  if set, skip compose up/down (Makefile owns lifecycle)
//	E2E_PROFILE         "fullchain" (default) — ContainerDriver + fullchain-dev.yaml
//	DOCKER_COMPOSE      additional flags for docker compose (e.g. "-f compose/other.yaml")
func TestMain(m *testing.M) {
	dockerManaged := os.Getenv("E2E_DOCKER_MANAGED") == "1"
	profile := os.Getenv("E2E_PROFILE")

	// Determine compose file and driver.
	// Default: fullchain profile with ContainerDriver + compose/fullchain-dev.yaml.
	// E2E_PROFILE is retained for backward compatibility with skip logic in test files.
	var composeFile string
	if profile == "" {
		profile = "fullchain"
	}
	if profile == "fullchain" {
		composeFile = "-f compose/fullchain-dev.yaml"
		sharedDriver = NewContainerDriver()
		if sharedDriver == nil {
			fmt.Fprintf(os.Stderr, "E2E_PROFILE=fullchain but FULLCHAIN_NRF_URL is not set\n")
			fmt.Fprintf(os.Stderr, "Set FULLCHAIN_NRF_URL and FULLCHAIN_UDM_URL environment variables\n")
			os.Exit(1)
		}
	} else {
		// E2E_PROFILE=mock: in-process mocks only (unit-level testing).
		// Requires mock_driver.go which will be removed.
		fmt.Fprintf(os.Stderr, "E2E_PROFILE=%q is no longer supported; use E2E_PROFILE=fullchain\n", profile)
		os.Exit(1)
	}

	// Override compose file via DOCKER_COMPOSE env var if set.
	if envCompose := os.Getenv("DOCKER_COMPOSE"); envCompose != "" {
		composeFile = envCompose
	}

	// 1. Bring up docker compose if not already managed by Makefile.
	if !dockerManaged {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := dockerComposeUp(ctx, composeFile); err != nil {
			dockerComposeDown(context.Background(), composeFile)
			os.Exit(1)
		}
		atomic.StoreInt32(&composeManaged, 1)
	}

	// 2. Create the shared Harness with the selected driver.
	// This harness is reused across all tests. DB/Redis connections are closed
	// once by FinalizeHarness() below, after all tests finish.
	sharedHarness = NewHarnessFromDriver(&testing.T{}, sharedDriver)
	defer func() {
		FinalizeHarness()
		if sharedDriver != nil {
			sharedDriver.Close()
		}
	}()

	// 3. Run all tests.
	code := m.Run()

	// 4. Tear down docker compose if we own the lifecycle.
	if !dockerManaged && atomic.LoadInt32(&composeManaged) == 1 {
		dockerComposeDown(context.Background(), composeFile)
	}

	os.Exit(code)
}

// dockerComposeUp runs `docker compose up -d` and waits for the infrastructure
// containers to become healthy.
func dockerComposeUp(ctx context.Context, composeFile string) error {
	args := append(parseComposeFlags(composeFile), "up", "-d", "--quiet-pull")
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	cmd.Dir = projectRoot()
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("compose up: %v\n%s", err, out)
	}
	// Wait for containers to be healthy (match fullchain-dev.yaml healthcheck intervals).
	time.Sleep(10 * time.Second)
	return nil
}

// dockerComposeDown runs `docker compose down` and kills the process group on timeout.
func dockerComposeDown(ctx context.Context, composeFile string) {
	args := append(parseComposeFlags(composeFile), "down", "--remove-orphans")
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	cmd.Dir = projectRoot()
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	_ = cmd.Run()
	if ctx.Err() != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// SharedHarness returns the singleton Harness created by TestMain.
// If compose could not be started, it returns nil.
func SharedHarness() *Harness { return sharedHarness }

// ─── Exported helpers used by test cases ────────────────────────────────────

// sharedHarnessMu protects lazy access to sharedHarness within a test.
// A test calls NewHarness(t) which returns sharedHarness and calls ResetState.
var sharedHarnessMu sync.Mutex

// NewHarnessForTest returns the shared harness and calls ResetState.
// Call this at the start of every test case instead of NewHarness(t).
//
// On first call, this function creates the shared harness by calling NewHarness.
// Subsequent calls reuse the same instance.
//
// Example:
//
//	h := NewHarnessForTest(t)   // shared, resets state
//	defer h.Close()             // closes mocks; does NOT restart binaries
func NewHarnessForTest(t *testing.T) *Harness {
	t.Helper()
	sharedHarnessMu.Lock()
	defer sharedHarnessMu.Unlock()
	if sharedHarness == nil {
		// First call: lazily initialize the shared harness.
		// This calls NewHarness which uses the default MockDriver.
		// TestMain sets sharedHarness directly, so this is only for
		// direct binary runs (not through TestMain).
		sharedHarness = NewHarness(&testing.T{})
	}
	sharedHarness.t = t
	sharedHarness.ResetState()
	return sharedHarness
}

// parseComposeFlags splits a "docker compose" flag string into individual tokens,
// handling -f/--file specially so that the compose file path is a separate arg.
func parseComposeFlags(s string) []string {
	var args []string
	var cur string
	for _, tok := range splitFields(s) {
		t := tok
		if t == "-f" || t == "--file" {
			cur = t
		} else if cur == "-f" || cur == "--file" {
			args = append(args, cur, t)
			cur = ""
		} else {
			args = append(args, t)
		}
	}
	return args
}

func splitFields(s string) []string {
	var out []string
	var word []rune
	inQuote := false
	for _, r := range s {
		if r == '"' || r == '\'' {
			inQuote = !inQuote
			continue
		}
		if !inQuote && r == ' ' || r == '\t' || r == '\n' {
			if len(word) > 0 {
				out = append(out, string(word))
				word = nil
			}
		} else {
			word = append(word, r)
		}
	}
	if len(word) > 0 {
		out = append(out, string(word))
	}
	return out
}

// projectRoot returns the module root (where go.mod lives) by walking up from cwd.
func projectRoot() string {
	if cwd, err := os.Getwd(); err == nil {
		for dir := cwd; dir != "/"; dir = filepath.Dir(dir) {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return dir
			}
		}
		return cwd
	}
	return "."
}
