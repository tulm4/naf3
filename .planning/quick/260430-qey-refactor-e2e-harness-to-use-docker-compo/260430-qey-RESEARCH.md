# Phase quick: E2E Harness Refactor — Research

**Researched:** 2026-04-30
**Domain:** Go E2E test infrastructure, TLS/HTTPS in Go, YAML config loading
**Confidence:** HIGH

## Summary

The existing E2E harness in `test/e2e/harness.go` conflates infrastructure config (hardcoded fallbacks in `pgURL()`/`redisURL()`) with lifecycle management. The refactor goal is to:

1. **Replace hardcoded URL fallbacks with YAML config** — `pgURL()` (lines 71-76) and `redisURL()` (lines 79-84) have hardcoded defaults (`postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable`, `localhost:6379`); these must come from a YAML config file with no hardcoded fallbacks
2. **Add HTTPS TLS client for health checks** — `http.DefaultClient` at line 390 is used for `https://localhost:8443` health checks; this will fail TLS verification with self-signed certs from `/tmp/e2e-tls/`
3. **Align Makefile and harness binary paths** — both already use `./bin/{biz,http-gateway,aaa-gateway}` (confirmed in harness.go lines 100-102 and Makefile lines 32-34)
4. **Fix `make test-e2e`** — likely failing due to TLS handshake on HTTPS health checks, not binary paths

The primary failure mode is: `http.DefaultClient` cannot verify the self-signed TLS cert at `https://localhost:8443`, causing `waitHealthy()` to timeout.

## User Constraints

### Locked Decisions
- Makefile owns docker compose lifecycle; harness NEVER calls compose
- Custom CA cert pool from `/tmp/e2e-tls/server.crt` for HTTPS clients
- YAML config file with environment variable overrides; no hardcoded fallbacks
- No new external dependencies (use stdlib + `gopkg.in/yaml.v3` already in go.mod)

### Out of Scope
- Changing the docker compose service definitions
- Modifying component binary interfaces or config schemas
- Adding integration test coverage beyond fixing the harness

## Standard Stack

| Library | Version | Purpose | Status |
|---------|---------|---------|--------|
| `crypto/x509` | stdlib | Custom CA cert pool | already available |
| `crypto/tls` | stdlib | TLS config for HTTPS clients | already available |
| `gopkg.in/yaml.v3` | latest | Direct YAML decode | already in go.mod |
| `net/http` | stdlib | HTTP client | already available |

**Decision:** Use `gopkg.in/yaml.v3` directly. No viper dependency needed — it is heavy for test-only use and requires global state. Direct YAML decode matches the project's existing pattern in `compose/configs/`.

## Go HTTPS Client with Custom CA Cert

The canonical pattern for a Go HTTP client that trusts a self-signed cert:

```go
import (
    "crypto/tls"
    "crypto/x509"
    "net/http"
    "os"
)

func newHTTPSClient(caCertPath string) (*http.Client, error) {
    caCert, err := os.ReadFile(caCertPath)
    if err != nil {
        return nil, fmt.Errorf("read CA cert: %w", err)
    }
    caCertPool := x509.NewCertPool()
    if !caCertPool.AppendCertsFromPEM(caCert) {
        return nil, errors.New("failed to append CA cert")
    }
    return &http.Client{
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{
                RootCAs: caCertPool,
            },
        },
    }, nil
}
```

**Key gotcha:** `http.DefaultClient` has `nil` `TLSClientConfig`, meaning it uses the system cert pool. The harness must use a custom `*http.Client` for HTTPS requests to the HTTP Gateway.

The existing harness code at line 390 uses `http.DefaultClient` for health checks against `https://localhost:8443`. This will fail TLS verification with a self-signed cert. Solution: construct a custom client with the CA pool and use it for both the HTTP Gateway health check and any test HTTP requests.

## YAML Harness Config

The harness config should mirror the existing component config style (e.g., `compose/configs/biz.yaml`). Structure:

```yaml
# test/e2e/harness.yaml
# Loaded by the E2E harness; env vars override these values.
# NO hardcoded fallbacks — all values must come from env or config.

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

**Loading with env var substitution** — implement a simple `expandEnvVars()` helper:

```go
// expandEnvVars replaces ${VAR} and $VAR patterns with os.Getenv values.
// Matches the ${VAR:-default} pattern used in compose/configs/*.yaml.
func expandEnvVars(s string) string {
    re := regexp.MustCompile(`\$\{([^}]+)\}`)
    return re.ReplaceAllStringFunc(s, func(match string) string {
        key := match[2 : len(match)-1] // strip ${}
        return os.Getenv(key)
    })
}
```

**Note:** For proper `${VAR:-default}` support, parse the key portion to extract variable name and optional default. If the env var is empty and no default exists, fail fast — per "no hardcoded fallbacks" constraint.

## Lifecycle Separation

The current architecture already has the right separation intent documented in harness.go lines 4-8:

```
//   - docker compose up/down is managed once by TestMain in e2e.go (via Makefile)
//   - NewHarness connects to the pre-started stack; it does NOT manage compose
//   - Each test gets a clean slate via h.ResetState() (TRUNCATE tables + Redis FLUSHDB)
//   - Close() only kills the binary processes, it does NOT tear down compose
```

Binary path defaults are already aligned:
- Harness: `./bin/{biz,http-gateway,aaa-gateway}` (lines 100-102)
- Makefile: `bin/{biz,http-gateway,aaa-gateway}` (lines 32-34, `BINARY_DIR = bin`)

**No binary path changes needed.** The actual hardcoded fallbacks that need fixing are:
- `pgURL()` hardcoded default at line 75: `postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable`
- `redisURL()` hardcoded default at line 83: `localhost:6379`
- `startBizPod()` hardcoded env at line 335: `BIZ_PG_URL=postgres://...`
- `startBizPod()` hardcoded env at line 334: `BIZ_REDIS_URL=redis://localhost:6379`

## HTTP Gateway HTTPS Client Fix

The harness uses `http.DefaultClient` for health checks against `https://localhost:8443`. This will fail TLS verification because the cert at `/tmp/e2e-tls/server.crt` is self-signed and `http.DefaultClient` uses the system CA cert pool.

**Solution:** Add a `tlsConfig` field to `Harness` and construct a custom `http.Client`:

```go
type Harness struct {
    t *testing.T
    // ... existing fields ...
    httpClient *http.Client // custom TLS client for HTTPS
}

func (h *Harness) initTLS(caPath string) error {
    caCert, err := os.ReadFile(caPath)
    if err != nil {
        return fmt.Errorf("read CA: %w", err)
    }
    pool := x509.NewCertPool()
    if !pool.AppendCertsFromPEM(caCert) {
        return errors.New("invalid CA certificate")
    }
    h.httpClient = &http.Client{
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{RootCAs: pool},
        },
    }
    return nil
}
```

Then in `waitHealthy()`, replace `http.DefaultClient` with `h.httpClient` for the HTTP Gateway health check.

## Makefile test-e2e Issues

The current `test-e2e` target:

```makefile
test-e2e: gen-certs build ## Build binaries then run E2E tests
    @echo "$(YELLOW)Starting docker compose infrastructure...$(NC)"
    docker compose -f compose/dev.yaml up -d --quiet-pull
    @sleep 10
    E2E_DOCKER_MANAGED=1 \
    BIZ_BINARY=$(BIZ_BINARY) \
    HTTPGW_BINARY=$(HTTPGW_BINARY) \
    AAAGW_BINARY=$(AAAGW_BINARY) \
    $(GOTEST) -v -count=1 ./test/e2e/... \
        || { docker compose -f compose/dev.yaml down --remove-orphans; exit 1; }
    @docker compose -f compose/dev.yaml down --remove-orphans
```

**Current issues:**

1. `sleep 10` is fragile — containers may not be healthy yet. Should use a health-check loop.
2. `E2E_TLS_CA` env var not passed — harness can't find the CA cert path to configure TLS client.
3. `BIZ_PG_URL` and `BIZ_REDIS_URL` not passed — harness falls back to hardcoded values.

**Fixes needed:**
- Add `E2E_TLS_CA=/tmp/e2e-tls/server.crt` to the env block
- Add `BIZ_PG_URL` and `BIZ_REDIS_URL` pointing to docker-compose service addresses
- Optionally replace `sleep 10` with a health-check polling loop

## Config File Location

The harness config should live at `test/e2e/harness.yaml` (next to the harness code), not `compose/configs/harness.yaml` — it is test-only infrastructure, not deployment config.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| TLS cert loading | Custom cert-parsing | `x509.NewCertPool()` + `AppendCertsFromPEM()` | Handles PEM/DER, expiry, chain |
| HTTPS client | Re-implement HTTP client | stdlib `http.Client` with `TLSClientConfig` | Correct connection pooling, timeouts |
| YAML config | Custom YAML parser | `gopkg.in/yaml.v3` | Already in go.mod |

## Common Pitfalls

1. **`http.DefaultClient` with self-signed certs** — using `http.DefaultClient` for HTTPS health checks silently fails or returns `x509: certificate signed by unknown authority`. Always construct a custom `*http.Client` with `TLSClientConfig`.

2. **Hardcoded fallbacks** — the harness has two `getEnv(key, default)` patterns: binary paths (correct defaults) and infra URLs (incorrect defaults). The infra URL defaults must be removed; missing env vars should be a fatal error.

3. **Goroutine leaks from Redis subscription** — `httpAAAClient.subscribeResponses` runs in a goroutine started in `newHTTPAAAClient`. The harness `Close()` calls `httpAAAClient.Close()` which closes the Redis client but does not stop the subscription goroutine. The subscription goroutine should be cancelled via a context with deadline.

4. **Process group cleanup** — `killProcess` sends `SIGTERM` but binary processes may spawn their own subprocesses. The `exec.Command` calls in `startBizPod`, `startHTTPGateway`, and `startAAAGateway` already set `SysProcAttr{Setpgid: true}` in `buildBinaries` only. This needs to be set on all `exec.Command` calls that start processes, not just the build step.

5. **Compose health wait** — `docker compose up -d --quiet-pull` then `sleep 10` is fragile. Use `docker compose ps --format json` with a polling loop instead, matching the pattern used in `waitHealthy()`.

## Code Examples

### YAML Config Loading

```go
type HarnessConfig struct {
    Infra    InfraConfig    `yaml:"infra"`
    Services ServicesConfig `yaml:"services"`
    Binaries BinariesConfig `yaml:"binaries"`
    TLS      TLSConfig      `yaml:"tls"`
    Timeouts TimeoutsConfig `yaml:"timeouts"`
}

func LoadHarnessConfig(path string) (*HarnessConfig, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    expanded := expandEnvVars(string(data))
    var cfg HarnessConfig
    if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
        return nil, err
    }
    return &cfg, nil
}
```

### TLS Custom CA Pool

```go
func buildHTTPSClient(caPath string) (*http.Client, error) {
    caCert, err := os.ReadFile(caPath)
    if err != nil {
        return nil, fmt.Errorf("read CA: %w", err)
    }
    pool := x509.NewCertPool()
    if !pool.AppendCertsFromPEM(caCert) {
        return nil, errors.New("invalid CA certificate")
    }
    return &http.Client{
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{RootCAs: pool},
        },
    }, nil
}
```

## Open Questions

1. **NRM in harness:** The harness has `nrmURL` pointing to `localhost:8081` but `compose/dev.yaml` doesn't include an NRM service. Should NRM be added to the test infrastructure or excluded from harness scope?

2. **smoke_manual_test.go URLs:** This file has hardcoded `http://localhost:8443`, `http://localhost:8080`, `http://localhost:8081` constants. Should these be replaced with harness config or kept as-is for manual testing convenience?

3. **Hardcoded env vars in startBizPod/startHTTPGateway/startAAAGateway:** Functions set hardcoded env vars (lines 316-339) for binary startup. Should these also move to YAML config, or is the current pattern acceptable since it's binary-specific?

4. **`expandEnvVars` default values:** The YAML uses `${VAR:-default}` syntax (matching compose configs). Should `expandEnvVars` parse defaults, or should missing env vars be a fatal error per "no hardcoded fallbacks"?

## Sources

- Go stdlib `crypto/x509` — CA cert pool: [VERIFIED: Go docs]
- Go stdlib `crypto/tls` — TLS config: [VERIFIED: Go docs]
- Go stdlib `net/http` — HTTP client with custom TLS: [VERIFIED: Go docs]
- `gopkg.in/yaml.v3` already in go.mod: [VERIFIED: grep go.mod]
- Existing harness.go binary defaults: [VERIFIED: test/e2e/harness.go lines 100-102]
- Makefile binary output: [VERIFIED: Makefile lines 32-34]
- `http.DefaultClient` at line 390: [VERIFIED: test/e2e/harness.go]
- Hardcoded pgURL/redisURL defaults: [VERIFIED: test/e2e/harness.go lines 71-84]
- Hardcoded Biz Pod env vars: [VERIFIED: test/e2e/harness.go lines 316-339]
- Component config style: [CITED: compose/configs/*.yaml]
- Makefile test-e2e target: [CITED: Makefile lines 150-164]

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all libraries are stdlib or already in go.mod
- Architecture: HIGH — lifecycle separation already documented in harness.go comments
- Pitfalls: HIGH — based on actual codebase analysis and verified TLS failure modes

**Research date:** 2026-04-30
**Valid until:** 30 days (stable domain)
