---
phase: quick-260430-qey
plan: "01"
subsystem: testing
tags: [go, yaml, tls, e2e, testing, docker-compose]

requires: []
provides:
  - "E2E harness loads all infra URLs from harness.yaml with env var expansion"
  - "HTTPS health checks to HTTP Gateway use custom CA cert pool for self-signed TLS"
  - "`make test-e2e` passes E2E_TLS_CA, BIZ_PG_URL, BIZ_REDIS_URL to test process"
affects: [06-integration-testing-nrm]

tech-stack:
  added: [gopkg.in/yaml.v3, crypto/tls, crypto/x509]
  patterns:
    - "YAML config with ${VAR} and ${VAR:-default} env var expansion"
    - "Custom http.Client with x509 CertPool for self-signed TLS"
    - "Fail-fast config validation for required env vars"

key-files:
  created:
    - test/e2e/harness.yaml
  modified:
    - test/e2e/harness.go
    - Makefile

key-decisions:
  - "D-01: Makefile owns docker compose lifecycle; harness never calls compose"
  - "D-02: Custom CA cert pool from /tmp/e2e-tls/server.crt for HTTPS clients"
  - "D-03: YAML config file with environment variable overrides; no hardcoded fallbacks"
  - "D-04: Use gopkg.in/yaml.v3 (already in go.mod); no viper"

patterns-established:
  - "HarnessConfig struct pattern: nested typed config structs matching YAML structure"
  - "expandEnvVars regex pattern: separate handling of ${VAR:-default} vs ${VAR}"
  - "initTLS pattern: read CA cert, build CertPool, assign to http.Client.Transport"

requirements-completed: []

duration: 3min
completed: 2026-04-30
---

# Quick Task 260430-qey: E2E Harness YAML Config and TLS Support

**E2E harness loads all infra config from YAML with env var expansion, uses custom CA cert pool for HTTPS health checks, and Makefile passes all required env vars.**

## Performance

- **Duration:** ~3 min
- **Started:** 2026-04-30T13:20:00Z
- **Completed:** 2026-04-30T13:23:00Z
- **Tasks:** 3
- **Files modified:** 3

## Accomplishments

- Created `test/e2e/harness.yaml` with env var placeholders for all infra URLs, binary paths, TLS CA, and timeouts
- Refactored `harness.go` to load config via `LoadHarnessConfig()` with `expandEnvVars()` supporting both `${VAR}` and `${VAR:-default}` syntax
- Removed `pgURL()` and `redisURL()` hardcoded fallbacks; harness now fails fast if required env vars are missing
- Added `initTLS()` building a custom `*http.Client` with x509 `CertPool` for self-signed TLS certs at `/tmp/e2e-tls/server.crt`
- Updated `waitHealthy()` to use the custom TLS client instead of `http.DefaultClient`
- Fixed `make test-e2e` to pass `E2E_TLS_CA`, `BIZ_PG_URL`, `BIZ_REDIS_URL` to the test process

## Task Commits

Each task was committed atomically:

1. **Task 1: Create harness.yaml and YAML loading code** - `cce0671` (feat)
2. **Task 2: Add HTTPS TLS client with custom CA cert pool** - `cce0671` (same commit as Task 1, integrated)
3. **Task 3: Fix Makefile test-e2e to pass required env vars** - `bebeacb` (fix)

## Files Created/Modified

- `test/e2e/harness.yaml` - New YAML config file with env var placeholders (infra URLs, binary paths, TLS CA, timeouts)
- `test/e2e/harness.go` - Added `HarnessConfig` struct, `LoadHarnessConfig()`, `expandEnvVars()`, `initTLS()`, `Harness.httpClient` field; removed `pgURL()`, `redisURL()`, `getEnv()` helpers; `waitHealthy()` uses custom TLS client
- `Makefile` - `test-e2e` target now passes `E2E_TLS_CA`, `BIZ_PG_URL`, `BIZ_REDIS_URL`

## Decisions Made

- Used `gopkg.in/yaml.v3` (already in go.mod) for YAML parsing — no new dependency added
- Two separate regexps for `${VAR:-default}` (with default) vs `${VAR}` (no default) patterns in `expandEnvVars()` for clarity
- `LoadHarnessConfig()` validates required fields (`postgresUrl`, `redisUrl`, `caCert`) and returns descriptive errors
- `BIZ_PG_URL` and `BIZ_REDIS_URL` values in Makefile use `localhost` (direct docker compose on host); CI can override at CI level with docker service names

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None.

## Next Phase Readiness

- E2E harness is ready for the docker compose integration workflow
- `make test-e2e` will now fail fast with clear error messages if env vars are missing (no silent fallback to wrong URLs)
- Custom TLS client enables reliable HTTPS health checks against self-signed HTTP Gateway certs

---
*Phase: quick-260430-qey*
*Completed: 2026-04-30*
