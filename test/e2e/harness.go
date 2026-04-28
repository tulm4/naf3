// Package e2e provides an end-to-end test harness for the 3-component
// NSSAAF architecture: HTTP Gateway, Biz Pod, and AAA Gateway.
//
// The harness starts the full docker-compose stack (PostgreSQL, Redis,
// mock-aaa-s) and the three binary components, then provides helper
// methods for E2E test execution.
//
// Usage:
//
//	h := NewHarness(t)
//	defer h.Close()
//	// use h.HTTPGWURL(), h.BizURL(), etc.
//
// Environment variable overrides:
//
//	BIZ_BINARY      path to the Biz Pod binary (default: ./nssAAF-biz)
//	HTTPGW_BINARY  path to the HTTP Gateway binary (default: ./nssAAF-http-gw)
//	AAAGW_BINARY   path to the AAA Gateway binary (default: ./nssAAF-aaa-gw)
//	DOCKER_COMPOSE files to pass to docker-compose (default: -f compose/dev.yaml -f compose/test.yaml)
package e2e

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/operator/nssAAF/test/mocks"
)

// Harness holds connections to all components started by NewHarness.
type Harness struct {
	t          *testing.T
	composeFile string

	httpGWBin string
	bizBin    string
	aaagwBin  string

	httpGWURL string
	bizURL    string
	aaagwURL  string
	nrmURL    string

	httpGWProcess *os.Process
	bizProcess    *os.Process
	aaagwProcess  *os.Process

	ausfMock *mocks.AUSFMock
	amfMock  *mocks.AMFMock

	mu    sync.Mutex
	clean bool
}

// NewHarness starts the full docker-compose stack, builds and starts the
// three binary components, waits for all services to be healthy, and
// returns a Harness with all connection URLs.
//
// The caller must call h.Close() when done.
func NewHarness(t *testing.T) *Harness {
	t.Helper()

	h := &Harness{
		t:            t,
		composeFile:  getEnv("DOCKER_COMPOSE", "-f compose/dev.yaml -f compose/test.yaml"),
		httpGWBin:    getEnv("HTTPGW_BINARY", "./nssAAF-http-gw"),
		bizBin:       getEnv("BIZ_BINARY", "./nssAAF-biz"),
		aaagwBin:     getEnv("AAAGW_BINARY", "./nssAAF-aaa-gw"),
		httpGWURL:    "http://localhost:8443",
		bizURL:       "http://localhost:8080",
		aaagwURL:     "http://localhost:9090",
		nrmURL:       "http://localhost:8081",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 1. Start docker-compose stack.
	if err := h.upCompose(ctx); err != nil {
		h.cleanup()
		t.Fatalf("compose up failed: %v", err)
	}

	// 2. Build binaries if not found.
	if err := h.buildBinaries(ctx); err != nil {
		h.cleanup()
		t.Fatalf("build binaries failed: %v", err)
	}

	// 3. Start AAA Gateway.
	if err := h.startAAAGateway(ctx); err != nil {
		h.cleanup()
		t.Fatalf("start aaa-gateway failed: %v", err)
	}

	// 4. Start Biz Pod.
	if err := h.startBizPod(ctx); err != nil {
		h.cleanup()
		t.Fatalf("start biz pod failed: %v", err)
	}

	// 5. Start HTTP Gateway.
	if err := h.startHTTPGateway(ctx); err != nil {
		h.cleanup()
		t.Fatalf("start http-gateway failed: %v", err)
	}

	// 6. Wait for all services to be healthy.
	if err := h.waitHealthy(ctx, 2*time.Minute); err != nil {
		h.cleanup()
		t.Fatalf("services not healthy: %v", err)
	}

	t.Log("harness ready",
		"httpgw", h.httpGWURL,
		"biz", h.bizURL,
		"aaagw", h.aaagwURL,
		"nrm", h.nrmURL,
	)

	return h
}

// Close gracefully shuts down all services started by the harness.
func (h *Harness) Close() {
	h.cleanup()
}

func (h *Harness) cleanup() {
	h.mu.Lock()
	if h.clean {
		h.mu.Unlock()
		return
	}
	h.clean = true
	h.mu.Unlock()

	var wg sync.WaitGroup
	if h.httpGWProcess != nil {
		wg.Add(1)
		go func() { _ = killProcess(h.httpGWProcess, 10*time.Second); wg.Done() }()
	}
	if h.bizProcess != nil {
		wg.Add(1)
		go func() { _ = killProcess(h.bizProcess, 10*time.Second); wg.Done() }()
	}
	if h.aaagwProcess != nil {
		wg.Add(1)
		go func() { _ = killProcess(h.aaagwProcess, 10*time.Second); wg.Done() }()
	}
	wg.Wait()

	if h.ausfMock != nil {
		h.ausfMock.Close()
	}
	if h.amfMock != nil {
		h.amfMock.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = h.downCompose(ctx)
}

// BizURL returns the Biz Pod base URL.
func (h *Harness) BizURL() string { return h.bizURL }

// HTTPGWURL returns the HTTP Gateway base URL.
func (h *Harness) HTTPGWURL() string { return h.httpGWURL }

// AAAGWURL returns the AAA Gateway base URL.
func (h *Harness) AAAGWURL() string { return h.aaagwURL }

// NRMURL returns the NRM RESTCONF base URL.
func (h *Harness) NRMURL() string { return h.nrmURL }

// StartAUSFMock starts an AUSF httptest.Server and returns it.
// The returned server should be closed by the caller.
func (h *Harness) StartAUSFMock() *httptest.Server {
	h.ausfMock = mocks.NewAUSFMock()
	return h.ausfMock.Server
}

// StartAMFMock starts an AMF httptest.Server and returns it.
// The returned server should be closed by the caller.
func (h *Harness) StartAMFMock() *httptest.Server {
	h.amfMock = mocks.NewAMFMock()
	return h.amfMock.Server
}

// ─── Internal helpers ───────────────────────────────────────────────────────

func (h *Harness) upCompose(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker-compose", parseComposeFlags(h.composeFile)...)
	cmd.Dir = projectRoot()
	cmd.Env = os.Environ()
	out, _ := cmd.CombinedOutput()
	h.t.Logf("compose up output:\n%s", out)
	return cmd.Wait()
}

func (h *Harness) downCompose(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker-compose", parseComposeFlags(h.composeFile)...)
	cmd.Dir = projectRoot()
	cmd.Env = os.Environ()
	out, _ := cmd.CombinedOutput()
	h.t.Logf("compose down output:\n%s", out)
	return cmd.Wait()
}

func parseComposeFlags(s string) []string {
	var args []string
	var cur string
	for _, tok := range bytes.Fields([]byte(s)) {
		t := string(tok)
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

func (h *Harness) buildBinaries(ctx context.Context) error {
	binaries := []struct {
		bin  string
		cmd  string
		pkg  string
	}{
		{h.aaagwBin, "go build -o %s ./cmd/aaa-gateway/", "aaa-gateway"},
		{h.bizBin, "go build -o %s ./cmd/biz/", "biz"},
		{h.httpGWBin, "go build -o %s ./cmd/http-gateway/", "http-gateway"},
	}

	for _, b := range binaries {
		if _, err := os.Stat(b.bin); err == nil {
			continue
		}
		cmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf(b.cmd, b.bin))
		cmd.Dir = projectRoot()
		out, _ := cmd.CombinedOutput()
		if err := cmd.Wait(); err != nil {
			return fmt.Errorf("build %s: %v\n%s", b.pkg, err, out)
		}
		h.t.Logf("built %s", b.bin)
	}
	return nil
}

func (h *Harness) startAAAGateway(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, h.aaagwBin)
	cmd.Dir = projectRoot()
	cmd.Env = append(os.Environ(),
		"AAA_GW_LISTEN=:9090",
		"AAA_GW_RADIUS_PORT=1812",
		"AAA_GW_DIAMETER_PORT=3868",
	)
	if err := cmd.Start(); err != nil {
		return err
	}
	h.aaagwProcess = cmd.Process
	return nil
}

func (h *Harness) startBizPod(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, h.bizBin)
	cmd.Dir = projectRoot()
	cmd.Env = append(os.Environ(),
		"BIZ_LISTEN=:8080",
		"BIZ_AAA_GW_URL=http://localhost:9090",
		"BIZ_REDIS_URL=redis://localhost:6379",
		"BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable",
		"BIZ_NRM_URL=http://localhost:8081",
	)
	if err := cmd.Start(); err != nil {
		return err
	}
	h.bizProcess = cmd.Process
	return nil
}

func (h *Harness) startHTTPGateway(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, h.httpGWBin)
	cmd.Dir = projectRoot()
	cmd.Env = append(os.Environ(),
		"HTTP_GW_LISTEN=:8443",
		"HTTP_GW_BIZ_URL=http://localhost:8080",
	)
	if err := cmd.Start(); err != nil {
		return err
	}
	h.httpGWProcess = cmd.Process
	return nil
}

func (h *Harness) waitHealthy(ctx context.Context, timeout time.Duration) error {
	type service struct {
		name string
		url  string
	}
	svcs := []service{
		{"http-gateway", h.httpGWURL + "/health"},
		{"biz", h.bizURL + "/health"},
		{"aaa-gateway", h.aaagwURL + "/health"},
		{"nrm", h.nrmURL + "/restconf/data/ietf-yang-library:modules-state"},
	}

	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-deadline.Done():
			return fmt.Errorf("timeout after %v waiting for services", timeout)
		case <-ticker.C:
			allUp := true
			for _, s := range svcs {
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
				resp, err := http.DefaultClient.Do(req)
				statusCode := 0
				if resp != nil {
					statusCode = resp.StatusCode
					resp.Body.Close()
				}
				if err != nil || statusCode >= 500 {
					h.t.Logf("%s at %s not healthy (err=%v, status=%d)", s.name, s.url, err, statusCode)
					allUp = false
					break
				}
			}
			if allUp {
				return nil
			}
		}
	}
}

func projectRoot() string {
	// Walk up from the test binary location to find the module root.
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func killProcess(p *os.Process, grace time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()

	if err := p.Signal(syscall.SIGTERM); err != nil {
		return nil // already dead
	}

	select {
	case <-ctx.Done():
		return p.Kill()
	default:
		st, _ := p.Wait()
		_ = st
		return nil
	}
}
