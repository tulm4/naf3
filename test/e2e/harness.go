//go:build e2e
// +build e2e

// Package e2e provides an end-to-end test harness for the 3-component
// NSSAAF architecture: HTTP Gateway, Biz Pod, and AAA Gateway.
//
// Lifecycle model (Phase 4.1 refactor):
//   - docker compose up/down is managed once by TestMain in e2e.go (via Makefile)
//   - NewHarness connects to the pre-started docker compose stack; it does NOT
//     start any binary processes
//   - Each test gets a clean slate via h.ResetState() (TRUNCATE tables + Redis FLUSHDB)
//   - Close() only cleans up harness state; it does NOT tear down compose
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
//	BIZ_PG_URL     postgres connection URL
//	BIZ_REDIS_URL  redis connection URL
//	E2E_TLS_CA     path to CA certificate for HTTPS health checks
package e2e

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/operator/nssAAF/internal/storage/postgres"
	"github.com/operator/nssAAF/test/mocks"
	"github.com/redis/go-redis/v9"
	"gopkg.in/yaml.v3"
)

// Harness holds connections to all docker compose containers started by Makefile.
// It does NOT start any binary processes — it only connects to pre-started containers.
type Harness struct {
	t          *testing.T
	config     *HarnessConfig
	httpClient *http.Client

	httpGWURL string
	bizURL    string
	aaagwURL  string
	nrmURL    string

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
	Infra    InfraConfig    `yaml:"infra"`
	Services ServicesConfig `yaml:"services"`
	Binaries BinariesConfig `yaml:"binaries"`
	TLS      TLSConfig      `yaml:"tls"`
	Timeouts TimeoutsConfig `yaml:"timeouts"`
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
	Startup       string `yaml:"startup"`
	HealthCheck   string `yaml:"healthCheck"`
	ShutdownGrace string `yaml:"shutdownGrace"`
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

// NewHarness connects to a pre-started docker compose stack. It does NOT
// start or stop docker compose — that is managed once by TestMain in e2e.go
// (via `make test-e2e`). It also does NOT start any binary processes.
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
		t:         t,
		config:    cfg,
		httpGWURL: cfg.Services.HTTPGatewayUrl,
		bizURL:    cfg.Services.BizPodUrl,
		aaagwURL:  cfg.Services.AAAGatewayUrl,
		nrmURL:    cfg.Services.NRMUrl,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Initialize custom TLS client for HTTPS health checks.
	if err := h.initTLS(h.config.TLS.CACert); err != nil {
		t.Fatalf("init TLS: %v", err)
	}

	// Establish direct connections to PG and Redis (used by ResetState).
	if err := h.initDBAndRedis(ctx); err != nil {
		t.Fatalf("init DB/Redis: %v", err)
	}

	// Wait for all docker compose services to be healthy.
	if err := h.waitHealthy(ctx, 2*time.Minute); err != nil {
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

	// Run pending migrations to ensure schema is up-to-date.
	if err := h.runMigrations(ctx); err != nil {
		return fmt.Errorf("run migrations: %w", err)
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

// runMigrations executes all pending SQL migrations in order.
func (h *Harness) runMigrations(ctx context.Context) error {
	entries, err := fs.ReadDir(postgres.MigrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var filenames []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			filenames = append(filenames, e.Name())
		}
	}
	sort.Strings(filenames)

	for _, name := range filenames {
		data, err := postgres.MigrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if _, err := h.pgConn.Exec(ctx, string(data)); err != nil {
			// Ignore "already exists" errors for idempotent migrations
			if !strings.Contains(err.Error(), "already exists") &&
				!strings.Contains(err.Error(), "duplicate key") &&
				!strings.Contains(err.Error(), "cannot be dropped") {
				return fmt.Errorf("exec %s: %w", name, err)
			}
		}
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

// Close cleans up harness state. It does NOT stop docker compose containers.
func (h *Harness) Close() {
	h.mu.Lock()
	if h.clean {
		h.mu.Unlock()
		return
	}
	h.clean = true
	h.mu.Unlock()

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
		{"nrm", h.nrmURL + "/healthz"},
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

// ofThisFile returns the directory containing the harness.go source file.
// Used to locate harness.yaml next to the test file.
func ofThisFile() string {
	_, file, _, _ := runtime.Caller(1)
	return filepath.Dir(file)
}
