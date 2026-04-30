# Phase quick: E2E Harness Refactor — Research

**Researched:** 2026-04-30
**Domain:** Go E2E test infrastructure, TLS/HTTPS in Go, YAML config loading
**Confidence:** HIGH

## Summary

The existing E2E harness in `test/e2e/harness.go` manages both docker compose lifecycle (via `e2e.go` TestMain) and binary process lifecycle (exec.Command). This conflates two separate concerns. The refactor goal is to:

1. **Move compose lifecycle to Makefile exclusively** — `test-e2e` target already does this (`E2E_DOCKER_MANAGED=1`); harness should remove compose calls
2. **Replace hardcoded URL constants with YAML config** — `compose/configs/harness.yaml` loaded via viper or direct YAML decoding
3. **Add HTTPS with custom CA cert** — HTTP Gateway terminates TLS on `:8443`, harness needs `tls.Config{RootCAs}` loading `/tmp/e2e-tls/server.crt`
4. **Fix `make test-e2e`** — Likely failing due to binary path defaults not matching `bin/` directory

The `make test-e2e` target runs `gen-certs build` then starts compose, but the harness defaults to `./nssAAF-biz` etc. while build outputs to `./bin/{biz,http-gateway,aaa-gateway}`. This path mismatch is the primary failure cause.

## User Constraints

### Locked Decisions
- Makefile owns docker compose lifecycle; harness NEVER calls compose
- Custom CA cert pool from `/tmp/e2e-tls/server.crt` for HTTPS clients
- YAML config file with environment variable overrides; no hardcoded fallbacks
- No new external dependencies beyond Go stdlib + viper for YAML

### Out of Scope
- Changing the docker compose service definitions
- Modifying component binary interfaces or config schemas
- Adding integration test coverage beyond fixing the harness

## Standard Stack

| Library | Version | Purpose |
|---------|---------|---------|
| `github.com/spf13/viper` | latest | YAML config loading with env var overrides |
| `crypto/x509` | stdlib | Custom CA cert pool |
| `crypto/tls` | stdlib | TLS config for HTTPS clients |
| `gopkg.in/yaml.v3` | latest | Direct YAML decode (simpler than viper for test-only) |

**Decision:** Use `gopkg.in/yaml.v3` directly for the harness config (no viper dependency). Viper is heavy for test-only use and requires global state. Direct YAML decode is cleaner and matches the project's existing YAML config pattern in `compose/configs/`.

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
                // For client certs (if needed later):
                // Certificates: []tls.Certificate{clientCert},
            },
        },
    }, nil
}
```

**Key gotcha:** `http.DefaultClient` has `nil` `TLSClientConfig`, which means system cert pool. The harness must use a custom `*http.Client` for HTTPS requests to the HTTP Gateway.

The existing harness code at line 390 uses `http.DefaultClient` for the health check against `https://localhost:8443`. This will fail TLS verification with a self-signed cert unless the CA is in the system store. Solution: use a custom client with the CA pool.

## YAML Harness Config

The harness config should mirror the existing component config style (e.g., `compose/configs/biz.yaml`). Structure:

```yaml
# test/e2e/harness.yaml
# Loaded by the E2E harness; env vars override these values.

infra:
  postgresUrl: "${BIZ_PG_URL}"
  redisUrl: "${BIZ_REDIS_URL}"

services:
  httpGatewayUrl: "https://localhost:8443"
  bizPodUrl: "http://localhost:8080"
  aaaGatewayUrl: "http://localhost:9090"
  nrmUrl: "http://localhost:8081"

binaries:
  biz: "./bin/biz"
  httpGateway: "./bin/http-gateway"
  aaaGateway: "./bin/aaa-gateway"

tls:
  caCert: "/tmp/e2e-tls/server.crt"
  # Key/cert for mTLS (future):
  # clientCert: "/tmp/e2e-tls/client.crt"
  # clientKey: "/tmp/e2e-tls/client.key"

timeouts:
  startup: "2m"
  healthCheck: "5s"
  shutdownGrace: "10s"
```

**Loading with env var substitution** — since there's no heavy framework, implement a simple env-substitution helper:

```go
// expandEnvVars replaces ${VAR} and $VAR patterns with os.Getenv values.
// Matches the behavior used in compose/configs/*.yaml.
func expandEnvVars(s string) string {
    re := regexp.MustCompile(`\$\{([^}]+)\}`)
    return re.ReplaceAllStringFunc(s, func(match string) string {
        key := match[2 : len(match)-1] // strip ${}
        return os.Getenv(key)
    })
}
```

**Alternative:** Use viper's ` AutomaticEnv()` + `SetEnvKey()` — but this adds a dependency. For a test-only config, the simple regex approach is sufficient and avoids coupling to viper's global state.

## Lifecycle Separation

The current architecture (`harness.go` + `e2e.go`):

```
TestMain (e2e.go)
  ├─ docker compose up   (if not E2E_DOCKER_MANAGED)
  ├─ NewHarness()        (harness.go)
  │    ├─ initDBAndRedis()
  │    ├─ buildBinaries()
  │    ├─ startAAAGateway()
  │    ├─ startBizPod()
  │    ├─ startHTTPGateway()
  │    └─ waitHealthy()
  ├─ tests...
  └─ Close() + compose down
```

**Problem:** `NewHarness` does too much — it both connects to infrastructure AND starts binaries. The refactor should:

1. **Harness** (`harness.go`) — connects to running stack (infra + binaries), provides test clients, manages state reset. Does NOT start compose or binaries.
2. **Binary startup** — lives in Makefile via `make test-e2e` or a dedicated `compose/test.yaml`. Harness reads binary paths from config.
3. **`ResetState()`** — truncates DB tables + flushes Redis (already correct)

This matches the lifecycle model already documented in the harness.go comments (lines 4-8):

```
//   - docker compose up/down is managed once by TestMain in e2e.go (via Makefile)
//   - NewHarness connects to the pre-started stack; it does NOT manage compose
//   - Each test gets a clean slate via h.ResetState() (TRUNCATE tables + Redis FLUSHDB)
//   - Close() only kills the binary processes, it does NOT tear down compose
```

The actual problem is the binary paths: `getEnv("HTTPGW_BINARY", "./bin/http-gateway")` is correct, but the Makefile builds to `bin/biz`, `bin/http-gateway`, `bin/aaa-gateway`. The harness defaults look for `./nssAAF-biz`, `./nssAAF-http-gw`, `./nssAAF-aaa-gw` — which don't match. The git status also shows untracked `nssAAF-aaa-gw` and `nssAAF-http-gw` directories.

**Fix needed:** Update harness default binary paths to `./bin/{biz,http-gateway,aaa-gateway}` OR update Makefile build output paths to match harness defaults.

## HTTP Gateway HTTPS Client Fix

The harness uses `http.DefaultClient` for health checks against `https://localhost:8443`. This will fail TLS verification because:
- The cert at `/tmp/e2e-tls/server.crt` is self-signed
- `http.DefaultClient` uses the system CA cert pool (not the self-signed cert)

**Solution:** Add a `tlsConfig` field to `Harness` and construct a custom `http.Client`:

```go
type Harness struct {
    // ... existing fields ...
    tlsConfig *tls.Config  // custom TLS config with CA cert pool
    httpClient *http.Client // HTTPS client for httpGatewayUrl
}
```

Then in `NewHarness`:

```go
caCertPath := getEnv("E2E_TLS_CA", "/tmp/e2e-tls/server.crt")
caCert, _ := os.ReadFile(caCertPath)
caPool := x509.NewCertPool()
caPool.AppendCertsFromPEM(caCert)
h.tlsConfig = &tls.Config{RootCAs: caPool}
h.httpClient = &http.Client{
    Transport: &http.Transport{TLSClientConfig: h.tlsConfig},
}
```

Use `h.httpClient` for health checks to `https://localhost:8443`.

## Makefile test-e2e Issues

The current `test-e2e` target:

```makefile
test-e2e: gen-certs build ## Build binaries then run E2E tests
```

**Issues:**
1. `build` outputs to `bin/{biz,http-gateway,aaa-gateway}`
2. Harness defaults look for `./nssAAF-biz`, `./nssAAF-http-gw`, `./nssAAF-aaa-gw`
3. The untracked `nssAAF-*` directories in git status suggest the build outputs were renamed or relocated

**Fix:** Align binary path defaults — either:
- Option A: Change harness defaults to `./bin/{biz,http-gateway,aaa-gateway}`
- Option B: Change Makefile to output to root: `go build -o ./nssAAF-biz ./cmd/biz/`

Option A is cleaner since `bin/` is the established output directory per Makefile `BINARY_DIR = bin`.

## Config File Location

The harness config should live at `test/e2e/harness.yaml` (next to the harness code) or `compose/configs/harness.yaml` (with other configs). Recommendation: `test/e2e/harness.yaml` since it's test-only infrastructure, not deployment config.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead |
|---------|-------------|-------------|
| TLS cert loading | Custom cert-parsing | `x509.NewCertPool()` + `AppendCertsFromPEM()` |
| HTTPS client | Re-implement HTTP client | stdlib `http.Client` with `TLSClientConfig` |
| YAML config | Custom YAML parser | `gopkg.in/yaml.v3` |

## Common Pitfalls

1. **`http.DefaultClient` with self-signed certs** — always construct a custom `*http.Client` with `TLSClientConfig` when the server uses self-signed certs. Using `http.DefaultClient` for HTTPS health checks will silently fail or return TLS errors.

2. **Binary path mismatch** — the single most common `make test-e2e` failure. Audit both Makefile `build` output paths and harness `getEnv` defaults.

3. **Goroutine leaks from Redis subscription** — `httpAAAClient.subscribeResponses` runs in a goroutine started in `newHTTPAAAClient`. The harness `Close()` calls `httpAAAClient.Close()` which closes the Redis client but does not stop the subscription goroutine. The subscription goroutine should be cancelled via a context.

4. **Compose health wait** — `docker compose up -d --quiet-pull` then `sleep 10` is fragile. Use `docker compose ps --format json` or a health-check loop instead.

5. **Process group cleanup** — `killProcess` sends `SIGTERM` but binary processes spawned by the harness may spawn their own subprocesses. Use `syscall.SysProcAttr{Setpgid: true}` on the parent `exec.Command` and kill the process group (negative PID) to ensure all children are terminated.

## Code Examples

### YAML Config Loading

```go
type HarnessConfig struct {
    Infra   InfraConfig   `yaml:"infra"`
    Services ServicesConfig `yaml:"services"`
    Binaries BinariesConfig `yaml:"binaries"`
    TLS      TLSConfig     `yaml:"tls"`
    Timeouts TimeoutsConfig `yaml:"timeouts"`
}

func LoadHarnessConfig(path string) (*HarnessConfig, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    // Expand env vars in YAML content
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
func buildTLSConfig(caPath string) (*tls.Config, error) {
    caCert, err := os.ReadFile(caPath)
    if err != nil {
        return nil, fmt.Errorf("read CA: %w", err)
    }
    pool := x509.NewCertPool()
    if !pool.AppendCertsFromPEM(caCert) {
        return nil, errors.New("invalid CA certificate")
    }
    return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13}, nil
}
```

## Open Questions

1. **Binary path standard:** Should the harness default to `./bin/{name}` or `./nssAAF-{name}`? The Makefile uses `bin/`; the untracked `nssAAF-*` directories suggest prior inconsistency. Recommendation: standardize on `bin/` matching Makefile `BINARY_DIR`.

2. **NRM in harness:** The harness has `nrmURL` pointing to `localhost:8081` but `compose/dev.yaml` doesn't include an NRM service. Should NRM be added to the test infrastructure or excluded from harness scope?

3. **smoke_manual_test.go URLs:** This file has hardcoded `http://localhost:8443`, `http://localhost:8080`, `http://localhost:8081` constants. Should these be replaced with harness config or kept as-is for manual testing convenience?

4. **Config file format decision:** Use `gopkg.in/yaml.v3` (direct decode) or viper (env var auto-expansion)? Recommendation: `gopkg.in/yaml.v3` + simple `expandEnvVars()` — avoids viper dependency and matches project simplicity.

## Sources

- Go stdlib `crypto/x509` — CA cert pool: [VERIFIED: Go docs]
- Go stdlib `crypto/tls` — TLS config: [VERIFIED: Go docs]
- Go stdlib `net/http` — HTTP client with custom TLS: [VERIFIED: Go docs]
- Existing harness.go pattern for binary startup: [CITED: test/e2e/harness.go]
- Makefile test-e2e target: [CITED: Makefile]
- Component config style: [CITED: compose/configs/*.yaml]

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all libraries are stdlib or minimal well-known packages
- Architecture: HIGH — clear separation already documented in harness.go comments
- Pitfalls: HIGH — based on actual codebase analysis and known TLS failure modes

**Research date:** 2026-04-30
**Valid until:** 30 days (stable domain)
