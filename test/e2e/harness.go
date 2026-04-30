// Package e2e provides an end-to-end test harness for the 3-component
// NSSAAF architecture: HTTP Gateway, Biz Pod, and AAA Gateway.
//
// Lifecycle model (Phase 4.1 refactor):
//   - docker compose up/down is managed once by TestMain in e2e.go (via Makefile)
//   - NewHarness connects to the pre-started stack; it does NOT manage compose
//   - Each test gets a clean slate via h.ResetState() (TRUNCATE tables + Redis FLUSHDB)
//   - Close() only kills the binary processes, it does NOT tear down compose
//
// Usage (shared harness pattern):
//
//	h := NewHarness(t)       // connects to running stack
//	defer h.Close()
//	h.ResetState()           // clean slate for this test
//	// run assertions
//
// Environment variable overrides:
//
//	BIZ_BINARY      path to the Biz Pod binary (default: ./nssAAF-biz)
//	HTTPGW_BINARY  path to the HTTP Gateway binary (default: ./nssAAF-http-gw)
//	AAAGW_BINARY   path to the AAA Gateway binary (default: ./nssAAF-aaa-gw)
//	DOCKER_COMPOSE files to pass to docker compose (default: -f compose/dev.yaml)
package e2e

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/operator/nssAAF/test/mocks"
	"github.com/redis/go-redis/v9"
	"gopkg.in/yaml.v3"
)

// Harness holds connections to all components started by NewHarness.
type Harness struct {
	t            *testing.T
	config       *HarnessConfig
	httpClient   *http.Client

	httpGWBin    string
	bizBin       string
	aaagwBin     string

	httpGWURL string
	bizURL    string
	aaagwURL  string
	nrmURL    string

	httpGWProcess *os.Process
	bizProcess    *os.Process
	aaagwProcess  *os.Process

	ausfMock *mocks.AUSFMock
	amfMock  *mocks.AMFMock

	pgConn *pgxpool.Pool   // direct PG connection for ResetState
	redis  *redis.Client   // direct Redis connection for ResetState

	mu    sync.Mutex
	clean bool
}

// HarnessConfig holds all infrastructure configuration loaded from harness.yaml.
// All values are expanded from environment variables at load time.
type HarnessConfig struct {
	Infra     InfraConfig     `yaml:"infra"`
	Services  ServicesConfig `yaml:"services"`
	Binaries  BinariesConfig  `yaml:"binaries"`
	TLS       TLSConfig       `yaml:"tls"`
	Timeouts  TimeoutsConfig  `yaml:"timeouts"`
}

type InfraConfig struct {
	PostgresUrl string `yaml:"postgresUrl"`
	RedisUrl    string `yaml:"redisUrl"`
}

type ServicesConfig struct {
	HTTPGatewayUrl string `yaml:"httpGatewayUrl"`
	BizPodUrl      string `yaml:"bizPodUrl"`
	AAAGatewayUrl  string `yaml:"aaaGatewayUrl"`
	NRMUrl         string `yaml:"nrmUrl"`
}

type BinariesConfig struct {
	Biz         string `yaml:"biz"`
	HTTPGateway string `yaml:"httpGateway"`
	AAAGateway  string `yaml:"aaaGateway"`
}

type TLSConfig struct {
	CACert string `yaml:"caCert"`
}

type TimeoutsConfig struct {
	Startup        string `yaml:"startup"`
	HealthCheck    string `yaml:"healthCheck"`
	ShutdownGrace  string `yaml:"shutdownGrace"`
}

// expandEnvVars replaces ${VAR} and ${VAR:-default} patterns in s with
// environment variable values. For ${VAR:-default}: uses default if the env
// var is empty; for ${VAR}: returns the string with the pattern intact if
// the env var is not set (caller validates non-empty).
func expandEnvVars(s string) string {
	// Handle ${VAR:-default} pattern
	s = reEnvDefault.ReplaceAllStringFunc(s, func(match string) string {
		parts := reEnvDefault.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		_, varName, defaultVal := parts[0], parts[1], parts[2]
		if v := os.Getenv(varName); v != "" {
			return v
		}
		return defaultVal
	})

	// Handle ${VAR} pattern (no default)
	s = reEnvVar.ReplaceAllStringFunc(s, func(match string) string {
		parts := reEnvVar.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		varName := parts[1]
		return os.Getenv(varName)
	})

	return s
}

// reEnvDefault matches ${VAR:-default} patterns.
var reEnvDefault = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*):-([^}]*)\}`)

// reEnvVar matches ${VAR} patterns without a default.
var reEnvVar = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// LoadHarnessConfig reads the YAML file at path, expands all environment
// variable placeholders, unmarshals into a HarnessConfig, and validates that
// required fields are non-empty.
func LoadHarnessConfig(path string) (*HarnessConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read harness config %s: %w", path, err)
	}

	// Expand all env var patterns before unmarshaling.
	expanded := expandEnvVars(string(data))

	var cfg HarnessConfig
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse harness config %s: %w", path, err)
	}

	// Validate required fields.
	if cfg.Infra.PostgresUrl == "" {
		return nil, errors.New("harness config: infra.postgresUrl is required (set BIZ_PG_URL)")
	}
	if cfg.Infra.RedisUrl == "" {
		return nil, errors.New("harness config: infra.redisUrl is required (set BIZ_REDIS_URL)")
	}
	if cfg.TLS.CACert == "" {
		return nil, errors.New("harness config: tls.caCert is required (set E2E_TLS_CA)")
	}

	return &cfg, nil
}

// NewHarness connects to a pre-started docker compose stack and launches the
// three binary components. It does NOT start or stop docker compose — that is
// managed once by TestMain in e2e.go (via `make test-e2e`).
//
// The caller must call h.Close() when done.
// Each test case MUST call h.ResetState() before running to ensure a clean DB
// and Redis state.
func NewHarness(t *testing.T) *Harness {
	t.Helper()

	// Load harness config from test/e2e/harness.yaml (next to this file).
	configPath := filepath.Join(filepath.Dir(ofThisFile()), "harness.yaml")
	cfg, err := LoadHarnessConfig(configPath)
	if err != nil {
		t.Fatalf("load harness config: %v", err)
	}

	h := &Harness{
		t:          t,
		config:     cfg,
		httpGWBin:  cfg.Binaries.HTTPGateway,
		bizBin:     cfg.Binaries.Biz,
		aaagwBin:   cfg.Binaries.AAAGateway,
		httpGWURL:  cfg.Services.HTTPGatewayUrl,
		bizURL:     cfg.Services.BizPodUrl,
		aaagwURL:   cfg.Services.AAAGatewayUrl,
		nrmURL:     cfg.Services.NRMUrl,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Initialize custom TLS client for HTTPS health checks.
	if err := h.initTLS(h.config.TLS.CACert); err != nil {
		h.cleanup()
		t.Fatalf("init TLS: %v", err)
	}

	// 1. Establish direct connections to PG and Redis (used by ResetState).
	if err := h.initDBAndRedis(ctx); err != nil {
		t.Fatalf("init DB/Redis: %v", err)
	}

	// 2. Build binaries if not found.
	if err := h.buildBinaries(ctx); err != nil {
		h.cleanup()
		t.Fatalf("build binaries: %v", err)
	}

	// 3. Start AAA Gateway.
	if err := h.startAAAGateway(ctx); err != nil {
		h.cleanup()
		t.Fatalf("start aaa-gateway: %v", err)
	}

	// 4. Start Biz Pod.
	if err := h.startBizPod(ctx); err != nil {
		h.cleanup()
		t.Fatalf("start biz pod: %v", err)
	}

	// 5. Start HTTP Gateway.
	if err := h.startHTTPGateway(ctx); err != nil {
		h.cleanup()
		t.Fatalf("start http-gateway: %v", err)
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

// initDBAndRedis opens a direct pgxpool and Redis client on the harness.
// These connections survive process restarts and are used only by ResetState.
func (h *Harness) initDBAndRedis(ctx context.Context) error {
	pgCfg, err := pgxpool.ParseConfig(h.config.Infra.PostgresUrl)
	if err != nil {
		return fmt.Errorf("parse PG URL: %w", err)
	}
	pgCfg.MaxConns = 2
	pgCfg.MinConns = 1
	h.pgConn, err = pgxpool.NewWithConfig(ctx, pgCfg)
	if err != nil {
		return fmt.Errorf("open PG pool: %w", err)
	}
	if err := h.pgConn.Ping(ctx); err != nil {
		return fmt.Errorf("ping PG: %w", err)
	}

	// BIZ_REDIS_URL may be redis://host:port or just host:port; ParseURL handles both.
	rOpts, err := redis.ParseURL(h.config.Infra.RedisUrl)
	if err != nil {
		return fmt.Errorf("parse Redis URL: %w", err)
	}
	h.redis = redis.NewClient(rOpts)
	if err := h.redis.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping Redis: %w", err)
	}
	return nil
}

// ResetState truncates all session tables and flushes Redis, giving each test
// a clean slate without restarting the process hierarchy. Call this at the
// start of every test that modifies persistent state.
func (h *Harness) ResetState() {
	h.mu.Lock()
	defer h.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Truncate known session tables.
	tables := []string{
		"slice_auth_sessions",
		"aiw_auth_sessions",
		"audit_log",
	}
	for _, tbl := range tables {
		_, err := h.pgConn.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s CASCADE", tbl))
		if err != nil {
			h.t.Logf("TRUNCATE %s (may not exist): %v", tbl, err)
		}
	}

	if err := h.redis.FlushDB(ctx).Err(); err != nil {
		h.t.Logf("FLUSHDB: %v", err)
	}
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

	// Close direct DB/Redis connections used for state resets.
	if h.pgConn != nil {
		h.pgConn.Close()
	}
	if h.redis != nil {
		_ = h.redis.Close()
	}
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
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		out, err := cmd.CombinedOutput()
		if err != nil {
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
	cmd := exec.CommandContext(ctx, h.bizBin, "-config", "compose/configs/biz.yaml")
	cmd.Dir = projectRoot()
	cmd.Env = append(os.Environ(),
		"BIZ_LISTEN=:8080",
		"BIZ_AAA_GW_URL=http://localhost:9090",
		"BIZ_REDIS_URL="+h.config.Infra.RedisUrl,
		"BIZ_PG_URL="+h.config.Infra.PostgresUrl,
		"BIZ_NRM_URL=http://localhost:8081",
		// SoftKeyManager for E2E: 32-byte hex key (Phase 5 D-01)
		"MASTER_KEY_HEX=6767a7ad0416a19ea174608288761dde35dfabba2a8dda9602fc520b80e1af15",
	)
	h.t.Logf("biz: starting %s in %s", h.bizBin, cmd.Dir)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	h.bizProcess = cmd.Process
	h.t.Logf("biz: started pid=%d", cmd.Process.Pid)
	return nil
}

func (h *Harness) startHTTPGateway(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, h.httpGWBin, "-config", "compose/configs/http-gateway.yaml")
	cmd.Dir = projectRoot()
	cmd.Env = append(os.Environ(),
		"HTTP_GW_LISTEN=:8443",
		"HTTP_GW_BIZ_URL=http://localhost:8080",
		"NAF3_AUTH_DISABLED=1", // E2E mode: skip JWT validation
	)
	if err := cmd.Start(); err != nil {
		return err
	}
	h.httpGWProcess = cmd.Process
	return nil
}

// initTLS loads the CA certificate from caPath and builds a custom *http.Client
// that trusts it. This allows HTTPS health checks against self-signed certs.
func (h *Harness) initTLS(caPath string) error {
	caCert, err := os.ReadFile(caPath)
	if err != nil {
		return fmt.Errorf("read CA cert %s: %w", caPath, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		return errors.New("invalid CA certificate in " + caPath)
	}
	h.httpClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
	}
	return nil
}

func (h *Harness) waitHealthy(ctx context.Context, timeout time.Duration) error {
	type service struct {
		name string
		url  string
	}
	svcs := []service{
		{"biz", h.bizURL + "/healthz/ready"},
		{"aaa-gateway", h.aaagwURL + "/health"},
		{"http-gateway", h.httpGWURL + "/healthz/"},
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
				resp, err := h.httpClient.Do(req)
				statusCode := 0
				if resp != nil {
					statusCode = resp.StatusCode
					resp.Body.Close()
				}
				if err != nil || statusCode >= 500 {
					h.t.Logf("%s at %s not healthy (err=%v, status=%d)", s.name, s.url, err, statusCode)
					allUp = false
					break
				} else {
					h.t.Logf("%s at %s healthy (status=%d)", s.name, s.url, statusCode)
				}
			}
			if allUp {
				return nil
			}
		}
	}
}

func projectRoot() string {
	// Walk up from the test binary location to find the module root (where go.mod lives).
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

func ofThisFile() string {
	// Returns the directory containing this source file (harness.go).
	// Used to locate harness.yaml next to the harness.go file.
	_, file, _, _ := runtime.Caller(1)
	return filepath.Dir(file)
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
