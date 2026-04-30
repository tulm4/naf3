# Phase quick: Remove Binary Startup from E2E Harness — Research

**Researched:** 2026-04-30
**Domain:** docker compose service management, E2E test infrastructure
**Confidence:** HIGH

## Summary

The 260430-qey quick task added YAML config and TLS support but did NOT remove binary process startup from `harness.go`. The harness still calls `buildBinaries()`, `startAAAGateway()`, `startBizPod()`, and `startHTTPGateway()` even when `E2E_DOCKER_MANAGED=1`. This means:

1. Docker compose starts infra containers (redis, postgres, mock-aaa-s)
2. Then the harness starts binary processes for biz, http-gw, aaa-gw
3. Result: two competing sets of services listening on the same ports → conflict

The fix requires:
- Remove all binary startup from harness.go
- Add NRM service to compose/dev.yaml (currently referenced but missing)
- Fix smoke_manual_test.go to use TLS client
- Update Makefile to remove binary path env vars
- Commit 3 uncommitted files

## User Constraints

### Locked Decisions
- D-01: Makefile owns docker compose lifecycle; harness NEVER starts compose
- D-02: Custom CA cert pool from `/tmp/e2e-tls/server.crt` for HTTPS
- D-03: YAML config file with env var overrides; no hardcoded fallbacks
- D-04: All 5 services (biz, http-gw, aaa-gw, nrm, postgres, redis) via docker compose containers
- D-05: smoke_manual_test.go must use TLS client for HTTPS

### Out of Scope
- Changing component binary interfaces or config schemas
- Adding integration test coverage beyond fixing the harness
- Modifying component main.go files

## Docker Compose Service Analysis

### Current compose/dev.yaml services (6 total after adding nrm):
1. **redis** — port 6379 ✓
2. **postgres** — port 5432 ✓
3. **mock-aaa-s** — ports 18120, 38680 ✓
4. **aaa-gateway** — ports 9090, 18121, 38681 ✓ (already in compose)
5. **biz** — port 8080 ✓ (already in compose)
6. **http-gateway** — port 8443 ✓ (already in compose)
7. **nrm** — port 8081 ✗ MISSING

### NRM service to add:
```
nrm:
  build:
    context: ..
    dockerfile: Dockerfile.nrm  # Note: Dockerfile.nrm is at repo root
  image: nssaaf-nrm:latest
  depends_on:
    postgres:
      condition: service_healthy
  volumes:
    - ./configs/nrm.yaml:/etc/nssAAF/nrm.yaml:ro
  ports: ["8081:8081"]
  networks:
    - default
  healthcheck:
    test: ["CMD-SHELL", "wget -qO- http://localhost:8081/healthz || exit 1"]
    interval: 10s
    timeout: 5s
    retries: 3
```

Note: `Dockerfile.nrm` is at repo root (`/home/tulm/naf3/Dockerfile.nrm`), NOT in `compose/`. The `context: ..` in compose/dev.yaml resolves to the repo root, so `dockerfile: Dockerfile.nrm` works correctly.

## Binary Startup Functions to Remove

All in `test/e2e/harness.go`:

| Function | Lines | Reason |
|----------|-------|---------|
| `buildBinaries()` | ~397-421 | Starts go build for all 3 binaries |
| `startAAAGateway()` | ~424-436 | Starts aaa-gateway binary |
| `startBizPod()` | ~439-458 | Starts biz binary |
| `startHTTPGateway()` | ~461-473 | Starts http-gw binary |
| `projectRoot()` | ~541-551 | Used only by binary startup |
| `killProcess()` | ~561-576 | Used only by binary cleanup |

### Struct fields to remove:
- `httpGWBin`, `bizBin`, `aaagwBin` (strings — unused after removal)
- `httpGWProcess`, `bizProcess`, `aaagwProcess` (*os.Process — unused after removal)

### Struct fields to KEEP:
- `t`, `config`, `httpClient`
- `httpGWURL`, `bizURL`, `aaagwURL`, `nrmURL`
- `ausfMock`, `amfMock`
- `pgConn`, `redis`
- `mu`, `clean`

## Uncommitted Files Analysis

### cmd/biz/http_aaa_client.go (+14 lines)
```
+ slog import
+ defer recover() in subscribeResponses
+ nil channel guard
+ nil msg guard
+ slog.Debug for unmarshal failure
```
Intent: Robustness improvements to subscription goroutine. Should be committed.

### test/e2e/e2e.go (+6 lines)
```
+ Doc comment whitespace fix (cosmetic)
- sharedHarness = NewHarness(...) → sharedHarness = NewHarnessForTest(...)
```
Intent: Use `NewHarnessForTest` instead of `NewHarness` in TestMain. Should be committed.

### test/e2e/smoke_manual_test.go
Status: untracked file (??). The file exists but was not in git. This is actually already part of the e2e test suite. Should be committed.

## smoke_manual_test.go TLS Fix

The file uses `http.Client` (plain HTTP) for all requests, including `https://localhost:8443` (HTTP Gateway). This will fail TLS verification.

**Solution:** Create a package-level `e2eTLSClient *http.Client` initialized lazily from the harness YAML config (same as the harness uses). `doRequest()` uses this client for HTTPS URLs.

Pattern:
```go
var tlsClient *http.Client

func initTLSClient() {
    if tlsClient != nil { return }
    caPath := os.Getenv("E2E_TLS_CA")
    if caPath == "" { caPath = "/tmp/e2e-tls/server.crt" }
    caCert, _ := os.ReadFile(caPath)
    pool := x509.NewCertPool()
    pool.AppendCertsFromPEM(caCert)
    tlsClient = &http.Client{
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{RootCAs: pool},
        },
    }
}
```

## Makefile test-e2e Changes

Remove binary path env vars since harness no longer uses them:
```diff
- BIZ_BINARY=$(BIZ_BINARY) \
- HTTPGW_BINARY=$(HTTPGW_BINARY) \
- AAAGW_BINARY=$(AAAGW_BINARY) \
```

Keep infra config:
```yaml
+ E2E_TLS_CA=/tmp/e2e-tls/server.crt \
+ BIZ_PG_URL=postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable \
+ BIZ_REDIS_URL=redis://localhost:6379 \
```

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Docker compose health wait | `sleep 10` | `docker compose ps --format json` polling | More reliable |
| TLS cert loading | Custom cert-parsing | `x509.NewCertPool()` + `AppendCertsFromPEM()` | Handles PEM/DER |
| YAML config | Custom YAML parser | `gopkg.in/yaml.v3` | Already in go.mod |

## Common Pitfalls

1. **Dockerfile.nrm path** — The NRM Dockerfile is at repo root (`Dockerfile.nrm`), not in `compose/`. Use `dockerfile: Dockerfile.nrm` with `context: ..` in compose.

2. **Build context for nrm** — `compose/dev.yaml` has `context: ..` for all binary-based services (biz, http-gw, aaa-gw). This resolves to repo root. Works for `Dockerfile.biz`, `Dockerfile.http-gateway`, `Dockerfile.aaa-gateway`. For nrm, `Dockerfile.nrm` is at repo root — same pattern works.

3. **Biz health check** — Biz exposes `/healthz/ready` but in compose, biz also depends on nrm. The health check should wait for nrm to be healthy before starting biz.

4. **NRM standalone** — NRM is a standalone RESTCONF server that needs no external dependencies. It just needs the postgres connection for alarm storage.

## Open Questions

1. **NRM postgres dependency** — NRM stores alarms in postgres. Does it need a postgres volume mount? Check nrm.yaml config for database connection.
2. **mock-aaa-s startup** — mock-aaa-s has no healthcheck. This could cause a race condition. Consider adding a simple healthcheck.
3. **biz startup order** — biz depends on nrm being healthy. Should the health check for biz confirm it can reach nrm, or is the compose dependency sufficient?

## Sources

- harness.go binary startup code: [VERIFIED: test/e2e/harness.go lines 227-248]
- compose/dev.yaml services: [VERIFIED: compose/dev.yaml]
- NRM Dockerfile: [VERIFIED: Dockerfile.nrm]
- NRM config: [VERIFIED: compose/configs/nrm.yaml]
- Makefile test-e2e: [VERIFIED: Makefile lines 151-164]
- Uncommitted changes: [VERIFIED: git diff]

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — docker compose V2, already in codebase
- Architecture: HIGH — simple removal of binary startup functions
- Pitfalls: HIGH — based on actual codebase analysis

**Research date:** 2026-04-30
**Valid until:** 30 days (stable domain)
