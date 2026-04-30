---
phase: quick-260430-qey
plan: "01"
type: execute
wave: "1"
depends_on: []
files_modified:
  - test/e2e/harness.go
  - test/e2e/harness.yaml
  - Makefile
autonomous: true
requirements: []

must_haves:
  truths:
    - "E2E harness loads infra URLs from harness.yaml with env var expansion, no hardcoded fallbacks"
    - "HTTPS health checks to HTTP Gateway use custom CA cert pool for self-signed TLS"
    - "`make test-e2e` passes E2E_TLS_CA, BIZ_PG_URL, BIZ_REDIS_URL to harness"
  artifacts:
    - path: test/e2e/harness.yaml
      provides: "YAML config with infra URLs, binary paths, TLS CA path, timeouts"
      min_lines: 30
    - path: test/e2e/harness.go
      provides: "Harness reads harness.yaml, builds HTTPS client with CA pool"
      min_lines: 50
    - path: Makefile
      provides: "test-e2e passes required env vars"
      min_lines: 5
  key_links:
    - from: test/e2e/harness.go
      to: test/e2e/harness.yaml
      via: LoadHarnessConfig reads file and expands ${VAR} patterns
      pattern: "LoadHarnessConfig"
    - from: test/e2e/harness.go
      to: /tmp/e2e-tls/server.crt
      via: initTLS reads CA cert and builds custom http.Client with x509 CertPool
      pattern: "x509.NewCertPool"
    - from: Makefile
      to: test/e2e/harness.go
      via: E2E_TLS_CA, BIZ_PG_URL, BIZ_REDIS_URL env vars injected into test process
      pattern: "E2E_TLS_CA"
---

<objective>
Refactor the E2E harness to load all infrastructure configuration from a YAML file (no hardcoded URL fallbacks), add HTTPS/TLS support with a custom CA cert pool, and fix `make test-e2e` to pass the required environment variables.
</objective>

<execution_context>
@$HOME/.cursor/get-shit-done/workflows/execute-plan.md
</execution_context>

<context>
@test/e2e/harness.go
@test/e2e/e2e.go
@compose/configs/biz.yaml

**Locked Decisions (from RESEARCH.md):**
- D-01: Makefile owns docker compose lifecycle; harness NEVER calls compose
- D-02: Custom CA cert pool from `/tmp/e2e-tls/server.crt` for HTTPS clients
- D-03: YAML config file with environment variable overrides; no hardcoded fallbacks
- D-04: Use `gopkg.in/yaml.v3` (already in go.mod); no viper

**Key code locations:**
- `pgURL()` at harness.go:71-76 — hardcoded postgres URL fallback (must be removed)
- `redisURL()` at harness.go:79-84 — hardcoded Redis URL fallback (must be removed)
- `startBizPod()` at harness.go:334-335 — hardcoded BIZ_PG_URL and BIZ_REDIS_URL env values (must read from config)
- `waitHealthy()` at harness.go:390 — uses `http.DefaultClient` for HTTPS health check (must use custom TLS client)
- Makefile `test-e2e` target at line 151 — missing E2E_TLS_CA, BIZ_PG_URL, BIZ_REDIS_URL env vars

**CA cert path:** `/tmp/e2e-tls/server.crt` (set by Makefile `gen-certs` target)
**Compose PG URL:** `postgres://nssaa:nssaa@postgres:5432/nssaa?sslmode=disable`
**Compose Redis URL:** `redis://redis:6379`
</context>

<tasks>

<task type="auto">
  <name>Task 1: Create harness.yaml config and YAML loading code</name>
  <files>test/e2e/harness.yaml, test/e2e/harness.go</files>
  <action>
Create `test/e2e/harness.yaml` with this structure (env var expansion via ${VAR} syntax):

```yaml
# test/e2e/harness.yaml — E2E test infrastructure configuration
# All values come from env vars; no hardcoded fallbacks.
# Missing required env vars cause a fatal error at startup.

infra:
  postgresUrl: "${BIZ_PG_URL}"
  redisUrl: "${BIZ_REDIS_URL}"

services:
  httpGatewayUrl: "https://localhost:8443"
  bizPodUrl: "http://localhost:8080"
  aaaGatewayUrl: "http://localhost:9090"
  nrmUrl: "http://localhost:8081"

binaries:
  biz: "${BIZ_BINARY:-./bin/biz}"
  httpGateway: "${HTTPGW_BINARY:-./bin/http-gateway}"
  aaaGateway: "${AAAGW_BINARY:-./bin/aaa-gateway}"

tls:
  caCert: "${E2E_TLS_CA:-/tmp/e2e-tls/server.crt}"

timeouts:
  startup: "2m"
  healthCheck: "5s"
  shutdownGrace: "10s"
```

Then modify `test/e2e/harness.go` to add:

1. A `HarnessConfig` struct with the same fields (Infra, Services, Binaries, TLS, Timeouts nested structs).

2. An `expandEnvVars(s string) string` function that replaces `${VAR}` and `${VAR:-default}` patterns with `os.Getenv` values. For `${VAR:-default}` syntax: if the env var is empty, use the default; if no default is present, fail with an error. This function is called during YAML load so all values are expanded before unmarshaling.

3. A `LoadHarnessConfig(path string) (*HarnessConfig, error)` function that reads the YAML file, calls `expandEnvVars` on the raw bytes, unmarshals into `HarnessConfig`, and validates that required fields (postgresUrl, redisUrl) are non-empty — if empty, return an error.

4. In `NewHarness(t)`: load the config from `test/e2e/harness.yaml` (next to the harness.go file), use config values to populate Harness struct fields (binary paths, URLs), and remove the hardcoded `pgURL()` and `redisURL()` helper functions (replaced by config).

5. Import `gopkg.in/yaml.v3` (already in go.mod — verify with `grep yaml go.mod`; if not present, add it).
</action>
  <verify>
<automated>cd /home/tulm/naf3 && go build -o /dev/null ./test/e2e/... 2>&1 | head -20</automated>
  </verify>
  <done>Harness loads from test/e2e/harness.yaml with env expansion; pgURL()/redisURL() hardcoded fallbacks removed; missing required env vars cause fatal error</done>
</task>

<task type="auto">
  <name>Task 2: Add HTTPS TLS client with custom CA cert pool</name>
  <files>test/e2e/harness.go</files>
  <action>
Modify `Harness` struct to add an `httpClient *http.Client` field (custom TLS client for HTTPS).

Add an `initTLS(caPath string) error` method to Harness:

```go
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
```

In `NewHarness(t)`, after loading config and before `initDBAndRedis`, call `h.initTLS(h.config.TLS.CACert)`. If initTLS fails, call `h.cleanup()` and `t.Fatalf`.

In `waitHealthy()`, replace `http.DefaultClient` with `h.httpClient` for ALL health check requests (including the HTTP Gateway HTTPS endpoint). Keep the same retry/logic structure.

Add imports: `crypto/tls`, `crypto/x509`, `errors` (if not already present).
</action>
  <verify>
<automated>cd /home/tulm/naf3 && go build -o /dev/null ./test/e2e/... 2>&1</automated>
  </verify>
  <done>HTTPS health checks to HTTP Gateway use custom CA cert pool; self-signed TLS certs at /tmp/e2e-tls/server.crt are trusted</done>
</task>

<task type="auto">
  <name>Task 3: Fix Makefile test-e2e to pass required env vars</name>
  <files>Makefile</files>
  <action>
Update the `test-e2e` target in Makefile to pass three additional env vars to the test process:

```
E2E_TLS_CA=/tmp/e2e-tls/server.crt \
BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
BIZ_REDIS_URL=redis://localhost:6379 \
```

The updated target should look like:

```makefile
test-e2e: gen-certs build ## Build binaries then run E2E tests
	@echo "$(YELLOW)Starting docker compose infrastructure...$(NC)"
	docker compose -f compose/dev.yaml up -d --quiet-pull
	@sleep 10
	E2E_DOCKER_MANAGED=1 \
	E2E_TLS_CA=/tmp/e2e-tls/server.crt \
	BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
	BIZ_REDIS_URL=redis://localhost:6379 \
	BIZ_BINARY=$(BIZ_BINARY) \
	HTTPGW_BINARY=$(HTTPGW_BINARY) \
	AAAGW_BINARY=$(AAAGW_BINARY) \
	$(GOTEST) -v -count=1 \
		./test/e2e/... \
		|| { docker compose -f compose/dev.yaml down --remove-orphans; exit 1; }
	@echo "$(YELLOW)Tearing down docker compose infrastructure...$(NC)"
	docker compose -f compose/dev.yaml down --remove-orphans
	@echo "$(GREEN)E2E tests complete$(NC)"
```

Note: The `BIZ_PG_URL` and `BIZ_REDIS_URL` values here are for local `make test-e2e` (direct docker compose on localhost). For CI with docker compose service names, override these env vars at the CI level. The harness itself will fail fast if these are missing.
</action>
  <verify>
<automated>grep -A 20 "^test-e2e:" Makefile</automated>
  </verify>
  <done>`make test-e2e` passes E2E_TLS_CA, BIZ_PG_URL, BIZ_REDIS_URL to harness; harness starts without hardcoded fallbacks</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| Harness → HTTP Gateway (HTTPS) | Untrusted network; TLS cert validation occurs here |
| Harness → config file | Trusted file in repo; env vars are untrusted input |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|----------------|
| T-qey-01 | Tampering | harness.yaml | accept | File is version-controlled; untrusted env vars are data-only |
| T-qey-02 | Information Disclosure | BIZ_PG_URL in env | accept | Test infra only; not production credentials |
</threat_model>

<verification>
Run `make test-e2e` and verify all services start and health checks pass (no TLS handshake errors).
</verification>

<success_criteria>
- `test/e2e/harness.yaml` exists with env var placeholders and no hardcoded fallbacks
- `pgURL()` and `redisURL()` hardcoded fallbacks removed from harness.go
- Harness builds a custom `*http.Client` with CA cert pool from `E2E_TLS_CA`
- `waitHealthy()` uses the custom HTTPS client for the HTTP Gateway health check
- `make test-e2e` passes E2E_TLS_CA, BIZ_PG_URL, BIZ_REDIS_URL env vars to test process
- `go build ./test/e2e/...` compiles without errors
</success_criteria>

<output>
After completion, create `.planning/quick/260430-qey-refactor-e2e-harness-to-use-docker-compo/260430-qey-SUMMARY.md`
</output>
