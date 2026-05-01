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

// TestMain coordinates the shared lifecycle for the entire E2E test suite:
//
//  1. (if E2E_DOCKER_MANAGED not set) docker compose up — managed by Makefile
//  2. connect to docker compose services  (sharedHarness via NewHarness)
//  3. each test: NewHarnessForTest() → run
//  4. Close() sharedHarness (close DB/Redis connections and mocks)
//  5. (if E2E_DOCKER_MANAGED not set) docker compose down — managed by Makefile
//
// This avoids the O(n) compose cycle cost where each test case would
// independently start and tear down the infrastructure.
//
// Supported environment variables:
//
//	E2E_DOCKER_MANAGED  if set, skip compose up/down (Makefile owns lifecycle)
//	DOCKER_COMPOSE       additional flags for docker compose (e.g. "-f compose/dev.yaml")
//	BIZ_BINARY           path to Biz Pod binary
//	HTTPGW_BINARY       path to HTTP Gateway binary
//	AAAGW_BINARY        path to AAA Gateway binary
func TestMain(m *testing.M) {
	dockerManaged := os.Getenv("E2E_DOCKER_MANAGED") == "1"
	composeFile := os.Getenv("DOCKER_COMPOSE")
	if composeFile == "" {
		composeFile = "-f compose/dev.yaml"
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

	// 2. Create the shared Harness (connects to pre-started compose containers).
	// This harness is reused across all tests. DB/Redis connections are closed
	// once by FinalizeHarness() below, after all tests finish.
	// Individual tests must call NewHarnessForTest() to get a clean slate.
	sharedHarness = NewHarnessForTest(&testing.T{})
	defer func() {
		FinalizeHarness()
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
	// Wait for containers to be healthy (match compose/dev.yaml healthcheck intervals).
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
		// This calls the real NewHarness which waits for docker compose services.
		// Pass a fresh *testing.T so fatal errors don't corrupt the caller's test.
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
